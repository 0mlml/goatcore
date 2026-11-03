#!/usr/bin/env bash
# ops/ci/check-nation-fold-parity.sh — fail-closed guard that every copy of the
# ISO 3166-1 alpha-2 -> alpha-3 fold agrees, pair for pair.
#
# WHY THREE COPIES EXIST. Folding `US` onto `USA` is needed on both sides of the
# language boundary (ADR 0112) and in two separate Go modules:
#
#   ops/goat-id-console/internal/nation/nation.go        (console, canonical)
#   ops/goat-zenoh-portal/internal/access/coalition.go   (portal, own Go module)
#   ops/operators/gen-roster.py                          (roster projection)
#
# A shared dependency for thirty-odd constants is not worth the coupling between
# two Go modules and a script the operator laptop runs with no venv, so the
# duplication is deliberate — and this gate is the price of it.
#
# WHAT DRIFT COSTS. Every consumer decides something on a nation comparison:
# whether an org is the goatnet's own or foreign (derive_kind), whether a
# suggested org matches the applicant's nationality, whether an alliance
# framework claims the country. A pair present in one copy and missing from
# another answers those questions differently in different surfaces for the
# same org, which is how `usareur-af` came to be US in one registry and USA in
# another for the same country.
#
# Full-tree (not diff-scoped): asserts a property that passes today.
set -euo pipefail

GO_CONSOLE="${1:-ops/goat-id-console/internal/nation/nation.go}"
GO_PORTAL="${2:-ops/goat-zenoh-portal/internal/access/coalition.go}"
PY_ROSTER="${3:-ops/operators/gen-roster.py}"

fail=0
for f in "$GO_CONSOLE" "$GO_PORTAL" "$PY_ROSTER"; do
  if [[ ! -f "$f" ]]; then
    echo "check-nation-fold-parity: missing $f" >&2
    exit 1
  fi
done

# Every copy writes its pairs as "XX": "XXX", so one extraction serves all three.
# sed prints only the map body, then grep -o pulls each pair out of the shared
# multi-pair lines.
extract() {
  local file="$1" start="$2"
  sed -n "/${start}/,/^[})]/p" "$file" \
    | grep -oE '"[A-Z]{2}": *"[A-Z]{3}"' \
    | tr -d ' ' \
    | sort -u
}

a=$(extract "$GO_CONSOLE" 'var alpha2To3 = map\[string\]string{')
b=$(extract "$GO_PORTAL"  'var alpha2To3 = map\[string\]string{')
c=$(extract "$PY_ROSTER"  '^_ALPHA2_TO_3 = {')

n=$(printf '%s\n' "$a" | grep -c . || true)
if [[ "$n" -lt 20 ]]; then
  echo "check-nation-fold-parity: extracted only $n pairs from $GO_CONSOLE —" >&2
  echo "  the map moved or changed shape; fix this gate before trusting it." >&2
  exit 1
fi

compare() {
  local label="$1" other="$2"
  if ! diff <(printf '%s\n' "$a") <(printf '%s\n' "$other") >/tmp/nation-fold-diff.$$ 2>&1; then
    echo "check-nation-fold-parity: $label differs from $GO_CONSOLE" >&2
    echo "  (< console, > $label)" >&2
    sed 's/^/  /' /tmp/nation-fold-diff.$$ >&2
    fail=1
  fi
  rm -f /tmp/nation-fold-diff.$$
}

compare "$GO_PORTAL" "$b"
compare "$PY_ROSTER" "$c"

if [[ "$fail" -ne 0 ]]; then
  echo >&2
  echo "Fix: make all three carry the same pairs. $GO_CONSOLE is canonical." >&2
  exit 1
fi

echo "check-nation-fold-parity: ok — $n pairs, identical in all three copies"
