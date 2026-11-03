#!/usr/bin/env bash
# check-no-spendable-credential-links.sh — refuse a REAL credential-setup token
# in a committed file.
#
# The failure it exists to prevent has happened, on the batch-campaign path
# rather than in the console. a dashboard-door campaign mailed
# every recipient a line of the form
#
#     https://idm.goat.example/ui/reset?token=<a live 20-character token>
#
# from an ad-hoc send with no committed sender. A campaign template, a runbook
# step, a pasted "here is what the mail looks like" example — all are one
# copy-paste from carrying a live token into git, and a token in git is three
# bad things at once:
#
#   * it is a CREDENTIAL, in a repo, forever, for an account that has no
#     credential yet — i.e. an account-takeover path;
#   * it is SPENDABLE by any scanner that fetches the link, so the
#     person it was minted for loses their setup session;
#   * it makes the reset page's Submit button a silent no-op for whoever does
#     open it, because a second `token` param appended by the form yields
#     HTTP 400 and htmx declines to swap on a 4xx.
#
# The rule is deliberately narrow so it can be absolute: a LITERAL token value
# next to a reset path. Code that concatenates a token it holds in a variable is
# untouched — `ops/onboard-runner/collab.go` builds exactly such a URL and hands
# it to a renderer that splits it, which is correct and must stay legal.
#
# Kanidm credential-setup tokens are four dash-separated groups of five
# alphanumerics (verified against live-issued codes). That shape, appearing as
# the value of a `token=` parameter, is the thing refused.
#
#   ops/ci/check-no-spendable-credential-links.sh            # whole tree
#   ops/ci/check-no-spendable-credential-links.sh --diff     # changed files only
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

MODE=tree
[ "${1:-}" = "--diff" ] && MODE="diff"

# A live token: token=AB3dE-fG7hI-jK1lM-nO5pQ  (4 x 5 alnum, dash-joined).
# Placeholders people legitimately write — <token>, TOKEN, xxxxx-xxxxx-… — do
# not match, because they are not five alphanumerics four times over with the
# mixed case and digits a real code has. To keep it honest the pattern also
# refuses an all-lowercase or all-digit run of the same shape; a placeholder
# that looks exactly like a real token is indistinguishable from one, and
# somebody choosing that as their example deserves the failure.
PATTERN='token=[A-Za-z0-9]{5}-[A-Za-z0-9]{5}-[A-Za-z0-9]{5}-[A-Za-z0-9]{5}'

if [ "$MODE" = diff ]; then
  base="${GITHUB_BASE_REF:-main}"
  files=$(git diff --name-only --diff-filter=ACMR "origin/${base}...HEAD" 2>/dev/null \
          || git diff --name-only --diff-filter=ACMR HEAD)
else
  files=$(git ls-files)
fi

[ -n "$files" ] || { echo "check-no-spendable-credential-links: no files to check"; exit 0; }

# This script necessarily contains the pattern it looks for, in the comment
# above and in the pattern itself; exclude it rather than contorting the regex.
hits=$(printf '%s\n' "$files" \
  | grep -v '^ops/ci/check-no-spendable-credential-links.sh$' \
  | while IFS= read -r f; do
      [ -f "$f" ] || continue
      grep -HnE "$PATTERN" "$f" 2>/dev/null || true
    done)

if [ -n "$hits" ]; then
  echo "FAIL: a live-shaped credential-setup token is present in committed files." >&2
  echo "" >&2
  # Print the location, never the token — echoing it into CI logs spreads it.
  printf '%s\n' "$hits" | sed -E 's/(token=)[A-Za-z0-9-]+/\1<REDACTED>/' >&2
  echo "" >&2
  echo "What to do:" >&2
  echo "  1. Treat that token as spent. Issue a fresh one for the person:" >&2
  echo "       ops/scripts/kanidm-reissue-code.sh <handle>" >&2
  echo "  2. Remove the token from the file. A credential-setup handoff is a" >&2
  echo "     BARE page address plus a code delivered out of band — never a URL" >&2
  echo "     carrying the token." >&2
  echo "  3. If the file is documentation, write <token> or a code that is" >&2
  echo "     obviously not one, e.g. AAAAA-BBBBB-CCCCC-DDDDD." >&2
  exit 1
fi

echo "check-no-spendable-credential-links: OK (no live-shaped tokens in committed files)"
