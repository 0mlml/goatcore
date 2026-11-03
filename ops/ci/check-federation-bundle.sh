#!/usr/bin/env bash
# ops/ci/check-federation-bundle.sh — the partner conformance bundle is
# current, deterministic, and its verifier actually refuses.
#
# WHY: docs/design/federation-package-spec.md §10 makes the conformance vectors
# normative and §8.3 concedes that an external partner may not have them. The
# bundle in ops/federation/bundle/ closes that, and it introduces a staleness
# risk of its own: the specification or a vector changes, the bundle a partner
# was sent does not, and the two disagree. The partner discovers it as a digest
# mismatch against us — the one failure this format exists to make loud,
# arriving as a mystery.
#
# The brief was to make staleness DETECTABLE rather than documented. So the
# bundle's content-sha256 is committed at ops/federation/bundle/content.sha256
# and recomputed here. Change the specification or a vector without updating
# that file and this goes red, naming what to run.
#
# content-sha256 covers the bundle's FILE LIST and not its provenance header,
# so it does not churn on every unrelated commit — it moves when, and only
# when, what a partner would receive moves.
#
# Three checks, and the second and third are why this is trusted:
#
#   1. ratchet      the committed digest still describes the tree
#   2. determinism  two builds of the same tree are byte-identical
#   3. self-test    the verifier goes RED on a missing, altered, extra or
#                   tampered file — doctrine P6, a guard proves itself against
#                   a broken fixture before its verdict on the real tree means
#                   anything
#
# Usage:
#   ops/ci/check-federation-bundle.sh              # all three
#   ops/ci/check-federation-bundle.sh --self-test  # only the red-proofs
#   ops/ci/check-federation-bundle.sh --update     # rewrite content.sha256

set -euo pipefail

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
BUNDLE_DIR="$REPO_ROOT/ops/federation/bundle"
MAKE="$BUNDLE_DIR/make-bundle.sh"
RATCHET="$BUNDLE_DIR/content.sha256"

MODE=all
case "${1:-}" in
    --self-test) MODE=selftest ;;
    --update)    MODE=update ;;
    "")          ;;
    *) echo "check-federation-bundle: unknown argument: $1" >&2; exit 2 ;;
esac

[ -x "$MAKE" ] || { echo "check-federation-bundle: $MAKE not executable" >&2; exit 2; }

if command -v sha256sum >/dev/null 2>&1; then
    sha_file() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
    sha_file() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
    echo "check-federation-bundle: need sha256sum or shasum on PATH" >&2; exit 2
fi

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

fail=0
pass() { printf '  ok    %s\n' "$*"; }
bad()  { printf '  FAIL  %s\n' "$*"; fail=1; }

# ------------------------------------------------------ 1. the ratchet ----

ACTUAL=$(cd "$REPO_ROOT" && "$MAKE" --content-sha256)

if [ "$MODE" = update ]; then
    printf '%s\n' "$ACTUAL" > "$RATCHET"
    echo "check-federation-bundle: content.sha256 updated to $ACTUAL"
    exit 0
fi

if [ "$MODE" != selftest ]; then
    echo "Ratchet"
    if [ ! -f "$RATCHET" ]; then
        bad "$RATCHET is missing"
    else
        EXPECTED=$(tr -d ' \t\r\n' < "$RATCHET")
        if [ "$EXPECTED" = "$ACTUAL" ]; then
            pass "bundle content-sha256 $ACTUAL"
        else
            bad "the partner bundle is stale."
            cat >&2 <<EOF

    committed  $EXPECTED
    actual     $ACTUAL

  The specification or a conformance vector changed and the bundle a partner
  would be sent no longer matches it. Nothing is broken yet — this is the
  detection working. Rebuild and record it:

    ops/ci/check-federation-bundle.sh --update

  Then say in the commit message that partners holding the previous bundle
  need the new one, because they cannot tell from their side without asking.

EOF
        fi
    fi

    # ------------------------------------------------- 2. determinism ----
    echo "Determinism"
    ( cd "$REPO_ROOT" && "$MAKE" --out-dir "$WORK/d1" >/dev/null )
    ( cd "$REPO_ROOT" && "$MAKE" --out-dir "$WORK/d2" >/dev/null )
    a=$(sha_file "$(echo "$WORK"/d1/*.tar.gz)")
    b=$(sha_file "$(echo "$WORK"/d2/*.tar.gz)")
    if [ "$a" = "$b" ]; then
        pass "two builds are byte-identical ($a)"
    else
        bad "two builds of the same tree differ: $a vs $b"
    fi
fi

# ------------------------------------------- 3. the verifier goes red ----
#
# A verifier that only ever prints "intact" is indistinguishable from a
# verifier that does not look. Each case below breaks the bundle one way and
# asserts both a non-zero exit AND the phrase a partner would act on.

echo "Verifier self-test"

( cd "$REPO_ROOT" && "$MAKE" --out-dir "$WORK/src" >/dev/null )
TGZ=$(echo "$WORK"/src/*.tar.gz)

fresh() {
    # A clean extraction of the bundle at $WORK/case/<name>, path echoed.
    local name="$1"
    local dir="$WORK/case/$name"
    mkdir -p "$dir"
    tar -xzf "$TGZ" -C "$dir"
    echo "$dir"/goat-federation-v1-*
}

expect() {
    # expect <case> <want-exit> <phrase> -- <mutation shell>
    local name="$1" want="$2" phrase="$3"
    shift 3
    [ "${1:-}" = "--" ] && shift
    local dir out rc
    dir=$(fresh "$name")
    ( cd "$dir" && eval "$*" )
    set +e
    out=$( cd "$dir" && ./verify-bundle.sh 2>&1 )
    rc=$?
    set -e
    if [ "$rc" -ne "$want" ]; then
        bad "$name: verifier exited $rc, expected $want"
        return
    fi
    if ! printf '%s' "$out" | grep -qi -- "$phrase"; then
        bad "$name: exited $rc but never said '$phrase'"
        return
    fi
    pass "$name -> exit $rc, reported '$phrase'"
}

expect intact            0 "VERDICT: intact"                     -- "true"
expect missing-file      1 "1 file(s) missing"                   -- "rm vectors/compose-full/agreement.digest.txt"
expect altered-vector    1 "1 file(s) altered"                   -- "printf x >> vectors/compose-full/agreement.canonical.json"
expect altered-spec      1 "1 file(s) altered"                   -- "printf x >> federation-package-spec.md"
expect extra-file        1 "present but not in the manifest"     -- "printf x > vectors/compose-full/EXTRA.json"
expect tampered-manifest 1 "MANIFEST does not match"             -- "printf '#\n' >> MANIFEST"
expect no-manifest       1 "MANIFEST is missing"                 -- "rm MANIFEST"
expect wrong-now         1 "now.txt says"                        -- "printf '2020-01-01\n' > vectors/now.txt && \
    sed -i.bak 's|^\([0-9a-f]\{64\}\)\(  vectors/now.txt\)$|'\"\$(shasum -a 256 vectors/now.txt | cut -d' ' -f1)\"'\2|' MANIFEST && \
    rm -f MANIFEST.bak && shasum -a 256 MANIFEST | awk '{print \$1 \"  MANIFEST\"}' > MANIFEST.sha256"

echo ""
if [ "$fail" -ne 0 ]; then
    echo "check-federation-bundle: FAILED" >&2
    exit 1
fi
echo "check-federation-bundle: ok"
