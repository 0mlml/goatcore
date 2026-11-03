#!/usr/bin/env python3
"""Render and execute the production health script with synthetic host commands."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

from jinja2 import Environment, StrictUndefined

ROOT = Path(__file__).resolve().parents[1]
BASH = os.environ.get("BASH_UNDER_TEST") or shutil.which("bash")
MOCKS = r'''
function [() {
  if [[ "$1" == '-e' && "$2" == '/dev/mapper/test-volume' ]]; then
    [[ "$MOUNTED" == 1 ]]; return
  fi
  builtin [ "$@"
}
mountpoint() { [[ "$MOUNTED" == 1 ]]; }
clevis() {
  case "$BINDINGS" in
    error) return 23 ;;
    empty) return 0 ;;
    *) printf '1: tang {"url":"http://tang-one.example:7500"}\n2: tang {"url":"http://tang-two.example:7500"}\n' ;;
  esac
}
curl() {
  [[ "$ANSWERING" == 2 ]] && return 0
  [[ "$ANSWERING" == 1 && "${*: -1}" == http://tang-one.example:7500/adv ]]
}
systemctl() {
  case "$1" in
    is-enabled) return 1 ;;
    is-active) [[ "$UNIT_STATE" == active ]] ;;
    show)
      [[ "$UNIT_STATE" == unknown ]] && return 1
      case "$*" in
        *SubState*) printf '%s\n' "$UNIT_SUBSTATE" ;;
        *) printf '%s\n' "$UNIT_STATE" ;;
      esac ;;
    restart) printf '%s\n' "$*" >> "$CALL_LOG" ;;
    *) return 99 ;;
  esac
}
journalctl() { return 0; }
systemd-analyze() { return 0; }
logger() { return 0; }
'''


class Health(unittest.TestCase):
    def run_health(self, mode="tang_client", bindings="two", answering=2,
                   mounted=1, state="active", substate="exited"):
        env = Environment(undefined=StrictUndefined)
        env.filters["intersect"] = lambda a, b: [x for x in a if x in b]
        source = ROOT / "ops/ansible/roles/luks-unlock/templates/goat-unlock-health.sh.j2"
        with tempfile.TemporaryDirectory() as tmp:
            folder = Path(tmp)
            script = folder / "health.sh"
            script.write_text(env.from_string(source.read_text()).render(
                luks_health_textfile_dir=tmp, luks_tang_min_reachable=2,
                luks_volumes=[dict(name="test-volume", device="/dev/fixture",
                                   mount_point="/mnt/fixture", unlock_modes=[mode])]))
            calls = folder / "calls.log"
            calls.touch()
            variables = dict(PATH=os.environ["PATH"], MOUNTED=str(mounted), BINDINGS=bindings,
                             ANSWERING=str(answering), UNIT_STATE=state,
                             UNIT_SUBSTATE=substate, CALL_LOG=str(calls))
            result = subprocess.run([BASH, "-c", MOCKS + '\nsource "$1"', "fixture", str(script)],
                                    env=variables, capture_output=True, text=True)
            self.assertEqual(result.stderr, "")
            return result, calls.read_text(), (folder / "goat_luks_unlock.prom").read_text()

    def test_healthy(self):
        result, calls, _ = self.run_health()
        self.assertEqual(result.returncode, 0, result.stdout)
        self.assertIn("unlock-health: PASS", result.stdout)
        self.assertEqual(calls, "")

    def test_completed_unlock_retried(self):
        result, calls, _ = self.run_health(mounted=0)
        self.assertEqual(result.returncode, 1)
        self.assertIn("restart --no-block goat-unlock-test-volume.service", calls)

    def test_inactive_and_failed_unlock_retried(self):
        for state in ("inactive", "failed"):
            with self.subTest(state=state):
                _, calls, _ = self.run_health(mounted=0, state=state, substate="dead")
                self.assertIn("restart --no-block", calls)

    def test_running_or_transitioning_unlock_left_alone(self):
        for state, substate in (("active", "running"), ("activating", "start"),
                                ("deactivating", "stop"), ("reloading", "reload"),
                                ("unknown", "unknown")):
            with self.subTest(state=state, substate=substate):
                result, calls, _ = self.run_health(mounted=0, state=state, substate=substate)
                self.assertEqual(result.returncode, 1)
                self.assertEqual(calls, "")

    def test_retry_then_recovery(self):
        _, calls, _ = self.run_health(mounted=0)
        self.assertIn("restart --no-block", calls)
        result, calls, _ = self.run_health(mounted=1)
        self.assertEqual(result.returncode, 0)
        self.assertEqual(calls, "")

    def test_failed_binding_read_is_unhealthy(self):
        result, _, metrics = self.run_health(bindings="error")
        self.assertEqual(result.returncode, 1, result.stdout)
        self.assertIn("binding", result.stdout)
        self.assertIn('goat_luks_binding_read_ok{volume="test-volume"} 0', metrics)

    def test_required_bindings_missing_is_unhealthy(self):
        result, _, metrics = self.run_health(bindings="empty")
        self.assertEqual(result.returncode, 1, result.stdout)
        self.assertIn('goat_luks_tang_min_reachable{volume="test-volume"} 2', metrics)

    def test_degraded_quorum(self):
        result, _, _ = self.run_health(answering=1)
        self.assertEqual(result.returncode, 1)
        self.assertIn("1/2 Tang", result.stdout)

    def test_sss_without_tang_remains_valid(self):
        result, _, _ = self.run_health(mode="sss", bindings="empty")
        self.assertEqual(result.returncode, 0, result.stdout)

    def test_sss_binding_read_failure_not_hidden(self):
        result, _, _ = self.run_health(mode="sss", bindings="error")
        self.assertEqual(result.returncode, 1, result.stdout)


if __name__ == "__main__":
    unittest.main()
