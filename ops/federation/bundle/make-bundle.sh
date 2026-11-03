#!/usr/bin/env bash
#
# make-bundle.sh — pack the federation specification and its conformance
# vectors into one archive a partner can be handed.
#
# WHY: docs/design/federation-package-spec.md §10 makes the vectors normative,
# and §8.3 concedes the problem in the same breath — "an external partner may
# not have them", because they are 210 files in a repository nobody outside can
# reach. Handing them over was a human remembering to attach a directory: no
# manifest, no integrity check, and nothing binding the vectors to the
# specification revision they were derived from. A partner given a stale or
# short bundle finds out as a digest mismatch against us, which is the one
# failure this format exists to make loud, arriving instead as a mystery.
#
#   ./make-bundle.sh                          # into ./dist
#   ./make-bundle.sh --out-dir /tmp/send      # somewhere else
#   ./make-bundle.sh --with-doc path/to.md    # add a document (see below)
#   ./make-bundle.sh --content-sha256         # print the identity, build nothing
#
# DETERMINISTIC. The same inputs produce byte-identical output: fixed ordering,
# no timestamps, no build host, no build account, normalised modes. Two people
# building from one commit can compare digests, and that is the point — an
# archive whose bytes depend on who packed it cannot be used to check anything.
#
# The archive is named for its own content: goat-federation-v1-<12 hex>.tar.gz,
# where the hex is the first 12 of content-sha256. Two bundles with the same
# name hold the same files, and a stale one is visibly a different name rather
# than an identical name over different bytes.
#
# NOT SIGNED, deliberately. The argument is in README.md next to this script;
# the short form is that a signature would need a third value distributed over
# the §5.1 authenticated channel to solve a problem the manifest digest already
# solves with a value of the same shape, and that the bundle is not on the
# trust path at all.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "$SCRIPT_DIR/../../.." && pwd)

SPEC_SRC="$REPO_ROOT/docs/design/federation-package-spec.md"
VECTORS_SRC="$REPO_ROOT/ops/federation/vectors"
BUNDLE_README_SRC="$SCRIPT_DIR/bundle-README.md"
VERIFIER_SRC="$SCRIPT_DIR/verify-bundle.sh"
WRITE_TAR="$SCRIPT_DIR/write-tar.py"
FIXTURE_EXPIRY="$SCRIPT_DIR/fixture-expiry.py"

OUT_DIR="$SCRIPT_DIR/dist"
EXTRA_DOCS=()
MODE=build

usage() {
    sed -n '2,45p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
}

while [ $# -gt 0 ]; do
    case "$1" in
        --out-dir)        OUT_DIR="$2"; shift 2 ;;
        --with-doc)       EXTRA_DOCS+=("$2"); shift 2 ;;
        --content-sha256) MODE=content; shift ;;
        -h|--help)        usage; exit 0 ;;
        *) echo "make-bundle: unknown argument: $1" >&2; exit 2 ;;
    esac
done

for f in "$SPEC_SRC" "$BUNDLE_README_SRC" "$VERIFIER_SRC" "$WRITE_TAR" "$FIXTURE_EXPIRY"; do
    [ -f "$f" ] || { echo "make-bundle: missing input: $f" >&2; exit 2; }
done
[ -d "$VECTORS_SRC" ] || { echo "make-bundle: missing vectors: $VECTORS_SRC" >&2; exit 2; }

if command -v sha256sum >/dev/null 2>&1; then
    sha_file() { sha256sum "$1" | cut -d' ' -f1; }
    sha_stdin() { sha256sum | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
    sha_file() { shasum -a 256 "$1" | cut -d' ' -f1; }
    sha_stdin() { shasum -a 256 | cut -d' ' -f1; }
else
    echo "make-bundle: need sha256sum or shasum on PATH" >&2; exit 2
fi

# ------------------------------------------------------------- staging ----

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
STAGE="$WORK/stage"
mkdir -p "$STAGE"

cp "$SPEC_SRC"          "$STAGE/federation-package-spec.md"
cp "$BUNDLE_README_SRC" "$STAGE/README.md"
cp "$VERIFIER_SRC"      "$STAGE/verify-bundle.sh"
cp -R "$VECTORS_SRC"    "$STAGE/vectors"

for doc in ${EXTRA_DOCS+"${EXTRA_DOCS[@]}"}; do
    [ -f "$doc" ] || { echo "make-bundle: --with-doc not a file: $doc" >&2; exit 2; }
    cp "$doc" "$STAGE/$(basename "$doc")"
done

# Nothing that is not a regular file belongs in a bundle a partner extracts.
find "$STAGE" \( -type l -o -type p -o -type s \) -print -quit | grep -q . && {
    echo "make-bundle: non-regular file staged, refusing" >&2; exit 2
}

# Normalise modes so the archive does not carry the umask of whoever ran this.
find "$STAGE" -type f -exec chmod 644 {} +
chmod 755 "$STAGE/verify-bundle.sh"

# ------------------------------------------- the facts that go in the header ----

SPEC_VERSION=$(grep -o 'federation-package/v[0-9]\+' "$SPEC_SRC" | head -1)
AGREEMENT_VERSION=$(grep -o 'federation-agreement/v[0-9]\+' "$SPEC_SRC" | head -1)
[ -n "$SPEC_VERSION" ] || { echo "make-bundle: no spec version found in $SPEC_SRC" >&2; exit 2; }

EVAL_DATE=$(tr -d ' \t\r\n' < "$STAGE/vectors/now.txt")
case "$EVAL_DATE" in
    [0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]) ;;
    *) echo "make-bundle: vectors/now.txt is not YYYY-MM-DD: '$EVAL_DATE'" >&2; exit 2 ;;
esac

SOURCE_COMMIT=$(git -C "$REPO_ROOT" rev-parse HEAD 2>/dev/null || echo unknown)
if [ -n "$(git -C "$REPO_ROOT" status --porcelain 2>/dev/null || true)" ]; then
    SOURCE_TREE=dirty
else
    SOURCE_TREE=clean
fi

SPEC_SHA=$(sha_file "$STAGE/federation-package-spec.md")

# Per-fixture earliest `expires` and the rule each fixture pins, so the
# verifier can name exactly which fixtures change verdict when a partner uses
# today's date instead of now.txt. String comparison on YYYY-MM-DD is the whole
# of the date arithmetic, which is why the table is computed here and merely
# read there. See fixture-expiry.py for why the §6 step is part of it.
python3 "$FIXTURE_EXPIRY" "$STAGE/vectors" > "$WORK/expiry"

# ------------------------------------------------------- the file list ----
#
# LC_ALL=C so the order is byte-wise on the path — the same ordering
# write-tar.py applies to the archive, so the two cannot disagree.

( cd "$STAGE" && find . -type f | sed 's|^\./||' | LC_ALL=C sort ) > "$WORK/paths"

: > "$WORK/list"
while IFS= read -r rel; do
    printf '%s  %s\n' "$(sha_file "$STAGE/$rel")" "$rel" >> "$WORK/list"
done < "$WORK/paths"

CONTENT_SHA=$(sha_stdin < "$WORK/list")
FILE_COUNT=$(wc -l < "$WORK/list" | tr -d ' ')
PREFIX="goat-federation-v1-${CONTENT_SHA:0:12}"

if [ "$MODE" = content ]; then
    printf '%s\n' "$CONTENT_SHA"
    exit 0
fi

# ---------------------------------------------------------- the manifest ----

{
cat <<EOF
# federation-bundle/v1
#
# The conformance bundle for the goat federation package format: one
# specification, every conformance vector, and this manifest binding them to
# each other. Everything here is public. Nothing here is signed — README.md,
# "Why this bundle is not signed", says why, and what to do instead.
#
# spec-version:       $SPEC_VERSION
# agreement-version:  $AGREEMENT_VERSION
# spec-document:      federation-package-spec.md
# spec-sha256:        $SPEC_SHA
# evaluation-date:    $EVAL_DATE
# file-count:         $FILE_COUNT
# content-sha256:     $CONTENT_SHA
# source-commit:      $SOURCE_COMMIT
# source-tree:        $SOURCE_TREE
#
# ---------------------------------------------------------------------------
# THE EVALUATION DATE IS $EVAL_DATE. IT IS NOT TODAY'S DATE.
#
# Composition compares each package's \`expires\` against a date called \`now\`
# (specification §6). In production that is the current UTC date. Here it is
# supplied as an input, in vectors/now.txt, and it is $EVAL_DATE.
#
# Read it from that file. A composer that reads the clock instead refuses every
# composing pair, which is noisy and you will find it — and silently takes the
# step-4 refusal fixtures over with expiry (§6 step 3 runs first), so they still
# refuse, for the wrong rule, and report a pass. That is a passing test hiding a
# broken implementation (§10). The step-0, -1 and -2 fixtures are unaffected, so
# what you get is a PARTIAL pass, which looks like progress.
#
# ./verify-bundle.sh names every affected fixture, and marks which of them
# refuse for the wrong rule, on the day you run it.
# ---------------------------------------------------------------------------
#
# WHAT IS NOT HERE, so absence is not read as omission:
#
#   No implementation. No library, no container image, no composer. The
#   specification and these vectors are the whole of what you need; if they
#   are not, that is the defect we want reported.
#   No private key material except vectors/testkeys/*.seed.hex, which are
#   published on purpose, are known to everyone, and are worthless. Never bind
#   one to a sovereign_id.
#   No signing key of ours, no certificate authority material, no endpoint
#   addresses, no partner declarations. Those reach you over the channel of
#   §5.1, which is a telephone call or a meeting, and never a file.
#   No reference implementation. Ours predates this revision of the
#   specification and disagrees with it in about twenty places; shipping it
#   would teach the wrong format.
#
# ---------------------------------------------------------------------------
# CHECKING THIS BUNDLE
#
# With the verifier, which also reports missing and extra files:
#
#   ./verify-bundle.sh
#
# Or with nothing but standard tools, which checks contents only:
#
#   grep -E '^[0-9a-f]{64}  ' MANIFEST | sha256sum -c        # GNU
#   grep -E '^[0-9a-f]{64}  ' MANIFEST | shasum -a 256 -c    # macOS, BSD
#
# MANIFEST cannot cover itself. Its digest is in MANIFEST.sha256, and that
# value — or content-sha256 above, which covers the file list and not the
# provenance header — is what to confirm with us over the channel that carried
# our CA fingerprint. It is one more 64-character value on a call that is
# already reading one.
# ---------------------------------------------------------------------------
#
# Per-fixture earliest expiry, read by ./verify-bundle.sh:
EOF
cat "$WORK/expiry"
echo "#"
cat "$WORK/list"
} > "$STAGE/MANIFEST"

sha_file "$STAGE/MANIFEST" | awk '{print $1 "  MANIFEST"}' > "$STAGE/MANIFEST.sha256"
chmod 644 "$STAGE/MANIFEST" "$STAGE/MANIFEST.sha256"

# ------------------------------------------------------------ the archive ----

mkdir -p "$OUT_DIR"
TGZ="$OUT_DIR/$PREFIX.tar.gz"
python3 "$WRITE_TAR" \
    --root "$STAGE" \
    --prefix "$PREFIX" \
    --tgz-out "$TGZ" \
    --tar-digest-out "$WORK/bundle.tar"

TAR_SHA=$(sha_file "$WORK/bundle.tar")
TGZ_SHA=$(sha_file "$TGZ")

cat <<EOF

Bundle written.

  archive          $TGZ
  files            $FILE_COUNT
  specification    $SPEC_VERSION
  evaluation date  $EVAL_DATE   (vectors/now.txt — the partner must supply this, not the clock)
  source commit    $SOURCE_COMMIT ($SOURCE_TREE)

  content-sha256   $CONTENT_SHA
    The bundle's identity, and the value to confirm with the partner. It covers
    the file list only, so it is the same for the same files however they were
    packed. Recompute it any time with --content-sha256.

  tar sha256       $TAR_SHA
  tar.gz sha256    $TGZ_SHA
    If two builds' .tar.gz differ while their .tar agree, the difference is the
    gzip version, not the content.

EOF

if [ "$SOURCE_TREE" = dirty ]; then
    cat <<'EOF'
  WARNING: built from a dirty tree. source-commit in the manifest names a
  commit that does not describe these bytes. Do not send this one.

EOF
fi
