#!/usr/bin/env bash
# ops/ci/check-onboard-schema-sync.sh — fail-closed guard that the onboarding
# engine's embedded copy of the operator-manifest JSON Schema is byte-identical
# to the canonical one.
#
# WHY: the onboarding linter-as-a-service (internal/onboardlint, ADR 1061)
# validates manifests in Go because the runtime is distroless (no Python to run
# validate.py). To stay a SINGLE source of truth, it embeds the SAME schema file
# the canonical `validate.py` gate uses — it does NOT fork the rules. The schema
# lives outside the engine's Go module, so it is copied into the package for
# go:embed. This guard asserts that copy never drifts from the canonical SoT; a
# drift would silently let the form accept manifests the onboard gate rejects
# (the exact failure ADR 1061 removes).
#
# The onboarding engine is goat-id-console (docs/design/onboarding-extraction-plan.md);
# the portal shed its onboardlint copy in slice 5. Pairs with the console parity
# test (ops/goat-id-console/internal/onboardlint/onboardlint_parity_test.go),
# which asserts the Go linter and `validate.py --json` return identical verdicts.
#
# Full-tree (not diff-scoped): asserts a property that passes today, so there
# are no grandfathered violations.
set -euo pipefail

CANONICAL="${1:-ops/operators/schema/operator-manifest.schema.json}"
EMBEDDED="${2:-ops/goat-id-console/internal/onboardlint/operator-manifest.schema.json}"

for f in "$CANONICAL" "$EMBEDDED"; do
  if [[ ! -f "$f" ]]; then
    echo "FAIL: missing schema file: $f" >&2
    exit 1
  fi
done

if ! cmp -s "$CANONICAL" "$EMBEDDED"; then
  echo "FAIL: the onboarding engine's embedded schema has drifted from the canonical SoT." >&2
  echo "  canonical: $CANONICAL" >&2
  echo "  embedded:  $EMBEDDED" >&2
  echo "  diff:" >&2
  diff "$CANONICAL" "$EMBEDDED" | sed 's/^/    /' >&2 || true
  echo >&2
  echo "  Re-sync with:" >&2
  echo "    cp $CANONICAL $EMBEDDED" >&2
  exit 1
fi

echo "OK: onboarding engine's embedded schema matches canonical SoT ($CANONICAL)."
