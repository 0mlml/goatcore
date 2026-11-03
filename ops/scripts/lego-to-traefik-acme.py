#!/usr/bin/env python3
"""Convert a lego state directory into a Traefik-seedable acme.json.

WHY. Traefik mints its own Let's Encrypt certs at container start, which is
correct steady-state behaviour and wrong for a standup hour: the first mint is
DNS-01, and DNS-01 propagation is the one wait in a bringup that is not yours
to schedule (the standup runbook calls it "the single most
schedule-unpredictable phase"). Nothing about it has to happen in the hour --
the zone is live well before power-on. So issue the certs from the operator
laptop days earlier with lego, convert them here, and stage the result as
/opt/mgmt-host/letsencrypt/acme.json. Traefik finds a store that already
covers every router it is about to serve, mints nothing, and Phase 9 collapses
to a file copy.

Traefik keeps renewal ownership. It re-orders ~30 days before expiry using the
ACCOUNT recorded in this file, so the account key and its registration URI have
to travel with the certs -- a store carrying certs but a mismatched account
renews into an error at day 60, in production, silently. That is why this
script refuses to emit a store it cannot assemble both halves of.

FORMAT. Traefik's store is {"<resolver>": {"Account": {...},
"Certificates": [...]}}. Account.PrivateKey is base64 of the DER account key,
which is exactly the body of its PEM. Each certificate entry is
{"domain": {"main": ..., "sans": [...]}, "certificate": b64(fullchain PEM),
"key": b64(private key PEM), "Store": "default"} -- lowercase keys on the
Certificate fields, capitalised on Store, because that is what Traefik's
struct tags produce. The resolver name must match the Traefik command flag
(--certificatesresolvers.<name>.acme.*); for this fleet that is `le`, set in
templates/traefik-acme/docker-compose.acme.yml.j2.

Traefik tolerates a staged cert carrying SANs beyond what its routers ask for,
but a cert MISSING a name any router asks for triggers a fresh order for that
name. Run ops/ansible/mgmt-cert-sans-preview.yml first: it prints the exact
anchor set the config will demand, which is the list to hand lego.

Usage:
    lego-to-traefik-acme.py --lego-path ~/.goat/state/zagros-prod-acme/prod \\
                            --resolver le \\
                            --out ~/.goat/state/site-a-acme/acme.json

Verify what you built before staging it:
    lego-to-traefik-acme.py ... --print-summary
"""

from __future__ import annotations

import argparse
import base64
import json
import os
import re
import subprocess
import sys
import tempfile
from pathlib import Path

# Traefik's Account.KeyType drives the key type of RENEWED certificates (it is
# handed to lego as config.Certificate.KeyType). Keep it equal to the key type
# the pre-issued certs were minted with, or the first renewal silently changes
# the fleet's key shape.
KEY_TYPE_BY_BITS = {2048: "2048", 4096: "4096", 3072: "3072"}


def die(msg: str) -> "NoReturn":  # type: ignore[valid-type]
    print(f"ERROR: {msg}", file=sys.stderr)
    sys.exit(1)


def pem_body_b64(pem_text: str) -> str:
    """Return base64(DER) for a single-object PEM.

    A PEM body IS base64 of the DER, so this is a re-wrap rather than a
    re-encode -- but it has to be a SINGLE object. An account key file with a
    stray second block would produce a store Traefik parses and then fails to
    sign with.
    """
    blocks = re.findall(
        r"-----BEGIN [^-]+-----(.*?)-----END [^-]+-----", pem_text, re.DOTALL
    )
    if len(blocks) != 1:
        die(f"expected exactly one PEM block in the account key, found {len(blocks)}")
    return "".join(blocks[0].split())


def load_account(lego_path: Path, ca_host: str) -> dict:
    acct_dir = lego_path / "accounts" / ca_host
    if not acct_dir.is_dir():
        available = [p.name for p in (lego_path / "accounts").glob("*")] or ["<none>"]
        die(
            f"no lego account for CA host {ca_host!r} under {acct_dir}. "
            f"Accounts present: {', '.join(available)}. "
            "Pass --ca-host to pick one (staging and production are separate accounts)."
        )

    emails = [p for p in acct_dir.iterdir() if p.is_dir()]
    if len(emails) != 1:
        die(f"expected exactly one account email dir under {acct_dir}, found {len(emails)}")
    email_dir = emails[0]

    meta = json.loads((email_dir / "account.json").read_text())
    key_files = list((email_dir / "keys").glob("*.key"))
    if len(key_files) != 1:
        die(f"expected exactly one account key under {email_dir / 'keys'}")

    return {
        "Email": meta["email"],
        "Registration": meta["registration"],
        "PrivateKey": pem_body_b64(key_files[0].read_text()),
    }


def cert_san_dns_names(crt: Path) -> list[str]:
    """DNS names in a leaf's subjectAltName, in certificate order.

    Shells out to openssl rather than taking a `cryptography` dependency --
    this script runs on an operator laptop mid-standup, where "pip install"
    is not a step anyone wants to discover they need.
    """
    proc = subprocess.run(
        ["openssl", "x509", "-in", str(crt), "-noout", "-ext", "subjectAltName"],
        capture_output=True,
        text=True,
    )
    if proc.returncode != 0:
        die(f"openssl could not read SANs from {crt}: {proc.stderr.strip()}")
    return re.findall(r"DNS:([^,\s]+)", proc.stdout)


def load_certificates(lego_path: Path) -> list[dict]:
    cert_dir = lego_path / "certificates"
    if not cert_dir.is_dir():
        die(f"no certificates directory at {cert_dir}")

    entries = []
    for crt in sorted(cert_dir.glob("*.crt")):
        # lego writes "<name>.issuer.crt" alongside "<name>.crt". The former is
        # the intermediate on its own and is NOT a leaf -- staging it as one
        # gives Traefik a store whose "certificate" serves no hostname.
        if crt.name.endswith(".issuer.crt"):
            continue
        stem = crt.name[: -len(".crt")]
        key = cert_dir / f"{stem}.key"
        if not key.exists():
            die(f"certificate {crt.name} has no matching {key.name}")

        # SANs come from the ISSUED CERTIFICATE, not from lego's sidecar
        # "<name>.json" -- that file records only `domain` (the main name),
        # `certUrl` and `certStableUrl`. Reading the sidecar instead silently
        # produces a single-name entry for a multi-name cert, and Traefik then
        # believes the store does not cover the other five hostnames and fires
        # an ACME order for each of them at start. The cert's own SAN extension
        # is the authoritative list of what it actually covers.
        sans = [n for n in cert_san_dns_names(crt) if n != stem]

        chain = crt.read_text()
        if "BEGIN CERTIFICATE" not in chain:
            die(f"{crt} does not look like a PEM certificate")

        domain: dict = {"main": stem}
        if sans:
            domain["sans"] = sans

        entries.append(
            {
                "domain": domain,
                "certificate": base64.b64encode(chain.encode()).decode(),
                "key": base64.b64encode(key.read_text().encode()).decode(),
                "Store": "default",
            }
        )

    if not entries:
        die(f"no leaf certificates found under {cert_dir}")
    return entries


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--lego-path", required=True, type=Path, help="lego --path directory")
    ap.add_argument("--out", required=True, type=Path, help="acme.json to write (mode 0600)")
    ap.add_argument(
        "--resolver",
        default="le",
        help="Traefik certificatesresolvers name; MUST match the Traefik flag (fleet default: le)",
    )
    ap.add_argument(
        "--ca-host",
        default="acme-v02.api.letsencrypt.org",
        help="which lego account to embed; the staging host is acme-staging-v02.api.letsencrypt.org",
    )
    ap.add_argument("--key-type", default="4096", help="Traefik Account.KeyType (renewal key shape)")
    ap.add_argument("--print-summary", action="store_true", help="print what was assembled")
    args = ap.parse_args()

    account = load_account(args.lego_path, args.ca_host)
    account["KeyType"] = args.key_type
    certificates = load_certificates(args.lego_path)

    store = {args.resolver: {"Account": account, "Certificates": certificates}}

    args.out.parent.mkdir(parents=True, exist_ok=True)
    # Write-then-rename at 0600. Traefik REFUSES to start on an acme.json that
    # is group- or world-readable, so a plain open() with the ambient umask is
    # a boot failure waiting for the standup hour.
    fd, tmp = tempfile.mkstemp(dir=str(args.out.parent))
    with os.fdopen(fd, "w") as fh:
        json.dump(store, fh)
    os.chmod(tmp, 0o600)
    os.replace(tmp, args.out)

    print(f"wrote {args.out} (mode 0600), resolver {args.resolver!r}")
    if args.print_summary:
        print(f"  account: {account['Email']} @ {account['Registration']['uri']}")
        for c in certificates:
            names = [c["domain"]["main"], *c["domain"].get("sans", [])]
            print(f"  cert: {c['domain']['main']} ({len(names)} name(s))")
            for n in names:
                print(f"    - {n}")


if __name__ == "__main__":
    main()
