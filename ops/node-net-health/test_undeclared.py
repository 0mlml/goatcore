#!/usr/bin/env python3
"""Local alert persistence: never checkpoint an event ahead of its output."""
import contextlib
import importlib.util
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("nnh_undeclared", HERE / "nnh-snapshot.py")
nnh = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(nnh)


def conn(remote="198.51.100.1:443", bucket="undeclared"):
    return {"bucket": bucket, "proto": "tcp", "state": "estab",
            "local_addr": "192.0.2.1:50000", "remote_addr": remote,
            "pid": 123, "comm": "fixture", "container": "",
            "first_seen_unix_ms": 0, "last_seen_unix_ms": 0}


class UndeclaredTests(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.root = Path(tmp.name)
        self.state = self.root / "state.json"
        self.tail = self.root / "events.ndjson"
        for name, value in (("UNDECLARED_STATE", str(self.state)),
                            ("UNDECLARED_TAIL", str(self.tail))):
            self.enterContext(patch.object(nnh, name, value))
        self.clock = self.enterContext(patch.object(nnh, "now_ms", return_value=1000))

    def events(self):
        return [json.loads(line) for line in self.tail.read_text().splitlines()]

    def test_normal_delivery_then_repeat_preserves_first_seen(self):
        first = conn()
        self.assertEqual(nnh.track_undeclared([first, conn(bucket="mesh")]), 1)
        event = self.events()[0]
        self.assertEqual(event["event"], "net_undeclared_conn")
        self.assertEqual(event["remote_addr"], first["remote_addr"])
        self.assertEqual(event["ts_unix_ms"], 1000)
        self.assertEqual(self.tail.stat().st_mode & 0o777, 0o600)
        self.clock.return_value = 2000
        again = conn()
        self.assertEqual(nnh.track_undeclared([again]), 1)
        self.assertEqual(len(self.events()), 1)
        self.assertEqual(again["first_seen_unix_ms"], 1000)
        self.assertEqual(again["last_seen_unix_ms"], 2000)
        self.assertEqual(next(iter(json.loads(self.state.read_text()).values())),
                         {"first_seen_unix_ms": 1000, "last_seen_unix_ms": 2000})

    def test_append_failure_preserves_checkpoint_and_retry_emits_missing_event(self):
        nnh.track_undeclared([conn()])
        checkpoint = self.state.read_bytes()
        real_open = open

        def fail_append(path, *args, **kwargs):
            if str(path) == str(self.tail):
                raise OSError("synthetic append failure")
            return real_open(path, *args, **kwargs)

        self.clock.return_value = 2000
        with patch("builtins.open", side_effect=fail_append):
            with self.assertRaisesRegex(OSError, "append failure"):
                nnh.track_undeclared([conn(), conn("198.51.100.2:443")])
        self.assertEqual(self.state.read_bytes(), checkpoint)
        self.assertEqual(len(self.events()), 1)
        nnh.track_undeclared([conn(), conn("198.51.100.2:443")])
        self.assertEqual([e["remote_addr"] for e in self.events()],
                         ["198.51.100.1:443", "198.51.100.2:443"])

    def test_partial_batch_failure_retries_without_suppressing_missing_events(self):
        real_open = open

        @contextlib.contextmanager
        def partial_append():
            with real_open(self.tail, "ab+") as fh:
                class PartialWriter:
                    writes = 0

                    def __getattr__(self, name):
                        return getattr(fh, name)

                    def write(self, data):
                        self.writes += 1
                        if self.writes == 2:
                            raise OSError("synthetic second write failure")
                        return fh.write(data)
                yield PartialWriter()

        def opening(path, *args, **kwargs):
            return partial_append() if str(path) == str(self.tail) else real_open(path, *args, **kwargs)

        with patch("builtins.open", side_effect=opening):
            with self.assertRaisesRegex(OSError, "second write failure"):
                nnh.track_undeclared([conn(), conn("198.51.100.2:443")])
        self.assertFalse(self.state.exists())
        self.assertEqual(len(self.events()), 1)
        nnh.track_undeclared([conn(), conn("198.51.100.2:443")])
        self.assertEqual([e["remote_addr"] for e in self.events()],
                         ["198.51.100.1:443", "198.51.100.1:443", "198.51.100.2:443"])

    def test_close_failure_does_not_checkpoint_event(self):
        real_open = open

        @contextlib.contextmanager
        def failed_close():
            with real_open(self.tail, "ab+") as fh:
                yield fh
            raise OSError("synthetic close failure")

        def opening(path, *args, **kwargs):
            return failed_close() if str(path) == str(self.tail) else real_open(path, *args, **kwargs)

        with patch("builtins.open", side_effect=opening):
            with self.assertRaisesRegex(OSError, "close failure"):
                nnh.track_undeclared([conn()])
        self.assertFalse(self.state.exists())
        nnh.track_undeclared([conn()])
        self.assertEqual(len(self.events()), 2)

    def test_retry_separates_a_partially_written_record(self):
        real_open = open

        @contextlib.contextmanager
        def partial_record():
            with real_open(self.tail, "ab+") as fh:
                class PartialWriter:
                    def __getattr__(self, name):
                        return getattr(fh, name)

                    def write(self, data):
                        fh.write(data[:10])
                        raise OSError("synthetic partial record")
                yield PartialWriter()

        def opening(path, *args, **kwargs):
            return partial_record() if str(path) == str(self.tail) else real_open(path, *args, **kwargs)

        with patch("builtins.open", side_effect=opening):
            with self.assertRaisesRegex(OSError, "partial record"):
                nnh.track_undeclared([conn()])
        self.assertFalse(self.state.exists())
        nnh.track_undeclared([conn()])
        lines = self.tail.read_text().splitlines()
        self.assertEqual(len(lines), 2)
        self.assertEqual(json.loads(lines[1])["remote_addr"], "198.51.100.1:443")
        with self.assertRaises(json.JSONDecodeError):
            json.loads(lines[0])
        self.assertTrue(self.state.exists())

    def test_permission_failure_does_not_checkpoint_event(self):
        real_chmod = nnh.os.chmod

        def fail_tail_mode(path, mode):
            if str(path) == str(self.tail):
                raise OSError("synthetic permission failure")
            return real_chmod(path, mode)

        with patch.object(nnh.os, "chmod", side_effect=fail_tail_mode):
            with self.assertRaisesRegex(OSError, "permission failure"):
                nnh.track_undeclared([conn()])
        self.assertFalse(self.state.exists())
        nnh.track_undeclared([conn()])
        self.assertEqual(len(self.events()), 2)

    def test_checkpoint_failure_happens_after_append_and_retry_can_duplicate(self):
        with patch.object(nnh, "atomic_write", side_effect=OSError("synthetic checkpoint failure")):
            with self.assertRaisesRegex(OSError, "checkpoint failure"):
                nnh.track_undeclared([conn()])
        self.assertFalse(self.state.exists())
        self.assertEqual(len(self.events()), 1)
        nnh.track_undeclared([conn()])
        self.assertEqual(len(self.events()), 2)
        nnh.track_undeclared([conn()])
        self.assertEqual(len(self.events()), 2)

    def test_no_undeclared_connections_does_not_open_tail(self):
        self.assertEqual(nnh.track_undeclared([conn(bucket="mesh")]), 0)
        self.assertFalse(self.tail.exists())
        self.assertEqual(json.loads(self.state.read_text()), {})


if __name__ == "__main__":
    unittest.main()
