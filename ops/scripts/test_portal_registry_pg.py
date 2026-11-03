#!/usr/bin/env python3
"""Tests for portal_registry_pg.py and the Postgres path of its callers.

No database is needed: subprocess.run is replaced by a fake psql.

Run: python3 ops/scripts/test_portal_registry_pg.py
"""
from __future__ import annotations

import importlib.util
import io
import json
import sys
import unittest
import unittest.mock
from contextlib import redirect_stderr
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import portal_registry_pg as prp  # noqa: E402


def load(name: str, path: Path):
    spec = importlib.util.spec_from_file_location(name, path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


reap = load("reap_orphans", HERE / "reap-orphans.py")
sync = load("sync_slot_anchors", HERE / "sync-slot-anchors.py")
backfill = load("slot_identity_backfill",
                HERE.parent / "goat-id-console" / "scripts" / "slot-identity-backfill.py")


def fake_psql(rows=None, rc=0, stdout=None):
    calls = []

    def run(argv, input=None, **kw):
        calls.append((argv, input))
        out = stdout if stdout is not None else json.dumps(rows or [])
        return unittest.mock.Mock(returncode=rc, stdout=out, stderr="" if rc == 0 else "boom")
    return calls, run


class TestQuery(unittest.TestCase):
    def test_read_only_session_and_bound_parameters(self):
        calls, run = fake_psql([{"slot_id": "a", "n": None}])
        with unittest.mock.patch.object(prp.subprocess, "run", side_effect=run):
            rows = prp.query("SELECT slot_id FROM slots WHERE slot_id = :'s'", {"s": "x'; DROP"})
        self.assertEqual(rows, [{"slot_id": "a", "n": None}])
        argv, stmt = calls[0]
        self.assertEqual(argv[:3], ["docker", "exec", "-i"])
        self.assertIn("PGOPTIONS=-c default_transaction_read_only=on", argv)
        self.assertEqual(argv[argv.index("-U") + 1], "portal")
        self.assertEqual(argv[argv.index("-d") + 1], "portal_registry")
        self.assertIn("s=x'; DROP", argv)
        self.assertNotIn("DROP", stmt)
        self.assertTrue(stmt.startswith("SELECT coalesce(json_agg(t)"))

    def test_ssh_wraps_the_same_command(self):
        calls, run = fake_psql([])
        with unittest.mock.patch.object(prp.subprocess, "run", side_effect=run):
            prp.query("SELECT 1 AS x", ssh="mgmt-host")
        argv, _ = calls[0]
        self.assertEqual(argv[:2], ["ssh", "mgmt-host"])
        self.assertTrue(argv[2].startswith("sudo -n docker exec -i -e "))
        self.assertIn("'PGOPTIONS=-c default_transaction_read_only=on'", argv[2])

    def test_failures_are_unreadable_not_empty(self):
        for kw in ({"rc": 2}, {"stdout": "not json"}, {"stdout": "{}"}):
            _, run = fake_psql(**kw)
            with unittest.mock.patch.object(prp.subprocess, "run", side_effect=run):
                with self.assertRaises(prp.RegistryUnreadable, msg=str(kw)):
                    prp.query("SELECT 1 AS x")
        with unittest.mock.patch.object(prp.subprocess, "run", side_effect=FileNotFoundError("docker")):
            with self.assertRaises(prp.RegistryUnreadable):
                prp.query("SELECT 1 AS x")


class TestCallers(unittest.TestCase):
    def test_reap_orphans_reads_slots(self):
        slots = [{"slot_id": "s1", "assigned_to": "alice", "assigned_at": 1, "cert_minted_at": 2}]
        calls, run = fake_psql(slots)
        with unittest.mock.patch.object(prp.subprocess, "run", side_effect=run):
            self.assertEqual(reap.read_slots_pg({"container": "c", "db": "d", "user": "u", "ssh": ""}), slots)
        self.assertIn("revoked_at IS NULL", calls[0][1])

    def test_sync_slot_anchors_sorted_and_unreadable_is_exit_2(self):
        _, run = fake_psql([{"slot_id": "b"}, {"slot_id": "a"}])
        with unittest.mock.patch.object(prp.subprocess, "run", side_effect=run):
            self.assertEqual(sync.live_anchors_pg({}), ["a", "b"])
        _, run = fake_psql(rc=2)
        with unittest.mock.patch.object(prp.subprocess, "run", side_effect=run), redirect_stderr(io.StringIO()):
            with self.assertRaises(SystemExit) as cm:
                sync.live_anchors_pg({})
        self.assertEqual(cm.exception.code, 2, "an unreadable registry is never a drift verdict")

    def test_slot_identity_backfill_reads_unlinked_slots(self):
        calls, run = fake_psql([{"slot_id": "s1", "assigned_to": "alice"}])
        with unittest.mock.patch.object(prp.subprocess, "run", side_effect=run):
            rows = backfill.unlinked_slots_pg({})
        self.assertEqual(rows, [{"slot_id": "s1", "assigned_to": "alice"}])
        self.assertIn("cert_minted_at IS NOT NULL", calls[0][1])


if __name__ == "__main__":
    unittest.main()
