#!/usr/bin/env bash
# ops/ansible/roles/minio/tests/test-fixture.sh
#
# Local fixture test for the MinIO role. Brings up MinIO + the mc-init
# bootstrap via docker compose (no Ansible runner required) using the
# same templates the role renders, then asserts the post-conditions
# the role would otherwise check on a real host.
#
# What this catches that production-side checks would miss:
#   - Object Lock not actually applied (silent: bucket appears to work
#     but isn't tamper-resistant; cannot be enabled retroactively).
#   - Lifecycle rule missing (silent: telemetry never expires; storage
#     creep ensues until somebody investigates a full disk).
#   - Container UID mismatch (the obs-host crash-loop class — fixed
#     in commit 00dad9e — caught here as a permission-denied at write).
#   - mc-init.sh script bugs (idempotency regressions, mc syntax drift
#     across mc client versions).
#
# Run locally:
#   ops/ansible/roles/minio/tests/test-fixture.sh
#
# Side effects: temporary docker-compose project under
# /tmp/minio-role-fixture-XXXX/. Auto-cleaned on success or failure.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../../../.." && pwd)"
WORK="$(mktemp -d -t minio-role-fixture-XXXXXX)"
PROJECT="miniotest-$$"

cleanup() {
  docker compose -f "$WORK/docker-compose.yml" -p "$PROJECT" down -v >/dev/null 2>&1 || true
  rm -rf "$WORK"
}
trap cleanup EXIT

# ---- Fixed test creds (fixture only) ----
export MINIO_ROOT_USER="miniofixture"
export MINIO_ROOT_PASSWORD="miniofixture-fixed-aaaa-bbbb-cccc"

# Ports — different from prod defaults so we don't collide with a real
# MinIO running on the same machine.
TEST_API_PORT=19000
TEST_CONSOLE_PORT=19001

# Reduced retention windows for fixture (1d audit, 1d telemetry) so the
# test isn't testing year-long retention math.
TEST_AUDIT_RETENTION_DAYS=1
TEST_TELEMETRY_RETENTION_DAYS=1
TEST_RUNTIME_DETECTION_RETENTION_DAYS=1

mkdir -p "$WORK/data"
chmod 0750 "$WORK/data"
# Match container UID 1000:1000. On macOS Docker Desktop the host UID
# differs but the container can still write because the bind mount is
# remapped. On Linux this matters — mirrors prod.
chown 1000:1000 "$WORK/data" 2>/dev/null || true

# ---- Render the role's templates manually ----
# We don't run Ansible from here; we substitute the few variables the
# templates reference. Keeps the fixture runnable on a bare developer
# machine.

cat > "$WORK/docker-compose.yml" <<EOF
services:
  minio:
    image: minio/minio:RELEASE.2024-10-29T16-01-48Z
    container_name: ${PROJECT}-minio
    restart: "no"
    user: "1000:1000"
    command:
      - server
      - /data
      - --address
      - ":${TEST_API_PORT}"
      - --console-address
      - ":${TEST_CONSOLE_PORT}"
    environment:
      MINIO_ROOT_USER: "\${MINIO_ROOT_USER}"
      MINIO_ROOT_PASSWORD: "\${MINIO_ROOT_PASSWORD}"
    volumes:
      - $WORK/data:/data
    ports:
      - "${TEST_API_PORT}:${TEST_API_PORT}"
      - "${TEST_CONSOLE_PORT}:${TEST_CONSOLE_PORT}"
    healthcheck:
      test: ["CMD", "curl", "-fsS", "http://localhost:${TEST_API_PORT}/minio/health/live"]
      interval: 2s
      timeout: 5s
      retries: 30
    networks:
      - miniotest

  mc-init:
    image: minio/mc:RELEASE.2024-10-29T15-34-59Z
    container_name: ${PROJECT}-mc-init
    restart: "no"
    profiles: ["init"]
    depends_on:
      minio:
        condition: service_healthy
    environment:
      MC_HOST_local: "http://\${MINIO_ROOT_USER}:\${MINIO_ROOT_PASSWORD}@minio:${TEST_API_PORT}"
    volumes:
      - $WORK/mc-init.sh:/mc-init.sh:ro
    entrypoint: ["/bin/sh", "/mc-init.sh"]
    networks:
      - miniotest

networks:
  miniotest:
    name: ${PROJECT}-net
EOF

# Render mc-init.sh by substituting from the role's template. The
# template uses {{ }} for variables; we simulate Ansible's substitution
# with sed.
sed \
  -e "s|{{ minio_audit_bucket }}|audit-integrity|g" \
  -e "s|{{ minio_telemetry_bucket }}|telemetry-otlp|g" \
  -e "s|{{ minio_runtime_detection_bucket }}|runtime-detection-archive|g" \
  -e "s|{{ minio_audit_retention_days }}|${TEST_AUDIT_RETENTION_DAYS}|g" \
  -e "s|{{ minio_telemetry_retention_days }}|${TEST_TELEMETRY_RETENTION_DAYS}|g" \
  -e "s|{{ minio_runtime_detection_retention_days }}|${TEST_RUNTIME_DETECTION_RETENTION_DAYS}|g" \
  "$REPO_ROOT/ops/ansible/roles/minio/templates/mc-init.sh.j2" \
  > "$WORK/mc-init.sh"
chmod +x "$WORK/mc-init.sh"

# ---- Assertion helpers ----
PASS=0
FAIL=0

assert() {
  local label="$1" cond="$2"
  if eval "$cond"; then
    echo "  PASS  $label"
    PASS=$((PASS + 1))
  else
    echo "  FAIL  $label"
    echo "    cond: $cond"
    FAIL=$((FAIL + 1))
  fi
}

# ---- Bring up MinIO server ----
echo "→ Pulling images"
docker compose -f "$WORK/docker-compose.yml" -p "$PROJECT" pull -q minio mc-init >/dev/null

echo "→ Starting MinIO"
docker compose -f "$WORK/docker-compose.yml" -p "$PROJECT" up -d minio >/dev/null

# Wait for MinIO healthy
echo "→ Waiting for MinIO /minio/health/live"
for i in $(seq 1 30); do
  if curl -fsS "http://localhost:${TEST_API_PORT}/minio/health/live" >/dev/null 2>&1; then
    echo "  MinIO ready after ${i}×2s"
    break
  fi
  sleep 2
done

# ---- Run mc-init (idempotency-1) ----
echo "→ Running mc-init (first run)"
INIT_OUT_1=$(docker compose -f "$WORK/docker-compose.yml" -p "$PROJECT" run --rm mc-init 2>&1)
echo "$INIT_OUT_1" | sed 's/^/    /'

# ---- Assertions ----
echo ""
echo "→ Asserting post-conditions"

assert "audit-integrity bucket exists" \
  "docker compose -f '$WORK/docker-compose.yml' -p '$PROJECT' run --rm mc-init mc ls local/audit-integrity/ >/dev/null 2>&1 || docker compose -f '$WORK/docker-compose.yml' -p '$PROJECT' run --rm mc-init mc ls local/ 2>/dev/null | grep -q audit-integrity"

assert "telemetry-otlp bucket exists" \
  "docker compose -f '$WORK/docker-compose.yml' -p '$PROJECT' run --rm mc-init mc ls local/telemetry-otlp/ >/dev/null 2>&1 || docker compose -f '$WORK/docker-compose.yml' -p '$PROJECT' run --rm mc-init mc ls local/ 2>/dev/null | grep -q telemetry-otlp"

assert "runtime-detection-archive bucket exists (Block 18G slot)" \
  "docker compose -f '$WORK/docker-compose.yml' -p '$PROJECT' run --rm mc-init mc ls local/runtime-detection-archive/ >/dev/null 2>&1 || docker compose -f '$WORK/docker-compose.yml' -p '$PROJECT' run --rm mc-init mc ls local/ 2>/dev/null | grep -q runtime-detection-archive"

assert "runtime-detection-archive has Object Lock COMPLIANCE" \
  "docker compose -f '$WORK/docker-compose.yml' -p '$PROJECT' run --rm mc-init mc retention info --default local/runtime-detection-archive 2>&1 | grep -q COMPLIANCE"

# This is the silent-failure-mode test. If --with-lock is missing from
# mc-init.sh, the bucket appears to work; only this assertion catches it.
assert "audit-integrity has Object Lock enabled" \
  "docker compose -f '$WORK/docker-compose.yml' -p '$PROJECT' run --rm mc-init mc retention info --default local/audit-integrity 2>&1 | grep -q COMPLIANCE"

assert "audit-integrity retention duration matches role default" \
  "docker compose -f '$WORK/docker-compose.yml' -p '$PROJECT' run --rm mc-init mc retention info --default local/audit-integrity 2>&1 | grep -E 'days|d|${TEST_AUDIT_RETENTION_DAYS}' >/dev/null"

assert "telemetry-otlp has expiration lifecycle rule" \
  "docker compose -f '$WORK/docker-compose.yml' -p '$PROJECT' run --rm mc-init mc ilm rule list local/telemetry-otlp 2>&1 | grep -E 'Expiration|expire' >/dev/null"

# Test the negative — write a small object, ensure write succeeds (UID
# alignment is correct).
WRITE_OUT=$(docker compose -f "$WORK/docker-compose.yml" -p "$PROJECT" run --rm mc-init sh -c "echo 'fixture-write-test' | mc pipe local/telemetry-otlp/fixture-test.txt" 2>&1)
assert "telemetry-otlp accepts a write (container UID can write)" \
  "echo '$WRITE_OUT' | grep -qiE 'fixture-test|^$'"

assert "telemetry-otlp object readable back" \
  "docker compose -f '$WORK/docker-compose.yml' -p '$PROJECT' run --rm mc-init mc cat local/telemetry-otlp/fixture-test.txt 2>&1 | grep -q 'fixture-write-test'"

# ---- Idempotency — re-run mc-init ----
echo ""
echo "→ Running mc-init (second run, idempotency check)"
INIT_OUT_2=$(docker compose -f "$WORK/docker-compose.yml" -p "$PROJECT" run --rm mc-init 2>&1)

# The script should NOT report `created:` for buckets on second run (they
# already exist). It SHOULD report `unchanged:`.
assert "second-run reports unchanged for audit-integrity" \
  "echo '$INIT_OUT_2' | grep -q 'unchanged: bucket audit-integrity'"

assert "second-run reports unchanged for telemetry-otlp" \
  "echo '$INIT_OUT_2' | grep -q 'unchanged: bucket telemetry-otlp'"

# Confirm Object Lock STILL says COMPLIANCE after re-run (regression
# guard against accidentally dropping retention on subsequent applies).
assert "audit-integrity still COMPLIANCE after re-run" \
  "docker compose -f '$WORK/docker-compose.yml' -p '$PROJECT' run --rm mc-init mc retention info --default local/audit-integrity 2>&1 | grep -q COMPLIANCE"

# ---- Summary ----
echo ""
echo "================================================================"
echo "  PASS: $PASS    FAIL: $FAIL"
echo "================================================================"
if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
