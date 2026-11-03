#!/usr/bin/env bash
# ftpm-availability-spike.sh — Block 65A.0 spike per ADR 0612 + ADR 0605.
#
# PURPOSE: determine whether NVIDIA's OP-TEE fTPM Trusted Application is
# available + functional on a flashed Jetson AGX Orin running JetPack 6.1
# (L4T R36.4.7). The spike outcome decides Block 65A's path:
#
#   PASS  → /dev/tpm0 exposed, PCRs read cleanly, systemd-cryptenroll
#           accepts the device → integrate via the existing `tpm2` mode
#           in ops/ansible/roles/luks-unlock/. Implementation in 65A.1
#           is ~2-4 days.
#
#   FAIL  → no /dev/tpm0 or PCRs unreadable → fall back to custom OP-TEE
#           TA implementation (the multi-week version). 65A.1 reverts to
#           the `tegra_se` mode work in luks-unlock README's TODO §117.
#
# This script is deliberately read-only against the TPM. It does NOT
# enroll a LUKS keyslot; that's 65A.1's job once the spike outcome is
# known. Run on a flashed Orin (any of: prov-rla Orin-D, vega Orin-A
# day-1 pre-cutover, prov-x). Output is machine-parseable JSONL plus a
# human verdict at the end.
#
# Operator action: run this script on the target Orin via ssh-over-mesh,
# capture stdout, and paste the verdict into the Block 65A.0 evidence
# row in docs/project/validation-state.md.
#
# Exit codes:
#   0  — spike PASS (proceed to 65A.1 fTPM path)
#   1  — spike FAIL (proceed to 65A.1 OP-TEE TA fallback)
#   2  — script error (re-run after fixing)

set -euo pipefail

REPORT_DIR="${REPORT_DIR:-/tmp/ftpm-spike}"
mkdir -p "$REPORT_DIR"
JSONL_PATH="$REPORT_DIR/ftpm-spike-$(date -u +%Y%m%dT%H%M%SZ).jsonl"

emit() {
    # emit <key=value pairs as one JSON object>
    python3 -c "import json,sys; print(json.dumps(dict(s.split('=',1) for s in sys.argv[1:])))" "$@" >> "$JSONL_PATH"
}

emit \
    event=spike-start \
    timestamp="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    host="$(hostname)" \
    kernel="$(uname -r)" \
    spike=Block-65A.0

# --- Step 1: kernel capability probes ---

echo "[1/5] Kernel TPM capability probes"

if [[ -e /dev/tpm0 ]]; then
    TPM_DEV_PRESENT=true
    TPM_DEV_PERMS="$(stat -c '%a' /dev/tpm0)"
    TPM_DEV_OWNER="$(stat -c '%U:%G' /dev/tpm0)"
    echo "  /dev/tpm0 exists (perms=$TPM_DEV_PERMS owner=$TPM_DEV_OWNER)"
else
    TPM_DEV_PRESENT=false
    echo "  /dev/tpm0 ABSENT"
fi

if [[ -e /dev/tpmrm0 ]]; then
    TPMRM_PRESENT=true
    echo "  /dev/tpmrm0 exists"
else
    TPMRM_PRESENT=false
    echo "  /dev/tpmrm0 absent (resource-manager kernel module not loaded)"
fi

emit \
    event=device-probe \
    tpm_dev_present="$TPM_DEV_PRESENT" \
    tpmrm_present="$TPMRM_PRESENT"

# Kernel module check
TPM_MODULES_LOADED="$(lsmod 2>/dev/null | awk '$1 ~ /^tpm/ {print $1}' | sort -u | paste -sd,)"
emit event=kernel-modules loaded="${TPM_MODULES_LOADED:-none}"
echo "  TPM-related kernel modules: ${TPM_MODULES_LOADED:-none loaded}"

# --- Step 2: OP-TEE presence + fTPM TA detection ---

echo "[2/5] OP-TEE + fTPM TA probes"

if [[ -e /dev/tee0 ]] || [[ -e /dev/teepriv0 ]]; then
    OPTEE_DEV_PRESENT=true
    echo "  OP-TEE devices present"
else
    OPTEE_DEV_PRESENT=false
    echo "  OP-TEE devices absent — TEE supplicant likely not running"
fi

if systemctl list-unit-files --no-legend 'tee-supplicant*' 2>/dev/null | grep -q '\.service'; then
    if systemctl is-active --quiet tee-supplicant.service 2>/dev/null; then
        OPTEE_SUPPLICANT_ACTIVE=true
        echo "  tee-supplicant.service is active"
    else
        OPTEE_SUPPLICANT_ACTIVE=false
        echo "  tee-supplicant.service is installed but NOT active"
    fi
else
    OPTEE_SUPPLICANT_ACTIVE=unknown
    echo "  tee-supplicant.service not found in systemd unit catalog"
fi

emit \
    event=optee-probe \
    optee_dev_present="$OPTEE_DEV_PRESENT" \
    supplicant_active="$OPTEE_SUPPLICANT_ACTIVE"

# --- Step 3: tpm2-tools functional checks ---

echo "[3/5] tpm2-tools functional probes"

if ! command -v tpm2_pcrread >/dev/null 2>&1; then
    echo "  tpm2-tools not installed; attempting apt install"
    if sudo apt-get install -y tpm2-tools >/dev/null 2>&1; then
        echo "  tpm2-tools installed"
    else
        echo "  tpm2-tools install FAILED — continuing without functional checks"
        emit event=tpm2-tools install_failed=true
        TPM2_TOOLS_AVAILABLE=false
    fi
fi
command -v tpm2_pcrread >/dev/null 2>&1 && TPM2_TOOLS_AVAILABLE=true || TPM2_TOOLS_AVAILABLE=${TPM2_TOOLS_AVAILABLE:-false}

PCR_READ_OK=false
if [[ "$TPM2_TOOLS_AVAILABLE" == "true" ]] && [[ "$TPM_DEV_PRESENT" == "true" ]]; then
    if tpm2_pcrread sha256:0,2,4,7,11 >/dev/null 2>&1; then
        PCR_READ_OK=true
        echo "  tpm2_pcrread OK — PCRs 0/2/4/7/11 readable in sha256 bank"
    else
        echo "  tpm2_pcrread FAILED — TPM device present but PCRs not readable"
    fi
fi

emit \
    event=pcr-probe \
    tpm2_tools_available="$TPM2_TOOLS_AVAILABLE" \
    pcr_read_ok="$PCR_READ_OK"

# --- Step 4: systemd-cryptenroll device probe ---

echo "[4/5] systemd-cryptenroll TPM device probe"

CRYPTENROLL_OK=false
if command -v systemd-cryptenroll >/dev/null 2>&1; then
    if systemd-cryptenroll --tpm2-device=list 2>/dev/null | grep -q '/dev/tpm'; then
        CRYPTENROLL_OK=true
        echo "  systemd-cryptenroll lists at least one TPM device"
    else
        echo "  systemd-cryptenroll lists NO TPM device"
    fi
else
    echo "  systemd-cryptenroll not installed"
fi

emit event=cryptenroll-probe cryptenroll_lists_tpm="$CRYPTENROLL_OK"

# --- Step 5: verdict ---

echo "[5/5] Verdict"

VERDICT=FAIL
if [[ "$TPM_DEV_PRESENT" == "true" ]] \
   && [[ "$PCR_READ_OK" == "true" ]] \
   && [[ "$CRYPTENROLL_OK" == "true" ]]; then
    VERDICT=PASS
fi

emit \
    event=verdict \
    verdict="$VERDICT" \
    block=65A.0

cat <<EOF

============================================================
  Block 65A.0 fTPM availability spike — VERDICT: $VERDICT
============================================================

  /dev/tpm0 present:          $TPM_DEV_PRESENT
  TPM kernel modules:         ${TPM_MODULES_LOADED:-none}
  OP-TEE supplicant active:   $OPTEE_SUPPLICANT_ACTIVE
  tpm2-tools available:       $TPM2_TOOLS_AVAILABLE
  tpm2_pcrread succeeded:     $PCR_READ_OK
  systemd-cryptenroll lists:  $CRYPTENROLL_OK

  Evidence: $JSONL_PATH

  Next step:
EOF

if [[ "$VERDICT" == "PASS" ]]; then
    cat <<EOF
    Proceed to 65A.1 fTPM path — enroll tpm2 keyslot via the existing
    luks-unlock role with mode 'tpm2' and luks_tpm2_pcrs=[0,2,4,7,11].
    Estimate: 2-4 days end-to-end on prov-rla, then roll to vega.
EOF
    exit 0
else
    cat <<EOF
    Proceed to 65A.1 OP-TEE TA fallback — open Block 65A-fallback for
    custom Tegra-SE TA development per ADR 0605. Estimate: 2-4 weeks.
    65B/65C/65D unaffected — they ship in parallel.
EOF
    exit 1
fi
