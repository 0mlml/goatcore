#!/usr/bin/env python3
"""Concurrent production exporters and loopback HTTP publication regressions."""
import concurrent.futures
import http.client
import importlib.util
import io
import json
import os
import stat
import tempfile
import threading
import time
import unittest
from contextlib import contextmanager, redirect_stderr
from pathlib import Path
from unittest.mock import patch

HERE = Path(__file__).resolve().parent


class AtomicIngestTests(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.root = Path(tmp.name)
        env = {"RELAY_METRICS_BEARER_TOKEN": "synthetic-local-fixture-token",
               "RELAY_METRICS_ALLOWED_NAMES": "relay-a,relay-b",
               "HOST_NET_ALLOWED_NAMES": "host-a,host-b",
               "ZENOH_HEALTH_ALLOWED_NAMES": "router-a,router-b",
               "CANARY_ALLOWED_NAMES": "canary-a,canary-b",
               "CROSS_FLEET_TOKEN": "synthetic-cross-fleet-token",
               "CROSS_FLEET_ALLOWED_NAMES": "fleet-a",
               "RELAY_METRICS_INGEST_DIR": str(self.root)}
        spec = importlib.util.spec_from_file_location("atomic_ingest_fixture", HERE / "relay-metrics-ingest.py")
        self.m = importlib.util.module_from_spec(spec)
        with patch.dict(os.environ, env):
            spec.loader.exec_module(self.m)
        self.m.init_series_db()
        self.m.init_host_series_db()
        self.m.init_zenoh_series_db()
        self.enterContext(redirect_stderr(io.StringIO()))

    def run_pair(self, first, second, release):
        with concurrent.futures.ThreadPoolExecutor(2) as pool:
            a = pool.submit(first)
            try:
                second(pool)
            finally:
                release.set()
            a.result(timeout=5)

    @contextmanager
    def http_server(self):
        errors = []

        class Server(self.m.ThreadingHTTPServer):
            def handle_error(self, request, client_address):
                errors.append("request failed")

        server = Server(("127.0.0.1", 0), self.m.Handler)
        thread = threading.Thread(target=lambda: server.serve_forever(poll_interval=0.01))
        thread.start()
        try:
            yield server, errors
        finally:
            server.shutdown()
            server.server_close()
            thread.join(3)

    def host_post(self, server, state):
        client = http.client.HTTPConnection("127.0.0.1", server.server_port, timeout=6)
        try:
            client.request("POST", "/host-net-ingest/host-a", json.dumps({"host_net": {"state": state}}),
                           {"Authorization": "Bearer " + self.m.TOKEN})
            response = client.getresponse()
            response.read()
            return response.status
        finally:
            client.close()

    def test_same_publisher_posts_each_own_the_file_they_rename(self):
        dst = self.root / "host-host-a.json"
        barrier = threading.Barrier(2)
        replace = os.replace

        def simultaneous(src, target):
            if str(target) == str(dst):
                barrier.wait(timeout=4)
            return replace(src, target)

        with self.http_server() as (server, errors), \
                patch.object(self.m.os, "replace", side_effect=simultaneous), \
                concurrent.futures.ThreadPoolExecutor(2) as pool:
            calls = [pool.submit(self.host_post, server, state) for state in ("first", "second")]
            self.assertEqual([call.result(timeout=5) for call in calls], [204, 204])
            self.assertEqual(errors, [])
        self.assertIn(json.loads(dst.read_text())["host_net"]["state"], ("first", "second"))

    def test_slow_post_cannot_truncate_another_posts_published_inode(self):
        dst = self.root / "host-host-a.json"
        first_ready, slow_ready = threading.Event(), threading.Event()
        first_release, slow_release = threading.Event(), threading.Event()
        replace, dump = os.replace, json.dump
        first = [True]

        def hold_replace(src, target):
            if str(target) == str(dst) and first[0]:
                first[0] = False
                first_ready.set()
                if not first_release.wait(4):
                    raise TimeoutError("first post not released")
            return replace(src, target)

        def hold_dump(doc, stream, *args, **kwargs):
            if (doc.get("host_net") or {}).get("state") == "slow":
                slow_ready.set()
                if not slow_release.wait(4):
                    raise TimeoutError("slow post not released")
            return dump(doc, stream, *args, **kwargs)

        with self.http_server() as (server, errors), \
                patch.object(self.m.os, "replace", side_effect=hold_replace), \
                patch.object(self.m.json, "dump", side_effect=hold_dump), \
                concurrent.futures.ThreadPoolExecutor(2) as pool:
            a = pool.submit(self.host_post, server, "first")
            try:
                self.assertTrue(first_ready.wait(3))
                b = pool.submit(self.host_post, server, "slow")
                self.assertTrue(slow_ready.wait(3))  # second file is open, before any bytes
                first_release.set()
                self.assertEqual(a.result(timeout=3), 204)
                self.assertEqual(json.loads(dst.read_text())["host_net"]["state"], "first")
            finally:
                first_release.set()
                slow_release.set()
            self.assertEqual(b.result(timeout=3), 204)
            self.assertEqual(errors, [])
        self.assertEqual(json.loads(dst.read_text())["host_net"]["state"], "slow")

    def test_aggregate_collision_and_slow_writer_preserve_both_reports(self):
        dst = Path(self.m.HOSTS_JSON)
        dst.write_text('{"hosts":{}}')
        (self.root / "host-host-a.json").write_text('{"host":"host-a"}')
        entered, release = threading.Event(), threading.Event()
        replace = os.replace
        first = [True]

        def hold_replace(src, target):
            if str(target) == str(dst) and first[0]:
                first[0] = False
                entered.set()
                if not release.wait(4):
                    raise TimeoutError("test writer not released")
            return replace(src, target)

        def second(pool):
            self.assertTrue(entered.wait(3))
            (self.root / "host-host-b.json").write_text('{"host":"host-b"}')
            b = pool.submit(self.m.export_hosts_aggregate)
            # The old destination stays readable while publication is held.
            for _ in range(30):
                json.loads(dst.read_text())
            release.set()
            b.result(timeout=5)

        with patch.object(self.m.os, "replace", side_effect=hold_replace):
            self.run_pair(self.m.export_hosts_aggregate, second, release)
        self.assertEqual(set(json.loads(dst.read_text())["hosts"]), {"host-a", "host-b"})
        self.assertEqual(list(self.root.glob("*.tmp")), [])

    def test_slow_json_writer_cannot_expose_an_empty_document(self):
        dst = self.root / "slow.json"
        dst.write_text('{"old":true}')
        entered, release = threading.Event(), threading.Event()
        dump = json.dump

        def slow_dump(doc, stream, *args, **kwargs):
            if doc.get("slow"):
                entered.set()
                if not release.wait(4):
                    raise TimeoutError("test writer not released")
            return dump(doc, stream, *args, **kwargs)

        def second(pool):
            self.assertTrue(entered.wait(3))
            b = pool.submit(self.m.atomic_json_write, str(dst), {"fast": True})
            b.result(timeout=3)
            self.assertEqual(json.loads(dst.read_text()), {"fast": True})

        with patch.object(self.m.json, "dump", side_effect=slow_dump):
            self.run_pair(lambda: self.m.atomic_json_write(str(dst), {"slow": True}), second, release)
        self.assertEqual(json.loads(dst.read_text()), {"slow": True})

    def test_failures_keep_last_document_cleanup_temp_and_recover(self):
        dst = self.root / "last.json"
        for failure in ("write", "close", "replace", "permissions"):
            with self.subTest(failure=failure):
                dst.write_text('{"old":true}')
                if failure == "write":
                    context = patch.object(self.m.json, "dump", side_effect=OSError("disk full"))
                elif failure == "replace":
                    context = patch.object(self.m.os, "replace", side_effect=OSError("rename denied"))
                elif failure == "permissions":
                    context = patch.object(self.m.os, "fchmod", side_effect=OSError("permissions denied"))
                else:
                    original = self.m.tempfile.NamedTemporaryFile

                    @contextmanager
                    def bad_close(*args, _original=original, **kwargs):
                        with _original(*args, **kwargs) as f:
                            yield f
                        raise OSError("close failed")

                    context = patch.object(self.m.tempfile, "NamedTemporaryFile", side_effect=bad_close)
                with context, self.assertRaises(OSError):
                    self.m.atomic_json_write(str(dst), {"new": True})
                self.assertEqual(json.loads(dst.read_text()), {"old": True})
                self.assertEqual(list(self.root.glob("*.tmp")), [])
                self.m.atomic_json_write(str(dst), {"new": True})
                self.assertEqual(json.loads(dst.read_text()), {"new": True})

    def test_existing_permissions_preserved_and_new_outputs_readable(self):
        dst = self.root / "mode.json"
        self.m.atomic_json_write(str(dst), {})
        self.assertEqual(stat.S_IMODE(dst.stat().st_mode), 0o644)
        dst.chmod(0o640)
        self.m.atomic_json_write(str(dst), {"new": True})
        self.assertEqual(stat.S_IMODE(dst.stat().st_mode), 0o640)

    def test_completed_publication_does_not_remove_a_reused_scratch_path(self):
        dst = self.root / "last.json"
        replace = os.replace
        reused = []

        def publish_then_reuse(src, target):
            replace(src, target)
            # Once renamed, this pathname is no longer owned by the writer.
            Path(src).write_text("another operation owns this file")
            reused.append(Path(src))

        with patch.object(self.m.os, "replace", side_effect=publish_then_reuse):
            self.m.atomic_json_write(str(dst), {"new": True})
        self.assertEqual(json.loads(dst.read_text()), {"new": True})
        self.assertEqual(reused[0].read_text(), "another operation owns this file")

    def test_writer_refuses_destinations_outside_ingest_before_touching_files(self):
        with tempfile.TemporaryDirectory() as other:
            outside = Path(other) / "outside.json"
            outside.write_text('{"preserve":true}')
            for path in (str(outside), str(self.root / ".." / Path(other).name / "outside.json")):
                with self.subTest(path=path), self.assertRaises(ValueError):
                    self.m.atomic_json_write(path, {"overwrite": True})
            self.assertEqual(json.loads(outside.read_text()), {"preserve": True})
        self.assertEqual(list(self.root.glob("*.tmp")), [])

    def test_aggregate_cannot_publish_an_older_view_after_a_newer_export(self):
        dst = Path(self.m.HOSTS_JSON)
        (self.root / "host-host-a.json").write_text('{"host":"host-a"}')
        entered, release, competing = threading.Event(), threading.Event(), threading.Event()
        replace = os.replace
        first = [True]

        def hold_replace(src, target):
            if str(target) == str(dst) and first[0]:
                first[0] = False
                entered.set()
                if not release.wait(4):
                    raise TimeoutError("aggregate writer not released")
            return replace(src, target)

        def export_again():
            competing.set()
            self.m.export_hosts_aggregate()

        with patch.object(self.m.os, "replace", side_effect=hold_replace), \
                concurrent.futures.ThreadPoolExecutor(2) as pool:
            a = pool.submit(self.m.export_hosts_aggregate)
            try:
                self.assertTrue(entered.wait(3))
                (self.root / "host-host-b.json").write_text('{"host":"host-b"}')
                b = pool.submit(export_again)
                self.assertTrue(competing.wait(3))
                with self.assertRaises(concurrent.futures.TimeoutError):
                    b.result(timeout=0.2)
            finally:
                release.set()
            a.result(timeout=3)
            b.result(timeout=3)
        self.assertEqual(set(json.loads(dst.read_text())["hosts"]), {"host-a", "host-b"})

    def test_all_exporters_tolerate_concurrent_calls(self):
        funcs = [self.m.export_hosts_aggregate, self.m.export_host_series,
                 self.m.export_zenoh_aggregate, self.m.export_zenoh_series,
                 self.m.export_series, self.m.export_canaries_aggregate,
                 lambda: self.m.export_zenoh_wedges(1000)]
        for func in funcs:
            with self.subTest(exporter=func.__name__), concurrent.futures.ThreadPoolExecutor(6) as pool:
                futures = [pool.submit(func) for _ in range(12)]
                for f in futures:
                    f.result(timeout=5)
        for path in self.root.glob("*.json"):
            json.loads(path.read_text())
        self.assertEqual(list(self.root.glob("*.tmp")), [])

    def test_wedge_updates_keep_both_routers(self):
        z = {"container": {"health": "unhealthy", "uptime_s": 100}}
        with concurrent.futures.ThreadPoolExecutor(2) as pool:
            fs = [pool.submit(self.m.track_zenoh_wedges, n, z, 1000) for n in ("router-a", "router-b")]
            for f in fs:
                f.result(timeout=5)
        doc = json.loads(Path(self.m.ZENOH_WEDGES_JSON).read_text())
        self.assertEqual({e["router"] for e in doc["events"]}, {"router-a", "router-b"})

    def test_slow_history_export_does_not_block_current_aggregate(self):
        entered, release = threading.Event(), threading.Event()
        dump = json.dump

        def hold(doc, stream, *args, **kwargs):
            if "series" in doc:
                entered.set()
                if not release.wait(4):
                    raise TimeoutError("test writer not released")
            return dump(doc, stream, *args, **kwargs)

        def second(pool):
            self.assertTrue(entered.wait(3))
            pool.submit(self.m.export_hosts_aggregate).result(timeout=2)

        with patch.object(self.m.json, "dump", side_effect=hold):
            self.run_pair(self.m.export_host_series, second, release)

    def test_concurrent_http_routes_publish_complete_json(self):
        errors = []

        class Server(self.m.ThreadingHTTPServer):
            def handle_error(self, request, client_address):
                errors.append("request failed")

        server = Server(("127.0.0.1", 0), self.m.Handler)
        thread = threading.Thread(target=lambda: server.serve_forever(poll_interval=0.01))
        thread.start()
        stopped = threading.Event()
        read_errors = []

        def reader():
            while not stopped.is_set():
                for path in self.root.glob("*.json"):
                    try:
                        json.loads(path.read_text())
                    except Exception as e:
                        read_errors.append(e)
                time.sleep(0.001)

        reading = threading.Thread(target=reader)
        reading.start()

        def post(route, body, token=None):
            client = http.client.HTTPConnection("127.0.0.1", server.server_port, timeout=10)
            try:
                client.request("POST", route, json.dumps(body), {"Authorization": "Bearer " + (token or self.m.TOKEN)})
                response = client.getresponse()
                response.read()
                return response.status
            finally:
                client.close()

        routes = []
        for letter in ("a", "b"):
            routes += [(f"/host-net-ingest/host-{letter}", {"host_net": {}}, None),
                       (f"/zenoh-health-ingest/router-{letter}", {"zenoh": {}}, None),
                       (f"/canary-ingest/canary-{letter}", {"canary": {"journey": {}}}, None),
                       (f"/relay-metrics-ingest/relay-{letter}", {"deep": {}}, None)]
        routes += [("/relay-metrics-ingest/peer-names", {"peer": "fixture"}, None),
                   ("/goatnet-summary-ingest/fleet-a", {"generated": 1000, "relays_up": 1}, self.m.CROSS_FLEET_TOKEN)]
        try:
            for _ in range(3):
                with concurrent.futures.ThreadPoolExecutor(6) as pool:
                    results = list(pool.map(lambda args: post(*args), routes * 2))
                self.assertTrue(all(r in (200, 204) for r in results), results)
            self.assertEqual(set(json.loads(Path(self.m.HOSTS_JSON).read_text())["hosts"]), {"host-a", "host-b"})
            self.assertEqual(set(json.loads(Path(self.m.ZENOH_HEALTH_JSON).read_text())["routers"]), {"router-a", "router-b"})
            self.assertEqual(set(json.loads(Path(self.m.PEERS_JSON).read_text())["peers"]), {"canary-a", "canary-b"})
            self.assertEqual(errors, [])
            self.assertEqual(read_errors, [])
            self.assertEqual(list(self.root.glob("*.tmp")), [])
        finally:
            stopped.set()
            reading.join(3)
            server.shutdown()
            server.server_close()
            thread.join(3)


if __name__ == "__main__":
    unittest.main()
