#!/usr/bin/env bash
# Block 68C pull-image.sh — multi-tier rapid-bringup image fetch.
#
# Operator-side script. Runs on the laptop or builder host, NOT on the
# target Orin. Pulls the named image from T2 (Cloudflare edge) →
# T1 (S3 sovereign primary) → T3 (sovereign mirror, TBD), verifies
# Ed25519 signature against the offline-CA pubkey, exits non-zero on
# any tier+verify failure.
#
# Spec: docs/design/rapid-site-bringup.md §5
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
. "$REPO_ROOT/ops/bootstrap/lib-hardware-classes.sh"


# T2 lives on mirror.example (separate Cloudflare zone from goat.example so the
# rapid-bringup public-DNS surface is decoupled from production-data-plane DNS).
DEFAULT_T2="https://image-mirror.example"
DEFAULT_T1_BUCKET="example-image-mirror"
DEFAULT_SIGNERS=""   # empty => generated from the operator roster at run time

usage() {
  cat <<EOF
pull-image.sh — Block 68 multi-tier rapid-bringup image puller

Usage:
  $(basename "$0") --hardware-class <class> \\
                   --tag <bundle-vYYYYMMDD> \\
                   --output <path> \\
                   [--tier <T1|T2|T3>] \\
                   [--recipients <path>]

Options:
  --hardware-class <class>  Image flavor to fetch
  --tag <tag>               Bundle tag (e.g. bundle-v20260506)
  --output <path>           Local destination path (image will land here)
  --tier <T1|T2|T3>         Force a specific tier (default: try T2 → T1 → T3)
  --signers <path>          allowed_signers file for ssh-keygen -Y verify
                            (default: $DEFAULT_SIGNERS)
  --signer-id <principal>   accept only this principal (default: any listed)

Tier sources (override via env):
  T2_BASE_URL  default: $DEFAULT_T2
  T1_S3_BUCKET default: $DEFAULT_T1_BUCKET
  T3_BASE_URL  no default — set env when T3 lands
EOF
  exit 1
}

HW_CLASS=""
TAG=""
OUTPUT=""
FORCE_TIER=""
SIGNERS="$DEFAULT_SIGNERS"
SIGNER_ID=""   # principal to accept; empty = any in the file
while [[ $# -gt 0 ]]; do
  case "$1" in
    --hardware-class) HW_CLASS=$2; shift 2;;
    --tag) TAG=$2; shift 2;;
    --output) OUTPUT=$2; shift 2;;
    --tier) FORCE_TIER=$2; shift 2;;
    --signers) SIGNERS=$2; shift 2;;
    --recipients) SIGNERS=$2; shift 2;;   # back-compat
    --signer-id) SIGNER_ID=$2; shift 2;;
    -h|--help) usage;;
    *) echo "unknown arg: $1" >&2; usage;;
  esac
done

[[ -n "$HW_CLASS" && -n "$TAG" && -n "$OUTPUT" ]] || usage
_require_class "$HW_CLASS" || exit 2

T2_BASE_URL=${T2_BASE_URL:-$DEFAULT_T2}
T1_S3_BUCKET=${T1_S3_BUCKET:-$DEFAULT_T1_BUCKET}
T3_BASE_URL=${T3_BASE_URL:-}

OBJ="${HW_CLASS}-${TAG}.img"
SIG="${OBJ}.sig"

mkdir -p "$(dirname "$OUTPUT")"


_ensure_signers() {
  [ -n "${SIGNERS:-}" ] && [ -f "$SIGNERS" ] && return 0
  SIGNERS="$(mktemp)"; trap 'rm -f "$SIGNERS"' EXIT
  "$REPO_ROOT/ops/bootstrap/gen-allowed-signers.sh" "$SIGNERS" >/dev/null || return 1
}

log() { printf '[pull-image] %s\n' "$*" >&2; }

try_t2() {
  log "T2: trying $T2_BASE_URL/$OBJ"
  if curl --fail --silent --location -o "$OUTPUT" "$T2_BASE_URL/$OBJ" \
     && curl --fail --silent --location -o "${OUTPUT}.sig" "$T2_BASE_URL/$SIG"; then
    return 0
  fi
  return 1
}

try_t1() {
  log "T1: trying s3://$T1_S3_BUCKET/$OBJ"
  command -v aws >/dev/null \
    || { log "T1: aws CLI not in PATH; skipping"; return 1; }
  if aws s3 cp "s3://$T1_S3_BUCKET/$OBJ" "$OUTPUT" >/dev/null \
     && aws s3 cp "s3://$T1_S3_BUCKET/$SIG" "${OUTPUT}.sig" >/dev/null; then
    return 0
  fi
  return 1
}

try_t3() {
  if [[ -z "$T3_BASE_URL" ]]; then
    log "T3: not configured (set T3_BASE_URL env)"
    return 1
  fi
  log "T3: trying $T3_BASE_URL/$OBJ"
  if curl --fail --silent --location -o "$OUTPUT" "$T3_BASE_URL/$OBJ" \
     && curl --fail --silent --location -o "${OUTPUT}.sig" "$T3_BASE_URL/$SIG"; then
    return 0
  fi
  return 1
}

verify_signature() {
  # ssh-keygen -Y verify REQUIRES -I <principal>; without it the command exits
  # "Too few arguments for verify: missing principal identity" and this function
  # could never succeed. It also needs an OpenSSH allowed_signers file — the
  # previous default was recipients.txt, the AGE roster, which holds no ssh keys
  # at all. Both bugs were latent because no test ever called this path.
  [[ -f "$SIGNERS" ]] || { log "verify: allowed_signers missing: $SIGNERS"; return 1; }
  [[ -f "${OUTPUT}.sig" ]] || { log "verify: no signature at ${OUTPUT}.sig"; return 1; }

  local ids=()
  if [[ -n "$SIGNER_ID" ]]; then
    ids=("$SIGNER_ID")
  else
    while read -r principal _rest; do
      [[ -z "$principal" || "$principal" == \#* ]] && continue
      ids+=("$principal")
    done < "$SIGNERS"
  fi
  [[ ${#ids[@]} -gt 0 ]] || { log "verify: no principals in $SIGNERS"; return 1; }

  local id
  for id in "${ids[@]}"; do
    if ssh-keygen -Y verify -f "$SIGNERS" -I "$id" -n bootstrap-image \
         -s "${OUTPUT}.sig" < "$OUTPUT" >/dev/null 2>&1; then
      log "verify: PASS (signed by $id)"
      return 0
    fi
  done
  log "verify: FAIL — signature does not chain to any principal in $SIGNERS"
  return 1
}

# ----- pull tier order ------------------------------------------------
PULLED=0
case "${FORCE_TIER:-auto}" in
  T1) try_t1 && PULLED=1;;
  T2) try_t2 && PULLED=1;;
  T3) try_t3 && PULLED=1;;
  auto)
    if try_t2; then PULLED=1
    elif try_t1; then PULLED=1
    elif try_t3; then PULLED=1
    fi
    ;;
  *) echo "invalid --tier: $FORCE_TIER" >&2; usage;;
esac

if [[ $PULLED -eq 0 ]]; then
  log "FAIL: no tier returned a usable image+signature pair"
  exit 1
fi

# ----- verify --------------------------------------------------------
if ! verify_signature; then
  rm -f "$OUTPUT" "${OUTPUT}.sig"
  log "FAIL: signature verification failed; image not trusted"
  exit 2
fi

log "DONE: $OUTPUT (verified)"
echo "$OUTPUT"
