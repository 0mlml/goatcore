#!/usr/bin/env python3
"""Standalone tests for gen-platform-spire-entries.py (ADR 1098). Run: python3 this.

Exercises the network-free core (platform_entries + to_yaml). No portal needed."""

import importlib.util
import os
import sys

_here = os.path.dirname(os.path.abspath(__file__))
_spec = importlib.util.spec_from_file_location(
    "gen_platform_spire_entries", os.path.join(_here, "gen-platform-spire-entries.py")
)
gen = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(gen)

FAILURES = []


def check(cond, msg):
    if not cond:
        FAILURES.append(msg)


# A fleet: one PLATFORM with three workloads, one PLATFORM with none, and a
# non-PLATFORM machine — only the first contributes entries.
MACHINES = [
    {
        "system_handle": "uas-1",
        "slot_id": "a1b2c3d4e5f60718293a4b5c6d7e8f90",
        "node_class": "PLATFORM",
        "workloads": ["SENSOR", "EFFECTOR", "PROCESSOR"],
    },
    {"system_handle": "uas-2", "slot_id": "bbbb", "node_class": "PLATFORM", "workloads": []},
    {"system_handle": "sensor-x", "slot_id": "cccc", "node_class": "SENSOR", "workloads": []},
]

entries = gen.platform_entries(MACHINES)

# Exactly the three workloads of the one composed platform.
check(len(entries) == 3, f"want 3 entries, got {len(entries)}")

# CN is the platform's slot anchor (compose-by-CN); spiffe_id is <handle>/<function>.
by_id = {e["spiffe_id"]: e for e in entries}
check("uas-1/sensor" in by_id, "missing uas-1/sensor spiffe_id")
check(
    all(e["cn_dns_name"] == "a1b2c3d4e5f60718293a4b5c6d7e8f90" for e in entries),
    "all workload CNs must be the platform's slot anchor",
)
check(by_id["uas-1/sensor"]["workload_class"] == "SENSOR", "workload_class not preserved")

# Selector is deploy-time — never guessed here.
check(all(e["selector"] is None for e in entries), "selector must be None (deploy-time)")

# Deterministic ordering (sorted by platform, spiffe_id).
ids = [e["spiffe_id"] for e in entries]
check(ids == sorted(ids), f"entries must be sorted: {ids}")

# YAML renders the NEEDS_DEPLOY_SELECTOR marker + the group_var key.
y = gen.to_yaml(entries)
check("spire_platform_workload_entries:" in y, "missing group_var key")
check("NEEDS_DEPLOY_SELECTOR" in y, "missing deploy-selector marker")
check('spiffe_id: "uas-1/sensor"' in y, "spiffe_id not rendered")

# Empty fleet → an explicit empty list, not a crash.
empty = gen.to_yaml(gen.platform_entries([]))
check("spire_platform_workload_entries: []" in empty, "empty fleet must render []")

if FAILURES:
    for f in FAILURES:
        print("FAIL:", f)
    sys.exit(1)
print("ok: gen-platform-spire-entries (all checks passed)")
