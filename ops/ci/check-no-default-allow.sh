#!/usr/bin/env bash
# ops/ci/check-no-default-allow.sh — no goatnet may carry a Default All->All
# NetBird policy, and nothing in the tree may write one (ADR 1185, F-551).
#
# WHY: goat-netbird-account-init.yml wrote an enabled All<->All policy straight
# into NetBird's Postgres on any site without a declared `delete`. On
# the uk joiner ran it and the policy landed in the live zagros
# account, with no audit event: every zagros peer could reach every other on any
# port. ADR 0807 already required default-deny; nothing enforced it.
#
# Fails when:
#   1. any file under ops/ (docs excluded) writes NetBird's policy tables with
#      raw SQL: INSERT INTO / UPDATE / DELETE FROM policies or policy_rules.
#      Policies change through the API, from the declared ACL, only.
#   2. templates/management.json.j2 does not set "DisableDefaultPolicy": true —
#      NetBird creates a Default All->All on every new account otherwise.
#   3. any goatnet in ops/ansible/netbird_acl_matrix.yml declares a
#      cutover.default_policy_action other than `delete`.
#
# Usage:  ops/ci/check-no-default-allow.sh [--self-test]
# Exit:   0 clean · 1 violation · 2 usage/dep error
set -euo pipefail
cd "$(git rev-parse --show-toplevel 2>/dev/null || pwd)"

SQL_RE='(insert[[:space:]]+into|update|delete[[:space:]]+from)[[:space:]]+"?(policies|policy_rules)"?([[:space:]]|\(|$)'

scan_sql() {  # files on stdin -> violations on stdout
  while IFS= read -r f; do
    [ -f "$f" ] || continue
    grep -nHiE "$SQL_RE" "$f" 2>/dev/null || true
  done
}

# Files allowed to touch the policy tables, each for a reason that is not "a
# goatnet gets a policy". Exact paths only; a new entry is a reviewed decision.
ALLOW=(
  ops/ci/check-no-default-allow.sh                 # this guard's own patterns
  ops/postgres/migrate.sh                          # DELETEs orphaned rules after a store migration; writes no policy
  ops/wave3/scripts/during-partition/modify-policy.sh  # Spock LWW soak: conflict row on a scratch store
  ops/wave3/soak-gen/main.go                       # Spock soak load generator, scratch store
  ops/postgres/spock/tests/partition-harness/scenarios/netbird-c5-policy-regenerate-no-duplicates.sh  # ADR 1165 partition harness: scratch two-node store in CI
  ops/wave-33/tier-4b/inchworm.sh                  # Block 33B throwaway load-test env, torn down after its verdict
)

run() {
  local rc=0 hits allow_re
  allow_re="^($(printf '%s|' "${ALLOW[@]}" | sed 's/[.]/\\./g; s/|$//'))$"
  hits="$(git ls-files ops | grep -vE '\.md$' | grep -vE "$allow_re" | scan_sql)"
  if [ -n "$hits" ]; then
    echo "FAIL: raw SQL write to NetBird's policy tables (ADR 1185 — policies come from the declared ACL, through the API):"
    printf '  %s\n' "$hits"; rc=1
  fi
  if ! grep -qE '"DisableDefaultPolicy":[[:space:]]*true' ops/ansible/templates/management.json.j2; then
    echo "FAIL: ops/ansible/templates/management.json.j2 must set \"DisableDefaultPolicy\": true (ADR 1185)."
    rc=1
  fi
  local bad
  bad="$(python3 - <<'PY'
import yaml
m = yaml.safe_load(open("ops/ansible/netbird_acl_matrix.yml"))["netbird_acl_matrix"]
for site, v in m.items():
    a = (v.get("cutover") or {}).get("default_policy_action")
    if a != "delete":
        print(f"{site}: default_policy_action={a!r}")
PY
)"
  if [ -n "$bad" ]; then
    echo "FAIL: every goatnet must declare cutover.default_policy_action: delete (ADR 1185):"
    printf '  %s\n' "$bad"; rc=1
  fi
  [ "$rc" = 0 ] && echo "no-default-allow: OK — no raw policy SQL, DisableDefaultPolicy set, every goatnet declares delete."
  return "$rc"
}

self_test() {
  local t; t="$(mktemp -d)"; trap 'rm -rf "$t"' RETURN
  printf 'x: |\n  INSERT INTO policies (id) VALUES (1);\n' > "$t/a.yml"
  printf 'x: |\n  update policy_rules set enabled=true;\n' > "$t/b.yml"
  printf 'x: |\n  INSERT INTO name_server_groups (id) VALUES (1);\n  -- policies are declared\n' > "$t/c.yml"
  local hits; hits="$(printf '%s\n' "$t/a.yml" "$t/b.yml" "$t/c.yml" | scan_sql)"
  echo "$hits" | grep -q 'a.yml' || { echo "self-test FAIL: missed INSERT INTO policies"; return 1; }
  echo "$hits" | grep -q 'b.yml' || { echo "self-test FAIL: missed UPDATE policy_rules"; return 1; }
  echo "$hits" | grep -q 'c.yml' && { echo "self-test FAIL: flagged a nameserver insert"; return 1; }
  echo "self-test OK"
}

case "${1:-}" in
  --self-test) self_test ;;
  "") run ;;
  *) echo "usage: $0 [--self-test]" >&2; exit 2 ;;
esac
