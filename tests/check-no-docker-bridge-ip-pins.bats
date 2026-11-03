#!/usr/bin/env bats

# Tests for ops/ci/check-no-docker-bridge-ip-pins.sh.

REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
SUBJECT="$REPO_ROOT/ops/ci/check-no-docker-bridge-ip-pins.sh"

setup() {
  if [ ! -f "$SUBJECT" ]; then
    echo "subject script not found at $SUBJECT" >&2
    return 1
  fi
  if ! command -v git >/dev/null 2>&1; then
    if [ -n "${CI:-}" ]; then
      echo "missing required tool: git (required in CI)" >&2
      return 1
    fi
    skip "git not available"
  fi
}

init_sandbox_repo() {
  sandbox="$BATS_TEST_TMPDIR/check-no-docker-bridge-ip-pins-$BATS_TEST_NUMBER"
  mkdir -p "$sandbox/ops/ci"
  (
    cd "$sandbox" &&
      git init --quiet &&
      git config user.email "test@example.com" &&
      git config user.name "Test User"
  )
  cp "$SUBJECT" "$sandbox/ops/ci/check-no-docker-bridge-ip-pins.sh"
}

@test "self-test goes red on the fixtures it must catch and green on the rest" {
  run "${BASH_UNDER_TEST:-/bin/bash}" -u "$SUBJECT" --self-test
  [ "$status" -eq 0 ]
  [[ "$output" == *"OK: self-test"* ]]
}

@test "the default that broke zagros is refused with file and line" {
  init_sandbox_repo
  mkdir -p "$sandbox/ops/ansible/roles/onboard-runner/defaults"
  cat >"$sandbox/ops/ansible/roles/onboard-runner/defaults/main.yml" <<'YAML'
onboard_runner_mgmt_fqdn_addr: "{{ onboard_runner_traefik_ip | default('172.18.0.5', true) }}"
YAML
  (cd "$sandbox" && git add -A)

  cd "$sandbox"
  run "${BASH_UNDER_TEST:-/bin/bash}" -u ./ops/ci/check-no-docker-bridge-ip-pins.sh
  [ "$status" -eq 1 ]
  [[ "$output" == *"FAIL: ops/ansible/roles/onboard-runner/defaults/main.yml:1:"* ]]
  [[ "$output" == *"host-gateway"* ]]
}

@test "host-gateway, CIDR, range, comments and a reasoned marker pass" {
  init_sandbox_repo
  mkdir -p "$sandbox/ops/ansible/roles/onboard-runner/defaults" \
           "$sandbox/ops/ansible/roles/onboard-runner/templates" \
           "$sandbox/ops/ansible/roles/coturn/templates" \
           "$sandbox/ops/compose/mgmt-host"
  cat >"$sandbox/ops/ansible/roles/onboard-runner/defaults/main.yml" <<'YAML'
onboard_runner_mgmt_fqdn_addr: "{{ onboard_runner_traefik_ip | default('host-gateway', true) }}"
node_net_health_internal_cidrs: "172.16.0.0/12"
YAML
  cat >"$sandbox/ops/ansible/roles/onboard-runner/templates/docker-compose.yml.j2" <<'J2'
    # was 172.18.0.5 before host-gateway
    extra_hosts:
      - "{{ public_mgmt_fqdn }}:{{ onboard_runner_mgmt_fqdn_addr }}"  # via 172.18.0.1 on the host
J2
  cat >"$sandbox/ops/ansible/roles/coturn/templates/turnserver.conf.j2" <<'CONF'
denied-peer-ip=172.16.0.0-172.31.255.255
CONF
  cat >"$sandbox/ops/compose/mgmt-host/docker-compose.yml" <<'YAML'
networks:
  mgmt:
    ipam:
      config:
        - subnet: 172.20.0.0/16
          gateway: 172.20.0.1   # docker-bridge-ip-ok: this stanza defines the network
YAML
  (cd "$sandbox" && git add -A)

  cd "$sandbox"
  run "${BASH_UNDER_TEST:-/bin/bash}" -u ./ops/ci/check-no-docker-bridge-ip-pins.sh
  [ "$status" -eq 0 ]
  [[ "$output" == *"OK: no docker-bridge host literals"* ]]
}

@test "a bare marker is refused as a bypass" {
  init_sandbox_repo
  mkdir -p "$sandbox/ops/compose/mgmt-host"
  cat >"$sandbox/ops/compose/mgmt-host/docker-compose.yml" <<'YAML'
          gateway: 172.20.0.1   # docker-bridge-ip-ok:
YAML
  (cd "$sandbox" && git add -A)

  cd "$sandbox"
  run "${BASH_UNDER_TEST:-/bin/bash}" -u ./ops/ci/check-no-docker-bridge-ip-pins.sh
  [ "$status" -eq 1 ]
  [[ "$output" == *"needs a reason"* ]]
}

@test "files outside the shared trees are not scanned in tree mode" {
  init_sandbox_repo
  mkdir -p "$sandbox/ops/ansible/group_vars/edge" "$sandbox/ops/ansible/roles/x/defaults/tests" "$sandbox/docs"
  echo 'endpoint: "tls/172.20.0.1:7447"' >"$sandbox/ops/ansible/group_vars/edge/deployment.yml"
  echo 'fixture: 172.18.0.5' >"$sandbox/ops/ansible/roles/x/defaults/tests/fixture.yml"
  echo 'the runner sat at 172.18.0.5' >"$sandbox/docs/note.md"
  (cd "$sandbox" && git add -A)

  cd "$sandbox"
  run "${BASH_UNDER_TEST:-/bin/bash}" -u ./ops/ci/check-no-docker-bridge-ip-pins.sh
  [ "$status" -eq 0 ]
}

@test "--staged scans only the index, explicit paths scan anything" {
  init_sandbox_repo
  mkdir -p "$sandbox/ops/ansible/roles/x/defaults" "$sandbox/scratch"
  echo 'ok: host-gateway' >"$sandbox/ops/ansible/roles/x/defaults/main.yml"
  echo 'pin: 172.18.0.5' >"$sandbox/scratch/unstaged.yml"
  (cd "$sandbox" && git add ops)

  cd "$sandbox"
  run "${BASH_UNDER_TEST:-/bin/bash}" -u ./ops/ci/check-no-docker-bridge-ip-pins.sh --staged
  [ "$status" -eq 0 ]

  run "${BASH_UNDER_TEST:-/bin/bash}" -u ./ops/ci/check-no-docker-bridge-ip-pins.sh scratch
  [ "$status" -eq 1 ]
  [[ "$output" == *"FAIL: scratch/unstaged.yml:1:"* ]]
}
