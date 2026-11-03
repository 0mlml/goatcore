#!/usr/bin/env bash
# Run against repository source only, using synthetic fixture keys and payloads.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture_dir="$(mktemp -d "${TMPDIR:-/tmp}/goat-wire-interop.XXXXXX")"
trap 'rm -rf "$fixture_dir"' EXIT
GRANT_CRYPTO_WIRE_FIXTURE="$fixture_dir/fixture.json" cargo test \
  --manifest-path "$root/ops/grant-crypto/sdk/Cargo.toml" --locked \
  -p grant-crypto --test write_interop export_go_fixture -- --exact --ignored
(
  cd "$root/ops/grant-crypto"
  GOWORK=off go run ./conformance/rust-writer < "$fixture_dir/fixture.json"
)
