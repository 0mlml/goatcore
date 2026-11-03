#!/usr/bin/env bash
# check-sdk-docs-drift.sh — mechanical, Scaladoc-style gate that ties the
# grant-crypto SDK documentation site to the code it documents (ADR 1106).
#
# Fails (listing every offender) if:
#   1. any gc_* function declared in the committed C ABI header is NOT documented
#      on the C-ABI docs page (capi.html);
#   2. any GC_* status-code constant in the header is NOT documented there;
#   3. the version badge shown on the site (--sdk-version in styles.css) does not
#      match the core crate version (sdk/core/Cargo.toml [package] version).
#
# Dependency-free: bash + grep + awk + sed only. Runs from anywhere (it resolves
# its own paths). Wired into .github/workflows/grant-crypto.yml (rust-core job),
# after the C ABI header drift gate — so the header, the code, and the docs move
# together or CI stops the merge.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$here/../.." && pwd)"

HEADER="$root/ops/grant-crypto/sdk/capi/include/grant_crypto.h"
CAPI="$root/ops/grant-crypto/sdk/docs/site/capi.html"
STYLES="$root/ops/grant-crypto/sdk/docs/site/styles.css"
CARGO="$root/ops/grant-crypto/sdk/core/Cargo.toml"

for f in "$HEADER" "$CAPI" "$STYLES" "$CARGO"; do
  if [ ! -f "$f" ]; then
    echo "check-sdk-docs-drift: FATAL — missing required file: $f" >&2
    exit 2
  fi
done

fail=0

# ---- 1. every gc_* function in the header is documented in capi.html ----
# Header declarations look like `int32_t gc_foo(`, `uint32_t gc_abi_version(`,
# `const char *gc_error_message(`, `const uint8_t *gc_buf_ptr(`. Grab the
# gc_<name>( token, strip the paren, de-dupe.
fns="$(grep -oE '\bgc_[a-z0-9_]+\(' "$HEADER" | sed 's/(//' | sort -u)"
n_fn=0
miss_fn=0
for fn in $fns; do
  n_fn=$((n_fn + 1))
  if ! grep -qF "$fn" "$CAPI"; then
    echo "check-sdk-docs-drift: MISSING function in capi.html: $fn" >&2
    miss_fn=$((miss_fn + 1))
    fail=1
  fi
done

# ---- 2. every GC_* status-code constant in the header is documented ----
codes="$(grep -oE '#define[[:space:]]+GC_[A-Z0-9_]+' "$HEADER" | awk '{print $2}' | sort -u)"
n_code=0
miss_code=0
for c in $codes; do
  n_code=$((n_code + 1))
  if ! grep -qF "$c" "$CAPI"; then
    echo "check-sdk-docs-drift: MISSING status code in capi.html: $c" >&2
    miss_code=$((miss_code + 1))
    fail=1
  fi
done

# ---- 3. version badge on the site matches the core crate version ----
# Badge is the single-source custom property, e.g.  --sdk-version: "0.1.0";
badge="$(grep -oE -- '--sdk-version:[[:space:]]*"[^"]+"' "$STYLES" | head -1 | sed -E 's/.*"([^"]+)".*/\1/')"
# Core crate version: the `version = "..."` inside the [package] table.
cargo_ver="$(awk '
  /^\[package\]/ { in_pkg = 1; next }
  /^\[/          { in_pkg = 0 }
  in_pkg && /^[[:space:]]*version[[:space:]]*=/ {
    gsub(/[" ]/, ""); split($0, a, "="); print a[2]; exit
  }' "$CARGO")"

if [ -z "$badge" ]; then
  echo "check-sdk-docs-drift: could not read --sdk-version from styles.css" >&2
  fail=1
elif [ -z "$cargo_ver" ]; then
  echo "check-sdk-docs-drift: could not read [package] version from core/Cargo.toml" >&2
  fail=1
elif [ "$badge" != "$cargo_ver" ]; then
  echo "check-sdk-docs-drift: VERSION MISMATCH — site badge --sdk-version=\"$badge\" != core crate version \"$cargo_ver\"" >&2
  echo "  bump --sdk-version in ops/grant-crypto/sdk/docs/site/styles.css to match the release." >&2
  fail=1
fi

if [ "$fail" -ne 0 ]; then
  echo "check-sdk-docs-drift: FAILED (functions missing: $miss_fn/$n_fn, codes missing: $miss_code/$n_code)" >&2
  exit 1
fi

echo "check-sdk-docs-drift: OK — $n_fn gc_* functions + $n_code GC_* codes documented; version badge \"$badge\" matches the core crate."
