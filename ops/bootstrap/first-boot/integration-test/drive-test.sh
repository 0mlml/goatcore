#!/usr/bin/env bash
# Container-side driver — runs first-boot.sh end-to-end against a
# synthetic USB userdata + a mock site-bootstrap.yml that no-ops.
# Validates that the script:
#   1. Finds and parses USB userdata
#   2. Schema-validates correctly
#   3. Drives the secrets prompt via expect (simulated typing)
#   4. Renders .envrc.local + group_vars overlay
#   5. Invokes site-bootstrap.yml (mock; succeeds)
#   6. Writes the completion marker
#   7. Emits well-formed JSONL evidence
#
# Failure of any step exits non-zero; success exits 0 with summary.
set -euo pipefail

# Source repo is bind-mounted read-only; we work from a scratch copy so
# the test never writes back into the developer's tree.
REPO_SRC=/opt/goat/repo
REPO=/tmp/test-repo
JSONL=/var/log/goat/first-boot.jsonl
ENVRC=/etc/goat/.envrc.local
STATE=/var/lib/goat/first-boot-state.json

log() { printf '[drive-test] %s\n' "$*"; }
log "stage 0: copy source tree to scratch $REPO"
rm -rf "$REPO"
mkdir -p "$REPO"
# Copy minimum files first-boot.sh + the playbook overlay need.
cp -a "$REPO_SRC/ops" "$REPO/"
mkdir -p "$REPO/ops/ansible/group_vars"

green() { printf '\033[32m%s\033[0m\n' "$*"; }
red() { printf '\033[31m%s\033[0m\n' "$*"; }

fail() { red "FAIL: $*"; exit 1; }
pass_count=0
mark_pass() { pass_count=$((pass_count + 1)); green "PASS: $*"; }

# -- Stage 1: synthesize USB by binding userdata.yml to a fake block --
# The first-boot.sh hunt loop checks /dev/sd[a-z]*1 and /dev/disk/by-label/*.
# We can't fake a block device cleanly inside an unprivileged container;
# instead, we patch first-boot.sh to read a USERDATA_OVERRIDE env var
# when set. (This is the pattern most cloud-init flows use for
# integration testing — bypass the device-hunt with an explicit path.)
#
# Apply the patch idempotently inline:
log "stage 1: prep USERDATA_OVERRIDE shim"
PATCHED="$REPO/ops/bootstrap/first-boot/first-boot.test.sh"
cp "$REPO/ops/bootstrap/first-boot/first-boot.sh" "$PATCHED"
# Replace the device-hunt loop with a single conditional that honors
# USERDATA_OVERRIDE when set, AND override REPO_ROOT to the scratch path.
python3 - <<'PYEOF'
import re
import sys
path = "/tmp/test-repo/ops/bootstrap/first-boot/first-boot.test.sh"
src = open(path).read()
# Override REPO_ROOT default so the patched fork uses the scratch tree
src = src.replace(
    'REPO_ROOT="${REPO_ROOT:-/opt/goat/repo}"',
    'REPO_ROOT="${REPO_ROOT:-/tmp/test-repo}"',
    1,
)
# The original loop starts at "for _ in $(seq 1 12); do" and ends at
# the [[ -n "$USERDATA" ]] guard. Replace with a USERDATA_OVERRIDE check.
new = re.sub(
    r'for _ in \$\(seq 1 12\); do.*?done\s*\n\n',
    'if [[ -n "${USERDATA_OVERRIDE:-}" ]]; then\n  USERDATA="$USERDATA_OVERRIDE"\nfi\n\n',
    src,
    count=1,
    flags=re.DOTALL,
)
if new == src:
    print("FAIL: USB-hunt loop not found", file=sys.stderr)
    sys.exit(1)
open(path, 'w').write(new)
print("patched: USERDATA_OVERRIDE shim active + REPO_ROOT scoped to scratch")
PYEOF
chmod +x "$PATCHED"

# -- Stage 2: synthesize a USB userdata.yml --------------------------
log "stage 2: write synthetic userdata.yml"
USERDATA_OVERRIDE=/tmp/userdata.yml
cat > "$USERDATA_OVERRIDE" <<'EOF'
schema_version: 1
site_id: test-site
mgmt_stack_profile: postgres-spock-only
hardware_class: orin-mgmt
scenario: total-loss
wg_cp0_endpoint: relay-test.example.com:51820
wg_cp0_relay_pubkey: AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
EOF
export USERDATA_OVERRIDE

# -- Stage 3: stage a mock site-bootstrap.yml that no-ops -----------
log "stage 3: stage mock site-bootstrap.yml"
mkdir -p "$REPO/ops/ansible/inventory"
cat > "$REPO/ops/ansible/site-bootstrap.yml" <<'EOF'
---
- name: Mock site-bootstrap.yml (integration test)
  hosts: localhost
  gather_facts: no
  tasks:
    - name: Confirm site_id was passed
      ansible.builtin.assert:
        that:
          - site_id is defined
          - site_id | length > 0
    - name: Echo no-op success
      ansible.builtin.debug:
        msg: "mock site-bootstrap.yml: site_id={{ site_id }} (no-op success)"
EOF
# Minimal inventory for ansible-playbook -i
cat > "$REPO/ops/ansible/inventory/hosts.ini" <<'EOF'
[localhost]
localhost ansible_connection=local
EOF
cat > "$REPO/ops/ansible/inventory/mgmt-hosts.ini" <<'EOF'
EOF

# -- Stage 4: drive the secrets prompt via expect ------------------
log "stage 4: drive secrets prompt via expect"
# postgres-spock-only profile prompts for: PG_PASS, REPLICATOR_PASS,
# LUKS_PASS — three secrets, each with a confirm.
cat > /tmp/drive.expect <<'EOF'
#!/usr/bin/expect -f
set timeout 60
spawn /tmp/test-repo/ops/bootstrap/first-boot/first-boot.test.sh

expect "TEST_SITE_PG_PASS*:" { send "test-pg-pass\r" }
expect "TEST_SITE_PG_PASS*confirm*:" { send "test-pg-pass\r" }

expect "TEST_SITE_REPLICATOR_PASS*:" { send "test-replicator\r" }
expect "TEST_SITE_REPLICATOR_PASS*confirm*:" { send "test-replicator\r" }

expect "TEST_SITE_LUKS_PASS*:" { send "test-luks\r" }
expect "TEST_SITE_LUKS_PASS*confirm*:" { send "test-luks\r" }

expect eof
catch wait result
exit [lindex $result 3]
EOF
chmod +x /tmp/drive.expect

if /tmp/drive.expect; then
  mark_pass "first-boot.test.sh exited 0"
else
  fail "first-boot.test.sh exited non-zero"
fi

# -- Stage 5: validate outputs ---------------------------------------
log "stage 5: validate JSONL evidence"
[[ -f "$JSONL" ]] || fail "JSONL evidence file missing: $JSONL"
JSONL_LINES=$(wc -l < "$JSONL")
[[ $JSONL_LINES -ge 5 ]] || fail "JSONL has only $JSONL_LINES lines (expected ≥ 5)"

# Each line must be valid JSON
while IFS= read -r line; do
  echo "$line" | jq empty 2>/dev/null \
    || fail "malformed JSON in evidence: $line"
done < "$JSONL"
mark_pass "JSONL evidence: $JSONL_LINES well-formed records"

# Phase markers must include all expected phases
EXPECTED_PHASES=(
  "first-boot.start"
  "first-boot.userdata-read"
  "first-boot.userdata-validate"
  "first-boot.serial-prompt"
  "first-boot.envrc-assemble"
  "first-boot.handoff"
  "first-boot.done"
)
for phase in "${EXPECTED_PHASES[@]}"; do
  jq -r '.phase' "$JSONL" | grep -qF "$phase" \
    || fail "missing expected phase in JSONL: $phase"
done
mark_pass "all 7 expected phase markers present"

# .envrc.local must exist with mode 0600
[[ -f "$ENVRC" ]] || fail ".envrc.local missing: $ENVRC"
PERMS=$(stat -c %a "$ENVRC")
[[ "$PERMS" == "600" ]] || fail ".envrc.local has perms $PERMS (expected 600)"
grep -q "TEST_SITE_PG_PASS=" "$ENVRC" || fail ".envrc.local missing PG_PASS"
grep -q "TEST_SITE_REPLICATOR_PASS=" "$ENVRC" || fail ".envrc.local missing REPLICATOR_PASS"
grep -q "TEST_SITE_LUKS_PASS=" "$ENVRC" || fail ".envrc.local missing LUKS_PASS"
mark_pass ".envrc.local rendered with correct perms + all secrets"

# group_vars overlay must exist
GVAR_FILE="$REPO/ops/ansible/group_vars/test_site_hosts/local.yml"
[[ -f "$GVAR_FILE" ]] || fail "group_vars overlay missing: $GVAR_FILE"
grep -q "site_id: test-site" "$GVAR_FILE" || fail "group_vars overlay missing site_id"
grep -q "mgmt_stack_profile: postgres-spock-only" "$GVAR_FILE" \
  || fail "group_vars overlay missing profile"
mark_pass "group_vars overlay rendered"

# State marker must say complete=true
[[ -f "$STATE" ]] || fail "state marker missing: $STATE"
jq -e '.complete == true' "$STATE" >/dev/null || fail "state marker not complete"
jq -e '.site_id == "test-site"' "$STATE" >/dev/null || fail "state marker wrong site_id"
mark_pass "state marker shows complete=true"

# -- Stage 6: re-run safety ------------------------------------------
log "stage 6: re-run safety check"
# Without --force, second invocation should exit 0 immediately
/tmp/test-repo/ops/bootstrap/first-boot/first-boot.test.sh 2>&1 \
  | grep -q "already complete" \
  || fail "re-run did not detect existing state marker"
mark_pass "re-run safety: state marker prevents double-run"

# -- Summary -----------------------------------------------------------
green ""
green "================================================="
green "  first-boot integration test: $pass_count/7 PASS"
green "================================================="
echo ""
echo "Evidence: $JSONL"
echo "Lines:    $JSONL_LINES"
