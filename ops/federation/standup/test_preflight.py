"""The phase-12 dependency check fails closed without probing a real host."""
import contextlib
import importlib.util
import io
from pathlib import Path
import subprocess
import unittest
import unittest.mock as mock


class DockerPreflight(unittest.TestCase):
    def setUp(self):
        spec = importlib.util.spec_from_file_location("preflight", Path(__file__).with_name("preflight.py"))
        self.preflight = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.preflight)

    def check(self, binary="docker", result=None, error=None):
        out = io.StringIO()
        with mock.patch("shutil.which", return_value=binary), \
                mock.patch("subprocess.run", return_value=result, side_effect=error) as run, \
                contextlib.redirect_stdout(out):
            self.preflight.check_local_docker()
        return out.getvalue(), run

    def test_missing_binary_is_a_named_refusal(self):
        out, run = self.check(binary=None)
        self.assertEqual(self.preflight.problems, ["docker compose plugin (phase 12)"])
        self.assertIn("no docker", out)
        run.assert_not_called()

    def test_timeout_and_failed_execution_are_refused(self):
        for error in [subprocess.TimeoutExpired("docker", 15), OSError("cannot execute")]:
            with self.subTest(error=error):
                self.preflight.problems.clear()
                self.check(error=error)
                self.assertEqual(self.preflight.problems, ["docker compose plugin (phase 12)"])

    def test_missing_plugin_is_refused(self):
        out, _ = self.check(result=subprocess.CompletedProcess([], 1, "", "missing"))
        self.assertTrue(self.preflight.problems)
        self.assertIn("Install", out)

    def test_empty_success_is_not_evidence_of_a_version(self):
        out, _ = self.check(result=subprocess.CompletedProcess([], 0, "", ""))
        self.assertTrue(self.preflight.problems)
        self.assertIn("no version", out)

    def test_verified_version_passes(self):
        out, run = self.check(result=subprocess.CompletedProcess([], 0, "Docker Compose version v2.0\n", ""))
        self.assertEqual(self.preflight.problems, [])
        self.assertIn("Docker Compose version v2.0", out)
        run.assert_called_once_with(["docker", "compose", "version"], capture_output=True,
                                    text=True, timeout=15)


if __name__ == "__main__":
    unittest.main()
