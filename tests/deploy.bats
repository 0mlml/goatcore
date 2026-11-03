#!/usr/bin/env bats
# Tests for deploy.sh — validates structure and safety checks

REPO_ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"

# ── Script structure ────────────────────────────────────

@test "deploy.sh: uses set -euo pipefail" {
  head -5 "$REPO_ROOT/deploy.sh" | grep -q 'set -euo pipefail'
}

@test "deploy.sh: sources .env" {
  grep -q 'source.*\.env' "$REPO_ROOT/deploy.sh"
}

@test "deploy.sh: verifies SSH connectivity before proceeding" {
  # Should check connectivity early and exit if unreachable
  grep -q 'Cannot SSH\|Verifying SSH' "$REPO_ROOT/deploy.sh"
}

@test "deploy.sh: cleans up conflicting DNS records before tunnel" {
  grep -q 'EXISTING_A\|DELETE.*dns_records' "$REPO_ROOT/deploy.sh"
  grep -q 'EXISTING_CNAME\|DELETE.*dns_records' "$REPO_ROOT/deploy.sh"
}

@test "deploy.sh: Docker install has idempotency guard" {
  grep -q 'command -v docker' "$REPO_ROOT/deploy.sh"
}

@test "deploy.sh: Netbird deploy has idempotency guard" {
  grep -q 'docker-compose.yml' "$REPO_ROOT/deploy.sh"
}

@test "deploy.sh: adds /etc/hosts entry for LAN gRPC bypass" {
  grep -q 'etc/hosts' "$REPO_ROOT/deploy.sh"
}
