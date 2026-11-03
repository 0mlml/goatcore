#!/usr/bin/env python3
"""A goatlite's render writes the LAN DNS zone that step 10 deploys.

Nothing wrote it, so step 10 failed with "Could not find or access
ops/dns/<name>/<zone>.hosts".
"""
import importlib.util
from pathlib import Path
import sys
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("goatlite_render", ROOT / "ops/scripts/goatlite-render.py")
render = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = render
spec.loader.exec_module(render)

FACTS = {"name": "fixture", "zone": "fixture.lan", "host": "192.0.2.50", "host_is_ip": True}


class Zone(unittest.TestCase):
    def names(self):
        out = {}
        for line in render.zone_hosts(FACTS).splitlines():
            if line and not line.startswith("#"):
                ip, *names = line.split()
                out.update({name: ip for name in names})
        return out

    def test_names_the_plays_depend_on_resolve_to_the_host(self):
        names = self.names()
        for name in ("mgmt-fixture-01.fixture.lan", "netbird.fixture.lan",
                     "dashboard.fixture.fixture.lan", "oauth2.fixture.fixture.lan",
                     "portal.fixture.fixture.lan", "zenoh.fixture.fixture.lan"):
            self.assertEqual(names.get(name), "192.0.2.50", name)

    def test_first_line_marks_the_file_as_generated(self):
        self.assertTrue(render.zone_hosts(FACTS).startswith(render.HEADER + "\n"))

    def test_a_host_that_does_not_resolve_is_refused(self):
        with self.assertRaises(render.Refuse):
            render.zone_address({"host": "no-such-host.invalid", "host_is_ip": False})


if __name__ == "__main__":
    unittest.main()
