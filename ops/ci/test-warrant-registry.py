#!/usr/bin/env python3
"""
Negative tests for the warrant registry gate — every check must refuse something.

A guard that passes but never fails is worthless, and this one guards the
cutover from a hand-written mesh ACL to a generated one. So each of the five
checks in ops/ci/check-warrant-registry.py gets a mutation here that it is
supposed to catch, and the test fails if the guard stays green.

The byte-identical check earns two: one proving a changed PORT is caught (the
obvious case) and one proving a changed RULE SHAPE is caught (the case that
would actually change a goatnet's posture at cutover).

Each test copies the registry to a temp dir, mutates the copy, and runs the
guard against it with --policy-dir. The real registry is never touched.

Usage:  python3 ops/ci/test-warrant-registry.py
"""

from __future__ import annotations

import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
WARRANT_DIR = REPO_ROOT / "ops" / "warrant"
GUARD = REPO_ROOT / "ops" / "ci" / "check-warrant-registry.py"

FAILURES: list[str] = []


def run_guard(policy_dir: Path, *extra: str) -> tuple[int, str]:
    proc = subprocess.run(
        [sys.executable, str(GUARD), "--policy-dir", str(policy_dir), *extra],
        capture_output=True,
        text=True,
        cwd=str(REPO_ROOT),
    )
    return proc.returncode, proc.stdout + proc.stderr


def with_registry(fn):
    """Run fn against a throwaway copy of the registry."""
    with tempfile.TemporaryDirectory() as tmp:
        dest = Path(tmp) / "warrant"
        shutil.copytree(WARRANT_DIR, dest)
        return fn(dest)


def expect_refusal(label: str, marker: str, mutate, *extra: str) -> None:
    """The guard must fail, and its output must name the right thing."""

    def run(policy_dir: Path) -> tuple[int, str]:
        mutate(policy_dir)
        return run_guard(policy_dir, *extra)

    rc, out = with_registry(run)
    if rc == 0:
        FAILURES.append(f"{label}: guard PASSED on a registry it should refuse")
    elif marker.lower() not in out.lower():
        FAILURES.append(
            f"{label}: guard failed (good) but never mentioned '{marker}'.\n"
            f"    A refusal an operator cannot act on is half a guard.\n"
            f"    got: {out.strip()[:400]}"
        )
    else:
        print(f"  ok  {label}")


def sub(path: Path, old: str, new: str) -> None:
    text = path.read_text()
    if old not in text:
        raise AssertionError(f"test fixture drifted: {old!r} not in {path}")
    path.write_text(text.replace(old, new, 1))


def main() -> int:
    print("warrant guard — negative tests")

    # 1a. a changed PORT must be caught
    expect_refusal(
        "a port changed in the vocabulary is caught",
        "byte-identical",
        lambda d: sub(d / "port-sets.yaml", '"5353", "7447"', '"5353", "7999"'),
    )

    # 1b. a changed RULE SHAPE must be caught — the case that would actually
    #     move a goatnet's posture. Widening a destination list is the shape of
    #     an accidental over-grant.
    expect_refusal(
        "a widened destination list is caught",
        "byte-identical",
        lambda d: sub(
            d / "rule-intents" / "netbird-acl.yaml",
            "        sources: [QUARANTINED]",
            "        sources: [QUARANTINED, DEVELOPER]",
        ),
    )

    # 2. the vocabulary must stay unambiguous
    expect_refusal(
        "two names for one (protocol, ports) shape is caught",
        "re-fragmented",
        lambda d: (d / "port-sets.yaml").write_text(
            (d / "port-sets.yaml").read_text()
            + '\n  - name: shell-again\n'
            + '    purpose: A duplicate of shell, which must not be allowed to exist.\n'
            + '    owner: platform-admin\n'
            + '    members:\n'
            + '      - { protocol: tcp, ports: ["22"] }\n'
        ),
    )

    # 3. a set with no stated purpose is caught — `purpose` is the thing that
    #    currently exists in drifting copies, so an empty one defeats the point.
    expect_refusal(
        "a port-set with no purpose is caught",
        "purpose",
        # Empty the purpose while keeping the file valid YAML — the guard must
        # refuse on the empty purpose itself, not incidentally on a parse error.
        lambda d: sub(
            d / "port-sets.yaml",
            "    purpose: >\n      Interactive login — tcp/22, and nothing else.",
            '    purpose: ""\n    _former_purpose: >\n      Interactive login — tcp/22, and nothing else.',
        ),
    )

    # 4. required_by must actually be satisfied
    expect_refusal(
        "a required_by pair nothing grants is caught",
        "required_by",
        lambda d: sub(
            d / "port-sets.yaml",
            "      - { src: NETWORK, dst: INFRA }",
            "      - { src: NETWORK, dst: INFRA }\n      - { src: NOBODY, dst: NOWHERE }",
        ),
    )

    # 5a. an expired grant is caught
    expect_refusal(
        "a past-due expires: is caught",
        "expired on",
        lambda d: sub(
            d / "rule-intents" / "netbird-acl.yaml",
            '      - name: "breakglass → infra (ssh only)"',
            '      - name: "breakglass → infra (ssh only)"\n'
            "        expires: 2020-01-01\n"
            "        owner: platform-admin",
        ),
    )

    # 5b. an expiring grant with nobody to ask is caught
    expect_refusal(
        "expires: without owner: is caught",
        "no `owner:`",
        lambda d: sub(
            d / "rule-intents" / "netbird-acl.yaml",
            '      - name: "breakglass → infra (ssh only)"',
            '      - name: "breakglass → infra (ssh only)"\n'
            "        expires: 2099-01-01",
        ),
    )

    # 5c. and the expiry clock must be real: the same grant, not yet due, passes.
    #     Without this, "expired" could be firing on the presence of the field.
    def not_yet_due(policy_dir: Path):
        sub(
            policy_dir / "rule-intents" / "netbird-acl.yaml",
            '      - name: "breakglass → infra (ssh only)"',
            '      - name: "breakglass → infra (ssh only)"\n'
            "        expires: 2030-06-01\n"
            "        owner: platform-admin",
        )
        return run_guard(policy_dir, "--today", "2030-05-31")

    rc, out = with_registry(not_yet_due)
    if rc != 0:
        FAILURES.append(
            "a not-yet-due expires: was refused — the check is firing on the "
            f"field's presence, not on the date.\n    got: {out.strip()[:400]}"
        )
    else:
        print("  ok  a not-yet-due expires: still passes")

    if FAILURES:
        print("\nwarrant guard negative tests: FAIL", file=sys.stderr)
        for f in FAILURES:
            print(f"  - {f}", file=sys.stderr)
        return 1
    print("\nwarrant guard negative tests: OK — every check refuses something")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
