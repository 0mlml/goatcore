#!/usr/bin/env python3
"""Last-good inventory survives failed host and container enumeration."""
import importlib.util
import io
import json
import os
import subprocess
import sys
import tempfile
import unittest
from contextlib import redirect_stderr
from pathlib import Path
from unittest.mock import patch

HERE = Path(__file__).resolve().parent
SOCKET = 'tcp ESTAB 0 0 192.0.2.1:40000 203.0.113.1:4444 users:(("fixture",pid=42,fd=3))\n'


class InventoryTests(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.root = Path(tmp.name)
        spec = importlib.util.spec_from_file_location("inventory_fixture", HERE / "nnh-snapshot.py")
        self.m = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.m)
        for key in ("SNAPSHOT", "UNDECLARED_STATE", "UNDECLARED_TAIL", "BASELINE",
                    "PREV_MESH_FAST", "PREV_SOCK", "COUNTERS", "TEXTFILE"):
            self.enterContext(patch.object(self.m, key, str(self.root / key.lower())))
        self.enterContext(patch.object(self.m, "STATE_DIR", str(self.root)))
        self.enterContext(patch.object(self.m, "container_map", return_value={}))
        self.targets = self.m.netns_targets
        self.enterContext(patch.object(self.m, "netns_targets", return_value=[]))
        self.real_mesh = self.m.mesh_plane
        self.mesh = self.enterContext(patch.object(self.m, "mesh_plane", return_value=({"peers": []}, 60)))
        self.enterContext(patch.object(sys, "argv", ["nnh-snapshot.py"]))
        self.logs = self.enterContext(redirect_stderr(io.StringIO()))

    def scan(self, output=SOCKET):
        with patch.object(self.m.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, stdout=output.encode())):
            self.assertEqual(self.m.main() or 0, 0)

    def evidence(self):
        return {p.name: (p.read_bytes(), p.stat().st_mtime_ns)
                for p in self.root.iterdir() if p.is_file()}

    def test_failures_preserve_inventory_tracking_and_then_recover(self):
        self.scan()
        self.assertEqual(json.loads(Path(self.m.SNAPSHOT).read_text())["undeclared_count"], 1)
        original = self.evidence()
        self.mesh.reset_mock()
        for failure in (subprocess.CalledProcessError(1, ["ss"]),
                        subprocess.TimeoutExpired(["ss"], 15), FileNotFoundError("ss unavailable")):
            with self.subTest(failure=type(failure).__name__), patch.object(self.m.subprocess, "run", side_effect=failure):
                self.assertEqual(self.m.main(), 1)
                self.assertEqual(self.evidence(), original)
        self.mesh.assert_not_called()
        self.assertIn("host", self.logs.getvalue())
        self.scan("")
        snap = json.loads(Path(self.m.SNAPSHOT).read_text())
        self.assertEqual(snap["conns"], [])
        self.assertEqual(snap["undeclared_count"], 0)

    def test_failed_container_scan_preserves_last_good_not_partial_host_rows(self):
        self.scan()
        original = self.evidence()
        self.m.netns_targets.return_value = [("fixture-container", 123)]

        def run(cmd, **kwargs):
            if cmd[0] == "nsenter":
                raise subprocess.CalledProcessError(1, cmd)
            return subprocess.CompletedProcess(cmd, 0, stdout=SOCKET.encode())

        with patch.object(self.m.subprocess, "run", side_effect=run):
            self.assertEqual(self.m.main(), 1)
        self.assertEqual(self.evidence(), original)
        self.assertIn("123", self.logs.getvalue())

    def test_successful_empty_container_scan_counts_as_read(self):
        self.m.netns_targets.return_value = [("fixture-container", 123)]
        self.scan("")
        self.assertEqual(self.m.NETNS_STATS, {"targets": 1, "read": 1})
        self.assertEqual(json.loads(Path(self.m.SNAPSHOT).read_text())["conns"], [])

    def test_first_scan_failure_does_not_create_snapshot(self):
        with patch.object(self.m.subprocess, "run", side_effect=OSError("scan refused")):
            self.assertEqual(self.m.main(), 1)
        self.assertFalse(Path(self.m.SNAPSHOT).exists())
        self.assertFalse(Path(self.m.UNDECLARED_STATE).exists())

    def test_unrecognized_scan_output_preserves_last_good_inventory(self):
        self.scan()
        original = self.evidence()
        for scope in ("host", "container"):
            for output in ("unexpected socket output\n", SOCKET + "unrecognized row\n"):
                with self.subTest(scope=scope, output=output):
                    self.m.netns_targets.return_value = [] if scope == "host" else [("fixture-container", 123)]

                    def run(cmd, _scope=scope, _output=output, **kwargs):
                        data = _output if _scope == "host" or cmd[0] == "nsenter" else SOCKET
                        return subprocess.CompletedProcess(cmd, 0, stdout=data.encode())

                    with patch.object(self.m.subprocess, "run", side_effect=run):
                        self.assertEqual(self.m.main(), 1)
                    self.assertEqual(self.evidence(), original)
        self.assertIn("unrecognized socket inventory row", self.logs.getvalue())

    def test_filtered_and_blank_rows_are_valid_successful_empty_inventory(self):
        self.scan("\n  \n" + SOCKET.replace("ESTAB", "TIME-WAIT")
                  + "tcp LISTEN 0 128 127.0.0.1:443 0.0.0.0:*\n"
                  + "udp UNCONN 0 0 [::1]:5353 [::]:*\n")
        self.assertEqual(json.loads(Path(self.m.SNAPSHOT).read_text())["conns"], [])

    def test_required_parser_accepts_tcp_udp_ipv4_ipv6_and_optional_process_fields(self):
        sample = (SOCKET + "tcp LISTEN 0 128 [::]:443 [::]:*\n"
                  + "udp UNCONN 0 0 192.0.2.1:5353 0.0.0.0:*\n"
                  + "udp ESTAB 0 0 [2001:db8::1]:53000 [2001:db8::2]:53\n")
        rows = self.m.parse_ss_rows(sample, {}, {}, "host", required=True)
        self.assertEqual([r["proto"] for r in rows], ["tcp", "tcp", "udp", "udp"])
        self.assertEqual([r["state"] for r in rows], ["estab", "listen", "listen", "estab"])

    def test_unknown_socket_states_preserve_evidence_instead_of_becoming_empty(self):
        self.scan()
        original = self.evidence()
        for scope in ("host", "container"):
            for state in ("UNKNOWN", "UNDEF", "FUTURE-STATE"):
                with self.subTest(scope=scope, state=state):
                    self.m.netns_targets.return_value = [] if scope == "host" else [("fixture-container", 123)]

                    def run(cmd, _scope=scope, _state=state, **kwargs):
                        data = SOCKET.replace("ESTAB", _state) if _scope == "host" or cmd[0] == "nsenter" else SOCKET
                        return subprocess.CompletedProcess(cmd, 0, stdout=data.encode())

                    with patch.object(self.m.subprocess, "run", side_effect=run):
                        self.assertEqual(self.m.main(), 1)
                    self.assertEqual(self.evidence(), original)
        self.assertIn("unrecognized socket state", self.logs.getvalue())

    def test_known_filtered_states_remain_valid_and_optional_parser_stays_tolerant(self):
        for state in ("SYN-SENT", "SYN-RECV", "FIN-WAIT-1", "FIN-WAIT-2", "TIME-WAIT",
                      "CLOSE-WAIT", "LAST-ACK", "CLOSING"):
            with self.subTest(state=state):
                self.scan(SOCKET.replace("ESTAB", state))
                self.assertEqual(json.loads(Path(self.m.SNAPSHOT).read_text())["conns"], [])
        self.assertEqual(self.m.parse_ss_rows(SOCKET.replace("ESTAB", "UNKNOWN"), {}, {}, "host"), [])

    def test_optional_byte_counters_remain_best_effort(self):
        with patch.object(self.m.subprocess, "run", side_effect=OSError("counter unavailable")):
            self.assertEqual(self.m.tcp_byte_counters(), {})

    def test_failed_fast_scan_does_not_consume_mesh_byte_deltas(self):
        tick, received, fail = [1000], [100], [False]
        exists = os.path.exists

        def run(cmd, **kwargs):
            if cmd[0] == "netbird":
                body = {"peers": {"details": [{"fqdn": "fixture-peer", "status": "Connected",
                         "connectionType": "P2P", "transferReceived": received[0], "transferSent": received[0]}]}}
                return subprocess.CompletedProcess(cmd, 0, stdout=json.dumps(body).encode())
            if fail[0]:
                raise subprocess.CalledProcessError(1, cmd)
            return subprocess.CompletedProcess(cmd, 0, stdout=b"")

        with patch.object(self.m, "mesh_plane", self.real_mesh), \
                patch.object(self.m.os.path, "exists", side_effect=lambda p: p == "/var/run/netbird.sock" or exists(p)), \
                patch.object(self.m.time, "time", side_effect=lambda: tick[0]), \
                patch.object(self.m.subprocess, "run", side_effect=run):
            self.m.attribution_pass([])
            original = self.evidence()
            tick[0], received[0], fail[0] = 1010, 200, True
            with self.assertRaises(self.m.InventoryError):
                self.m.attribution_pass([])
            self.assertEqual(self.evidence(), original)
            tick[0], received[0], fail[0] = 1020, 300, False
            counters, gap = self.m.attribution_pass([])
            self.assertEqual(gap, 20)
            self.assertEqual(counters["transport|mesh-p2p|rx"], 200)
            self.assertEqual(counters["transport|mesh-p2p|tx"], 200)

    def test_real_cli_failure_preserves_snapshot_and_exits_nonzero(self):
        commands = self.root / "bin"
        commands.mkdir()
        ss = commands / "ss"
        ss.write_text("#!/usr/bin/python3\nprint(" + repr(SOCKET) + ")\n")
        ss.chmod(0o755)
        state = self.root / "cli-state"
        env = {**os.environ, "PATH": str(commands), "NNH_STATE_DIR": str(state),
               "NNH_BASELINE": str(self.root / "missing-baseline")}
        command = [sys.executable, str(HERE / "nnh-snapshot.py")]
        result = subprocess.run(command, env=env, capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 0, result.stderr)
        before = {p.name: (p.read_bytes(), p.stat().st_mtime_ns) for p in state.iterdir()}
        ss.write_text("#!/usr/bin/python3\nraise SystemExit(1)\n")
        result = subprocess.run(command, env=env, capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("inventory not refreshed", result.stderr)
        self.assertEqual({p.name: (p.read_bytes(), p.stat().st_mtime_ns) for p in state.iterdir()}, before)

    def test_discovery_failure_preserves_existing_evidence(self):
        self.scan()
        original = self.evidence()
        # Exercise real discovery after restoring only its test seam.
        def run(cmd, **kwargs):
            if cmd[0] == "ss":
                return subprocess.CompletedProcess(cmd, 0, stdout=b"")
            raise OSError("docker unavailable")

        with patch.object(self.m, "netns_targets", self.targets), \
                patch.object(self.m.shutil, "which", return_value="/fixture/docker"), \
                patch.object(self.m.subprocess, "run", side_effect=run):
            self.assertEqual(self.m.main(), 1)
        self.assertEqual(self.evidence(), original)

    def test_discovery_optional_docker_and_empty_namespace_controls(self):
        targets = self.targets
        with patch.object(self.m.shutil, "which", return_value=None):
            self.assertEqual(targets(required=True), [])
        with patch.object(self.m.shutil, "which", return_value="/fixture/bin"), \
                patch.object(self.m.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, stdout=b"")):
            self.assertEqual(targets(required=True), [])

    def test_discovery_decode_errors_are_explicit_or_best_effort(self):
        for stage in ("list", "inspect"):
            for required in (True, False):
                with self.subTest(stage=stage, required=required):
                    bad = subprocess.CompletedProcess([], 0, stdout=b"\xff")
                    results = [bad] if stage == "list" else [
                        subprocess.CompletedProcess([], 0, stdout=b"fixture-id\n"), bad]
                    with patch.object(self.m.shutil, "which", return_value="/fixture/bin"), \
                            patch.object(self.m.subprocess, "run", side_effect=results):
                        if required:
                            with self.assertRaises(self.m.InventoryError):
                                self.targets(required=True)
                        else:
                            self.assertEqual(self.targets(), [])
    def test_required_discovery_fails_on_inspection_or_missing_nsenter(self):
        targets = self.targets
        listed = subprocess.CompletedProcess([], 0, stdout=b"fixture-id\n")
        for case in ("inspect", "nsenter", "host-identity", "container-identity", "incomplete", "invalid-pid"):
            with self.subTest(case=case):
                def which(name, _case=case):
                    return None if name == "nsenter" and _case == "nsenter" else "/fixture/" + name

                inspection = {"incomplete": b"", "invalid-pid": b"/fixture-container invalid\n"}.get(
                    case, b"/fixture-container 123\n")
                results = [listed, subprocess.CalledProcessError(1, ["docker", "inspect"])] if case == "inspect" else [
                    listed, subprocess.CompletedProcess([], 0, stdout=inspection)]

                def readlink(path, _case=case):
                    if path == "/proc/self/ns/net" and _case != "host-identity":
                        return "net:[host]"
                    raise OSError("namespace unreadable")

                with patch.object(self.m.shutil, "which", side_effect=which), \
                        patch.object(self.m.subprocess, "run", side_effect=results), \
                        patch.object(self.m.os, "readlink", side_effect=readlink):
                    with self.assertRaises(self.m.InventoryError):
                        targets(required=True)

    def test_discovery_deduplicates_shared_namespaces_and_skips_host_and_stopped(self):
        def readlink(path):
            return {"/proc/self/ns/net": "net:[host]", "/proc/11/ns/net": "net:[host]",
                    "/proc/12/ns/net": "net:[shared]", "/proc/13/ns/net": "net:[shared]"}[path]

        results = [subprocess.CompletedProcess([], 0, stdout=b"a b c d"),
                   subprocess.CompletedProcess([], 0, stdout=b"/host 11\n/one 12\n/two 13\n/stopped 0\n")]
        with patch.object(self.m.shutil, "which", return_value="/fixture/bin"), \
                patch.object(self.m.subprocess, "run", side_effect=results), \
                patch.object(self.m.os, "readlink", side_effect=readlink):
            self.assertEqual(self.targets(required=True), [("one", 12)])


def publisher_fixtures(destination):
    """Exercise production collection and hand its exact saved bytes to Rust."""
    case = InventoryTests()
    try:
        case.setUp()
        case.mesh.return_value = ({"peers": [{"connected": True, "active": True}]}, 60)
        case.scan()
        original = case.evidence()
        for scope in ("host", "container"):
            case.m.netns_targets.return_value = [] if scope == "host" else [("fixture-container", 123)]

            def run(cmd, _scope=scope, **kwargs):
                if _scope == "host" or cmd[0] == "nsenter":
                    raise subprocess.CalledProcessError(1, cmd)
                return subprocess.CompletedProcess(cmd, 0, stdout=b"")

            with patch.object(case.m.subprocess, "run", side_effect=run):
                case.assertEqual(case.m.main(), 1)
            case.assertEqual(case.evidence(), original)
            (destination / f"retained-{scope}.json").write_bytes(Path(case.m.SNAPSHOT).read_bytes())
        case.scan("")
        (destination / "successful-empty.json").write_bytes(Path(case.m.SNAPSHOT).read_bytes())
    finally:
        case.doCleanups()


if __name__ == "__main__":
    if len(sys.argv) == 3 and sys.argv[1] == "--publisher-fixture":
        publisher_fixtures(Path(sys.argv[2]))
    else:
        unittest.main()
