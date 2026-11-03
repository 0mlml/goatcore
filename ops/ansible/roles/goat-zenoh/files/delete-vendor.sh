#!/usr/bin/env bash
#
# goat-zenoh-delete-vendor — deregister one vendor: strip its four ACL +
# storage stanzas from the portal-owned ORG FRAGMENT, re-assemble the live
# config from the role-owned SEED + the fragment, SIGHUP zenohd, and best-effort
# remove the per-vendor rocksdb dir.
#
# Symmetric counterpart to add-vendor.sh.
# Both now edit the ORG fragment — never the live config directly — and
# re-assemble, so a role re-render can no longer clobber runtime state. The
# portal's DELETE /api/vendors/{prefix} also does this work in-process; this
# script is the operator-fired escape hatch.
#
# The prune regexes are BYTE-IDENTICAL to the prior in-place-edit version (they
# match the same JSON5 block shapes add-vendor.sh emits) — only the target
# (fragment vs live config) and the reassemble step are new.
#
# Idempotent: if the vendor's stanzas aren't in the fragment, the script still
# reassembles + SIGHUPs (in case a prior partial-failure left the live config
# dirty) and exits 0.
#
# Output: structured JSON to stdout. Exit codes mirror add-vendor.sh:
#   0   removal completed (or vendor already absent) + reload succeeded
#   2   bad arguments
#   3   seed or org fragment not found
#   4   vendor name or storage parent failed validation
#   5   SIGHUP to zenohd container failed
#   6   fragment edit / assemble failed (markers missing, write-error, etc.)
#
# Usage:
#   goat-zenoh-delete-vendor --vendor org11x
#   goat-zenoh-delete-vendor --vendor org11x \
#     --seed /etc/goat-zenoh/config.seed.json5 \
#     --org  /etc/goat-zenoh/acl-org-fragment.json5 \
#     --config /etc/goat-zenoh/config.json5 --container goat-zenoh-router

set -euo pipefail

SEED_PATH="/etc/goat-zenoh/config.seed.json5"
ORG_PATH="/etc/goat-zenoh/acl-org-fragment.json5"
LIVE_PATH="/etc/goat-zenoh/config.json5"
ASSEMBLER="/usr/local/sbin/goat-zenoh-acl-assemble"
CONTAINER="goat-zenoh-router"
STORAGE_PARENT="/var/lib/goat-zenoh/rocksdb"
VENDOR=""

emit() {
  printf '{"vendor":"%s","action":"%s","ok":%s,"reason":"%s"}\n' \
    "${VENDOR}" "$1" "$2" "${3:-}"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --vendor)         VENDOR="$2"; shift 2 ;;
    --seed)           SEED_PATH="$2"; shift 2 ;;
    --org)            ORG_PATH="$2"; shift 2 ;;
    --acl|--config)   LIVE_PATH="$2"; shift 2 ;;
    --assembler)      ASSEMBLER="$2"; shift 2 ;;
    --container)      CONTAINER="$2"; shift 2 ;;
    --storage-parent) STORAGE_PARENT="$2"; shift 2 ;;
    --help|-h)
      cat <<'USAGE'
goat-zenoh-delete-vendor --vendor <name> [--seed PATH] [--org PATH] [--config PATH]
                              [--assembler PATH] [--container NAME] [--storage-parent PATH]

Strips the four per-vendor stanzas (write rule + subject + policy + storage) from
the portal-owned ORG FRAGMENT, re-assembles the live config from the role-owned
SEED + the fragment, SIGHUPs zenohd, and removes the per-vendor rocksdb dir.

Idempotent. Structured JSON to stdout. Non-zero exit on any failure.
USAGE
      exit 0
      ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done

if [[ -z "${VENDOR}" ]]; then
  echo '{"vendor":"","action":"validate","ok":false,"reason":"--vendor is required"}'
  exit 2
fi

# Vendor name validation — same constraint as add-vendor.sh.
if ! [[ "${VENDOR}" =~ ^[a-z0-9-]{1,32}$ ]]; then
  emit "validate" false "vendor name must match ^[a-z0-9-]{1,32}$"
  exit 4
fi

# Validate before editing configuration or reloading the router. Canonicalize
# existing parents so aliases such as /tmp/.. or a symlink to / are refused too.
if [[ -z "${STORAGE_PARENT}" ]]; then
  emit "validate" false "storage parent must not be empty"
  exit 4
fi
if [[ -d "${STORAGE_PARENT}" ]]; then
  if ! STORAGE_PARENT="$(cd -- "${STORAGE_PARENT}" && pwd -P)"; then
    emit "validate" false "storage parent cannot be resolved"
    exit 4
  fi
fi
if [[ "${STORAGE_PARENT}" =~ ^/+$ ]]; then
  emit "validate" false "storage parent must not resolve to /"
  exit 4
fi

if [[ ! -f "${SEED_PATH}" ]]; then
  emit "open-seed" false "seed config not found at ${SEED_PATH}"
  exit 3
fi
if [[ ! -f "${ORG_PATH}" ]]; then
  emit "open-org" false "org fragment not found at ${ORG_PATH}"
  exit 3
fi

storage_cleanup() {
  if [[ -d "${STORAGE_PARENT}/${VENDOR}" ]]; then
    if rm -rf -- "${STORAGE_PARENT:?}/${VENDOR:?}"; then
      emit "storage-cleanup" true ""
    else
      emit "storage-cleanup" false "rm -rf ${STORAGE_PARENT}/${VENDOR} failed; wire-layer revocation in effect; operator follow-up needed"
    fi
  fi
}

reassemble() {
  if ! "${ASSEMBLER}" assemble --seed "${SEED_PATH}" --org "${ORG_PATH}" --out "${LIVE_PATH}" >/dev/null; then
    emit "assemble" false "acl-assemble assemble failed"
    exit 6
  fi
  chmod 644 "${LIVE_PATH}"
  emit "assemble" true ""
  if docker kill --signal HUP "${CONTAINER}" >/dev/null 2>&1; then
    emit "reload" true ""
  else
    emit "reload" false "docker kill --signal HUP ${CONTAINER} failed; live config on disk is updated but the router did not reload"
    exit 5
  fi
}

# Absence check — if the rule id isn't in the fragment, skip the edit but still
# reassemble + SIGHUP (cheap idempotency) and attempt storage cleanup.
if ! grep -qF "\"trial-peer-w-${VENDOR}\"" "${ORG_PATH}"; then
  emit "already-absent" true ""
  reassemble
  storage_cleanup
  exit 0
fi

# Atomic-write pattern on the ORG fragment.
TMP="$(mktemp "${ORG_PATH}.XXXXXX")"
trap 'rm -f "${TMP}"' EXIT
cp "${ORG_PATH}" "${TMP}"

# Strip the four stanzas from the fragment. Patterns match the literal JSON5
# shape add-vendor.sh emits: indent + opening brace + id line tied to this
# vendor + non-greedy lines until the closing brace. Byte-identical to the prior
# in-place-edit regexes; acl.go's RemoveVendor uses the equivalent set, so the
# paths stay in lockstep by construction.
python3 - "${TMP}" "${VENDOR}" <<'PY' || { emit "edit-org" false "python3 fragment edit failed"; exit 6; }
import sys, re, pathlib
path = pathlib.Path(sys.argv[1])
vendor = sys.argv[2]
ve = re.escape(vendor)
text = path.read_text()

# rule { id: "trial-peer-w-<v>", ... }, — 6-space indent.
text = re.sub(
    r'(?m)^      \{\n        id: "trial-peer-w-' + ve + r'",\n(?:        [^\n]*\n)*?      \},\n',
    "", text)
# subject { id: "vendor-<v>", ... }, — 6-space indent.
text = re.sub(
    r'(?m)^      \{\n        id: "vendor-' + ve + r'",\n(?:        [^\n]*\n)*?      \},\n',
    "", text)
# policy { rules: ["trial-peer-w-<v>", ...], subjects: ["vendor-<v>"], }, — 6-space.
text = re.sub(
    r'(?m)^      \{\n        rules: \["trial-peer-w-' + ve + r'"[^]]*\],\n        subjects: \["vendor-' + ve + r'"\],\n      \},\n',
    "", text)
# storage <v>: { ... }, — 8-space indent (nested in storages: object).
text = re.sub(
    r'(?m)^        ' + ve + r': \{\n(?:          [^\n]*\n)*?        \},\n',
    "", text)

path.write_text(text)
PY

mv "${TMP}" "${ORG_PATH}"
chmod 644 "${ORG_PATH}"
trap - EXIT
emit "edit-org" true ""

# Rebuild the live config from SEED + the pruned fragment, reload, then remove
# the per-vendor storage dir (wire-layer revocation is already in effect).
reassemble
storage_cleanup
