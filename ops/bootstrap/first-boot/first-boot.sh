#!/usr/bin/env bash
# Block 68 first-boot.sh — runs once on a freshly-flashed image.
#
# Reads USB userdata.yml (non-secret params), prompts on serial console
# for per-profile secrets, assembles .envrc.local + group_vars overlay,
# invokes site-bootstrap.yml against localhost.
#
# Spec:    docs/project/block-68a-config-input-spike.md
# Design:  docs/design/rapid-site-bringup.md §4
# Re-run:  --force to bypass the completion marker; default is no-op
#          if /var/lib/goat/first-boot-state.json says complete.
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-/opt/goat/repo}"
STATE_DIR=/var/lib/goat
STATE_FILE="$STATE_DIR/first-boot-state.json"
LOG_DIR=/var/log/goat
JSONL="$LOG_DIR/first-boot.jsonl"
ENVRC=/etc/goat/.envrc.local
SCHEMA="$REPO_ROOT/ops/bootstrap/first-boot/userdata-schema.yml"

FORCE=0
if [[ "${1:-}" == "--force" ]]; then FORCE=1; fi

mkdir -p "$STATE_DIR" "$LOG_DIR" "$(dirname "$ENVRC")"

emit() {
  # emit <phase> <status> [<extra-json>]
  local phase=$1 status=$2 extra=${3:-"{}"}
  local now ts_unix
  now=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  ts_unix=$(date +%s)
  printf '{"ts":"%s","ts_unix":%s,"phase":"%s","status":"%s","extra":%s}\n' \
    "$now" "$ts_unix" "$phase" "$status" "$extra" >> "$JSONL"
}

die() {
  emit "first-boot.fail" "fail" "{\"reason\":\"$1\"}"
  echo "first-boot.sh: $1" >&2
  echo "" >&2
  echo "Dropping to login shell so you can triage." >&2
  exit 1
}

# -- Re-run guard --------------------------------------------------------
if [[ -f "$STATE_FILE" ]] && [[ $FORCE -eq 0 ]]; then
  if grep -q '"complete":[[:space:]]*true' "$STATE_FILE" 2>/dev/null; then
    emit "first-boot.skip" "ok" '{"reason":"already-complete"}'
    echo "first-boot.sh: already complete; pass --force to re-run." >&2
    exit 0
  fi
fi

emit "first-boot.start" "ok"

# -- Phase 1: find USB userdata ------------------------------------------
emit "first-boot.userdata-read" "start"
USERDATA=""
for _ in $(seq 1 12); do  # ~60s wait window
  for dev in /dev/sd[a-z]*1 /dev/disk/by-label/*; do
    [[ -b "$dev" ]] || continue
    mnt=$(mktemp -d)
    if mount -o ro "$dev" "$mnt" 2>/dev/null; then
      if [[ -f "$mnt/userdata.yml" ]]; then
        cp "$mnt/userdata.yml" /tmp/userdata.yml
        umount "$mnt"
        rmdir "$mnt"
        USERDATA=/tmp/userdata.yml
        break 2
      fi
      umount "$mnt"
    fi
    rmdir "$mnt"
  done
  sleep 5
done

[[ -n "$USERDATA" ]] || die "no USB stick with userdata.yml found after 60s"
emit "first-boot.userdata-read" "ok" "{\"path\":\"$USERDATA\"}"

# -- Phase 2: schema validate --------------------------------------------
emit "first-boot.userdata-validate" "start"
command -v yq >/dev/null || die "yq not installed in image (build error)"
command -v check-jsonschema >/dev/null \
  || die "check-jsonschema not installed in image (build error)"

# Convert YAML to JSON for the validator. Both yq + check-jsonschema
# pre-installed at image-build time.
yq -o=json "$USERDATA" > /tmp/userdata.json \
  || die "userdata.yml is not valid YAML"
yq -o=json "$SCHEMA" > /tmp/schema.json
check-jsonschema --schemafile /tmp/schema.json /tmp/userdata.json \
  || die "userdata.yml failed schema validation; see prior output"
emit "first-boot.userdata-validate" "ok"

# -- Extract userdata fields ---------------------------------------------
SITE_ID=$(yq -r '.site_id' "$USERDATA")
PROFILE=$(yq -r '.mgmt_stack_profile' "$USERDATA")
ROLE=$(yq -r '.router_role // ""' "$USERDATA")
SCENARIO=$(yq -r '.scenario' "$USERDATA")
HW_CLASS=$(yq -r '.hardware_class' "$USERDATA")
WG_ENDPOINT=$(yq -r '.wg_cp0_endpoint' "$USERDATA")
WG_PUBKEY=$(yq -r '.wg_cp0_relay_pubkey' "$USERDATA")
PEER_API=$(yq -r '.peer_site_mgmt_api // ""' "$USERDATA")
PEER_PG=$(yq -r '.peer_site_pg_endpoint // ""' "$USERDATA")

SITE_UPPER=$(echo "$SITE_ID" | tr '[:lower:]-' '[:upper:]_')

# -- Phase 3: serial-console secrets prompt ------------------------------
emit "first-boot.serial-prompt" "start" '{"clock":"paused"}'
echo ""
echo "=========================================================="
echo " Block 68 rapid-bringup — secrets prompt"
echo "=========================================================="
echo " Site:     $SITE_ID  (profile=$PROFILE, role=${ROLE:-none})"
echo " Scenario: $SCENARIO"
echo " Hardware: $HW_CLASS"
echo "----------------------------------------------------------"
echo " Type each secret. They go to /etc/goat/.envrc.local"
echo " (mode 0600), never to removable media."
echo " Ctrl-C aborts; secrets are not persisted on abort."
echo "=========================================================="
echo ""

prompt_secret() {
  # prompt_secret <varname> <description>
  local var=$1 desc=$2 val confirm
  while true; do
    read -r -s -p "$desc: " val
    echo ""
    if [[ -z "$val" ]]; then
      echo "  (empty value not accepted; retry)" >&2
      continue
    fi
    read -r -s -p "$desc (confirm): " confirm
    echo ""
    if [[ "$val" == "$confirm" ]]; then
      printf -v "$var" '%s' "$val"
      break
    fi
    echo "  values do not match; retry" >&2
  done
}

# Per-profile secret set per CLAUDE.md / vars/site-required-env-vars.yml
prompt_secret PG_PASS "${SITE_UPPER}_PG_PASS (postgres superuser)"
prompt_secret REPLICATOR_PASS "${SITE_UPPER}_REPLICATOR_PASS (Spock replicator role)"

if [[ "$PROFILE" == "full" ]]; then
  prompt_secret NB_ENCKEY "${SITE_UPPER}_NB_ENCKEY (netbird DataStoreEncryptionKey, MUST match cluster)"
  prompt_secret BKP_KEY "${SITE_UPPER}_BKP_KEY (pgBackRest cipher pass)"
  prompt_secret RELAY_SECRET "${SITE_UPPER}_RELAY_SECRET (relay shared auth secret, MUST match cluster)"
fi

if [[ -n "$ROLE" ]]; then
  prompt_secret LUKS_PASS "${SITE_UPPER}_${ROLE}_LUKS_PASS (this host's LUKS unlock passphrase)"
else
  prompt_secret LUKS_PASS "${SITE_UPPER}_LUKS_PASS (this host's LUKS unlock passphrase)"
fi

emit "first-boot.serial-prompt" "ok" '{"clock":"resumed"}'

# -- Phase 4: render .envrc.local ----------------------------------------
emit "first-boot.envrc-assemble" "start"
{
  echo "# Block 68 first-boot generated $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "# Site:     $SITE_ID"
  echo "# Profile:  $PROFILE"
  echo "# Scenario: $SCENARIO"
  echo "export ${SITE_UPPER}_PG_PASS='$PG_PASS'"
  echo "export ${SITE_UPPER}_REPLICATOR_PASS='$REPLICATOR_PASS'"
  if [[ "$PROFILE" == "full" ]]; then
    echo "export ${SITE_UPPER}_NB_ENCKEY='$NB_ENCKEY'"
    echo "export ${SITE_UPPER}_BKP_KEY='$BKP_KEY'"
    echo "export ${SITE_UPPER}_RELAY_SECRET='$RELAY_SECRET'"
  fi
  if [[ -n "$ROLE" ]]; then
    echo "export ${SITE_UPPER}_${ROLE}_LUKS_PASS='$LUKS_PASS'"
  else
    echo "export ${SITE_UPPER}_LUKS_PASS='$LUKS_PASS'"
  fi
} > "$ENVRC"
chmod 0600 "$ENVRC"
chown root:root "$ENVRC"
emit "first-boot.envrc-assemble" "ok" "{\"path\":\"$ENVRC\"}"

# -- Phase 5: render group_vars overlay ----------------------------------
GVAR_DIR="$REPO_ROOT/ops/ansible/group_vars/${SITE_ID//-/_}_hosts"
mkdir -p "$GVAR_DIR"
{
  echo "---"
  echo "# Block 68 first-boot generated overlay $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "# Sources: USB userdata.yml + serial-console secrets"
  echo "site_id: $SITE_ID"
  echo "mgmt_stack_profile: $PROFILE"
  if [[ -n "$ROLE" ]]; then
    echo "router_role: $ROLE"
  fi
  echo "hardware_class: $HW_CLASS"
  echo "scenario_at_bringup: $SCENARIO"
  echo "wg_cp0_endpoint: $WG_ENDPOINT"
  echo "wg_cp0_relay_pubkey: $WG_PUBKEY"
  if [[ "$SCENARIO" == "spock-join" ]]; then
    echo "peer_site_mgmt_api: $PEER_API"
    echo "peer_site_pg_endpoint: $PEER_PG"
  fi
} > "$GVAR_DIR/local.yml"

# -- Phase 6: invoke site-bootstrap.yml against localhost ----------------
emit "first-boot.handoff" "start"
cd "$REPO_ROOT/ops/ansible"
# shellcheck disable=SC1090
source "$ENVRC"
ANSIBLE_ARGS=(
  -i inventory/hosts.ini
  -i inventory/mgmt-hosts.ini
  -e site_id="$SITE_ID"
  -c local
  -l localhost
  site-bootstrap.yml
)
if ansible-playbook "${ANSIBLE_ARGS[@]}"; then
  emit "first-boot.handoff" "ok"
else
  emit "first-boot.handoff" "fail" '{"hint":"site-bootstrap.yml exited non-zero; see ansible log"}'
  die "site-bootstrap.yml failed; see /var/log/ansible.log"
fi

# -- Phase 7: mark complete ----------------------------------------------
{
  echo "{"
  echo "  \"complete\": true,"
  echo "  \"site_id\": \"$SITE_ID\","
  echo "  \"profile\": \"$PROFILE\","
  echo "  \"scenario\": \"$SCENARIO\","
  echo "  \"completed_at\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\""
  echo "}"
} > "$STATE_FILE"
chmod 0644 "$STATE_FILE"
emit "first-boot.done" "ok"
echo ""
echo "=========================================================="
echo " first-boot complete. Evidence: $JSONL"
echo "=========================================================="
