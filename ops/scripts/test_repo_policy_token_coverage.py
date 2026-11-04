"""The deploy-time coverage check refuses a token that cannot see every policy repository (#2107)."""
import http.server
import importlib.util
import json
import os
import pathlib
import tempfile
import threading
import unittest

HERE = pathlib.Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("cov", HERE / "repo-policy-token-coverage.py")
cov = importlib.util.module_from_spec(spec)
spec.loader.exec_module(cov)

POLICY = """
version: 1
default_owner: usareur-af
rules:
  - name: everyone
    match: {}
    grant:
      - repo: zagros-moon-pod
        permission: pull
      - repo: hello-galaxy-time-hygiene
        permission: pull
  - name: admins
    match: {tier: [platform-admin]}
    grant:
      - repo: hello-galaxy-goatnet-from-scratch
      - repo: zagros-moon-pod
"""


def serve(visible):
    """A stand-in GitHub API: 200 for repositories the token sees, 404 otherwise."""
    class H(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            ok = self.headers.get("Authorization") == "Bearer t0k" and self.path[len("/repos/"):] in visible
            self.send_response(200 if ok else 404)
            self.end_headers()
            self.wfile.write(b"{}")

        def log_message(self, *a):
            pass

    srv = http.server.HTTPServer(("127.0.0.1", 0), H)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return srv


class Coverage(unittest.TestCase):
    def setUp(self):
        self.policy = tempfile.NamedTemporaryFile("w", suffix=".yaml", delete=False)
        self.policy.write(POLICY)
        self.policy.close()
        os.environ["GITHUB_ONBOARD_TOKEN"] = "t0k"

    def tearDown(self):
        os.unlink(self.policy.name)

    def test_lists_each_repo_once_with_the_default_owner(self):
        self.assertEqual(cov.policy_repos(POLICY), [
            "USAREUR-AF/zagros-moon-pod", "usareur-af/hello-galaxy-time-hygiene",
            "USAREUR-AF/hello-galaxy-goatenv"])

    def test_passes_when_the_token_sees_every_repo(self):
        srv = serve({"USAREUR-AF/zagros-moon-pod", "usareur-af/hello-galaxy-time-hygiene",
                     "USAREUR-AF/hello-galaxy-goatenv"})
        try:
            self.assertEqual(cov.main(["--policy", self.policy.name, "--api", f"http://127.0.0.1:{srv.server_port}"]), 0)
        finally:
            srv.shutdown()

    def test_fails_when_one_policy_repo_is_outside_the_token(self):
        srv = serve({"USAREUR-AF/zagros-moon-pod", "USAREUR-AF/hello-galaxy-goatenv"})
        try:
            from io import StringIO
            from contextlib import redirect_stdout
            buf = StringIO()
            with redirect_stdout(buf):
                rc = cov.main(["--policy", self.policy.name, "--api", f"http://127.0.0.1:{srv.server_port}", "--json"])
            self.assertEqual(rc, 1)
            out = json.loads(buf.getvalue())
            self.assertEqual(out["not_covered"], [{"repo": "usareur-af/hello-galaxy-time-hygiene", "status": 404}])
        finally:
            srv.shutdown()

    def test_refuses_without_a_token(self):
        os.environ["GITHUB_ONBOARD_TOKEN"] = ""
        self.assertEqual(cov.main(["--policy", self.policy.name]), 2)


if __name__ == "__main__":
    unittest.main()
