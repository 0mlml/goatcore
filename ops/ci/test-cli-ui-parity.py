#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Regression tests for the three false-success paths reported on goat#1532.

Run offline with the standard library: python3 ops/ci/test-cli-ui-parity.py.
The executable tests stage the guard and matrix in a temporary tree so both
--docs and --goat-bin exercise the real exit-code contract without a Rust build.
"""
from __future__ import annotations

import importlib.util
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
GUARD = HERE / "check-cli-ui-parity.py"
MATRIX = HERE.parents[1] / "docs/design/goat-cli-ui-parity.md"
spec = importlib.util.spec_from_file_location("cli_ui_parity", GUARD)
parity = importlib.util.module_from_spec(spec)
spec.loader.exec_module(parity)


def inventory(*commands: str) -> str:
    return "# goat — command reference\n\n## `goat`\n\n" + "".join(
        f"## `goat {command}`\n\n" for command in commands
    )


def matrix(*entries: str) -> str:
    return "| Verb | Status | Note |\n|---|---|---|\n" + "".join(
        f"| `{entry}` | CLI-ONLY | Documented |\n" for entry in entries
    )


def parity_matrix(*rows: tuple[str, str, str]) -> str:
    return "| Verb | Status | Note |\n|---|---|---|\n" + "".join(
        f"| `{verb}` | {status} | {note} |\n" for verb, status, note in rows
    )


class CoverageTests(unittest.TestCase):
    def test_only_verb_cells_grant_coverage(self):
        text = """Prose mentions `topic` and `admin lan`.

| Verb | Status | Note |
|---|---|---|
| `topic ls\\|show`, `topic create` | `status-pretender` | `topic delete` |

| Field | Value |
|---|---|
| Example | `unrelated` |

```markdown
| Verb | Note |
|---|---|
| `example-only` | In a code block |
```

| Status | Verb | Note |
|:---|:---:|---:|
| CLI-ONLY | `doctor` | `note-pretender` |
"""
        exact, globs = parity.matrix_coverage(text)
        self.assertEqual(exact, {"topic ls", "topic show", "topic create", "doctor"})
        self.assertEqual(globs, [])

    def test_shorthand_and_argument_values_remain_supported(self):
        exact, globs = parity.matrix_coverage(matrix(
            r"admin queue ls\|approve\|reject\|sponsor-*",
            r"machine enroll[--bulk]\|mesh-key",
            "admin ssh-key submit <handle>",
            "admin onboard --revoke [--purge]",
            r"admin pqc on\|off",
            r"org\|topic ls\|show",
            "client *",
        ))
        for leaf in (
            "admin queue ls", "admin queue approve", "admin queue reject",
            "admin queue sponsor-approve", "admin queue sponsor-reject",
            "machine enroll", "machine mesh-key", "admin ssh-key submit",
            "admin onboard", "admin pqc", "org ls", "org show",
            "topic ls", "topic show", "client status",
        ):
            with self.subTest(leaf=leaf):
                self.assertTrue(parity.covered(leaf, exact, globs))
        for leaf in (
            "admin queue new-verb", "admin queue sponsorish",
            "admin queue sponsor-approve inspect", "client status inspect",
            "machine enroll inspect", "admin ssh-key submit inspect",
            "admin onboard inspect", "admin pqc inspect", "org show-more",
        ):
            with self.subTest(leaf=leaf):
                self.assertFalse(parity.covered(leaf, exact, globs))

    def test_parent_rows_do_not_cover_descendants(self):
        exact, globs = parity.matrix_coverage(matrix("admin members move"))
        self.assertTrue(parity.covered("admin members move", exact, globs))
        self.assertFalse(parity.covered("admin members move inspect", exact, globs))

    def test_leaf_inventory_deduplicates_and_omits_groups(self):
        self.assertEqual(parity.cli_leaf_commands(inventory(
            "topic", "topic ls", "topic show", "topic ls",
        )), ["topic ls", "topic show"])

    def test_only_gap_statuses_require_owners(self):
        text = parity_matrix(
            ("topic absent", "**ABSENT**", "Owned by goat#123"),
            ("topic partial", "PARTIAL (one action)", "Owned by #456"),
            ("topic sentence", "PARTIAL", "Goat#789 owns the rest"),
            ("topic console", "UI:console", "No issue"),
            ("topic portal", "UI:portal", "No issue"),
            ("topic sibling", "SIBLING(netvis)", "No issue"),
            ("topic local", "CLI-ONLY", "No issue"),
        )
        self.assertEqual(parity.unowned_gap_rows(text), [])


class GuardExitTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="cli-ui-parity-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.guard = self.root / "ops/ci/check-cli-ui-parity.py"
        self.matrix = self.root / "docs/design/goat-cli-ui-parity.md"
        self.guard.parent.mkdir(parents=True)
        self.matrix.parent.mkdir(parents=True)
        self.guard.write_text(GUARD.read_text(encoding="utf-8"), encoding="utf-8")
        self.matrix.write_text(MATRIX.read_text(encoding="utf-8"), encoding="utf-8")

    def run_guard(self, markdown: str, mode: str, binary_exit: int = 0):
        if mode == "docs":
            docs = self.root / "inventory.md"
            docs.write_text(markdown, encoding="utf-8")
            args = ["--docs", str(docs)]
        else:
            goat = self.root / "fake-goat"
            goat.write_text(
                f"#!{sys.executable}\nimport sys\n"
                "assert sys.argv[1:] == ['docs', '--markdown']\n"
                f"sys.stdout.write({markdown!r})\nsys.exit({binary_exit})\n",
                encoding="utf-8",
            )
            goat.chmod(0o755)
            args = ["--goat-bin", str(goat)]
        return self.invoke(*args)

    def invoke(self, *args):
        return subprocess.run(
            [sys.executable, str(self.guard), "--report", *args],
            capture_output=True, text=True, timeout=10,
        )

    def test_valid_inventory_passes_through_both_inputs(self):
        markdown = inventory(
            "docs", "topic ls", "topic show", "admin members move",
            "admin pqc", "admin ssh-key submit", "machine enroll",
            "admin queue sponsor-approve", "access key", "access key rotate",
            "client status", "audit emit",
        )
        for mode in ("docs", "binary"):
            with self.subTest(mode=mode):
                result = self.run_guard(markdown, mode)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertIn("all 9 operator-facing verbs", result.stdout)
                self.assertNotIn("goat client status", result.stdout)
                self.assertNotIn("goat audit emit", result.stdout)

    def test_prose_and_note_mentions_do_not_hide_new_verbs(self):
        for command in ("topic new-undocumented-verb", "admin lan totally-new-verb"):
            for mode in ("docs", "binary"):
                with self.subTest(command=command, mode=mode):
                    result = self.run_guard(inventory("docs", "topic ls", command), mode)
                    self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
                    self.assertIn(f"- goat {command}", result.stderr)
                    self.assertNotIn("cli-ui-parity: OK", result.stdout)

    def test_new_descendant_requires_its_own_row(self):
        self.matrix.write_text(matrix("admin members move"), encoding="utf-8")
        for mode in ("docs", "binary"):
            with self.subTest(mode=mode):
                result = self.run_guard(inventory(
                    "admin members move", "admin members move inspect",
                ), mode)
                self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
                self.assertIn("- goat admin members move inspect", result.stderr)

    def test_absent_and_partial_rows_fail_without_owner_and_pass_with_one(self):
        for status, issue_ref in (("**ABSENT**", "goat#123"), ("PARTIAL", "#456")):
            with self.subTest(status=status, owner="missing"):
                self.matrix.write_text(
                    parity_matrix(("topic ls", status, "No rendered surface")),
                    encoding="utf-8",
                )
                result = self.invoke("--goat-bin", str(self.root / "missing-goat"))
                self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
                self.assertIn("`topic ls`", result.stderr)
                self.assertIn(status, result.stderr)
                self.assertNotIn("inventory unavailable", result.stderr)

            with self.subTest(status=status, owner=issue_ref):
                self.matrix.write_text(
                    parity_matrix(("topic ls", status, f"Owned by {issue_ref}")),
                    encoding="utf-8",
                )
                result = self.run_guard(inventory("topic ls"), "docs")
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_covered_statuses_do_not_require_owners(self):
        rows = (
            ("topic console", "UI:console", "Covered"),
            ("topic portal", "UI:portal", "Covered"),
            ("topic sibling", "SIBLING(netvis)", "Covered"),
            ("topic local", "CLI-ONLY", "Deliberately local"),
        )
        self.matrix.write_text(parity_matrix(*rows), encoding="utf-8")
        result = self.run_guard(inventory(*(row[0] for row in rows)), "docs")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_unavailable_inventories_never_pass(self):
        invalid = (
            "", " \n\t", "this is not a command reference\n",
            "## `goat`\n", "## `goat docs`\n",
            "## `goat`\n## `goatdocs`\n",
            inventory("client status", "audit emit"),
        )
        for markdown in invalid:
            for mode in ("docs", "binary"):
                with self.subTest(markdown=markdown, mode=mode):
                    result = self.run_guard(markdown, mode)
                    self.assertEqual(result.returncode, 75, result.stdout + result.stderr)
                    self.assertIn("inventory unavailable", result.stderr)
                    self.assertNotIn("cli-ui-parity: OK", result.stdout)
                    self.assertNotIn("Traceback", result.stderr)

    def test_failed_generator_and_unreadable_inputs_return_75(self):
        results = [
            self.run_guard(inventory("docs"), "binary", binary_exit=1),
            self.invoke("--docs", str(self.root / "missing.md")),
            self.invoke("--goat-bin", str(self.root / "missing-goat")),
        ]
        invalid_utf8 = self.root / "invalid-utf8.md"
        invalid_utf8.write_bytes(b"\xff")
        results.append(self.invoke("--docs", str(invalid_utf8)))
        for result in results:
            with self.subTest(stderr=result.stderr):
                self.assertEqual(result.returncode, 75, result.stdout + result.stderr)
                self.assertIn("inventory unavailable", result.stderr)
                self.assertNotIn("cli-ui-parity: OK", result.stdout)
                self.assertNotIn("Traceback", result.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
