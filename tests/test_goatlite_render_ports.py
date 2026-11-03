#!/usr/bin/env python3
"""A goatlite puts every service on one host, so no two may claim one host port.

management-a and spire-server both defaulted to 127.0.0.1:8081, and step 9 failed
after about 14 minutes with "Bind for 127.0.0.1:8081 failed: port is already
allocated".
"""
from pathlib import Path
import re
import unittest

RENDERER = Path(__file__).resolve().parents[1] / "ops/scripts/goatlite-render.py"


class HostPorts(unittest.TestCase):
    def rendered_value(self, name):
        text = RENDERER.read_text(encoding="utf-8")
        match = re.search(rf'^{name}: "(\d+)"$', text, re.M)
        self.assertIsNotNone(match, f"goatlite-render.py does not set {name}")
        return match.group(1)

    def test_spire_and_management_use_different_host_ports(self):
        self.assertNotEqual(self.rendered_value("mgmt_container_host_port"),
                            self.rendered_value("spire_server_host_port"))


if __name__ == "__main__":
    unittest.main()
