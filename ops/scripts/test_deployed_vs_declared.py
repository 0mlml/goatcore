#!/usr/bin/env python3
"""Tests for deployed-vs-declared.py — synthetic pin/ledger/docker-inspect
fixtures driving the pure grading decision, no host needed.

#1964: the script graded `ok`/`DRIFT` purely from the declared pin vs the
ledger tip's own claimed ref, and only used the running-container check to
answer "is anything by this name here at all" (a loose name/tag/id match).
That falsifier is satisfied by a DIFFERENT BUILD running under the same
image name, so declared A / ledger tip A / host actually running B read as
`ok` — the ledger's self-report, never contradicted by the one thing that
could contradict it. These tests fix a concrete A/A/B fixture to that case,
plus the matching/unreadable/pending/multiple-container cases around it.

Run: python3 ops/scripts/test_deployed_vs_declared.py
"""
from __future__ import annotations

import importlib.util
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("deployed_vs_declared", HERE / "deployed-vs-declared.py")
dvd = importlib.util.module_from_spec(spec)
spec.loader.exec_module(dvd)

A = "ghcr.io/usareur-af/goat-id-console:sha-aaa@sha256:" + "a" * 64
B = "ghcr.io/usareur-af/goat-id-console:sha-bbb@sha256:" + "b" * 64


def tip(ref: str, action: str = "deploy", artifact_type: str = "image") -> dict:
    return {"ref": ref, "action": action, "artifact_type": artifact_type}


def detail_running(ref: str) -> list[dict]:
    """A single running container whose image id resolves to one RepoDigest
    matching `ref`'s digest."""
    config_image, _, image_id = ref.partition("@")
    return [{"config_image": config_image, "image_id": "sha256:localimageid",
             "repo_digests": [ref]}]


class GradeComponent(unittest.TestCase):
    def test_declared_a_ledger_a_running_b_is_running_drift(self):
        # The #1964 regression case: the pin and the ledger tip agree (both
        # A), so the ledger-only comparison says `ok`. The host is actually
        # running B under the same image name. Before this fix, the loose
        # name-only falsifier matched B's bare name against A's bare name
        # and returned True ("something is running"), so the row stayed
        # `ok`. The exact-digest observation must catch it.
        live = [B]  # loose list still finds SOMETHING by this name
        running_digs = [{dvd.digest_of(B)}]
        state = dvd.grade_component(A, tip(A), live, running_digs)
        self.assertEqual(state, "RUNNING DRIFT")

    def test_declared_a_ledger_a_running_a_is_ok(self):
        live = [A]
        running_digs = [{dvd.digest_of(A)}]
        state = dvd.grade_component(A, tip(A), live, running_digs)
        self.assertEqual(state, "ok")

    def test_declared_a_ledger_b_is_drift_regardless_of_running(self):
        # The ledger itself disagrees with the pin — already correctly
        # flagged without needing the running-digest observation at all.
        live = [B]
        running_digs = [{dvd.digest_of(B)}]
        state = dvd.grade_component(A, tip(B), live, running_digs)
        self.assertEqual(state, "DRIFT")

    def test_nothing_running_by_this_name_is_ledger_only_not_running(self):
        live: list[str] = []
        state = dvd.grade_component(A, tip(A), live, running_digs=[])
        self.assertEqual(state, "LEDGER ONLY, NOT RUNNING")

    def test_unreadable_docker_does_not_invent_running_drift(self):
        # live=None (docker ps unreadable) and running_digs=None (image
        # inspect unreadable) must never be read as "confirmed wrong" —
        # "I could not look" stays distinct from "I looked and it differs".
        state = dvd.grade_component(A, tip(A), live=None, running_digs=None)
        self.assertEqual(state, "RUNNING UNCONFIRMED")

    def test_running_digest_unreadable_but_live_list_confirms_name(self):
        # docker ps worked (live has a name match) but the per-image digest
        # inspect did not (running_digs=None): report unconfirmed, not drift
        # or a successful measurement.
        live = [A]
        state = dvd.grade_component(A, tip(A), live, running_digs=None)
        self.assertEqual(state, "RUNNING UNCONFIRMED")

    def test_pending_ledger_tip_is_unconfirmed_even_if_ref_matches(self):
        live = [A]
        state = dvd.grade_component(A, tip(A, action="pending"), live, running_digs=[])
        self.assertEqual(state, "LEDGER PENDING, UNCONFIRMED")

    def test_pending_ledger_tip_confirmed_ok_by_direct_observation(self):
        live = [A]
        running_digs = [{dvd.digest_of(A)}]
        state = dvd.grade_component(A, tip(A, action="pending"), live, running_digs)
        self.assertEqual(state, "ok")

    def test_no_ledger_directory_is_not_on_this_host(self):
        state = dvd.grade_component(A, None, live=[A], running_digs=[{dvd.digest_of(A)}])
        self.assertEqual(state, "NOT ON THIS HOST")

    def test_shared_ledger_tracking_a_different_artifact(self):
        other = "oryd/kratos:v1.3.1@sha256:" + "c" * 64
        state = dvd.grade_component(A, tip(other), live=[other], running_digs=[])
        self.assertEqual(state, "LEDGER TRACKS kratos")

    def test_pin_with_no_digest_is_indeterminate(self):
        state = dvd.grade_component("ghcr.io/usareur-af/goat-id-console:latest",
                                     tip(A), live=[A], running_digs=[])
        self.assertEqual(state, "INDETERMINATE")

    def test_floating_tag_on_host_is_unpinned(self):
        floating = "ghcr.io/usareur-af/goat-id-console:sha-aaa"  # no digest
        state = dvd.grade_component(A, tip(floating), live=[floating], running_digs=[])
        self.assertEqual(state, "UNPINNED ON HOST")

    def test_non_image_artifact_skips_the_running_falsifiers(self):
        # A package/kernel-module ledger row (ADR 1060) has no container to
        # look for; it keeps its ledger-only grading even with nothing live.
        state = dvd.grade_component(A, tip(A, artifact_type="script"), live=[], running_digs=[])
        self.assertEqual(state, "ok")

    def test_mixed_replica_digests_cannot_be_confirmed(self):
        for action in ("deploy", "pending"):
            evidence = dvd.running_digest_sets(detail_running(A) + detail_running(B), "goat-id-console")
            self.assertEqual(dvd.grade_component(A, tip(A, action), [A, B], evidence), "RUNNING DRIFT")

    def test_one_unknown_replica_cannot_be_confirmed(self):
        self.assertEqual(dvd.grade_component(A, tip(A), [A], [{dvd.digest_of(A)}, set()]), "RUNNING UNCONFIRMED")

    def test_aliases_on_one_image_are_not_mixed_replicas(self):
        self.assertEqual(dvd.grade_component(A, tip(A), [A], [{dvd.digest_of(A), dvd.digest_of(B)}]), "ok")


class RunningDigestSet(unittest.TestCase):
    def test_unreadable_detail_is_none_not_empty(self):
        self.assertIsNone(dvd.running_digest_sets(None, "goat-id-console"))

    def test_no_container_by_this_name_is_empty_set_not_none(self):
        detail = detail_running(A)  # named goat-id-console
        self.assertEqual(dvd.running_digest_sets(detail, "some-other-component"), [])

    def test_multiple_containers_keep_separate_digest_evidence(self):
        detail = detail_running(A) + detail_running(B)
        got = dvd.running_digest_sets(detail, "goat-id-console")
        self.assertEqual(got, [{dvd.digest_of(A)}, {dvd.digest_of(B)}])

    def test_container_with_no_repo_digests_remains_unknown(self):
        detail = [{"config_image": "ghcr.io/usareur-af/goat-id-console:local",
                   "image_id": "sha256:x", "repo_digests": []}]
        self.assertEqual(dvd.running_digest_sets(detail, "goat-id-console"), [set()])


class RunningImageDigestsParsing(unittest.TestCase):
    """running_image_digests() does its own ssh() call and parsing; cover
    the parsing half (rc/first-line contract, tab join, bad JSON) by
    stubbing ssh() rather than a live host."""

    def test_unreadable_host_returns_none(self):
        dvd.ssh = lambda host, port, remote, become: (7, "")
        self.assertIsNone(dvd.running_image_digests("h", 0, None))

    def test_parses_one_line_per_container(self):
        out = "OK\n" + f"{A}|sha256:local1\t[\"{B}\"]\n"
        dvd.ssh = lambda host, port, remote, become: (0, out)
        detail = dvd.running_image_digests("h", 0, None)
        self.assertEqual(len(detail), 1)
        self.assertEqual(detail[0]["config_image"], A)
        self.assertEqual(detail[0]["image_id"], "sha256:local1")
        self.assertEqual(detail[0]["repo_digests"], [B])

    def test_malformed_json_digest_list_is_unavailable_not_a_crash(self):
        out = "OK\n" + f"{A}|sha256:local1\tnot-json\n"
        dvd.ssh = lambda host, port, remote, become: (0, out)
        detail = dvd.running_image_digests("h", 0, None)
        self.assertIsNone(detail)


if __name__ == "__main__":
    unittest.main()
