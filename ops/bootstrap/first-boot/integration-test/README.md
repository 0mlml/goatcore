# first-boot integration test

End-to-end test of [`first-boot.sh`](../first-boot.sh) without real
Orin/Karbon hardware. Validates the full flow:

1. Synthesizes a USB userdata.yml at `/tmp/userdata.yml` and points
   first-boot.sh at it via `USERDATA_OVERRIDE` (a test-only shim that
   bypasses the device-hunt loop)
2. Stages a mock `site-bootstrap.yml` that no-ops successfully
3. Drives the serial-console secrets prompt via `expect`
4. Asserts the JSONL evidence file has all 7 expected phase markers
   and every record is valid JSON
5. Asserts `.envrc.local` is rendered at mode 0600 with all expected
   secrets
6. Asserts the `group_vars/<site>_hosts/local.yml` overlay exists and
   is well-formed
7. Asserts the state marker (`/var/lib/goat/first-boot-state.json`)
   shows `complete: true`
8. Asserts re-run safety — second invocation without `--force` is a no-op

## Run

```bash
./run-test.sh
```

Output is a 6-line PASS summary on success; any failure stops with a
red FAIL line and exits non-zero.

## Why this matters

Catches regressions in `first-boot.sh` between commits without needing
to flash an image to a real Orin every time. Real-Orin boot smoke
remains the higher-fidelity validation but is operator-fired against
hardware; this test catches everything *except* the hardware-specific
LUKS keyslot behavior, kernel-level tegra-rcm flow, and Tegra serial UX
quirks.

## What it does NOT cover

- Tegra-specific serial console behavior (early-boot kernel-message
  interleave, cable hot-plug detection)
- LUKS keyslot enrollment under multi-modal Block 65 setup
- Real `site-bootstrap.yml` execution against a real mgmt-host stack
- Per-hardware-class differences (Orin vs Karbon)
- Network-dependent steps (Spock subscription, wg-cp0 reachability)

These remain operator-fired against real hardware per
[`docs/operations/image-builder-host.md`](../../../../docs/operations/image-builder-host.md)
and the drill harness at [`../../drill/`](../../drill/).
