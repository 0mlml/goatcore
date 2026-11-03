#!/usr/bin/env bash
# Full 2-node integration test for ory-narrow-repset.
#
# STATUS: STUB. To be authored as a follow-on, mirroring the shape of
# ops/ansible/roles/spock-mesh-peering/tests/run-test.sh.
#
# See tests/README.md §"Full integration test (queued)" for the
# behavioral coverage this script will provide when authored.
#
# The parse-only smoke (syntax-check.sh) is what runs in CI today.

set -euo pipefail

cat <<'EOF'
[run-test] STUB — full 2-node integration test queued.

This script will be authored to mirror
ops/ansible/roles/spock-mesh-peering/tests/run-test.sh, extended with:

  - kratos + hydra container fixtures (with migrations pre-run)
  - inclusion-list membership verification (10 tables)
  - exclusion-list non-membership verification (~25 tables)
  - identity-row replication probe
  - flow-row site-locality probe
  - auth-code cross-site non-consumability probe
  - idempotent re-run check

For now: the parse-only smoke (syntax-check.sh) is what CI runs.

Manual exercise of the role against a real fixture lives at the
Sub-block D operator-fired pilot activation step.
EOF

exit 0
