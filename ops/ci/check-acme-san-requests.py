#!/usr/bin/env python3
"""check-acme-san-requests.py: no certificate request names a host twice or
names a host that a wildcard in the same request already covers (#2149).

Let's Encrypt refuses such an order outright ("Domain name X is redundant with
a wildcard domain in the same request"), and Traefik sends the same request at
renewal, so a redundant SAN list does not fail at deploy: it fails ~30 days
before the certificate expires, and every name on that certificate goes dark
on expiry. A wildcard *.A covers exactly one label: x.A, not y.x.A.

The check renders templates/traefik-acme/mgmt-dynamic.yml.j2 for every host in
ops/ansible/inventory/mgmt-hosts.ini that declares public_mgmt_fqdn (ops/ansible/render-mgmt-dynamic.yml, local,
no host contact) and reads every router's tls.domains.

  check-acme-san-requests.py              render every goatnet and judge it
  check-acme-san-requests.py FILE...      judge already-rendered files
  check-acme-san-requests.py --self-test  prove RED and GREEN on fixtures
"""
import os
import subprocess
import sys
import tempfile

import yaml

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
ANSIBLE = os.path.join(ROOT, "ops", "ansible")


def problems(doc):
    out = []
    routers = ((doc or {}).get("http") or {}).get("routers") or {}
    for name, router in routers.items():
        for dom in ((router or {}).get("tls") or {}).get("domains") or []:
            names = [dom["main"]] + list(dom.get("sans") or [])
            wild = {n[2:] for n in names if n.startswith("*.")}
            seen = set()
            for n in names:
                if n in seen:
                    out.append(f"router {name}: {n} is listed twice")
                seen.add(n)
                if not n.startswith("*.") and "." in n and n.split(".", 1)[1] in wild:
                    out.append(f"router {name}: {n} is redundant with *.{n.split('.', 1)[1]} in the same request")
    return out


def judge(files):
    bad = 0
    for f in files:
        with open(f) as fh:
            for p in problems(yaml.safe_load(fh)):
                print(f"FAIL {os.path.basename(f)}: {p}")
                bad += 1
    if bad:
        print(f"{bad} redundant or repeated name(s). Let's Encrypt refuses the whole order; "
              "drop the names the wildcard covers (templates/traefik-acme/mgmt-dynamic.yml.j2).")
        return 1
    print(f"ok: {len(files)} rendered config(s), no certificate request names a host its wildcard covers")
    return 0


def render(out):
    cmd = ["ansible-playbook", "-i", "inventory/mgmt-hosts.ini", "render-mgmt-dynamic.yml",
           "-e", "target=all", "-e", f"out={out}"]
    r = subprocess.run(cmd, cwd=ANSIBLE, capture_output=True, text=True)
    if r.returncode != 0:
        sys.stdout.write(r.stdout[-4000:])
        sys.stderr.write(r.stderr[-4000:])
        print("FAIL could not render mgmt-dynamic.yml.j2 for every mgmt host")
        sys.exit(1)
    return sorted(os.path.join(out, f) for f in os.listdir(out) if f.endswith(".yml"))


RED = """
http:
  routers:
    dashboard:
      tls:
        domains:
          - main: dashboard.s.z
            sans: ["*.s.z", "auth.s.z", "admin.auth.s.z"]
"""
GREEN = """
http:
  routers:
    dashboard:
      tls:
        domains:
          - main: "*.s.z"
            sans: ["admin.auth.s.z", "*.wbo.s.z"]
    public:
      tls:
        domains:
          - main: netbird.z
"""


def self_test():
    red = problems(yaml.safe_load(RED))
    green = problems(yaml.safe_load(GREEN))
    assert len(red) == 2, red  # dashboard.s.z and auth.s.z; admin.auth.s.z is two labels deep
    assert green == [], green
    print("self-test ok: RED fixture fails twice, GREEN fixture passes")
    return 0


if __name__ == "__main__":
    args = sys.argv[1:]
    if args == ["--self-test"]:
        sys.exit(self_test())
    if args:
        sys.exit(judge(args))
    with tempfile.TemporaryDirectory() as d:
        sys.exit(judge(render(d)))
