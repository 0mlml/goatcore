"""Exercise the pre-push CI gate with real Git ancestry and signed commits."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).with_name("pr-readiness.sh")


class CheckHeadTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="pr-readiness-ci-")
        self.addCleanup(self.temp.cleanup)
        root = Path(self.temp.name)
        self.repo = root / "repo"
        self.repo.mkdir()
        self.env = dict(os.environ, GIT_CONFIG_GLOBAL=os.devnull,
                        GIT_CONFIG_NOSYSTEM="1", GIT_TERMINAL_PROMPT="0")
        for key in list(self.env):
            if key.startswith("GIT_") and key not in {
                "GIT_CONFIG_GLOBAL", "GIT_CONFIG_NOSYSTEM", "GIT_TERMINAL_PROMPT"
            }:
                del self.env[key]
        self.git("init", "-q", "-b", "main")
        self.git("config", "user.name", "Fixture")
        self.git("config", "user.email", "fixture@example.test")
        self.git("config", "core.hooksPath", str(root / "no-hooks"))
        key = root / "signing-key"
        subprocess.run(["ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", str(key)], check=True)
        self.git("config", "gpg.format", "ssh")
        self.git("config", "user.signingkey", str(key))
        self.git("config", "commit.gpgsign", "true")
        signers = root / "allowed-signers"
        signers.write_text("fixture@example.test " + key.with_suffix(".pub").read_text())
        self.git("config", "gpg.ssh.allowedSignersFile", str(signers))
        self.git("commit", "--allow-empty", "-q", "-s", "-m", "seed")
        self.base = self.git("rev-parse", "HEAD")
        self.git("remote", "add", "origin", str(self.repo))
        self.git("fetch", "-q", "origin", "main")
        self.git("checkout", "-q", "-b", "fix/test")
        self.git("commit", "--allow-empty", "-q", "-s", "-m", "fix: original")
        self.ancestor = self.git("rev-parse", "HEAD")
        self.git("commit", "--allow-empty", "-q", "-s", "-m", "fix: correction")
        self.head = self.git("rev-parse", "HEAD")
        self.bin = root / "bin"
        self.bin.mkdir()
        gh = self.bin / "gh"
        gh.write_text("#!/bin/sh\ncase \" $* \" in\n"
                      "*' --state merged '*) echo '[]';;\n"
                      "*' pr view \'*) python3 -c 'import json,os; print(json.dumps(json.load(open(os.environ[\"PR_FIXTURE\"]))[0][\"statusCheckRollup\"]))';;\n"
                      "*) cat \"$PR_FIXTURE\"; exit \"$PR_FIXTURE_EXIT\";;\nesac\n")
        gh.chmod(0o755)
        self.response = root / "response.json"
        self.env.update(PATH=str(self.bin) + os.pathsep + os.environ["PATH"],
                        PR_FIXTURE=str(self.response), PR_FIXTURE_EXIT="0")

    def git(self, *args):
        return subprocess.check_output(["git", "-C", str(self.repo), *args],
                                       env=self.env, stderr=subprocess.PIPE, text=True).strip()

    def check(self, data, code, message):
        self.response.write_text(json.dumps(data))
        result = subprocess.run([shutil.which("bash"), str(SCRIPT)], cwd=self.repo,
                                env=self.env, text=True, capture_output=True)
        output = result.stdout + result.stderr
        self.assertEqual(result.returncode, code, output)
        self.assertIn(message, output)

    def pr(self, head=None, checks=None):
        rows = checks if checks is not None else [
            {"status": "COMPLETED", "conclusion": "FAILURE", "name": "guards"}]
        for row in rows:
            if isinstance(row, dict) and "status" in row:
                row.setdefault("__typename", "CheckRun")
        return [{"headRefOid": head or self.head, "statusCheckRollup": rows}]

    def test_current_failure_blocks(self):
        self.check(self.pr(), 1, "current PR check failed: guards")

    def test_ancestor_failure_allows_repair(self):
        self.check(self.pr(self.ancestor), 0, "Old failures remain unresolved until current-head CI passes after publication")

    def test_ancestor_empty_defers(self):
        self.check(self.pr(self.ancestor, []), 0, "PR checks belong to ancestor")

    def test_current_empty_blocks(self):
        self.check(self.pr(checks=[]), 1, "current PR head has no status-check evidence")

    def test_current_success(self):
        self.check(self.pr(checks=[{"status": "COMPLETED", "conclusion": "SUCCESS"}]),
                   0, "Current-head checks have no completed failures")

    def test_current_cancelled_blocks(self):
        self.check(self.pr(checks=[{"status": "COMPLETED", "conclusion": "CANCELLED", "name": "cancel"}]),
                   1, "current PR check failed: cancel")

    def test_pending_is_not_merge_certification(self):
        self.check(self.pr(checks=[{"status": "IN_PROGRESS", "conclusion": ""}]),
                   0, "no completed failures")

    def test_status_context_failure_blocks(self):
        self.check(self.pr(checks=[{"__typename": "StatusContext", "state": "FAILURE", "context": "external"}]),
                   1, "current PR status failed: external")

    def test_unknown_head_blocks(self):
        self.check(self.pr("f" * 40), 1, "not a verified ancestor")

    def test_diverged_head_blocks(self):
        self.git("checkout", "-q", "-b", "other", self.base)
        self.git("commit", "--allow-empty", "-q", "-s", "-m", "fix: divergent")
        other = self.git("rev-parse", "HEAD")
        self.git("checkout", "-q", "fix/test")
        self.check(self.pr(other), 1, "not a verified ancestor")

    def test_no_pr_defers(self):
        self.check([], 0, "No open PR")

    def test_api_failure_blocks(self):
        self.env["PR_FIXTURE_EXIT"] = "1"
        self.check([], 1, "could not read open PR state")

    def test_invalid_response_blocks(self):
        for data in ({}, self.pr("bad"), self.pr() * 2,
                     [{"headRefOid": self.head, "statusCheckRollup": None}]):
            with self.subTest(data=data):
                self.check(data, 1, "NOT READY")

    def test_malformed_check_blocks(self):
        for check in ({}, {"__typename": "Unknown", "status": "COMPLETED", "conclusion": "SUCCESS"}):
            with self.subTest(check=check):
                self.check(self.pr(checks=[check]), 1, "NOT READY")

    def test_failed_check_without_display_name_blocks(self):
        for name in (None, ""):
            with self.subTest(name=name):
                self.check(self.pr(checks=[{"status": "COMPLETED", "conclusion": "FAILURE", "name": name}]),
                           1, "current PR check failed:")

    def test_dirty_correction_still_blocks(self):
        (self.repo / "dirty").write_text("uncommitted")
        self.check(self.pr(self.ancestor), 1, "working tree dirty")


if __name__ == "__main__":
    unittest.main()
