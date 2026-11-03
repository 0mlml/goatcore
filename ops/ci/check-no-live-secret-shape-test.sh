#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# check-no-live-secret-shape-test.sh — does the name-vs-credential filter in
# check-no-live-secret.sh let a real secret through?
#
# That filter is the only thing standing between "the guard is usable" and "the
# guard is unsound". It exists because a live S3 access-key id whose value is a
# hyphenated service name refused ordinary prose edits in two sessions on
# training --no-verify. Skipping too much would be the worse bug, so
# the shapes every generator in this repo produces are pinned here.
#
#   ops/ci/check-no-live-secret-shape-test.sh
#
# Exit: 0 all cases hold · 1 a case failed.

set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")" || exit 1

# Source only the predicate and the constant it reads, not the whole guard
# (which would scan the tree). Both, or the function fails on an unbound
# variable under `set -u` — which is how this test first ran.
eval "$(sed -n '/^MAX_NAME_SEGMENT=/p;/^is_name_shaped()/,/^}/p' check-no-live-secret.sh)"

fail=0
# want=name  -> the value is a NAME and must be skipped (unprotectable by grep)
# want=cred  -> the value is a CREDENTIAL and must stay protected
check() { # check <want> <value> <why>
  local want=$1 val=$2 why=$3 got
  if is_name_shaped "$val"; then got=name; else got=cred; fi
  if [[ "$got" != "$want" ]]; then
    printf 'FAIL  want=%-4s got=%-4s  %s\n' "$want" "$got" "$why" >&2
    fail=1
  else
    printf 'ok    %-4s  %s\n' "$got" "$why"
  fi
}

# --- must stay PROTECTED: everything a generator in this repo emits ----------
#
# These fixtures are deliberately NOT shaped like any real provider's token.
# An earlier draft used a `xoxb-…` string to test "uppercase means credential";
# it is fake, but it matches Slack's published pattern, so GitHub push
# protection refused the branch. A test fixture that trips a scanner is a
# fixture that will be worked around — the same failure this whole change is
# about. The shape being tested is what matters, not the prefix.
check cred "$(printf 'a%.0s' {1..43})Zk9+/xQ=" "base64 — mixed case and +/= "
check cred "TmV2ZXJHb25uYUdpdmVZb3VVcAo=" "base64 of random bytes"
check cred "3f9a2b1c8d7e6f5a4b3c2d1e0f9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a" "64-char lowercase hex — no separator"
check cred "550e8400-e29b-41d4-a716-446655440000" "UUID — hyphens but every segment hex-only"
check cred "AGE-SECRET-KEY-1QYQSZQGPQYQSZQGPQYQSZQGPQYQSZQGPQ" "age secret key — uppercase"
check cred "prefix_16CharsOrMoreTokenValue0123456789" "underscore separator but mixed case"
check cred "aaaa-1234567890-ABCDefghIJKLmnop" "hyphens, but uppercase present"
check cred "9f8e7d6c5b4a39281706f5e4d3c2b1a0" "32-char hex, no separator"
check cred "deadbeef-cafe-babe-feed-face0123abcd" "hyphen-grouped hex — still hex-only segments"

check cred "snitch_$(printf 'q%.0s' {1..64})" "human prefix + 64-char generated segment — the SNITCH_*_KEY shape"
check cred "prefix_abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwx" "long lowercase segment is generated, not a word"
check name "an-bb-ccc-dddd-zzzzz" "many short segments is still a name (needs a non-hex letter)"

# --- must be SKIPPED: values that are names and appear in prose --------------
check name "zagros-auditreader" "the shape that caused this: hyphenated lowercase service name"
check name "goat-portal-audit-user" "multi-segment lowercase service account name"
check name "zagros_audit_subscriber" "underscore separator, same class"
check name "zenoh-router-health-probe" "a job name that appears in rule files and prose"

# --- boundary cases ----------------------------------------------------------
check cred "abcdef-abcdef-abcdef" "all segments hex-only, so NOT treated as a name"
check name "abcdefg-abcdef" "one non-hex letter (g) is enough to make it a word"
check cred "nohyphenslowercaseword" "no separator — stays protected even though it is a word"

if (( fail )); then
  echo >&2
  echo "The name-vs-credential filter changed behaviour. If a 'cred' case now reads" >&2
  echo "as 'name', the guard has stopped protecting that shape — that is a security" >&2
  echo "regression, not a test to update." >&2
  exit 1
fi

# --- whole-script runs under the operator's shell ---------------------------
#
# The predicate cases above run under whatever bash invoked this file. The
# pre-commit hook on macOS runs under /bin/bash 3.2, which treats "${arr[@]}"
# on an EMPTY array as an unbound variable under `set -u`. that
# made a message-only `git commit --amend` print "files[@]: unbound variable"
# and then report ok for a scan that never ran (GH #967). CI is bash 5 and
# cannot see it, so these cases pin /bin/bash explicitly where it exists.
#
# The environment is built from scratch (`env -i`) so the ONLY secret-bearing
# variables are the two fixtures below; the operator's real .envrc.local.d
# values never enter the child, and a laptop with none loaded still runs the
# case. The second fixture is a NAME-shaped value: the guard must skip it and
# say so in its NOTE on every exit-0 path, the empty-set one included.
GUARD="$PWD/check-no-live-secret.sh"
OLD_BASH=/bin/bash; [[ -x "$OLD_BASH" ]] || OLD_BASH=$(command -v bash)
FIXTURE_TOKEN="TmV2ZXJHb25uYUdpdmVZb3VVcAo="   # base64 shape, same as the cred case above
FIXTURE_WEAK="zagros-auditreader"                # the name shape from the cases above

# GUARD_PATH selects the child's PATH. The default is the caller's; the
# no-python cases below swap in a directory holding everything the guard's
# shasum fallback needs EXCEPT python3, so that path is exercised rather than
# assumed. Symlinks, not copies: the tools are whatever this host has.
GUARD_PATH=$PATH
run_guard() { # run_guard <repo-dir> <args...>  → stdout+stderr on stdout, rc in $?
  local repo=$1; shift
  ( cd "$repo" && env -i PATH="$GUARD_PATH" HOME="$repo" TMPDIR="${TMPDIR:-/tmp}" \
      GIT_CONFIG_NOSYSTEM=1 SHAPETEST_FIXTURE_SECRET_KEY="$FIXTURE_TOKEN" \
      SHAPETEST_FIXTURE_WEAK_KEY="$FIXTURE_WEAK" \
      "$OLD_BASH" "$GUARD" "$@" 2>&1 )
}

e2e_repo=$(mktemp -d "${TMPDIR:-/tmp}/.live-secret-e2e.XXXXXX")
trap 'rm -rf "$e2e_repo"' EXIT
git -C "$e2e_repo" init -q
git -C "$e2e_repo" -c user.name=shape-test -c user.email=shape-test@example.invalid \
  commit -q --allow-empty -m init

echo
echo "whole-script cases under $("$OLD_BASH" -c 'echo "bash $BASH_VERSION"')"

# 1. empty staged set: quiet, exit 0, says the set was empty, no bash error,
#    and the weak-credential NOTE still names the name-shaped variable.
out=$(run_guard "$e2e_repo" --staged); rc=$?
if [[ $rc -eq 0 && "$out" != *"unbound variable"* && "$out" == *"file set is empty"* \
      && "$out" == *"SHAPETEST_FIXTURE_WEAK_KEY"* ]]; then
  echo "ok    e2e   empty staged set is reported as empty, exit 0, no shell error, weak NOTE kept"
else
  printf 'FAIL  e2e   empty staged set: rc=%s\n%s\n' "$rc" "$out" >&2; fail=1
fi

# 2. named-path mode with an empty set behaves the same (git ls-files in a
#    repo with no tracked files).
out=$(run_guard "$e2e_repo"); rc=$?
if [[ $rc -eq 0 && "$out" != *"unbound variable"* && "$out" == *"file set is empty"* ]]; then
  echo "ok    e2e   empty tree scan is reported as empty, exit 0, no shell error"
else
  printf 'FAIL  e2e   empty tree scan: rc=%s\n%s\n' "$rc" "$out" >&2; fail=1
fi

# 3. the guard still refuses a staged file carrying the live value — the fix
#    must not have turned the empty-set shortcut into a general bypass.
printf 'key = %s\n' "$FIXTURE_TOKEN" > "$e2e_repo/leak.txt"
git -C "$e2e_repo" add leak.txt
out=$(run_guard "$e2e_repo" --staged); rc=$?
if [[ $rc -eq 1 && "$out" == *"REFUSED"* && "$out" == *"SHAPETEST_FIXTURE_SECRET_KEY"* && "$out" != *"$FIXTURE_TOKEN"* ]]; then
  echo "ok    e2e   staged live value is still refused, named by variable, value not echoed"
else
  printf 'FAIL  e2e   staged live value: rc=%s\n%s\n' "$rc" "$out" >&2; fail=1
fi

# 4./5. the same two outcomes with NO python3 on PATH, so the shasum fallback
#    is what runs. That path is the one that used to abort the whole hook.
nopy_bin="$e2e_repo/.nopy-bin"; mkdir -p "$nopy_bin"
for tool in git shasum cut grep sort head cat sed mktemp chmod rm; do
  tool_path=$(command -v "$tool") && ln -s "$tool_path" "$nopy_bin/$tool"
done
GUARD_PATH=$nopy_bin
if [[ -n "$(PATH=$nopy_bin command -v python3 2>/dev/null)" ]]; then
  echo "FAIL  e2e   no-python PATH still resolves python3; the fallback cases are void" >&2; fail=1
fi

out=$(run_guard "$e2e_repo" --staged); rc=$?   # leak.txt is still staged
if [[ $rc -eq 1 && "$out" == *"REFUSED"* && "$out" == *"SHAPETEST_FIXTURE_SECRET_KEY"* \
      && "$out" != *"$FIXTURE_TOKEN"* && "$out" != *"command not found"* ]]; then
  echo "ok    e2e   no python3: staged live value is still refused, value not echoed"
else
  printf 'FAIL  e2e   no python3, staged live value: rc=%s\n%s\n' "$rc" "$out" >&2; fail=1
fi

git -C "$e2e_repo" reset -q
out=$(run_guard "$e2e_repo" --staged); rc=$?
if [[ $rc -eq 0 && "$out" != *"unbound variable"* && "$out" != *"command not found"* \
      && "$out" == *"file set is empty"* && "$out" == *"SHAPETEST_FIXTURE_WEAK_KEY"* ]]; then
  echo "ok    e2e   no python3: empty staged set exits 0 with no shell error, weak NOTE kept"
else
  printf 'FAIL  e2e   no python3, empty staged set: rc=%s\n%s\n' "$rc" "$out" >&2; fail=1
fi
GUARD_PATH=$PATH

if (( fail )); then
  echo >&2
  echo "The whole-script cases failed. Read the captured output above: a bash" >&2
  echo "'unbound variable' line means an empty-array expansion crept back in." >&2
  exit 1
fi
echo
echo "ok: the filter skips names and protects every credential shape this repo generates"
