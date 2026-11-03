#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Tests for help-desk-qr.py (docs/design/help-desk-service-pattern.md §4)."""
from __future__ import annotations

import importlib.util
import os
import subprocess
import sys
import tempfile
import unittest
import xml.etree.ElementTree as ET
from pathlib import Path

HERE = Path(__file__).resolve().parent
SCRIPT = HERE / "help-desk-qr.py"

spec = importlib.util.spec_from_file_location("help_desk_qr", SCRIPT)
hq = importlib.util.module_from_spec(spec)
spec.loader.exec_module(hq)

INSIDE = "https://help.zagros.netbird.zagros.example"
OUTSIDE = "https://example.invalid/help"


def run(*args):
    return subprocess.run([sys.executable, str(SCRIPT), *args], capture_output=True, text=True)


class RenderWithOverrides(unittest.TestCase):
    def test_three_files_and_wording(self):
        with tempfile.TemporaryDirectory() as tmp:
            r = run("--site", "nosuchsite", "--inside-url", INSIDE, "--outside-url", OUTSIDE,
                    "--product-name", "Test Fabric", "--out", tmp)
            self.assertEqual(r.returncode, 0, r.stderr)
            for name in ("inside.svg", "outside.svg", "card.html"):
                self.assertTrue(os.path.exists(os.path.join(tmp, name)), name)
                if name.endswith(".svg"):
                    ET.parse(os.path.join(tmp, name))  # well-formed XML
            card = Path(tmp, "card.html").read_text()
            self.assertIn(INSIDE, card)
            self.assertIn(OUTSIDE, card)
            self.assertIn("A — on the Test Fabric", card)
            self.assertIn("B — cannot get on", card)
            self.assertIn("Works on a phone on mobile data", card)

    def test_check_mode_keeps_nothing(self):
        with tempfile.TemporaryDirectory() as tmp:
            r = run("--site", "nosuchsite", "--inside-url", INSIDE, "--outside-url", OUTSIDE,
                    "--check", "--out", tmp)
            self.assertEqual(r.returncode, 0, r.stderr)
            self.assertEqual(os.listdir(tmp), [])


class RefusesMissingAddress(unittest.TestCase):
    def test_unknown_site_exits_2(self):
        r = run("--site", "nosuchsite")
        self.assertEqual(r.returncode, 2)
        self.assertIn("nosuchsite", r.stderr)

    @unittest.skipUnless((Path(__file__).resolve().parents[2] / "ops/ansible/group_vars/zagros_hosts").is_dir(), "no declared goatnet in this tree")
    def test_real_site_without_the_rows_exits_2(self):
        # zagros declares services but (until phases 4-5 land) no help row and
        # no help_desk_public_url; an inside override alone must not suffice.
        r = run("--site", "zagros", "--inside-url", INSIDE)
        self.assertEqual(r.returncode, 2)
        self.assertIn("help_desk_public_url", r.stderr)

    def test_non_url_refused(self):
        r = run("--site", "nosuchsite", "--inside-url", "help.example", "--outside-url", OUTSIDE)
        self.assertEqual(r.returncode, 2)


class BuiltinEncoder(unittest.TestCase):
    def test_matrix_shape_and_finders(self):
        g = hq.qr_matrix(INSIDE)
        n = len(g)
        self.assertEqual((n - 17) % 4, 0)
        self.assertTrue(all(len(row) == n for row in g))
        # the three finder patterns' outer rings are dark
        for r0, c0 in ((0, 0), (0, n - 7), (n - 7, 0)):
            self.assertEqual(g[r0][c0:c0 + 7], [1] * 7)
            self.assertEqual(g[r0 + 6][c0:c0 + 7], [1] * 7)
        self.assertEqual(g[n - 8][8], 1)  # the fixed dark module

    def test_too_long_refused(self):
        with self.assertRaises(ValueError):
            hq.qr_matrix("https://" + "x" * 400)


if __name__ == "__main__":
    unittest.main()
