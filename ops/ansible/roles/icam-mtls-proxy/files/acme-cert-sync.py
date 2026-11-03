#!/usr/bin/env python3
"""acme-cert-sync.py — give nginx the certificate Traefik already holds for a hostname.

Traefik keeps every Let's Encrypt certificate it minted in acme.json (resolver -> list of
{domain: {main, sans}, certificate, key}, both base64 PEM). The card sign-in proxy must
terminate TLS itself (the client certificate is read there), so it serves the SAME
certificate: this script picks the entry whose main or SAN names cover HOSTNAME (an exact
name first, then a one-label wildcard), writes server.crt / server.key into OUT_DIR when
they differ from what is there, and exits 3 when it changed something (0 when not, 1 on
error). With --reload it also asks the running nginx to reload, so the daily timer keeps
the proxy's certificate current across Traefik's renewals. Runs as root (acme.json is 0600).
"""
import argparse
import base64
import json
import os
import subprocess
import sys
import tempfile


def covers(name, host):
    if name == host:
        return 2
    if name.startswith("*.") and host.endswith(name[1:]) and "." not in host[: -len(name[1:])]:
        return 1
    return 0


def pick(entries, host):
    best, score = None, 0
    for e in entries:
        d = e.get("domain") or {}
        names = [d.get("main", "")] + list(d.get("sans") or [])
        s = max((covers(n, host) for n in names), default=0)
        if s > score:
            best, score = e, s
    return best


def write_if_changed(path, data, mode):
    try:
        with open(path, "rb") as f:
            if f.read() == data:
                return False
    except FileNotFoundError:
        # No prior file on the first sync; write the new content below.
        pass
    d = os.path.dirname(path)
    fd, tmp = tempfile.mkstemp(dir=d, prefix=".sync-")
    with os.fdopen(fd, "wb") as f:
        f.write(data)
    os.chmod(tmp, mode)
    os.replace(tmp, path)
    return True


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--acme", required=True, help="Traefik acme.json")
    ap.add_argument("--resolver", default="le", help="certificate resolver key inside acme.json")
    ap.add_argument("--hostname", required=True, help="the name the proxy serves")
    ap.add_argument("--out-dir", required=True, help="where server.crt and server.key go")
    ap.add_argument("--reload", metavar="COMPOSE_DIR", help="on change, `docker compose exec nginx nginx -s reload` here")
    a = ap.parse_args()

    with open(a.acme, "rb") as f:
        store = json.load(f)
    entries = (store.get(a.resolver) or {}).get("Certificates") or []
    e = pick(entries, a.hostname)
    if e is None:
        print(f"acme-cert-sync: no certificate in {a.acme} ({a.resolver}) covers {a.hostname}", file=sys.stderr)
        return 1
    crt = base64.b64decode(e["certificate"])
    key = base64.b64decode(e["key"])
    if b"BEGIN CERTIFICATE" not in crt or b"PRIVATE KEY" not in key:
        print("acme-cert-sync: acme.json entry is not PEM", file=sys.stderr)
        return 1
    os.makedirs(a.out_dir, mode=0o750, exist_ok=True)
    changed = write_if_changed(os.path.join(a.out_dir, "server.crt"), crt, 0o644)
    changed = write_if_changed(os.path.join(a.out_dir, "server.key"), key, 0o600) or changed
    main = (e.get("domain") or {}).get("main", "?")
    if not changed:
        print(f"acme-cert-sync: {a.hostname} unchanged (certificate for {main})")
        return 0
    print(f"acme-cert-sync: {a.hostname} updated from the certificate for {main}")
    if a.reload:
        subprocess.run(["docker", "compose", "exec", "-T", "nginx", "nginx", "-s", "reload"],
                       cwd=a.reload, check=False)
    return 3


if __name__ == "__main__":
    sys.exit(main())
