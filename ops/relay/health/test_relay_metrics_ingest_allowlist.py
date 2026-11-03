"""Allowlist-route tests for relay-metrics-ingest.py (stdlib only; run with unittest discover)."""
import http.client
import importlib.util
import json
import os
import tempfile
import threading
import unittest
from http.server import ThreadingHTTPServer
from pathlib import Path

HERE = Path(__file__).resolve().parent
EMAIL_MAX = 254  # SMTP path limit; literal so the test does not trust the module
TOKEN = "test-token-0123456789abcdef"
_tmp = tempfile.TemporaryDirectory()
ALLOWLIST_PATH = os.path.join(_tmp.name, "authenticated-emails.txt")
os.environ.update({
    "RELAY_METRICS_BEARER_TOKEN": TOKEN,
    "RELAY_METRICS_ALLOWED_NAMES": "relay-a",
    "RELAY_METRICS_INGEST_DIR": os.path.join(_tmp.name, "ingest"),
    "ALLOWLIST_PUBLISHERS": "thor-a",
    "ALLOWLIST_FILE": ALLOWLIST_PATH,
    "ALLOWLIST_MIN": "2",
})
_spec = importlib.util.spec_from_file_location("ingest", HERE / "relay-metrics-ingest.py")
ingest = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(ingest)
ingest.log = lambda msg: None


def tearDownModule():
    _tmp.cleanup()


class RecordingRe:
    """Stands in for EMAIL_RE and records the length of every input it sees."""

    def __init__(self, real):
        self.real = real
        self.lengths = []

    def match(self, s):
        self.lengths.append(len(s))
        # Never run the real pattern on a long input: on the old code that is
        # the quadratic backtrack this test exists to keep out.
        return self.real.match(s) if len(s) <= 1024 else None


class AllowlistIngestTests(unittest.TestCase):
    def setUp(self):
        self.real_re = ingest.EMAIL_RE
        self.rec = RecordingRe(self.real_re)
        ingest.EMAIL_RE = self.rec
        self.srv = ThreadingHTTPServer(("127.0.0.1", 0), ingest.Handler)
        threading.Thread(target=self.srv.serve_forever, daemon=True).start()

    def tearDown(self):
        ingest.EMAIL_RE = self.real_re
        self.srv.shutdown()
        self.srv.server_close()

    def post(self, body):
        conn = http.client.HTTPConnection("127.0.0.1", self.srv.server_address[1], timeout=10)
        try:
            conn.request("POST", "/relay-health-allowlist-ingest/thor-a", body=body,
                         headers={"Authorization": f"Bearer {TOKEN}"})
            resp = conn.getresponse()
            return resp.status, resp.read()
        finally:
            conn.close()

    def written(self):
        with open(ALLOWLIST_PATH) as f:
            return f.read().split()

    def test_valid_roster_is_written(self):
        status, _ = self.post(json.dumps({"emails": ["B@example.com", "a@example.com"]}))
        self.assertEqual(status, 200)
        self.assertEqual(self.written(), ["a@example.com", "b@example.com"])

    def test_overlong_token_never_reaches_the_regex(self):
        # '!@!.' followed by many '!.' and a trailing '@' is the input CodeQL
        # reports as quadratic for EMAIL_RE. It must be dropped by length.
        evil = "!@!." + "!." * 50000 + "@"
        status, _ = self.post(json.dumps({"emails": ["a@example.com", "b@example.com", evil]}))
        self.assertEqual(status, 200)
        self.assertLessEqual(max(self.rec.lengths), EMAIL_MAX)
        self.assertEqual(self.written(), ["a@example.com", "b@example.com"])

    def test_plaintext_body_is_capped_too(self):
        evil = "!@!." + "!." * 50000 + "@"
        status, _ = self.post(f"a@example.com\nb@example.com\n{evil}\n")
        self.assertEqual(status, 200)
        self.assertLessEqual(max(self.rec.lengths), EMAIL_MAX)

    def test_longest_legal_address_is_kept(self):
        longest = "a" * 64 + "@" + "b" * 185 + ".com"
        self.assertEqual(len(longest), EMAIL_MAX)
        status, _ = self.post(json.dumps({"emails": ["a@example.com", longest]}))
        self.assertEqual(status, 200)
        self.assertIn(longest, self.written())


if __name__ == "__main__":
    unittest.main()
