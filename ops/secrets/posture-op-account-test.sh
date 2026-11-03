#!/usr/bin/env bash
# posture-op-account-test.sh — the OP_ACCOUNT pin and its fail-closed guard.
#
# With OP_ACCOUNT unset and more than one 1Password account configured, `op`
# fans out a prompt across every account (ADR 1186). with-secrets and
# ops/secrets/posture.py must pin the account — from OP_ACCOUNT, else the
# declared `secrets: op_account:` — and REFUSE rather than let op fan out.
#
# No real `op` is run: a shim on PATH stands in for `op account list` and
# `op run`, so the test is deterministic and needs no 1Password session.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
POSTURE="$REPO/ops/secrets/posture.py"
WITH_SECRETS="$REPO/ops/scripts/with-secrets"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }

# A fake `op`: `op account list --format=json` prints $FAKE_OP_ACCOUNTS; any other
# invocation is `op run -- CMD...`, which just execs CMD so the wrapped command runs.
mkdir -p "$WORK/bin"
cat >"$WORK/bin/op" <<'SHIM'
#!/usr/bin/env bash
if [ "${1:-}" = "account" ] && [ "${2:-}" = "list" ]; then
  accts="$FAKE_OP_ACCOUNTS"; [ -n "$accts" ] || accts="[]"
  printf '%s' "$accts"
  exit 0
fi
# op run [--env-file=..] -- CMD...
shift || true                 # drop 'run'
while [ "${1:-}" != "--" ] && [ $# -gt 0 ]; do shift; done
[ "${1:-}" = "--" ] && shift
exec "$@"
SHIM
chmod +x "$WORK/bin/op"
PATH="$WORK/bin:$PATH"; export PATH

# A goatbuilder.yaml fixture that declares the 1Password store with NO op_account.
cat >"$WORK/no-account.yaml" <<'YAML'
schema: goatbuilder/v1
secrets:
  store: 1password
YAML
# ... and one that declares the account.
cat >"$WORK/with-account.yaml" <<'YAML'
schema: goatbuilder/v1
secrets:
  store: 1password
  op_account: example.1password.com
YAML

TWO='[{"shorthand":"personal","url":"my.1password.com"},{"shorthand":"team","url":"example.1password.com"}]'
ONE='[{"shorthand":"team","url":"example.1password.com"}]'

# 1. posture.py op-account prints the declared account when OP_ACCOUNT is unset.
out="$(env -u OP_ACCOUNT GOAT_BUILDER_CONFIG="$WORK/with-account.yaml" python3 "$POSTURE" op-account)"
[ "$out" = "example.1password.com" ] || fail "declared account not resolved (got '$out')"

# 2. OP_ACCOUNT in the environment overrides the declared account.
out="$(OP_ACCOUNT=override.1password.com GOAT_BUILDER_CONFIG="$WORK/with-account.yaml" python3 "$POSTURE" op-account)"
[ "$out" = "override.1password.com" ] || fail "OP_ACCOUNT did not override (got '$out')"

# 3. No declared account + unset OP_ACCOUNT + >1 account: posture.py REFUSES (exit 3).
set +e
err="$(env -u OP_ACCOUNT GOAT_BUILDER_CONFIG="$WORK/no-account.yaml" FAKE_OP_ACCOUNTS="$TWO" python3 "$POSTURE" op-account 2>&1 >/dev/null)"
rc=$?
set -e
[ "$rc" = 3 ] || fail "op-account should exit 3 on ambiguity (got $rc)"
echo "$err" | grep -qi "OP_ACCOUNT is unset" || fail "refusal message missing the ask: $err"

# 4. No declared account + unset OP_ACCOUNT + exactly 1 account: not ambiguous, prints nothing, exits 0.
set +e
out="$(env -u OP_ACCOUNT GOAT_BUILDER_CONFIG="$WORK/no-account.yaml" FAKE_OP_ACCOUNTS="$ONE" python3 "$POSTURE" op-account)"
rc=$?
set -e
[ "$rc" = 0 ] || fail "op-account should exit 0 with a single account (got $rc)"
[ -z "$out" ] || fail "op-account should print nothing with a single undeclared account (got '$out')"

# ---- with-secrets end to end (op:// reference in the environment -> the op path) ----

# 5. >1 account, nothing pinned: with-secrets REFUSES, nonzero, and never runs the command.
set +e
out="$(env -u OP_ACCOUNT -u GOAT_SECRETS_RESOLVED GOAT_SECRET_STORE=1password \
        GOAT_BUILDER_CONFIG="$WORK/no-account.yaml" FAKE_OP_ACCOUNTS="$TWO" \
        TEST_REF='op://v/i/f' "$WITH_SECRETS" sh -c 'echo COMMAND_RAN' 2>&1)"
rc=$?
set -e
[ "$rc" != 0 ] || fail "with-secrets should refuse when op would fan out (rc=$rc)"
echo "$out" | grep -q "COMMAND_RAN" && fail "with-secrets ran the command despite the fan-out risk"

# 6. Declared account: with-secrets pins OP_ACCOUNT and the command runs with it set.
out="$(env -u OP_ACCOUNT -u GOAT_SECRETS_RESOLVED GOAT_SECRET_STORE=1password \
        GOAT_BUILDER_CONFIG="$WORK/with-account.yaml" FAKE_OP_ACCOUNTS="$TWO" \
        TEST_REF='op://v/i/f' "$WITH_SECRETS" sh -c 'echo PINNED=$OP_ACCOUNT')"
[ "$out" = "PINNED=example.1password.com" ] || fail "with-secrets did not pin the declared account (got '$out')"

# 7. OP_ACCOUNT already set: with-secrets keeps it (no fan-out check needed), command runs.
out="$(env -u GOAT_SECRETS_RESOLVED GOAT_SECRET_STORE=1password OP_ACCOUNT=preset.1password.com \
        GOAT_BUILDER_CONFIG="$WORK/no-account.yaml" FAKE_OP_ACCOUNTS="$TWO" \
        TEST_REF='op://v/i/f' "$WITH_SECRETS" sh -c 'echo PINNED=$OP_ACCOUNT')"
[ "$out" = "PINNED=preset.1password.com" ] || fail "with-secrets did not honor a preset OP_ACCOUNT (got '$out')"

# 8. A value store (env-file) never gates on OP_ACCOUNT, even with >1 account and none pinned.
printf 'TEST_VAL=plain\n' >"$WORK/secrets.env"; chmod 600 "$WORK/secrets.env"
out="$(env -u OP_ACCOUNT -u GOAT_SECRETS_RESOLVED GOAT_SECRET_STORE=env-file \
        GOAT_SECRET_ENV_FILE="$WORK/secrets.env" FAKE_OP_ACCOUNTS="$TWO" \
        "$WITH_SECRETS" sh -c 'echo VAL=$TEST_VAL')"
[ "$out" = "VAL=plain" ] || fail "env-file store should not gate on OP_ACCOUNT (got '$out')"

echo "ok: posture-op-account-test.sh — all 8 cases pass"
