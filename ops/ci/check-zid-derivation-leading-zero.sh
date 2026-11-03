#!/usr/bin/env bash
# ops/ci/check-zid-derivation-leading-zero.sh — every derived Zenoh ZID must be
# stripped of leading zeros before it reaches a zenoh config.
#
# WHY: a Zenoh ID is a u128 whose string form is MINIMAL lowercase hex, so
# `uhlc::ID::from_str` refuses any hex beginning with `0`:
#
#   Invalid id: 0c21d85d… - Leading 0s are not valid
#   (uhlc-0.8.2/src/id.rs:258, surfaced via zenoh-protocol core/mod.rs:210)
#
# We derive ZIDs from hashes in two places, and a hash starts with `0` about
# one time in sixteen:
#   * router side — `sha256(<hostname>|<realm>|zenoh-router)[:32]` in the
#     ansible role defaults. A bad draw crash-loops zenohd (exit 133). zagros-b
#     hit it.
#   * client side — goat-cli pins the publish session's ZID to the slot anchor
#     (the cert CN). A bad draw killed every publish-path command for that
#     identity while read-only commands kept working (F-350, field-reported
#     ).
#
# F-281 fixed ONE role and declared the class addressed; two other roles and
# the CLI still carried it. That is the reason this guard exists: the remedy is
# a one-expression strip that is trivially forgotten at the next call site.
#
# Rules:
#   1. any ansible var named `*_zid` derived with `hash('sha256')` must also
#      carry `regex_replace('^0+', '')`;
#   2. any Rust file that writes a zenoh config `id` must normalize it (call
#      `zid_pin_value`, the one helper that strips) rather than insert a raw
#      32-hex anchor.
#
# Full-tree (not diff-scoped): both properties hold across the tree today, so
# there is nothing to grandfather.
#
# Doctrine P6: `--self-test` proves the guard goes RED on a broken fixture
# before its verdict on the real tree is trusted.
set -euo pipefail

fail=0

scan() {
  local root="$1"

  # Rule 1 — ansible ZID derivations.
  while IFS= read -r hit; do
    local file="${hit%%:*}"
    local rest="${hit#*:}"
    local line="${rest%%:*}"
    local text="${rest#*:}"
    case "$text" in
      *"regex_replace('^0+'"*) ;;
      *)
        echo "FAIL: ${file}:${line} derives a ZID from a hash without stripping leading zeros" >&2
        echo "      ${text# }" >&2
        echo "      add: | regex_replace('^0+', '')   — a no-op unless the draw starts with 0" >&2
        fail=1
        ;;
    esac
  done < <(grep -rn -E "^[a-z_]*zid[a-z_]*:.*hash\('sha256'\)" "${root}/ops/ansible/roles" 2>/dev/null || true)

  # Rule 2 — Rust zenoh config `id` writes.
  while IFS= read -r file; do
    if ! grep -q "zid_pin_value" "$file"; then
      echo "FAIL: ${file} sets a zenoh config \`id\` without normalizing it" >&2
      echo "      route the value through zid_pin_value() — zenoh rejects leading-zero ids" >&2
      fail=1
    fi
  done < <(grep -rl -E 'insert_json5\("id"' "${root}/crates" "${root}/ops" \
             --include="*.rs" 2>/dev/null | grep -v '/target/' || true)
}

if [ "${1:-}" = "--self-test" ]; then
  fixture="$(mktemp -d)"
  trap 'rm -rf "${fixture}"' EXIT
  mkdir -p "${fixture}/ops/ansible/roles/broken/defaults" "${fixture}/crates/broken/src"
  cat > "${fixture}/ops/ansible/roles/broken/defaults/main.yml" <<'YAML'
broken_router_zid: "{{ (broken_zid_seed | hash('sha256'))[:32] }}"
YAML
  cat > "${fixture}/crates/broken/src/main.rs" <<'RUST'
fn main() { let _ = config.insert_json5("id", &format!("\"{anchor}\"")); }
RUST
  # Each rule is proven separately — a self-test that only asserts "something
  # failed" would still pass if one of the two rules had rotted into a no-op.
  out="$(scan "${fixture}" 2>&1 || true)"
  case "$out" in
    *"derives a ZID from a hash without stripping"*) ;;
    *) echo "SELF-TEST FAIL: rule 1 (ansible derivation) did not fire" >&2; exit 2 ;;
  esac
  case "$out" in
    *"without normalizing it"*) ;;
    *) echo "SELF-TEST FAIL: rule 2 (rust zenoh id write) did not fire" >&2; exit 2 ;;
  esac
  echo "OK: self-test — guard goes RED on an unstripped derivation and on an unnormalized pin."
  exit 0
fi

scan "$(git rev-parse --show-toplevel)"

if [ "$fail" -ne 0 ]; then
  echo "" >&2
  echo "See docs/findings/F-350-*.md (client pin) and F-281 (router crash loop)." >&2
  exit 1
fi

echo "OK: every derived ZID strips leading zeros; every zenoh \`id\` write is normalized."
