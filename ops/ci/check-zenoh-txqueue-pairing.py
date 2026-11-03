#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
#
# check-zenoh-txqueue-pairing.py — the two transmit-queue levers move together,
# or the router gets WORSE than leaving both alone.
#
# WHY THIS EXISTS. zagros's router blocked its receive threads for
# about five seconds per push to subscribers that were not draining. Fabric-wide
# delivery stopped twice, for 50 and 46 minutes, while the process, its
# healthcheck and every wedge alert read healthy. 1,111 transport closes across
# 115 distinct ZIDs that day.
#
# M-119 measured the fix across a matrix. The numbers that matter here are not
# the winning row but the LOSING ones, because they are what a well-meaning edit
# lands on:
#
#   rx workers | wait_before_close | stall     | loss    | transport closes
#   -----------|-------------------|-----------|---------|------------------
#   2 (stock)  | 5 s (stock)       | 59-119 s  | 0-27 %  | 33-41
#   8          | 5 s               | 5-10 s    | ~6 %    | -
#   2          | 2 s or 0.5 s      | 42-59 s   | -       | 3-7x  <- HARMFUL
#   4          | 2 s               | 4.2 s     | 0 %     | 10
#   8          | 2 s               | 2.1 s     | 0 %     | 4     <- SHIPPED
#   8          | 8-deep queue, 2 s | 2.1-4.0 s | 0 %     | 4     (no better)
#
# Read row three. Tightening the deadline ALONE is not a partial fix that helps
# a little — it measured worse than stock on evictions, by three to seven times,
# while barely moving the stall. The deadline only pays once there are enough
# receive workers that a parked one is not most of the pool. zenoh 1.9.0 ships
# two workers, so at stock a single stuck link owns half of ingress.
#
# That coupling is the thing this file defends. It is recorded today in a
# comment block in the role defaults, and a comment cannot fail a pull request.
#
# WHAT THIS ASSERTS, deliberately narrow — two rules, no mandated values:
#
#   1. `goat_zenoh_tx_block_wait_before_close_us` >= lease/4. Below a quarter of
#      the 5000 ms lease, a slow-but-alive link is evicted between its own
#      keepalives: the link is healthy and we hang up on it anyway.
#
#   2. If that deadline is tightened below zenoh's own 5 s default, then
#      `goat_zenoh_rx_worker_threads` must be at least 4. This is row three
#      refusing to be re-landed.
#
# It does NOT pin the shipped pair. A goatnet may sit at stock, or choose 4
# workers instead of 8, or widen the deadline back out — those are operator
# calls with measured costs. What it refuses is the combination that measured
# worse than doing nothing.
#
# NOT a check on queue depth. `goat_zenoh_tx_queue_size` is deliberately unset
# (M-119: it bought nothing measurable and multiplies the per-link memory
# ceiling — 8 priorities x depth x 64 KiB x sessions, roughly 450 MiB extra at
# depth 8 on a 113-session router). A goatnet that sets it is making a memory
# tradeoff this script cannot evaluate, so it reports it and does not judge it.
#
# Exit 0 clean, 1 on a violation, 2 on a usage/parse error.

import sys
from pathlib import Path

try:
    import yaml
except ImportError:
    sys.stderr.write("PyYAML required: pip install pyyaml\n")
    raise SystemExit(2)

REPO = Path(__file__).resolve().parents[2]
ROLE_DEFAULTS = REPO / "ops/ansible/roles/goat-zenoh/defaults/main.yml"
GROUP_VARS = REPO / "ops/ansible/group_vars"

WAIT = "goat_zenoh_tx_block_wait_before_close_us"
WORKERS = "goat_zenoh_rx_worker_threads"
DEPTH = "goat_zenoh_tx_queue_size"

# zenoh's own defaults, i.e. what applies when we render nothing.
ZENOH_DEFAULT_WAIT_US = 5_000_000
# config.json5.j2 renders `lease: 5000` (ms). A deadline under a quarter of it
# evicts links that are still sending keepalives.
LEASE_MS = 5000
FLOOR_US = (LEASE_MS // 4) * 1000
# Below this many receive workers, one parked worker is too large a share of the
# pool for a tightened deadline to help.
MIN_WORKERS_FOR_TIGHTENED_WAIT = 4


def load(path: Path) -> dict:
    try:
        return yaml.safe_load(path.read_text()) or {}
    except Exception as exc:  # noqa: BLE001 - surfaced to the operator verbatim
        sys.stderr.write(f"could not parse {path.relative_to(REPO)}: {exc}\n")
        raise SystemExit(2)


def main() -> int:
    if not ROLE_DEFAULTS.is_file():
        sys.stderr.write(f"missing {ROLE_DEFAULTS}\n")
        return 2

    defaults = load(ROLE_DEFAULTS)
    base = {
        WAIT: defaults.get(WAIT, ZENOH_DEFAULT_WAIT_US),
        WORKERS: defaults.get(WORKERS),
        DEPTH: defaults.get(DEPTH),
    }

    # Every group that could override, plus the role default itself as the
    # baseline every unlisted group inherits.
    scopes: list[tuple[str, dict]] = [("<role default>", base)]
    if GROUP_VARS.is_dir():
        for group in sorted(p for p in GROUP_VARS.iterdir() if p.is_dir()):
            merged = dict(base)
            found = False
            for f in sorted(group.glob("*.yml")):
                data = load(f)
                for key in (WAIT, WORKERS, DEPTH):
                    if key in data:
                        merged[key] = data[key]
                        found = True
            if found:
                scopes.append((group.name, merged))

    failures: list[str] = []
    notes: list[str] = []

    for name, vals in scopes:
        wait = vals.get(WAIT)
        workers = vals.get(WORKERS)
        depth = vals.get(DEPTH)

        if wait is None:
            wait = ZENOH_DEFAULT_WAIT_US
        try:
            wait = int(wait)
        except (TypeError, ValueError):
            failures.append(f"{name}: {WAIT} is not a number: {wait!r}")
            continue

        if wait < FLOOR_US:
            failures.append(
                f"{name}: {WAIT} is {wait} us, below the lease/4 floor of "
                f"{FLOOR_US} us. At {LEASE_MS} ms lease a link sending its own "
                f"keepalives would be evicted between them — the link is alive "
                f"and we hang up on it."
            )

        tightened = wait < ZENOH_DEFAULT_WAIT_US
        if tightened:
            if workers is None:
                failures.append(
                    f"{name}: {WAIT} is tightened to {wait} us but {WORKERS} is "
                    f"unset, so zenoh's 2 receive workers apply. M-119 measured "
                    f"that pair at 42-59 s stalls and 3-7x the transport closes "
                    f"— WORSE than leaving both at stock. Raise {WORKERS} to at "
                    f"least {MIN_WORKERS_FOR_TIGHTENED_WAIT}, or widen {WAIT} "
                    f"back to {ZENOH_DEFAULT_WAIT_US}."
                )
            else:
                try:
                    w = int(workers)
                except (TypeError, ValueError):
                    failures.append(f"{name}: {WORKERS} is not a number: {workers!r}")
                    continue
                if w < MIN_WORKERS_FOR_TIGHTENED_WAIT:
                    failures.append(
                        f"{name}: {WAIT} is tightened to {wait} us with only {w} "
                        f"receive workers. M-119 measured that pair at 42-59 s "
                        f"stalls and 3-7x the transport closes — WORSE than "
                        f"leaving both at stock. Raise {WORKERS} to at least "
                        f"{MIN_WORKERS_FOR_TIGHTENED_WAIT}, or widen {WAIT} back "
                        f"to {ZENOH_DEFAULT_WAIT_US}."
                    )

        if depth is not None:
            notes.append(
                f"{name}: {DEPTH} is set to {depth}. M-119 measured no stall "
                f"improvement from depth and a per-link ceiling of 8 x depth x "
                f"64 KiB; confirm the router's memory headroom carries it."
            )

    for n in notes:
        print(f"note: {n}")

    if failures:
        print(f"FAIL: zenoh transmit-queue pairing — {len(failures)} violation(s)")
        for f in failures:
            print(f"  {f}")
        print(
            "\nContext: docs/measurements/"
            "M-119-one-slow-subscriber-stalls-the-zenoh-router.md"
        )
        return 1

    print(
        f"ok: zenoh transmit-queue pairing — {len(scopes)} scope(s) checked; "
        f"every tightened deadline carries >= {MIN_WORKERS_FOR_TIGHTENED_WAIT} "
        f"receive workers and stays above the {FLOOR_US} us lease/4 floor"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
