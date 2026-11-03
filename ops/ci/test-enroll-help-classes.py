#!/usr/bin/env python3
"""Negative tests for check-enroll-help-classes.py.

A guard that cannot fail proves nothing. test_catches_the_real_regression
replays the --node-class help as it shipped before #1400 and requires the guard
to reject it; the other cases each break one rule of the current help.
"""

import importlib.util
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent
_spec = importlib.util.spec_from_file_location("guard", HERE / "check-enroll-help-classes.py")
guard = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(guard)

GO = (ROOT / guard.MACHINES_GO).read_text()
RS = (ROOT / guard.MACHINE_RS).read_text()

# The help text as it stood before #1400.
PRE_1400 = """
        /// Node-class: SENSOR / EFFECTOR / PROCESSOR / PLATFORM (function,
        /// self-service); MGMT-HOST / ZENOH / SNITCH / SERVICES (infrastructure,
        /// operator-only); or NETWORK (bearer gear — routers/switches/radios;
        /// operator, or a custodian holding the network-admin entitlement for
        /// its own site, ADR 1113). Case-insensitive; normalized to uppercase.
        #[arg(long, required_unless_present = "bulk", conflicts_with = "bulk")]
        node_class: Option<String>,
"""


def test_current_help_passes():
    assert guard.check(RS, GO) == []


def test_catches_the_real_regression():
    errors = guard.check(PRE_1400, GO)
    assert any("SENSOR-FEED" in e for e in errors), errors
    assert any("NETWORK-JUMP" in e for e in errors), errors
    assert any(e.startswith("SENSOR is named") for e in errors), errors


def test_reserved_class_offered_as_self_service():
    bad = RS.replace("Custodian self-service: SENSOR-FEED", "Custodian self-service: SENSOR, SENSOR-FEED", 1)
    assert bad != RS
    assert any("offers SENSOR as self-service" in e for e in guard.check(bad, GO))


def test_unmapped_class_offered_as_self_service():
    bad = RS.replace("Custodian self-service: SENSOR-FEED", "Custodian self-service: PROCESSOR, SENSOR-FEED", 1)
    assert any("offers PROCESSOR as self-service" in e for e in guard.check(bad, GO))


def test_gear_class_dropped_from_help():
    bad = RS.replace("FUSION, CONTROL;", "CONTROL;", 1)
    assert bad != RS
    assert any("FUSION is missing" in e for e in guard.check(bad, GO))


def test_pod_or_moon_dropped_from_help():
    for node_class in ("POD", "MOON"):
        bad = RS.replace(f"{node_class} / ", "", 1)
        assert bad != RS
        assert any(f"{node_class} is missing" in e for e in guard.check(bad, GO))


def test_infra_class_dropped_from_help():
    bad = RS.replace(" / BREAKGLASS /", " /", 1)
    assert bad != RS
    assert any("BREAKGLASS is not named" in e for e in guard.check(bad, GO))


def main() -> int:
    tests = [v for k, v in sorted(globals().items()) if k.startswith("test_")]
    failed = 0
    for t in tests:
        try:
            t()
            print(f"ok   {t.__name__}")
        except AssertionError as e:
            failed += 1
            print(f"FAIL {t.__name__}: {e}", file=sys.stderr)
    print(f"{len(tests) - failed}/{len(tests)} passed")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
