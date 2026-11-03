"""Admission probe and both exporters: configuration, failures, and unknowns."""
import ast
import contextlib
import io
import json
import os
from pathlib import Path
import ssl
import socket
import threading
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

HERE = Path(__file__).resolve().parent


class SocketDouble:
    def __init__(self):
        self.closed = False

    def __enter__(self):
        return self

    def __exit__(self, *args):
        self.closed = True

    def close(self):
        self.closed = True


class ProbeTests(unittest.TestCase):
    def execute(self, cert_error=None, connect_error=None, tls_error=None, duration=.010):
        text = (HERE / "zenoh-health-probe.sh").read_text()
        source = "import os, socket, ssl, sys, time\n" + text.split(
            "import os, socket, ssl, sys, time\n", 1)[1].split("\nPY\n", 1)[0]
        raw, wrapped = SocketDouble(), SocketDouble()
        from unittest.mock import Mock
        ctx = Mock()
        ctx.load_cert_chain.side_effect = cert_error
        ctx.wrap_socket.side_effect = tls_error
        ctx.wrap_socket.return_value = wrapped
        output = io.StringIO()
        with patch("ssl.create_default_context", return_value=ctx), \
             patch("socket.create_connection", side_effect=connect_error, return_value=raw) as connect, \
             patch("time.monotonic", side_effect=[0, duration]), \
             patch("time.time", side_effect=[0, duration]), \
             patch("sys.argv", ["probe", "fixture", "7447"]), \
             patch.dict(os.environ, {"ZH_CERT": "fixture.pem", "ZH_KEY": "fixture.key"}), \
             contextlib.redirect_stdout(output):
            try:
                exec(compile(source, "production-admission-probe", "exec"), {})
            except SystemExit as e:
                self.assertEqual(e.code, 0)
        return output.getvalue().strip(), connect.call_count, raw, wrapped

    def test_success_and_socket_cleanup(self):
        out, calls, raw, wrapped = self.execute()
        self.assertEqual(out, "OK 10 ok")
        self.assertEqual(calls, 1)
        self.assertTrue(raw.closed)
        self.assertTrue(wrapped.closed)

    def test_bad_local_certificate_is_unmeasured_before_connect(self):
        for exc in [FileNotFoundError("private path"), ssl.SSLError("private certificate detail")]:
            with self.subTest(exc=type(exc).__name__):
                out, calls, _, _ = self.execute(cert_error=exc)
                self.assertEqual(out, "UNMEASURED null client_config_error")
                self.assertEqual(calls, 0)

    def test_real_ssl_rejects_invalid_pem_before_connect(self):
        text = (HERE / "zenoh-health-probe.sh").read_text()
        source = "import os, socket, ssl, sys, time\n" + text.split(
            "import os, socket, ssl, sys, time\n", 1)[1].split("\nPY\n", 1)[0]
        with tempfile.TemporaryDirectory() as td:
            cert = Path(td) / "invalid.pem"
            cert.write_text("invalid synthetic certificate\n")
            output = io.StringIO()
            with patch("socket.create_connection") as connect, \
                 patch("sys.argv", ["probe", "fixture", "7447"]), \
                 patch.dict(os.environ, {"ZH_CERT": str(cert), "ZH_KEY": str(cert)}), \
                 contextlib.redirect_stdout(output), self.assertRaises(SystemExit) as exit_:
                exec(compile(source, "production-admission-probe", "exec"), {})
            self.assertEqual(exit_.exception.code, 0)
            self.assertEqual(output.getvalue().strip(), "UNMEASURED null client_config_error")
            connect.assert_not_called()

    def test_timeout_preserves_incident_latency(self):
        out, _, raw, _ = self.execute(tls_error=TimeoutError(), duration=12.019)
        self.assertEqual(out, "FAIL 12019 timeout")
        self.assertTrue(raw.closed)

    def test_connection_refusal_is_a_measured_failure(self):
        out, _, _, _ = self.execute(connect_error=ConnectionRefusedError(), duration=.005)
        self.assertEqual(out, "FAIL 5 connection_refused")

    def test_only_recognized_client_auth_alerts_use_the_page_exemption(self):
        for reason in ["TLSV13_ALERT_CERTIFICATE_REQUIRED", "SSLV3_ALERT_BAD_CERTIFICATE",
                       "TLSV1_ALERT_UNKNOWN_CA"]:
            with self.subTest(reason=reason):
                error = ssl.SSLError("private peer details")
                error.reason = reason
                out, _, raw, _ = self.execute(tls_error=error, duration=.040)
                self.assertEqual(out, "FAIL 40 tls_error")
                self.assertTrue(raw.closed)

    def test_eof_clean_close_and_protocol_errors_remain_transport_failures(self):
        protocol = ssl.SSLError("private protocol details")
        protocol.reason = "TLSV1_ALERT_DECODE_ERROR"
        for error in [ssl.SSLEOFError("private EOF"), ssl.SSLZeroReturnError("closed"),
                      protocol, ssl.SSLError("private unclassified SSL failure")]:
            with self.subTest(error=type(error).__name__, reason=getattr(error, "reason", None)):
                out, _, raw, _ = self.execute(tls_error=error, duration=.040)
                self.assertEqual(out, "FAIL 40 connection_error")
                self.assertTrue(raw.closed)

    def test_real_truncated_handshake_is_a_transport_failure(self):
        # No certificate is needed to reach this failure: the peer closes after
        # ClientHello, before any client authentication. Only fixture credential
        # loading is doubled; the socket and TLS engine are real.
        from unittest.mock import Mock
        real_context = ssl.create_default_context()
        real_context.check_hostname = False
        real_context.verify_mode = ssl.CERT_NONE
        context = Mock(wraps=real_context)
        context.load_cert_chain = Mock()
        received = []
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            listener.listen(1)
            listener.settimeout(5)
            def peer():
                with listener.accept()[0] as connection:
                    connection.settimeout(5)
                    received.append(connection.recv(4096))
                    connection.shutdown(socket.SHUT_WR)
            thread = threading.Thread(target=peer, daemon=True)
            thread.start()
            text = (HERE / "zenoh-health-probe.sh").read_text()
            source = "import os, socket, ssl, sys, time\n" + text.split(
                "import os, socket, ssl, sys, time\n", 1)[1].split("\nPY\n", 1)[0]
            output = io.StringIO()
            try:
                with patch("ssl.create_default_context", return_value=context), \
                     patch("sys.argv", ["probe", "127.0.0.1", str(listener.getsockname()[1])]), \
                     patch.dict(os.environ, {"ZH_CERT": "fixture.pem", "ZH_KEY": "fixture.key"}), \
                     contextlib.redirect_stdout(output):
                    exec(compile(source, "production-admission-probe", "exec"), {})
            finally:
                thread.join(timeout=6)
            self.assertFalse(thread.is_alive())
        self.assertTrue(received and received[0][0] == 22, "peer received TLS ClientHello")
        self.assertRegex(output.getvalue().strip(), r"^FAIL [0-9]+ connection_error$")

    def test_local_permission_error_is_unmeasured(self):
        out, _, _, _ = self.execute(connect_error=PermissionError("private path"))
        self.assertEqual(out, "UNMEASURED null probe_error")


class ExportTests(unittest.TestCase):
    def direct(self, response, configured=True):
        """Run the entire host script with Docker/netns/network commands doubled."""
        with tempfile.TemporaryDirectory() as td:
            p = Path(td)
            b = p / "bin"
            b.mkdir()
            # The environment inventory scans this module too. Keep fixture
            # state local rather than inventing deployment environment variables.
            (b / "admission-response.txt").write_text(response)
            programs = {
                "docker": '''import sys
s=" ".join(sys.argv)
if "inspect" in s:
 if "NetworkSettings" in s: print("fixture")
 else: print("true\\trunning\\thealthy\\t0\\t2026-01-01T00:00:00Z\\tfalse\\tfixture-image\\tfixture-digest\\t99999999\\t/fixture/not-present")
''',
                "nsenter": '''import sys
from pathlib import Path
s=" ".join(sys.argv)
if "python3" in s: print(Path(__file__).with_name("admission-response.txt").read_text())
elif "curl" in s: print('{"version":"fixture"}')
elif "ss -tan" in s: print("State Recv-Q Send-Q Local Peer")
''',
                "date": '''import sys
print("1000000" if "%s%3N" in sys.argv else "1000")
''',
                "curl": '''import sys
from pathlib import Path
a=sys.argv
if "--data-binary" in a:
 (Path(__file__).resolve().parent.parent / "payload.json").write_text(a[a.index("--data-binary")+1])
print("200")
''',
            }
            for name, body in programs.items():
                f = b / name
                f.write_text("#!" + sys.executable + "\n" + body)
                f.chmod(0o755)
            env = dict(os.environ, PATH=str(b) + os.pathsep + os.environ["PATH"],
                       ZENOH_ROUTER_NAME="fixture", ZENOH_ROUTER_CONTAINER="fixture",
                       ZENOH_GOATNET="fixture", ZENOH_VANTAGE="fixture",
                       ZENOH_INGEST_URLS="https://example.invalid/ingest", RELAY_METRICS_BEARER_TOKEN="fixture",
                       STATE_DIR=str(p / "state"), ZENOH_PROM_OUT=str(p / "direct.prom"))
            if configured:
                env["ZENOH_CLIENT_CERT"] = str(p / "missing.pem")
            else:
                env.pop("ZENOH_CLIENT_CERT", None)
            r = subprocess.run(["bash", str(HERE / "zenoh-health-probe.sh")], env=env,
                               text=True, capture_output=True, timeout=20)
            self.assertEqual(r.returncode, 0, r.stderr)
            return json.loads((p / "payload.json").read_text()), (p / "direct.prom").read_text()

    def bridge(self, admission):
        with tempfile.TemporaryDirectory() as td:
            p = Path(td)
            doc = {"routers": {"fixture": {"received_at": 1000, "zenoh": {
                "container": {"health": "healthy"}, "admission": admission}}}}
            (p / "health.json").write_text(json.dumps(doc))
            r = subprocess.run([sys.executable, str(HERE / "zenoh-health-to-prom.py"),
                                "--health", str(p / "health.json"), "--out", str(p / "bridge.prom")],
                               text=True, capture_output=True, timeout=10)
            self.assertEqual(r.returncode, 0, r.stderr)
            return (p / "bridge.prom").read_text()

    def test_both_exporters_keep_success_failure_and_unknown_distinct(self):
        for response, ok, ms, reason in [
            ("OK 10 ok", True, 10, "ok"),
            ("FAIL 12019 timeout", False, 12019, "timeout"),
            ("FAIL 40 connection_error", False, 40, "connection_error"),
            ("FAIL 40 tls_error", False, 40, "tls_error"),
            ("UNMEASURED null client_config_error", None, None, "client_config_error"),
            ("", None, None, "probe_error"),
        ]:
            with self.subTest(reason=reason):
                payload, direct = self.direct(response)
                admission = payload["zenoh"]["admission"]
                self.assertIs(admission["open_ok"], ok)
                self.assertEqual(admission["open_ms"], ms)
                self.assertEqual(admission["open_reason"], reason)
                bridge = self.bridge(admission)
                for output in [direct, bridge]:
                    samples = [line for line in output.splitlines() if line.startswith("goat_zenoh_open_ok{")]
                    if ok is None:
                        self.assertEqual(samples, [])
                    else:
                        self.assertEqual(samples, ['goat_zenoh_open_ok{router="fixture"} ' + str(int(ok))])
                    self.assertIn('goat_zenoh_open_probe_status{router="fixture",reason="' + reason + '"} 1', output)

    def test_unconfigured_probe_is_honestly_unmeasured(self):
        payload, direct = self.direct("", configured=False)
        a = payload["zenoh"]["admission"]
        self.assertIsNone(a["open_ok"])
        self.assertEqual(a["open_reason"], "not_configured")
        self.assertNotIn('goat_zenoh_open_ok{', direct)

    def test_malformed_helper_output_is_unmeasured_without_label_spillage(self):
        for response in ["OK 10", "OK 10 private", "OK 10 ok extra", "OK -1 ok", "FAIL nan timeout"]:
            with self.subTest(response=response):
                payload, output = self.direct(response)
                a = payload["zenoh"]["admission"]
                self.assertIsNone(a["open_ok"])
                self.assertIsNone(a["open_ms"])
                self.assertEqual(a["open_reason"], "probe_error")
                self.assertNotIn('goat_zenoh_open_ok{', output)

    def test_bridge_omits_unknown_legacy_reports(self):
        for value in [None, "false", 0]:
            output = self.bridge({"open_ok": value})
            self.assertNotIn('goat_zenoh_open_ok{', output)

    def test_bridge_drops_unbounded_reason_labels(self):
        output = self.bridge({"open_ok": True, "open_reason": 'private"label'})
        self.assertNotIn('goat_zenoh_open_probe_status{', output)


class IngestWedgeTests(unittest.TestCase):
    def setUp(self):
        # Load the actual classifier without starting the ingest server, opening
        # its databases, or requiring production bearer/allowlist configuration.
        tree = ast.parse((HERE / "relay-metrics-ingest.py").read_text())
        nodes = [n for n in tree.body if
                 isinstance(n, ast.FunctionDef) and n.name == "_zenoh_is_wedged" or
                 isinstance(n, ast.Assign) and any(isinstance(t, ast.Name) and
                     t.id == "WEDGE_OPEN_MS" for t in n.targets)]
        namespace = {}
        exec(compile(ast.Module(body=nodes, type_ignores=[]), "production-wedge-classifier", "exec"), namespace)
        self.classify = namespace["_zenoh_is_wedged"]

    def test_bridge_wedge_flag_cannot_bypass_the_tls_page_guard(self):
        for reason, ms, cw, expected in [
            ("tls_error", 40, 0, False),
            ("tls_error", 12019, 0, False),
            ("tls_error", 12019, 146, True),
            ("timeout", 12019, 0, True),
            ("connection_refused", 5, 0, True),
            ("connection_error", 40, 0, True),
            (None, 40, 0, True),  # older producers keep their existing behavior
        ]:
            with self.subTest(reason=reason, ms=ms, cw=cw):
                z = {"container": {"health": "healthy"}, "admission": {
                    "open_ok": False, "open_ms": ms, "open_reason": reason,
                    "rest_ok": True}, "wedge": {"close_wait": cw}}
                self.assertIs(self.classify(z)[0], expected)

    def test_tls_guard_does_not_mask_rest_or_container_failure(self):
        for health, rest in [("unhealthy", True), ("healthy", False)]:
            with self.subTest(health=health, rest=rest):
                z = {"container": {"health": health}, "admission": {
                    "open_ok": False, "open_ms": 40, "open_reason": "tls_error", "rest_ok": rest}}
                self.assertTrue(self.classify(z)[0])

    def test_eof_failure_reaches_both_exporters_and_ingest(self):
        result, _, _, _ = ProbeTests().execute(tls_error=ssl.SSLEOFError(), duration=.040)
        payload, direct = ExportTests().direct(result)
        admission = payload["zenoh"]["admission"]
        self.assertEqual(admission["open_reason"], "connection_error")
        for output in [direct, ExportTests().bridge(admission)]:
            self.assertIn('goat_zenoh_open_ok{router="fixture"} 0', output)
            self.assertIn('reason="connection_error"} 1', output)
        payload["zenoh"]["wedge"] = {"close_wait": 0}
        self.assertTrue(self.classify(payload["zenoh"])[0])

    def test_unmeasured_config_is_not_a_wedge(self):
        z = {"container": {"health": "healthy"}, "admission": {
            "open_ok": None, "open_ms": None, "open_reason": "client_config_error", "rest_ok": True}}
        self.assertFalse(self.classify(z)[0])


if __name__ == "__main__":
    unittest.main()
