# drill — Block 68D rapid-bringup drill harness

Operator-fired drill harness that times rapid-bringup end-to-end against
the `docs/design/rapid-site-bringup.md`
§6 phase budgets. Emits per-phase JSONL evidence; renders a
PASS/WARN/FAIL verdict; FAIL → F-NNN finding draft per
[ADR 1011](../../../docs/adr/1011-findings-register.md).

## Files

- [`run-rapid-bringup-drill.sh`](run-rapid-bringup-drill.sh) — driver.
  v1 is operator-paced (each phase boundary is a manual Enter); v2
  becomes end-to-end-driveable once the bringup steps are
  automation-ready.
- [`summarize.py`](summarize.py) — JSONL → markdown rollup. Output
  shape matches the
  [`ops/ansible/roles/ory/tests/soak/summarize.py`](../../ansible/roles/ory/tests/soak/summarize.py)
  pattern referenced in CLAUDE.md.

## Usage

```bash
# S1 single-Orin self-drill
./run-rapid-bringup-drill.sh \
  --scenario S1 \
  --hardware-class orin-mgmt

# S2 against a remote target
./run-rapid-bringup-drill.sh \
  --scenario S2 \
  --hardware-class orin-mgmt \
  --target operator@192.168.1.99

# S1 with tactical-edge bandwidth shape (Block 68E)
./run-rapid-bringup-drill.sh \
  --scenario S1 \
  --hardware-class orin-mgmt \
  --target operator@192.168.1.99 \
  --bandwidth-shape

# Render the rollup
./summarize.py docs/reports/2026-05-06-rapid-bringup-drill-s1-orin-mgmt.jsonl
```

## Cadence

Per design §9:

- **Quarterly** — operator-fired against a clean Orin
- **On image rebuild** — smoke drill before publishing the image tag
- **Ad-hoc** — every real new-site bringup self-measures via the same
  evidence shape (the first-boot script emits compatible JSONL)

## Verdict thresholds

- **PASS** — total elapsed within 10% of total budget
- **WARN** — total elapsed within 25% of total budget
- **FAIL** — total elapsed > 25% over total budget

Two consecutive PASS drills (one S1, one S2) earn the ADR promotion
per Block 68F.
