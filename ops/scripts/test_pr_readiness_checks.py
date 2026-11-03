"""Exercise the production PR-check gate without network or repository writes."""

import json
import os
import shlex
import subprocess
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).with_name("pr-readiness.sh").read_text()
GATE = SCRIPT[
    SCRIPT.index("# Check 7 —") : SCRIPT.index(
        "\n\necho\n", SCRIPT.index("# Check 7 —")
    )
]
HEAD = "a" * 40
OLDER = "b" * 40


class CheckGateTests(unittest.TestCase):
    def run_gate(self, prs, *, api_status=0, ancestor_status=0, raw=False):
        payload = prs if raw else json.dumps(prs)
        setup = f"""
FAIL=0
BRANCH=feature
HEAD_SHA={HEAD}
SCRIPT_DIR={shlex.quote(str(Path(__file__).resolve().parent))}
fail() {{ echo "FAIL: $*"; FAIL=$((FAIL + 1)); }}
pass() {{ echo "PASS: $*"; }}
yel() {{ echo "WARN: $*"; }}
gh() {{ printf '%s' {shlex.quote(payload)}; return {api_status}; }}
"""
        # Main's shared Python validator invokes git in a subprocess, so a
        # shell function is insufficient. Keep the ancestry fixture offline.
        with tempfile.TemporaryDirectory(prefix="readiness-gate-") as root:
            fake_git = Path(root) / "git"
            fake_git.write_text(
                "#!/bin/sh\n"
                f'[ "$*" = "merge-base --is-ancestor {OLDER} {HEAD}" ] || exit 99\n'
                f"exit {ancestor_status}\n"
            )
            fake_git.chmod(0o755)
            return subprocess.run(
                ["bash", "-c", setup + GATE + '\nexit "$FAIL"\n'],
                env={**os.environ, "PATH": root + os.pathsep + os.environ["PATH"]},
                capture_output=True,
                text=True,
                check=False,
            )

    @staticmethod
    def pr(head=HEAD, checks=None):
        if isinstance(checks, list):
            checks = [({"__typename": "CheckRun", **row} if isinstance(row, dict)
                       and "status" in row else row) for row in checks]
        return [{"headRefOid": head, "statusCheckRollup": checks}]

    def test_current_completed_failures_block(self):
        for conclusion in ["FAILURE", "CANCELLED", "TIMED_OUT", "ACTION_REQUIRED"]:
            with self.subTest(conclusion=conclusion):
                result = self.run_gate(
                    self.pr(
                        checks=[
                            {
                                "name": "guards",
                                "status": "COMPLETED",
                                "conclusion": conclusion,
                            }
                        ]
                    )
                )
                self.assertEqual(result.returncode, 1)
                self.assertIn("current PR check failed:", result.stdout)

    def test_failure_with_empty_or_missing_name_blocks(self):
        for name in ["", None, []]:
            with self.subTest(name=name):
                result = self.run_gate(
                    self.pr(
                        checks=[
                            {
                                "name": name,
                                "status": "COMPLETED",
                                "conclusion": "FAILURE",
                            }
                        ]
                    )
                )
                self.assertEqual(result.returncode, 1)
                self.assertIn("current PR check failed:", result.stdout)

    def test_unclassifiable_status_blocks(self):
        result = self.run_gate(
            self.pr(
                checks=[
                    {
                        "status": "COMPLETED",
                        "conclusion": [],
                    }
                ]
            )
        )
        self.assertEqual(result.returncode, 1)
        self.assertIn("FAIL:", result.stdout)

    def test_current_empty_rollups_block(self):
        for checks in [None, []]:
            with self.subTest(checks=checks):
                result = self.run_gate(self.pr(checks=checks))
                self.assertEqual(result.returncode, 1)
                self.assertIn("no status-check evidence", result.stdout)

    def test_current_success_and_running_checks_allow_push(self):
        for status, conclusion in [("COMPLETED", "SUCCESS"), ("IN_PROGRESS", "")]:
            with self.subTest(status=status):
                result = self.run_gate(
                    self.pr(
                        checks=[
                            {
                                "name": "guards",
                                "status": status,
                                "conclusion": conclusion,
                            }
                        ]
                    )
                )
                self.assertEqual(result.returncode, 0, result.stderr)

    def test_older_failed_commit_requires_fresh_ci(self):
        result = self.run_gate(
            self.pr(
                OLDER,
                [
                    {
                        "name": "guards",
                        "status": "COMPLETED",
                        "conclusion": "FAILURE",
                    }
                ],
            )
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("PR checks belong to ancestor", result.stdout)
        self.assertIn("current-head CI passes after publication", result.stdout)
        self.assertNotIn("Current-head checks have no completed failures", result.stdout)

    def test_older_empty_rollup_is_not_called_green(self):
        result = self.run_gate(self.pr(OLDER, []))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Old failures remain unresolved", result.stdout)
        self.assertNotIn("Current-head checks have no completed failures", result.stdout)

    def test_diverged_or_unknown_remote_head_blocks(self):
        for code in [1, 128]:
            with self.subTest(code=code):
                result = self.run_gate(self.pr(OLDER, []), ancestor_status=code)
                self.assertEqual(result.returncode, 1)
                self.assertIn("not a verified ancestor", result.stdout)

    def test_api_failure_blocks(self):
        result = self.run_gate([], api_status=1)
        self.assertEqual(result.returncode, 1)
        self.assertIn("could not read", result.stdout)

    def test_invalid_response_blocks(self):
        for payload in [
            "oops",
            "{}",
            "[{}]",
            json.dumps(self.pr("invalid")),
            json.dumps(self.pr(checks=[1])),
            json.dumps(self.pr() * 2),
        ]:
            with self.subTest(payload=payload):
                result = self.run_gate(payload, raw=True)
                self.assertEqual(result.returncode, 1)
                self.assertIn("FAIL:", result.stdout)

    def test_no_pr_defers_to_premerge(self):
        result = self.run_gate([])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("No open PR", result.stdout)

    def test_legacy_failed_status_blocks(self):
        result = self.run_gate(
            self.pr(
                checks=[
                    {
                        "__typename": "StatusContext",
                        "state": "FAILURE",
                        "context": "external",
                    }
                ]
            )
        )
        self.assertEqual(result.returncode, 1)
        self.assertIn("current PR status failed: external", result.stdout)


if __name__ == "__main__":
    unittest.main()
