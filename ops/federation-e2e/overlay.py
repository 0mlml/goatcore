#!/usr/bin/env python3
"""One sovereign's own declarations, written into that sovereign's clone.

    overlay.py <clone> <us> <them> <subnet-octet> <image>
               [--core-addr A] [--spire-listen A] [--local]

Each party has its own repository, as two organisations would. Only synthetic names
and addresses: this run reaches no goatnet.

  --core-addr     where the core router and the SPIRE server listen
                  (default 10.77.<octet>.10, the container core host)
  --spire-listen  where the outbound bundle-endpoint forward listens
                  (default 10.77.<octet>.20, the container edge host)
  --local         ansible reaches the edge and the core on this machine with a local
                  connection: the live run, where one host carries both
"""
import argparse
from pathlib import Path

import yaml

ap = argparse.ArgumentParser()
for name in ("clone", "us", "them", "octet", "image"):
    ap.add_argument(name)
ap.add_argument("--core-addr")
ap.add_argument("--spire-listen")
ap.add_argument("--local", action="store_true")
a = ap.parse_args()

clone, us, them = Path(a.clone), a.us, a.them
core_ip = a.core_addr or f"10.77.{a.octet}.10"
spire_listen = a.spire_listen or f"10.77.{a.octet}.20"
work = f"/var/lib/goat-federation/{them}"
group = f"{them}_edge"
conn = "ansible_connection=local" if a.local else "ansible_user=goat"
core_host = "127.0.0.1" if a.local else core_ip

(clone / "ops/ansible/inventory/peering-edges.ini").write_text(f"""\
[peering_edges:children]
{group}

[{group}]
{us}-edge ansible_host=127.0.0.1 {conn}

[fed_core]
{us}-core ansible_host={core_host} {conn}
""")

gv = clone / "ops/ansible/group_vars" / group
gv.mkdir(parents=True, exist_ok=True)
(gv / "deployment.yml").write_text(yaml.safe_dump({
    "goat_zenoh_edge_image": a.image,
    "goat_zenoh_edge_trust_mode": "fragment",
    "goat_zenoh_edge_partner_ca_path": f"{work}/pki/partner-ca.pem",
    "goat_zenoh_edge_our_ca_path": f"{work}/pki/our-ca.pem",
    "goat_zenoh_edge_outward_cert": f"{work}/pki/peering-outward.pem",
    "goat_zenoh_edge_outward_key": f"{work}/pki/peering-outward-key.pem",
    "goat_zenoh_edge_inward_cert": f"{work}/pki/peering-inward.pem",
    "goat_zenoh_edge_inward_key": f"{work}/pki/peering-inward-key.pem",
    "goat_zenoh_edge_core_cn": f"core-{us}",
    "goat_zenoh_edge_core_endpoints": [
        f"tls/{core_ip}:7447#root_ca_certificate_file={work}/pki/our-ca.pem"
    ],
    "goat_zenoh_edge_listen_port": 7448,
    # A refusal is silent to the sender, so the receiving edge's trace is the record.
    "goat_zenoh_edge_acl_trace": True,
    "peering_wg_enabled": True,
    "peering_wg_fips_waived_reason": "federation e2e on stock Ubuntu, DBB-WVR-001; no FIPS claim",
    "peering_wg_extra_ports": [8443],
    "peering_spire_enabled": True,
    "peering_spire_server_host": f"{us}-core",
    "peering_spire_server_addr": core_ip,
    "peering_spire_outbound_listen": spire_listen,
}, sort_keys=True))

cfg_path = clone / "ops/goatbuilder/goatbuilder.yaml"
cfg = yaml.safe_load(cfg_path.read_text())
cfg["environments"].append({
    "name": us,
    "converger": "push-ansible",
    "sites": [{"name": "home", "role": "founder", "classes": {"core": {}, "edge": {}}}],
})
cfg["peerings"] = [{"with": them, "environment": us, "site": "home", "edge_class": "edge"}]
cfg_path.write_text(yaml.safe_dump(cfg, sort_keys=False))
