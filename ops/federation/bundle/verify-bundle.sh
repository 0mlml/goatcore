#!/bin/sh
#
# verify-bundle.sh — check this bundle against its own manifest, before you
# trust anything in it.
#
# You do not need any of our software to run this. POSIX shell, and one of
# sha256sum, shasum or openssl — you already have at least one.
#
# What it answers:
#
#   Is every file the manifest lists present and unmodified?   (altered/missing)
#   Is there anything here the manifest does not list?         (extra)
#   Does the manifest itself match its own recorded digest?    (tampering)
#   What evaluation date must I supply as `now`?               (printed always)
#
# What it does NOT do, and cannot: it says nothing about whether your
# implementation of the format is correct. That is what the vectors are for,
# and running them is your work. This checks that you received the vectors
# intact.
#
# Exit status: 0 everything matches, 1 the bundle is not intact, 2 the check
# could not be run.

set -eu

# ---------------------------------------------------------------- setup ----

usage() {
    cat <<'EOF'
Usage: ./verify-bundle.sh [BUNDLE_DIR]

  BUNDLE_DIR   the extracted bundle directory. Defaults to the directory
               this script is in, which is what you want if you ran it as
               ./verify-bundle.sh from inside the extracted bundle.
EOF
}

case "${1:-}" in
    -h|--help) usage; exit 0 ;;
esac

BUNDLE_DIR="${1:-}"
if [ -z "$BUNDLE_DIR" ]; then
    BUNDLE_DIR=$(dirname "$0")
fi

if [ ! -d "$BUNDLE_DIR" ]; then
    echo "verify-bundle: not a directory: $BUNDLE_DIR" >&2
    exit 2
fi

cd "$BUNDLE_DIR" || exit 2

# One of these three exists on every system this bundle can plausibly reach.
# They are checked in order of how exactly they print, not of preference.
if command -v sha256sum >/dev/null 2>&1; then
    sha_file() { sha256sum "$1" | cut -d' ' -f1; }
    sha_stdin() { sha256sum | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
    sha_file() { shasum -a 256 "$1" | cut -d' ' -f1; }
    sha_stdin() { shasum -a 256 | cut -d' ' -f1; }
elif command -v openssl >/dev/null 2>&1; then
    sha_file() { openssl dgst -sha256 "$1" | awk '{print $NF}'; }
    sha_stdin() { openssl dgst -sha256 | awk '{print $NF}'; }
else
    echo "verify-bundle: need one of sha256sum, shasum or openssl on PATH" >&2
    exit 2
fi

MANIFEST=MANIFEST
MANIFEST_DIGEST_FILE=MANIFEST.sha256

for f in "$MANIFEST" "$MANIFEST_DIGEST_FILE"; do
    if [ ! -f "$f" ]; then
        echo "verify-bundle: $f is missing — this is not an intact bundle" >&2
        exit 1
    fi
done

WORK=$(mktemp -d 2>/dev/null || mktemp -d -t fedbundle)
# shellcheck disable=SC2064  # WORK is set now and must expand now, not at exit
trap "rm -rf '$WORK'" EXIT INT TERM

fail=0
note() { printf '%s\n' "$*"; }

# ------------------------------------------------- 1. the manifest itself ----
#
# The manifest cannot cover itself, so its digest is in a second file, and that
# digest is the one value you should have received over the authenticated
# channel that carried our signing key and CA fingerprint. If you did not
# receive it that way, this step proves the bundle is internally consistent and
# nothing about where it came from.

recorded=$(awk '{print $1; exit}' "$MANIFEST_DIGEST_FILE")
actual=$(sha_file "$MANIFEST")

note "Bundle:   $(pwd)"
note "Manifest: $actual"
if [ "$recorded" != "$actual" ]; then
    note ""
    note "FAIL  MANIFEST does not match MANIFEST.sha256."
    note "      recorded  $recorded"
    note "      actual    $actual"
    note ""
    note "      Nothing below this line means anything. Stop, and ask for the"
    note "      bundle again."
    exit 1
fi

# --------------------------------------------------- 2. header of record ----

# Header values are padded for alignment in the manifest, so the padding is
# stripped here rather than being carried into a string comparison.
hdr() {
    awk -v k="$1" '
        $0 ~ "^# " k ": " {
            sub("^# " k ": ", "")
            sub(/^[ \t]+/, "")
            sub(/[ \t]+$/, "")
            print
            exit
        }
    ' "$MANIFEST"
}

SPEC_VERSION=$(hdr spec-version)
EVAL_DATE=$(hdr evaluation-date)
CONTENT_RECORDED=$(hdr content-sha256)
SOURCE_COMMIT=$(hdr source-commit)
FILE_COUNT=$(hdr file-count)

# ------------------------------------------------ 3. content-sha256 again ----
#
# The bundle's identity. It covers the file list and not the provenance header,
# so it is the same value for the same files however and whenever they were
# packed — which is what makes it the thing to quote back to us, and the thing
# that changes when the specification or a vector changes.

grep -E '^[0-9a-f]{64}  ' "$MANIFEST" > "$WORK/list" || true
CONTENT_ACTUAL=$(sha_stdin < "$WORK/list")

if [ "$CONTENT_RECORDED" != "$CONTENT_ACTUAL" ]; then
    note "FAIL  content-sha256 in the header does not cover the file list below it."
    fail=1
fi

# ------------------------------------------- 4. every listed file, in turn ----

: > "$WORK/missing"
: > "$WORK/altered"
: > "$WORK/listed"

while IFS= read -r line; do
    want=$(printf '%s' "$line" | cut -c1-64)
    path=$(printf '%s' "$line" | cut -c67-)
    printf '%s\n' "$path" >> "$WORK/listed"
    if [ ! -f "$path" ]; then
        printf '%s\n' "$path" >> "$WORK/missing"
        continue
    fi
    got=$(sha_file "$path")
    if [ "$want" != "$got" ]; then
        printf '%s\n' "$path" >> "$WORK/altered"
    fi
done < "$WORK/list"

# ------------------------------------------------------- 5. extra files ----
#
# Absence of a file is not the only way a bundle is wrong. A file nobody
# accounted for is how an implementation ends up reading a vector we never
# wrote, so it is a failure here and not a warning.

find . -type f \
    ! -name "$MANIFEST" \
    ! -name "$MANIFEST_DIGEST_FILE" \
    | sed 's|^\./||' | LC_ALL=C sort > "$WORK/present"
LC_ALL=C sort "$WORK/listed" > "$WORK/listed.sorted"
comm -23 "$WORK/present" "$WORK/listed.sorted" > "$WORK/extra"

# ------------------------------------------------- 6. now.txt agreement ----

NOW_FILE=vectors/now.txt
NOW_VALUE=""
if [ -f "$NOW_FILE" ]; then
    NOW_VALUE=$(tr -d ' \t\r\n' < "$NOW_FILE")
fi

# ------------------------------------------------------------ 7. report ----

report_list() {
    label=$1
    file=$2
    n=$(wc -l < "$file" | tr -d ' ')
    if [ "$n" -gt 0 ]; then
        note ""
        note "FAIL  $n file(s) $label:"
        sed 's/^/        /' "$file"
        fail=1
    fi
}

report_list "missing" "$WORK/missing"
report_list "altered" "$WORK/altered"
report_list "present but not in the manifest" "$WORK/extra"

listed_n=$(wc -l < "$WORK/listed" | tr -d ' ')

note ""
note "Specification:  $SPEC_VERSION"
note "Built from:     $SOURCE_COMMIT"
note "Identity:       $CONTENT_ACTUAL"
note "Files:          $listed_n checked, $FILE_COUNT expected"

if [ "$listed_n" != "$FILE_COUNT" ]; then
    note ""
    note "FAIL  the manifest's file-count header and its own list disagree."
    fail=1
fi

# --------------------------------------------------- 8. the evaluation date ----
#
# Printed on every run, success or failure, because getting this wrong is the
# single most likely way a correct implementation reports itself broken.

note ""
note "============================================================"
note " EVALUATION DATE: $EVAL_DATE"
note "============================================================"
note ""
note " The vectors are evaluated at $EVAL_DATE, supplied to your"
note " composer as \`now\`. It is in vectors/now.txt. Read it from"
note " there. Do NOT read the clock."
note ""

if [ -z "$NOW_VALUE" ]; then
    note " FAIL  vectors/now.txt is missing or empty."
    fail=1
elif [ "$NOW_VALUE" != "$EVAL_DATE" ]; then
    note " FAIL  vectors/now.txt says $NOW_VALUE, the manifest says $EVAL_DATE."
    fail=1
fi

TODAY=$(date -u +%Y-%m-%d 2>/dev/null || echo "")
if [ -n "$TODAY" ] && [ "$TODAY" != "$EVAL_DATE" ]; then
    note " Today, on this machine, in UTC: $TODAY"
    note ""
    # Which fixtures actually change answer, stated precisely rather than
    # generously. Expiry is §6 step 3, so a stale `now` pre-empts only the
    # rules tested after it:
    #
    #   compose-*         flip from agreement to refusal. All of them.
    #   refuse-step-4     silently taken over by expiry — they still refuse,
    #                     for the wrong rule, which looks like a pass.
    #   refuse-step-0..3  unaffected. Their rule fires before expiry is
    #                     reached, or IS expiry.
    #
    # Overstating this would be its own failure: a partner told that twenty
    # fixtures are unreliable stops reading the list.
    awk -v today="$TODAY" '
        /^# fixture-expiry: / {
            split($0, f, " ")
            name = f[3]; until_date = f[4]; kind = f[5]
            if (until_date >= today) next
            if (kind == "agree") {
                printf "        %-38s expires %s   would refuse as expired\n", name, until_date
                flipped++
            } else if (kind == "refuse-step-4") {
                printf "        %-38s expires %s   refuses for the WRONG rule\n", name, until_date
                flipped++
                wrongrule++
            }
        }
        END { printf "COUNT %d %d\n", flipped + 0, wrongrule + 0 }
    ' "$MANIFEST" > "$WORK/flip" || true

    flips=$(awk '/^COUNT /{print $2}' "$WORK/flip")
    wrongrule=$(awk '/^COUNT /{print $3}' "$WORK/flip")

    if [ "${flips:-0}" -gt 0 ]; then
        note " Composing at $TODAY instead of $EVAL_DATE changes the answer for"
        note " $flips of the fixture pairs:"
        note ""
        grep -v '^COUNT ' "$WORK/flip" || true
        note ""
        if [ "${wrongrule:-0}" -gt 0 ]; then
            note " The $wrongrule marked WRONG rule are the dangerous ones. They still"
            note " refuse, so they look like passes, but expiry (§6 step 3) has"
            note " pre-empted the rule they were written to exercise — a passing"
            note " test hiding a broken implementation (§10)."
            note ""
        fi
        note " The remaining fixtures are unaffected: their rule fires at §6"
        note " step 0, 1 or 2, before expiry is reached, or it IS expiry."
    else
        note " No fixture changes answer at today's date — but that is luck,"
        note " not correctness, and it expires. Supply $EVAL_DATE."
    fi
    note ""
fi

note "============================================================"
note ""

if [ "$fail" -ne 0 ]; then
    note "VERDICT: this bundle is NOT intact. Do not implement against it."
    note "         Ask for it again, and compare the manifest digest over the"
    note "         same channel that carried the CA fingerprint."
    exit 1
fi

note "VERDICT: intact. Every file the manifest lists is present and unmodified,"
note "         and nothing else is here."
note ""
note "What this does not tell you: whether the bundle is ours. Nothing in it is"
note "signed, deliberately — see README.md, \"Why this bundle is not signed\"."
note "Confirm $CONTENT_ACTUAL"
note "over the channel that carried our CA fingerprint, and you have both."
exit 0
