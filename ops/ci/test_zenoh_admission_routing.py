"""Rendered routing must preserve short admission failures and peer fan-out."""
from pathlib import Path
import re
import unittest

import yaml
from jinja2 import Environment, FileSystemLoader, select_autoescape

ROOT = Path(__file__).resolve().parents[2]


def matches(route, labels):
    for matcher in route.get("matchers", []):
        key, value = matcher.split("=", 1)
        if labels.get(key.strip()) != value.strip().strip('"'):
            return False
    return True


class AdmissionRoutingTests(unittest.TestCase):
    def test_both_roles_and_all_receiver_configurations(self):
        for role in ["obs-host", "observability"]:
            for pd, ntfy in [(False, False), (True, False), (False, True), (True, True)]:
                with self.subTest(role=role, pagerduty=pd, ntfy=ntfy):
                    env = Environment(loader=FileSystemLoader(str(
                        ROOT / "ops/ansible/roles" / role / "templates")),
                        autoescape=select_autoescape(),
                        trim_blocks=True, lstrip_blocks=False)
                    env.filters["bool"] = lambda v: str(v).lower() in ("true", "yes", "1", "on")
                    env.filters["regex_escape"] = re.escape
                    doc = yaml.safe_load(env.get_template("alertmanager.yml.j2").render(
                        pagerduty_integration_key="fixture" if pd else "",
                        ntfy_alertmanager_webhook_url="https://example.invalid/notify" if ntfy else "",
                        deadman_webhook_url="https://example.invalid/heartbeat",
                        deadman_webhook_token="fixture", site_id="fixture", obs_site="fixture"))
                    root = doc["route"]
                    labels = {"alertname": "ZenohRouterAdmissionSeizure", "severity": "critical"}
                    # Alertmanager stops at the first matching sibling unless continue=true.
                    route = next(r for r in root["routes"] if matches(r, labels))
                    self.assertEqual(route.get("group_wait", root["group_wait"]), "0s")
                    self.assertEqual(route.get("group_interval", root["group_interval"]), "30s")
                    self.assertEqual(route.get("group_by", root["group_by"]),
                                     ["alertname", "site", "component", "router"])
                    self.assertFalse(route.get("continue", False))
                    expected = "critical-fanout" if pd or ntfy else "webhook-noop"
                    self.assertEqual(route["receiver"], expected)
                    receivers = {r["name"]: r for r in doc["receivers"]}
                    receiver = receivers[expected]
                    self.assertEqual(len(receiver.get("pagerduty_configs", [])), int(pd))
                    if pd or ntfy:
                        self.assertEqual(len(receiver.get("webhook_configs", [])), int(ntfy))
                    # Other critical incidents retain normal batching and the same receiver.
                    generic = next(r for r in root["routes"] if matches(
                        r, {"alertname": "ZenohRouterMemoryCritical", "severity": "critical"}))
                    self.assertEqual(generic.get("group_wait", root["group_wait"]), "30s")
                    self.assertEqual(generic.get("group_interval", root["group_interval"]), "5m")
                    self.assertEqual(generic["receiver"], expected)
                    if role == "obs-host":
                        watchdog = next(r for r in root["routes"] if matches(
                            r, {"alertname": "Watchdog", "severity": "none"}))
                        self.assertEqual(watchdog["receiver"], "deadman")


if __name__ == "__main__":
    unittest.main()
