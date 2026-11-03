#!/usr/bin/env python3
"""aggregate.py — Step 5 wrapper for goat-relay-prod-canary.md.

Reads the iperf3 driver's JSONL output, emits PASS/FAIL summary against
the runbook's acceptance criteria.

Usage:
    python3 aggregate.py /path/to/canary.jsonl

Exit code:
    0 — PASS (≥40/48 cycles ok, no error-rate breach)
    1 — FAIL or partial run

Acceptance per runbook §"PASS criteria":
    - ≥40 of 48 cycles complete successfully (≥83%)
    - Throughput median sanity-checked against M-040 anchor (operator's call)
    - Error rate <20% (else halt + investigate)

Relay-side restart / OOM / panic checks are out-of-scope here (see
ssm-probe-loop.sh + manual triage).
"""

import json
import sys
from statistics import mean, median


def main(path: str) -> int:
    with open(path) as f:
        cycles = [json.loads(l) for l in f if l.strip()]

    ok = [c for c in cycles if isinstance(c.get("result"), dict) and not c["result"].get("error")]
    err = [c for c in cycles if isinstance(c.get("result"), dict) and c["result"].get("error")]

    print(f"cycles_total={len(cycles)} cycles_ok={len(ok)} cycles_err={len(err)}")

    if ok:
        bps = [c["result"]["end"]["sum_received"]["bits_per_second"] / 1e6 for c in ok]
        print(
            f"throughput_Mbps: min={min(bps):.1f} median={median(bps):.1f} "
            f"mean={mean(bps):.1f} max={max(bps):.1f}"
        )
        if median(bps) > 0:
            spread_pct = (max(bps) - min(bps)) / median(bps) * 100
            print(f"spread_pct_of_median: {spread_pct:.1f}%")

    cycles_pass = len(ok) >= 40
    err_rate_pass = (len(err) / max(len(cycles), 1)) < 0.20

    verdict = "PASS" if (cycles_pass and err_rate_pass) else "FAIL"
    print(f"verdict={verdict} (cycles_ok>=40: {cycles_pass}, err_rate<20%: {err_rate_pass})")

    return 0 if verdict == "PASS" else 1


if __name__ == "__main__":
    if len(sys.argv) != 2:
        print(__doc__, file=sys.stderr)
        sys.exit(2)
    sys.exit(main(sys.argv[1]))
