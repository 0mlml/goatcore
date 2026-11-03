"""Offline catalog guard: generated controls, planted drift and unreadable inputs."""

import contextlib
import copy
import importlib.util
import io
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
import unittest.mock

HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("check_org_catalog", HERE / "check-org-catalog.py")
guard = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(guard)

EXPORT = {"orgs": [
    {"id": "zulu", "display_name": "Unit: 'north' # 1", "kind": "partner",
     "status": "active", "aliases": ["z-last", "a-first"]},
    {"id": "acme", "display_name": "Tartu Ülikool, Institute of Computer Science and "
     "a long name that wraps onto another line", "kind": "civil", "status": "active",
     "aliases": ["yes", "null", "007", "2026-01-01"]},
]}


class CatalogGuardTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.directory = Path(tmp.name)
        self.path = self.directory / "test.yaml"
        self.generated = guard.SYNC.render_catalog(copy.deepcopy(EXPORT), "test")
        self.path.write_text(self.generated, encoding="utf-8")

    def run_guard(self):
        out, err = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err), unittest.mock.patch.object(
            guard.SYNC, "fetch_catalog", side_effect=AssertionError("guard must not fetch")
        ):
            code = guard.main(["--catalog-dir", str(self.directory)])
        return code, out.getvalue(), err.getvalue()

    def test_generator_output_passes_without_writing_or_fetching(self):
        before = self.path.stat().st_mtime_ns
        code, out, err = self.run_guard()
        self.assertEqual((code, err), (0, ""))
        self.assertIn("test.yaml matches generated form (2 orgs)", out)
        self.assertEqual(self.path.read_text(), self.generated)
        self.assertEqual(self.path.stat().st_mtime_ns, before)

    def test_planted_drift_fails_with_diff_and_leaves_input_untouched(self):
        cases = {
            "comment": self.generated.replace("- id: acme", "- id: acme # edited"),
            "trailing spaces": self.generated.replace("- id: acme", "- id: acme   "),
            "extra field": self.generated + "extra: ignored\n",
            "environment": self.generated.replace("env: test", "env: other"),
            "key order": self.generated.replace("schema_version: 1\nenv: test", "env: test\nschema_version: 1"),
            "newline": self.generated.rstrip("\n"),
            "crlf": self.generated.replace("\n", "\r\n"),
        }
        for label, text in cases.items():
            with self.subTest(label=label):
                self.path.write_bytes(text.encode("utf-8"))
                code, _, err = self.run_guard()
                self.assertEqual(code, 1)
                self.assertIn(f"DRIFT: {self.path}", err)
                self.assertIn("@@", err)
                self.assertEqual(self.path.read_bytes(), text.encode("utf-8"))

    def test_malformed_and_invalid_catalogs_fail(self):
        cases = ["orgs: [", "", "[]", "{}", "orgs: {}", "orgs: [false]",
                 "orgs: [{}]", "orgs: [{id: 7}]", "orgs: [{id: ''}]",
                 "orgs: [{id: acme, aliases: alias}]",
                 "orgs: [{id: acme, aliases: [false]}]",
                 "!!python/object/apply:os.system ['false']"]
        for text in cases:
            with self.subTest(text=text):
                self.path.write_text(text)
                code, _, err = self.run_guard()
                self.assertEqual(code, 1)
                self.assertIn(f"ERROR: {self.path}", err)
                self.assertEqual(self.path.read_text(), text)

    def test_unsorted_orgs_and_aliases_fail(self):
        for field in ("orgs", "aliases"):
            with self.subTest(field=field):
                document = guard.yaml.safe_load(self.generated)
                if field == "orgs":
                    document["orgs"].reverse()
                else:
                    document["orgs"][0]["aliases"].reverse()
                text = guard.yaml.safe_dump(document, sort_keys=False, allow_unicode=True)
                self.path.write_text(text, encoding="utf-8")
                code, _, err = self.run_guard()
                self.assertEqual(code, 1)
                self.assertIn("DRIFT:", err)
                self.assertEqual(self.path.read_text(), text)

    def test_invalid_utf8_is_reported(self):
        self.path.write_bytes(b"\xff")
        code, _, err = self.run_guard()
        self.assertEqual(code, 1)
        self.assertIn(str(self.path), err)

    def test_unreadable_catalog_is_reported(self):
        with unittest.mock.patch.object(Path, "read_bytes", side_effect=PermissionError("denied")):
            code, _, err = self.run_guard()
        self.assertEqual(code, 1)
        self.assertIn(f"ERROR: {self.path}: denied", err)

    def test_empty_and_missing_directories_fail(self):
        self.path.unlink()
        code, _, err = self.run_guard()
        self.assertEqual(code, 1)
        self.assertIn("no YAML catalogs found", err)
        self.directory = self.directory / "missing"
        code, _, err = self.run_guard()
        self.assertEqual(code, 1)
        self.assertIn(str(self.directory), err)

    def test_all_catalogs_are_checked_even_after_a_failure(self):
        second = self.directory / "z-test.yml"
        second.write_text(guard.SYNC.render_catalog(EXPORT, "z-test"))
        for text in ("orgs: [", "orgs: [{id: acme, display_name: !!timestamp garbage}]",
                     "orgs: [{id: acme, display_name: !!bool garbage}]",
                     "orgs: [{id: acme, display_name: !!int ''}]",
                     "orgs: [{id: acme, display_name: !!float ''}]"):
            with self.subTest(text=text):
                self.path.write_text(text)
                code, out, err = self.run_guard()
                self.assertEqual(code, 1)
                self.assertIn(str(self.path), err)
                self.assertIn("z-test.yml matches generated form", out)
                self.assertEqual(self.path.read_text(), text)
                result = subprocess.run(
                    [sys.executable, str(HERE / "check-org-catalog.py"),
                     "--catalog-dir", str(self.directory)], capture_output=True, text=True, check=False)
                self.assertEqual(result.returncode, 1)
                self.assertIn(f"ERROR: {self.path}", result.stderr)
                self.assertNotIn("Traceback", result.stderr)
                self.assertIn("z-test.yml matches generated form", result.stdout)

    def test_cli_reports_failure_and_success_exit_codes(self):
        command = [sys.executable, str(HERE / "check-org-catalog.py"),
                   "--catalog-dir", str(self.directory)]
        result = subprocess.run(command, capture_output=True, text=True, check=False)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.path.write_text(self.generated + "# edit\n")
        result = subprocess.run(command, capture_output=True, text=True, check=False)
        self.assertEqual(result.returncode, 1)
        self.assertIn("DRIFT:", result.stderr)

    @unittest.skipUnless(guard.CATALOG_DIR.is_dir(), "no committed org catalog in this tree")
    def test_committed_catalogs_match_the_renderer(self):
        out, err = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            code = guard.main([])
        self.assertEqual(code, 0, err.getvalue())
        self.assertIn("matches generated form", out.getvalue())


if __name__ == "__main__":
    unittest.main()
