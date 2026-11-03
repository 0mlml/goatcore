#!/usr/bin/env bash
# role-fixture.sh — docker-fixture test for the wg-cp0-client Ansible role.
#
# Block 22M follow-on. Validates that the role's BUNDLE-FLOW branch
# correctly delivers wg-cp0-bundle-apply + the bundle + the CA pubkey
# to the target, invokes the apply tool, and renders the wg-cp0.conf +
# privkey on disk. Complements the CLI-level integration test at
# ops/enrollment/cmd/wg-cp0-bundle-apply/integration_test.go which
# validates the binary surface; this fixture validates the Ansible
# role's plumbing on top of that surface.
#
# Test-mode (defaults/main.yml `wg_cp0_test_mode: true`) gates the
# apt-install + systemd-enable + wg-show tasks so the role runs to
# completion against a clean Ubuntu container without needing real
# WireGuard tooling or a running systemd. The bundle-flow / legacy-flow
# branching + file delivery + bundle-apply invocation + shred discipline
# DO run, which is the load-bearing logic this fixture exists to test.
#
# Auto-skips when:
#   - docker is not on PATH
#   - ansible-playbook is not on PATH
#   - go (needed to build the wg-cp0-bundle-apply binary) is not on PATH
#
# Usage:
#   ops/ansible/roles/wg-cp0-client/tests/role-fixture.sh
#
# Exit codes:
#   0  fixture green
#   1  test failure
#   2  prerequisite missing (auto-skip)

set -euo pipefail

# Locate the repo root regardless of where the script is invoked from.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../../../.." && pwd)"
ENROLLMENT_DIR="$REPO_ROOT/ops/enrollment"

CONTAINER_NAME="wg-cp0-client-fixture-$$"
CONTAINER_IMAGE="ubuntu:24.04"

cleanup() {
  # Set GOAT_FIXTURE_KEEP=1 to leave the container + workdir behind for post-mortem debug.
  if [[ "${GOAT_FIXTURE_KEEP:-0}" == "1" ]]; then
    echo "wg-cp0-client role-fixture: GOAT_FIXTURE_KEEP=1 — leaving container $CONTAINER_NAME + workdir $WORKDIR"
    return
  fi
  if docker ps -aq --filter "name=$CONTAINER_NAME" 2>/dev/null | grep -q .; then
    docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
  fi
  if [[ -n "${WORKDIR:-}" && -d "$WORKDIR" ]]; then
    rm -rf "$WORKDIR"
  fi
}
trap cleanup EXIT

skip() {
  echo "wg-cp0-client role-fixture: SKIP — $1" >&2
  exit 0
}

fail() {
  echo "wg-cp0-client role-fixture: FAIL — $1" >&2
  exit 1
}

# --- Prerequisites ---------------------------------------------------------
command -v docker >/dev/null 2>&1 || skip "docker not on PATH"
command -v ansible-playbook >/dev/null 2>&1 || skip "ansible-playbook not on PATH"
command -v go >/dev/null 2>&1 || skip "go not on PATH"
command -v age-keygen >/dev/null 2>&1 || skip "age-keygen not on PATH (brew install age, or apt install age)"
command -v python3 >/dev/null 2>&1 || skip "python3 not on PATH (used to write the relays JSON fixture)"
docker info >/dev/null 2>&1 || skip "docker daemon not reachable"

WORKDIR="$(mktemp -d -t wg-cp0-client-fixture.XXXXXX)"
echo "wg-cp0-client role-fixture: workdir $WORKDIR" >&2

# --- Build a fixture CA + bundle ------------------------------------------
# Use bundle-create's existing dev-mode CA path. Generate fresh so the
# fixture is self-contained — no shared state with the operator's real
# dev CA.

cd "$ENROLLMENT_DIR"
# bundle-create + ca-init run on the controller (host) for the test —
# native arch is fine.
go build -o "$WORKDIR/bundle-create" ./cmd/bundle-create
go build -o "$WORKDIR/ca-init" ./cmd/ca-init
# wg-cp0-bundle-apply runs INSIDE the container (Linux). Cross-compile
# to match the container's arch — detect via `docker image inspect`.
CONTAINER_ARCH="$(docker image inspect --format '{{.Architecture}}' "$CONTAINER_IMAGE" 2>/dev/null || true)"
if [[ -z "$CONTAINER_ARCH" ]]; then
  # Image not pulled yet — pull once then inspect.
  docker pull --quiet "$CONTAINER_IMAGE" >/dev/null
  CONTAINER_ARCH="$(docker image inspect --format '{{.Architecture}}' "$CONTAINER_IMAGE")"
fi
case "$CONTAINER_ARCH" in
  amd64)  GOARCH=amd64 ;;
  arm64)  GOARCH=arm64 ;;
  *)      fail "unsupported container arch: $CONTAINER_ARCH" ;;
esac
GOOS=linux GOARCH="$GOARCH" CGO_ENABLED=0 go build \
  -o "$WORKDIR/wg-cp0-bundle-apply" ./cmd/wg-cp0-bundle-apply
cd "$REPO_ROOT"

# Generate an ephemeral age identity + CA.
AGE_KEY="$WORKDIR/age-keys.txt"
age-keygen -o "$AGE_KEY" 2>"$WORKDIR/age-keygen.stderr"
AGE_PUB="$(grep -i '^# *public key:' "$WORKDIR/age-keygen.stderr" "$AGE_KEY" 2>/dev/null | head -1 | sed 's/^[^:]*: *//; s/^[^:]*: *//')"
if [[ -z "${AGE_PUB:-}" || ! -s "$AGE_KEY" ]]; then
  fail "could not parse age public key from age-keygen output"
fi

CA_KEY_PATH="$WORKDIR/ca.key.age"
CA_PUB_PATH="$WORKDIR/ca.pub"
"$WORKDIR/ca-init" \
  --mode dev \
  --out "$CA_KEY_PATH" \
  --recipients "$AGE_PUB" \
  --allow-single-recipient \
  --id "test-fixture-ca" \
  >"$CA_PUB_PATH" 2>"$WORKDIR/ca-init.stderr" \
  || fail "ca-init failed: $(cat "$WORKDIR/ca-init.stderr")"

# Build a fixture relays roster.
RELAYS_PATH="$WORKDIR/relays.json"
python3 - <<PY > "$RELAYS_PATH"
import base64, os
relays = []
for name, addr, ip in (("relay-rla","203.0.113.1:51821","198.18.0.2"),
                       ("relay-rlb","203.0.113.23:51821","198.18.0.3"),
                       ("relay-rlc","203.0.113.10:51821","198.18.0.4")):
    pub = base64.b64encode(os.urandom(32)).decode()
    relays.append({"name":name,"endpoint":addr,"address":ip,"pubkey":pub})
import json; print(json.dumps(relays, indent=2))
PY

# Issue a fixture bundle.
BUNDLE_PATH="$WORKDIR/fixture.bundle.cbor"
"$WORKDIR/bundle-create" \
  --site test-site \
  --device-id test-device-01 \
  --wg-cp0-address 198.18.0.99/24 \
  --wg-cp0-relays "$RELAYS_PATH" \
  --acl-groups TEST,FIXTURE \
  --ca-id "dev-test-fixture-ca" \
  --ca-key "$CA_KEY_PATH" \
  --age-identity "$AGE_KEY" \
  --out "$BUNDLE_PATH" \
  --first-relay-route-subnet "198.18.0.0/24" \
  >"$WORKDIR/bundle-create.stderr" 2>&1 \
  || fail "bundle-create failed: $(cat "$WORKDIR/bundle-create.stderr")"

[[ -s "$BUNDLE_PATH" ]] || fail "bundle.cbor empty after bundle-create"
[[ -s "$CA_PUB_PATH" ]] || fail "ca.pub empty after ca-init"

# --- Spin up a clean Ubuntu container with python3 (for ansible) ----------
echo "wg-cp0-client role-fixture: starting container $CONTAINER_NAME" >&2
docker run -d --name "$CONTAINER_NAME" \
  --entrypoint /bin/bash \
  "$CONTAINER_IMAGE" -c "tail -f /dev/null" >/dev/null

# Install python3 inside the container (ansible needs it) and `shred`
# (coreutils — present by default on ubuntu:24.04 but explicit-install
# keeps the harness robust if base image changes).
docker exec "$CONTAINER_NAME" bash -c "
  set -e
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get install -y -qq python3 python3-minimal coreutils >/dev/null
" || fail "container python3 install failed"

# --- Run the role against the container ----------------------------------
INVENTORY="$WORKDIR/inventory.ini"
cat >"$INVENTORY" <<EOF
[targets]
$CONTAINER_NAME ansible_connection=docker ansible_user=root ansible_python_interpreter=/usr/bin/python3
EOF

PLAYBOOK="$WORKDIR/play.yml"
cat >"$PLAYBOOK" <<EOF
---
- hosts: targets
  gather_facts: true   # role's cross-compile task reads ansible_architecture
  vars:
    wg_cp0_test_mode: true
    wg_cp0_bundle_path: "$BUNDLE_PATH"
    wg_cp0_ca_pubkey_local_path: "$CA_PUB_PATH"
    # Use the pre-built binary the harness produced — avoids the role's
    # default {{ playbook_dir }}/../enrollment chdir which doesn't apply
    # when the playbook lives outside ops/ansible/.
    wg_cp0_bundle_apply_binary: "$WORKDIR/wg-cp0-bundle-apply"
  roles:
    - role: wg-cp0-client
EOF

ANSIBLE_ROLES_PATH="$REPO_ROOT/ops/ansible/roles" \
ANSIBLE_RETRY_FILES_ENABLED=false \
ansible-playbook \
  -i "$INVENTORY" \
  "$PLAYBOOK" \
  >"$WORKDIR/ansible.log" 2>&1 \
  || fail "ansible-playbook failed:
$(tail -50 "$WORKDIR/ansible.log")"

# --- Assert the role's outputs ----------------------------------
echo "wg-cp0-client role-fixture: asserting outputs" >&2

assert_in_container() {
  local cmd="$1" desc="$2"
  if ! docker exec "$CONTAINER_NAME" bash -c "$cmd" >/dev/null 2>&1; then
    fail "$desc"
  fi
}

# wg-cp0.conf must exist with mode 0600.
assert_in_container "test -f /etc/wireguard/wg-cp0.conf" \
  "/etc/wireguard/wg-cp0.conf missing"
PERM=$(docker exec "$CONTAINER_NAME" stat -c '%a' /etc/wireguard/wg-cp0.conf)
[[ "$PERM" == "600" ]] || fail "wg-cp0.conf mode = $PERM; want 600"

# Privkey file must exist with mode 0600.
assert_in_container "test -f /etc/wireguard/wg-cp0-priv.key" \
  "/etc/wireguard/wg-cp0-priv.key missing"
PERM=$(docker exec "$CONTAINER_NAME" stat -c '%a' /etc/wireguard/wg-cp0-priv.key)
[[ "$PERM" == "600" ]] || fail "wg-cp0-priv.key mode = $PERM; want 600"

# Conf content sanity.
CONF="$(docker exec "$CONTAINER_NAME" cat /etc/wireguard/wg-cp0.conf)"
for line in '[Interface]' 'PrivateKey = ' 'Address    = 198.18.0.99/24' '[Peer]' 'Endpoint   = 203.0.113.1:51821' 'AllowedIPs = 198.18.0.2/32, 198.18.0.0/24' 'PersistentKeepalive = 25' '# Site:        test-site' '# Device:      test-device-01'; do
  if ! grep -qF -- "$line" <<<"$CONF"; then
    echo "wg-cp0-client role-fixture: missing line: $line" >&2
    echo "--- conf ---" >&2
    echo "$CONF" >&2
    fail "wg-cp0.conf does not contain expected line"
  fi
done

# Bundle MUST have been shredded (post-apply discipline).
if docker exec "$CONTAINER_NAME" test -f /etc/goat/bundle.cbor 2>/dev/null; then
  fail "/etc/goat/bundle.cbor still present — shred-after-apply did not run"
fi

# Privkey content matches the [Interface] PrivateKey line — proves the
# privkey file came from the bundle and not from a stray local source.
PRIVKEY_LINE=$(grep -E '^PrivateKey = ' <<<"$CONF" | cut -d= -f2- | tr -d ' ')
PRIVKEY_FILE=$(docker exec "$CONTAINER_NAME" cat /etc/wireguard/wg-cp0-priv.key | tr -d '\n')
[[ "$PRIVKEY_LINE" == "$PRIVKEY_FILE" ]] || fail "privkey file != [Interface] PrivateKey"

# --- Idempotency: re-run the playbook + assert no re-shred regression ---
echo "wg-cp0-client role-fixture: idempotency re-run" >&2
ANSIBLE_ROLES_PATH="$REPO_ROOT/ops/ansible/roles" \
ANSIBLE_RETRY_FILES_ENABLED=false \
ansible-playbook \
  -i "$INVENTORY" \
  "$PLAYBOOK" \
  >"$WORKDIR/ansible-rerun.log" 2>&1 \
  || fail "ansible-playbook re-run failed:
$(tail -50 "$WORKDIR/ansible-rerun.log")"

# Re-run should still produce a valid conf + privkey.
assert_in_container "test -f /etc/wireguard/wg-cp0.conf" \
  "/etc/wireguard/wg-cp0.conf missing after re-run"
assert_in_container "test -f /etc/wireguard/wg-cp0-priv.key" \
  "/etc/wireguard/wg-cp0-priv.key missing after re-run"

echo "wg-cp0-client role-fixture: PASS"
