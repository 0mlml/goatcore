#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
#
# Tests for the Python peer→person client.
#
# WHAT IS PINNED HERE. Not the HTTP call — that is I/O and mocking it tests the
# mock. What is pinned is the READING of the wire format, because that reading
# is the entire reason this module exists: the endpoint says `resolved: false`
# for two completely different situations (a handle that matched no roster
# member, and a handle that matched one but reconciled to no live identity), and
# it cannot say "we could not ask" at all. Every consumer that gets those
# distinctions wrong renders a blank that a reader takes for "unowned".
#
# Run: python3 ops/identity/test_peerowner.py

import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import peerowner as po  # noqa: E402


class TestReadWireFormat(unittest.TestCase):

    def test_a_live_identity_earns_the_recorded_grade(self):
        row = po._read({"netbird_name": "peer-a-zagros-2026-06-10-x", "resolved": True,
                        "handle": "a", "org": "acme", "status": "active",
                        "person": {"ory_identity_id": "ory-1", "email": "a@x",
                                   "name": "A Person"},
                        "deep_links": {"id_console_user": "/users/ory-1"}}, False)
        self.assertEqual(row["attribution"], po.REGISTRY)
        self.assertEqual(row["ory_id"], "ory-1")
        self.assertEqual(row["id_console_path"], "/users/ory-1")

    def test_pii_is_dropped_unless_asked_for(self):
        # The endpoint is operator-scoped and returns an email and a display
        # name. A surface that renders handles must not leak a roster of email
        # addresses into a served json by forgetting to strip a field, so the
        # default is to drop them here rather than at each call site.
        wire = {"resolved": True, "handle": "a",
                "person": {"ory_identity_id": "ory-1", "email": "a@x", "name": "A"}}
        self.assertNotIn("email", po._read(wire, False))
        self.assertEqual(po._read(wire, True)["email"], "a@x")

    def test_handle_without_live_identity_is_a_lead_not_a_record(self):
        row = po._read({"resolved": False, "handle": "a", "org": "acme",
                        "reason": "handle resolved to org acme but no live Ory "
                                  "identity matched the roster email-hash"}, False)
        self.assertEqual(row["attribution"], po.NAME)
        self.assertEqual(row["handle"], "a")
        self.assertIsNone(row["ory_id"])

    def test_no_handle_is_unmatched_not_a_failure(self):
        row = po._read({"resolved": False,
                        "reason": "no roster handle matched this peer name"}, False)
        self.assertEqual(row["attribution"], po.UNMATCHED)
        self.assertIsNone(row["handle"])

    def test_a_missing_row_is_unread_not_unmatched(self):
        # The two are one character apart on screen and opposite in meaning:
        # one is about the peer, the other about our ability to look.
        self.assertEqual(po._read(None, False)["attribution"], po.UNREAD)

    def test_resolved_true_without_an_ory_id_does_not_earn_recorded(self):
        # Defensive: the grade is the LIVE identity, not the flag. A future
        # server that sets resolved without a person must not silently promote
        # a lead to a record.
        row = po._read({"resolved": True, "handle": "a", "person": {}}, False)
        self.assertEqual(row["attribution"], po.NAME)

    def test_a_machine_block_makes_the_handle_a_custodian(self):
        # The resolver names a portal-enrolled machine's custodian in `handle`
        # and says so in `machine`. Reading `handle` alone would present the
        # custodian as the machine's owner with no sign it is a custody edge.
        row = po._read({"resolved": True, "handle": "a", "org": "acme",
                        "person": {"ory_identity_id": "ory-1"},
                        "machine": {"system_handle": "box-1", "node_class": "FUSION"}},
                       False)
        self.assertEqual(row["attribution"], po.CUSTODY)
        self.assertEqual(row["handle"], "a")
        self.assertEqual(row["custody_of"], "box-1")
        self.assertEqual(row["ory_id"], "ory-1")

    def test_a_machine_with_no_custodian_is_unmatched_and_still_names_it(self):
        row = po._read({"resolved": False, "machine": {"system_handle": "box-1"},
                        "reason": "machine box-1 is enrolled, but its custodian "
                                  "matches no declared roster member"}, False)
        self.assertEqual(row["attribution"], po.UNMATCHED)
        self.assertIsNone(row["handle"])
        self.assertEqual(row["custody_of"], "box-1")


class TestResolveNeverRaises(unittest.TestCase):
    """Ownership is an enrichment. A resolver that is down, unset or slow must
    never take down the surface that asked — every name still comes back."""

    def test_unconfigured_returns_a_row_per_name(self):
        rows = po.resolve("", "", ["a", "b"])
        self.assertEqual(set(rows), {"a", "b"})
        for r in rows.values():
            self.assertEqual(r["attribution"], po.UNREAD)
            self.assertIn("portal", r["reason"])

    def test_missing_key_is_named_as_such(self):
        rows = po.resolve("http://127.0.0.1:1", "", ["a"])
        self.assertIn("API key", rows["a"]["reason"])

    def test_unreachable_portal_is_unread_not_an_exception(self):
        # Port 1 refuses immediately on every platform this runs on.
        rows = po.resolve("http://127.0.0.1:1", "k", ["a"], timeout=2)
        self.assertEqual(rows["a"]["attribution"], po.UNREAD)
        self.assertIn("unreachable", rows["a"]["reason"])

    def test_empty_and_duplicate_names_are_handled_without_a_call(self):
        self.assertEqual(po.resolve("", "", []), {})
        rows = po.resolve("", "", ["a", "a", " ", "a "])
        self.assertEqual(list(rows), ["a"])

    def test_every_state_has_an_explanation(self):
        for a in po.ATTRIBUTIONS:
            self.assertTrue(po.EXPLAIN[a], a)


if __name__ == "__main__":
    unittest.main(verbosity=2)
