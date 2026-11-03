#!/usr/bin/env bash
# ops/scripts/parity-report.sh — the operator-facing parity entry point.
#
# RE-POINTED AT THE LIVE JOIN by CF-3 (docs/design/consistency-foundation.md
# §5.4). Until then this was a thin wrapper that rendered a committed markdown
# file, ops/pins/parity-report.md, whose own closing section said the honest
# thing on every render: "Live host state — everything. No host is contacted."
# That file is deleted; this script now reaches the question it could not.
#
#   ./ops/scripts/parity-report.sh --env site-a   # the LIVE join (default mode)
#     (--env names the GOATNET; it maps to goat-cli's global --context)
#   ./ops/scripts/parity-report.sh --stage1     # intent only, no host contacted
#   ./ops/scripts/parity-report.sh --help
#
# THE TWO HALVES, and why the split is visible rather than hidden:
#
#   stage 1 (intent)   what a goatnet is SUPPOSED to run. Repo-side, always
#                      available, contacts nothing. Rendered from the same
#                      resolver the committed intent.json comes from.
#
#   the live join      what each node IS running, and which generation of that
#                      intent it converged against. This needs the fabric: the
#                      facts live on the nodes and ride
#                      ops/consistency/report/<host_id>, rolled onto
#                      ops/consistency/health/<site>.
#
# When the live half cannot run, this script SAYS SO and names the command. It
# does NOT quietly fall back to the repo-side render — that fallback is how
# "the repo says X" gets read as "the fleet is X", which is the confusion the
# deleted file spent a whole closing section warning about.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
REPORT="${REPO_ROOT}/ops/pins/report.py"

usage() {
  sed -n '2,29p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
}

stage1() {
  echo "== STAGE 1 — INTENT (the committed tree; NO host is contacted) =="
  echo
  exec python3 "$REPORT" --stdout
}

live() {
  local env_name="${1:-}"
  if [[ -z "$env_name" ]]; then
    echo "parity-report: --env <goatnet> is required for the live join (e.g. --env zagros)." >&2
    echo "               For the repo-side half alone: $0 --stage1" >&2
    exit 2
  fi

  if ! command -v goat >/dev/null 2>&1; then
    cat >&2 <<EOF
parity-report: the live join needs \`goat\` on PATH, and it is not there.

  The live half reads the rolled consistency envelope off the fabric:

      goat health --domain consistency --context ${env_name}

  Install goat (docs/design/goat-cli-mesh-download.md) and re-run, or read the
  repo-side half alone with:

      $0 --stage1

  NOT falling back to the repo-side render on its own: "the repo says X" is a
  different claim from "the fleet is X", and printing one while you asked for
  the other is how those two get confused.
EOF
    exit 3
  fi

  echo "== LIVE JOIN — intent x node records, goatnet ${env_name} =="
  echo
  # `goat health` is the reader; this script does not re-implement the read and,
  # above all, does not re-implement the GRADING. A projection may choose what it
  # can see and how it renders; it may not grade (§4.1). One command, one
  # comparison, one set of remediation pointers.
  if ! goat health --domain consistency --context "$env_name"; then
    cat >&2 <<EOF

parity-report: no consistency reading came back for ${env_name}.

  That is itself a reading — absence of signal is \`unmetered\`, never green
  (ADR 1036 §6). The usual causes, in the order worth checking:

    1. goat-consistency-health is not deployed on this goatnet yet. CF-3 is
       complete repo-side; deploying it is a separate, gated step.
    2. The rollup is running but no node has published within its window.
    3. The router ACL does not permit ops/consistency/** — and a denied write is
       SILENT, so check with a live subscriber, never a storage read:

           goat sub --context ${env_name} 'ops/consistency/report/**' --take 2
EOF
    exit 4
  fi
}

case "${1:-}" in
  -h|--help)   usage ;;
  --stage1)    stage1 ;;
  --env)       live "${2:-}" ;;
  --write|--check)
    cat >&2 <<EOF
parity-report: \`$1\` is gone. CF-3 deleted the committed render it operated on
(ops/pins/parity-report.md) and moved its --check gate onto the artifact that
replaced it (docs/design/consistency-foundation.md §5.4).

  regenerate the artifact:  python3 ops/pins/generate-pins.py
  gate it as CI does:       python3 ops/pins/generate-pins.py --check
  read the intent matrix:   $0 --stage1
  read the live join:       $0 --env <goatnet>
EOF
    exit 2 ;;
  "")          live "${GOAT_ENV:-}" ;;
  *)
    echo "parity-report: unknown argument \`$1\`." >&2
    usage >&2
    exit 2 ;;
esac
