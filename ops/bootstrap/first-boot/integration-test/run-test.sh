#!/usr/bin/env bash
# Block 68A first-boot integration test runner — host side.
#
# Builds the test image (if needed), bind-mounts the repo, runs the
# in-container drive-test.sh which exercises first-boot.sh end-to-end.
# No real Orin/Karbon hardware required.
#
# Spec:    docs/project/block-68a-config-input-spike.md
# Container side: ./drive-test.sh
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"
TEST_DIR="$REPO_ROOT/ops/bootstrap/first-boot/integration-test"
IMG_TAG=goat-firstboot-test:latest

step() { printf '\n=== %s ===\n' "$*"; }
log() { printf '[run-test] %s\n' "$*"; }

step "Step 1: build container image"
docker build -t "$IMG_TAG" "$TEST_DIR"

step "Step 2: run integration test in container (repo bind-mounted read-only)"
docker run --rm \
  -v "$REPO_ROOT:/opt/goat/repo:ro" \
  --tty \
  "$IMG_TAG"

step "Integration test PASS"
log "first-boot.sh validated end-to-end without hardware"
