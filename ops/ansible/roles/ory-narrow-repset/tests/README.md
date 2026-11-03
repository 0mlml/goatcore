# ory-narrow-repset role tests

Tests for the `ory-narrow-repset` Ansible role, per the Block 50H
role-test-parity discipline.

## Coverage levels

### Parse-only smoke (today) — `syntax-check.sh`

`ansible-playbook --syntax-check` over `playbook.yml` (a fixture
that includes the role with `ory_spock_enabled: false` so the role
short-circuits without trying to talk to Postgres). Catches:

- YAML syntax errors in `tasks/main.yml`, `tasks/per_db.yml`,
  `tasks/per_subscription.yml`, `defaults/main.yml`.
- Jinja2 template syntax errors.
- Missing required role files.

CI-gated via [`.github/workflows/ansible-syntax-check.yml`](../../../../../.github/workflows/ansible-syntax-check.yml).

### Full integration test (queued) — `run-test.sh`

Mirrors [`ops/ansible/roles/spock-mesh-peering/tests/run-test.sh`](../../spock-mesh-peering/tests/run-test.sh)
shape. Brings up a 2-node docker-compose harness with:

- Two `postgres-spock` containers
- Two `kratos` containers (with `kratos migrate` pre-run)
- Two `hydra` containers (with `hydra migrate` pre-run)

Then runs the role on both nodes and verifies:

- Spock extension installed in both `kratos` and `hydra` DBs.
- `ory_narrow` repset created in both DBs at both nodes.
- All 10 inclusion-list tables are repset members.
- Zero exclusion-list tables are repset members (the role's
  `RAISE EXCEPTION` invariant holds).
- New identity row at site A replicates to site B.
- New Kratos flow at site A does NOT appear at site B.
- New Hydra auth code at site A is NOT consumable at site B.
- Re-running the role is idempotent (no changes on second run).

**Status:** stub. The integration harness is queued as a follow-on
to land before the operator-fired Sub-block D pilot activation.

## Notes

- The role uses `docker exec` against `postgres-a` for SQL. Local
  test fixtures need docker-compose service names matching
  `ory_spock_postgres_container` (default `postgres-a`).
- The `_REPLICATOR_PASS` env var is required pre-flight; the
  integration test fixture sets it to a test value before invoking
  the role.
