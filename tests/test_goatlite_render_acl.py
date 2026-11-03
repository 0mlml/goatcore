#!/usr/bin/env python3
"""A goatlite render declares the goatnet's ACL, because ADR 1185 refuses a
goatnet without one.

Nothing wrote it, so phase 1's gate failed with "'<name>' has no entry in
ops/ansible/netbird_acl_matrix.yml", and phase 12's group bootstrap would have
failed on the taxonomy the same way. Both machines running the tutorial hit
this independently (G-18).
"""
import importlib.util
from pathlib import Path
import sys
import unittest

import yaml

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("goatlite_render", ROOT / "ops/scripts/goatlite-render.py")
render = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = render
spec.loader.exec_module(render)

FACTS = {"name": "fixture", "name_us": "fixture", "upper": "FIXTURE", "zone": "fixture.lan"}


class Acl(unittest.TestCase):
    def test_the_intents_block_satisfies_the_adr_1185_gate(self):
        entry = yaml.safe_load(render.acl_intents_block(FACTS))["fixture"]
        self.assertEqual(entry["cutover"]["default_policy_action"], "delete")
        self.assertTrue(entry["rules"], "an empty rule list means no reach at all after cutover")
        self.assertEqual(entry["site_prefix"], "FIXTURE")

    def test_every_rule_names_only_groups_the_taxonomy_declares(self):
        tax = yaml.safe_load(render.acl_taxonomy_block(FACTS))["fixture"]
        declared = {g["suffix"] for g in tax["groups"]}
        entry = yaml.safe_load(render.acl_intents_block(FACTS))["fixture"]
        for rule in entry["rules"]:
            for group in rule["sources"] + rule["destinations"]:
                self.assertIn(group, declared, f"rule {rule['name']!r} names {group}, "
                              "which the bootstrap would never create")

    def test_the_administrator_keeps_any_port_reach_to_the_box(self):
        # The cutover deletes the Default policy; a rule set that locks the
        # only administrator out of the box is the known mistake the intents
        # file warns about.
        entry = yaml.safe_load(render.acl_intents_block(FACTS))["fixture"]
        self.assertTrue(any(r["protocol"] == "all" and "PLATFORM-ADMIN" in r["sources"]
                            and "INFRA" in r["destinations"] for r in entry["rules"]))


if __name__ == "__main__":
    unittest.main()
