#!/usr/bin/env python3
import subprocess
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).with_name('check_pr_head_checks.py')
import json


class HeadChecks(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.git('init', '-q')
        self.git('config', 'user.name', 'Fixture')
        self.git('config', 'user.email', 'fixture@example.test')
        self.git('-c', 'commit.gpgsign=false', 'commit', '--allow-empty', '-qm', 'base')
        self.base = self.git('rev-parse', 'HEAD')
        self.git('-c', 'commit.gpgsign=false', 'commit', '--allow-empty', '-qm', 'repair')
        self.head = self.git('rev-parse', 'HEAD')

    def git(self, *args):
        return subprocess.check_output(['git', '-C', str(self.root), *args], text=True).strip()

    def run_check(self, remote, rows, expected, head=None):
        data = [{'headRefOid': remote, 'statusCheckRollup': rows}]
        result = subprocess.run(['python3', str(SCRIPT), head or self.head], cwd=self.root,
                                input=json.dumps(data), capture_output=True, text=True)
        self.assertEqual(result.returncode, expected, result.stdout + result.stderr)
        return result.stdout + result.stderr

    def test_failed_ancestor_permits_repair_but_demands_new_ci(self):
        text = self.run_check(self.base, [self.row('FAILURE')], 0)
        self.assertIn('Old failures remain unresolved', text)

    @staticmethod
    def row(conclusion='SUCCESS', status='COMPLETED'):
        return {'__typename': 'CheckRun', 'name': 'scan', 'status': status, 'conclusion': conclusion}

    def test_same_head_success_and_pending(self):
        self.run_check(self.head, [self.row()], 0)
        self.run_check(self.head, [self.row('', 'IN_PROGRESS')], 0)

    def test_same_head_failures_and_missing_evidence(self):
        for rows in ([], None, [self.row('FAILURE')], [self.row('CANCELLED')],
                     [self.row('', 'UNKNOWN')], [{}], ['bad'],
                     [{'__typename': 'StatusContext', 'state': 'ERROR'}]):
            with self.subTest(rows=rows):
                self.run_check(self.head, rows, 1)

    def test_legacy_success(self):
        self.run_check(self.head, [{'__typename': 'StatusContext', 'state': 'SUCCESS'}], 0)

    def test_unrelated_or_unknown_remote_cannot_defer(self):
        self.run_check('f' * 40, [], 1)
        self.run_check('bad', [], 1)
        self.run_check(self.head, [], 1, head=self.base)

    def test_absent_pr_and_malformed_api(self):
        for data, code in [('[]', 0), ('{}', 1), ('invalid', 1), ('[{},{}]', 1)]:
            result = subprocess.run(['python3', str(SCRIPT), self.head], cwd=self.root,
                                    input=data, capture_output=True, text=True)
            self.assertEqual(result.returncode, code)


if __name__ == '__main__':
    unittest.main()
