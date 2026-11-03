#!/usr/bin/env bats
# Contract between the ACME Traefik application routers and goat-id-console's
# probation ForwardAuth handler. The handler tests cover classification; these
# assertions pin the security-sensitive proxy wiring that supplies its headers.

REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
COMPOSE="$REPO_ROOT/ops/ansible/templates/traefik-acme/docker-compose.acme.yml.j2"
DYNAMIC="$REPO_ROOT/ops/ansible/templates/traefik-acme/mgmt-dynamic.yml.j2"

@test "probation ForwardAuth is not an entrypoint-wide control-plane dependency" {
  run grep -F -- '--entrypoints.websecure.http.middlewares=goat-entrypoint-ceiling@file,goat-compress@file' "$COMPOSE"
  [ "$status" -eq 0 ]
  run grep -F -- 'entrypoints.websecure.http.middlewares=' "$COMPOSE"
  [ "$status" -eq 0 ]
  [[ "$output" != *'goat-probation-gate'* ]]
}

@test "probation ForwardAuth preserves allowed request headers" {
  run grep -A5 '^    goat-probation-gate:$' "$DYNAMIC"
  [ "$status" -eq 0 ]
  [[ "$output" == *'address: "http://goat-id-console:8080/internal/probation/auth"'* ]]
  [[ "$output" == *'trustForwardHeader: false'* ]]
  [[ "$output" != *'authResponseHeaders'* ]]
}

@test "application chain is gated while control-plane routers bypass the console" {
  run grep -A9 '^    mesh-only-allowlist:$' "$DYNAMIC"
  [ "$status" -eq 0 ]
  [[ "$output" == *'- mesh-source-allowlist'* ]]
  [[ "$output" == *'- goat-probation-gate'* ]]

  for router in signal-mesh-apex mgmt-mesh-apex mgmt-mesh mgmt-read mgmt-admin host-net-ingest zenoh-health-ingest kratos-api selfservice hydra; do
    run awk -v router="$router" '
      $0 == "    " router ":" { inside=1; next }
      inside && /^    [A-Za-z0-9_-]+:$/ { exit }
      inside { print }
    ' "$DYNAMIC"
    [ "$status" -eq 0 ]
    [[ "$output" == *'- mesh-source-allowlist'* ]]
    [[ "$output" != *'- mesh-only-allowlist'* ]]
  done

  for router in dashboard dashboard-admin grafana id-admin-console id-admin-console-guide-root id-admin-console-guide id-admin-console-onboard relay-health; do
    run awk -v router="$router" '
      $0 == "    " router ":" { inside=1; next }
      inside && /^    [A-Za-z0-9_-]+:$/ { exit }
      inside { print }
    ' "$DYNAMIC"
    [ "$status" -eq 0 ]
    [[ "$output" == *'- mesh-only-allowlist'* ]]
  done
}

@test "enabled gate readiness is rechecked after console recreation" {
  local tasks="$REPO_ROOT/ops/ansible/roles/id-admin-console/tasks/main.yml"

  run grep -A12 '^\- name: Verify probation classifier after console recreation$' "$tasks"
  [ "$status" -eq 0 ]
  [[ "$output" == *'/internal/probation/ready'* ]]
  [[ "$output" == *'status_code: 204'* ]]
  [[ "$output" == *'until: id_console_probation_ready.status == 204'* ]]
  [[ "$output" == *'retries: 10'* ]]
  [[ "$output" == *'when: probation_explainer_enabled | default(false) | bool'* ]]
}

@test "petition path is explicitly enabled and remains mesh-source-only" {
  run grep -B4 -A13 '^    probation-reactivation-request:$' "$DYNAMIC"
  [ "$status" -eq 0 ]
  [[ "$output" == *'{%   if probation_explainer_enabled | default(false) | bool %}'* ]]
  [[ "$output" == *'priority: 10000'* ]]
  [[ "$output" == *'rule: "Path(`/__goat/probation/request`)"'* ]]
  [[ "$output" == *'service: id-admin-console-onboard'* ]]
  [[ "$output" == *'- mesh-source-allowlist'* ]]
}

@test "activation defaults off and deploy checks console, records, and governance" {
  run grep -F 'probation_explainer_enabled: false' "$REPO_ROOT/ops/ansible/group_vars/all.yml"
  [ "$status" -eq 0 ]
  run grep -F 'running probation classifier is ready' "$REPO_ROOT/ops/ansible/traefik-acme.yml"
  [ "$status" -eq 0 ]

  run grep -A5 'running probation classifier is ready' "$REPO_ROOT/ops/ansible/traefik-acme.yml"
  [ "$status" -eq 0 ]
  [[ "$output" == *'/internal/probation/ready'* ]]
  [[ "$output" == *'id_admin_console_host_port | default(8088) }}/internal/probation/ready'* ]]
  [[ "$output" != *'198.51.100.7'* ]]
  run grep -F "!= 'enforce'" "$REPO_ROOT/ops/ansible/traefik-acme.yml"
  [ "$status" -eq 0 ]

  run grep -A14 'probation NetBird peer source is readable' "$REPO_ROOT/ops/ansible/traefik-acme.yml"
  [ "$status" -eq 0 ]
  [[ "$output" == *"/api/peers"* ]]
  [[ "$output" == *'Authorization: "Token {{ id_admin_console_enroll_watch_nb_token }}"'* ]]
  [[ "$output" == *'follow_redirects: none'* ]]
  [[ "$output" == *'status_code: 200'* ]]
  [[ "$output" == *'no_log: true'* ]]

  run grep -A12 'probation NetBird peer source has the expected shape' "$REPO_ROOT/ops/ansible/traefik-acme.yml"
  [ "$status" -eq 0 ]
  [[ "$output" == *"(_probation_peer_source.json | type_debug) == 'list'"* ]]
}
