#!/usr/bin/env python3
"""The Hydra guard accepts Ansible literals while retaining its refusals."""

import importlib.util
import tempfile
import unittest
from pathlib import Path

import yaml

SPEC = importlib.util.spec_from_file_location(
    "hydra_guard", Path(__file__).with_name("check-hydra-client-invariants.py"))
GUARD = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(GUARD)


class AnsibleLiteralTests(unittest.TestCase):
    def lint(self, text):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "tasks.yml"
            path.write_text(text)
            return GUARD.lint_file(path)

    def test_literal_does_not_hide_hydra_task(self):
        task = """
- name: Create fixture client
  ansible.builtin.uri:
    url: !unsafe 'http://hydra.invalid/admin/clients'
    method: POST
    body:
      skip_consent: true
      access_token_strategy: jwt
- ansible.builtin.command:
    argv: [docker, inspect, --format, !unsafe '{{json .Config.Labels}}']
"""
        self.assertEqual(self.lint(task), [])
        failures = self.lint(task.replace("      skip_consent: true\n", ""))
        self.assertEqual(len(failures), 1)
        self.assertIn("missing required body field(s): skip_consent", failures[0])

    def test_unknown_tags_are_still_refused(self):
        for tag in ("!unrecognized", "!!python/object:builtins.object"):
            with self.subTest(tag=tag):
                self.assertIn("YAML parse error", self.lint(f"- value: {tag} {{}}\n")[0])

    def test_literal_tag_requires_scalar(self):
        self.assertIn("YAML parse error", self.lint("- value: !unsafe [one, two]\n")[0])

    def test_default_safe_loader_is_unchanged(self):
        with self.assertRaises(yaml.constructor.ConstructorError):
            yaml.safe_load("value: !unsafe literal\n")


if __name__ == "__main__":
    unittest.main()
