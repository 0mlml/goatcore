#!/usr/bin/env python3
"""Check declared relay listeners and every advertised enrollment pool address."""

import argparse
import ipaddress
import json
import math
import os
from pathlib import Path
import re
import socket
import sys

PORT = 33073
FLEETS = {
    "zagros": "relays.conf",
    "zagros-dev": "relays-zagros-dev.conf",
    "zagros-prod": "relays-zagros-prod.conf",
}
INVENTORY = Path(__file__).resolve().parents[1] / "relay" / "health"
HOSTNAME = re.compile(r"(?=.{1,253}$)[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?\Z")


def load_fleet(environment, directory=INVENTORY):
    """Read the dashboard inventory, rejecting ambiguous or empty fleets."""
    relays = []
    names = set()
    for number, line in enumerate(
        (directory / FLEETS[environment]).read_text().splitlines(), 1
    ):
        fields = line.split("#", 1)[0].split()
        if not fields:
            continue
        location = f"{FLEETS[environment]}:{number}"
        if len(fields) < 5:
            raise ValueError(
                f"{location}: expected five columns plus optional key=value tokens"
            )
        name, address, region, instance, goatnet, *tokens = fields
        if (
            not re.fullmatch(r"[a-z0-9-]+", name)
            or name in names
            or goatnet != environment
        ):
            raise ValueError(
                f"{location}: invalid/duplicate relay name or mismatched environment"
            )
        ipaddress.IPv4Address(address)
        options = {}
        for token in tokens:
            key, sep, value = token.partition("=")
            if not sep or not key or not value or key in options:
                raise ValueError(f"{location}: invalid/duplicate inventory token")
            options[key] = value
        host = options.get("enroll_host")
        if host is None and environment == "zagros":
            host = "netbird.zagros.example"
        if (
            not host
            or not HOSTNAME.fullmatch(host)
            or any(
                not label
                or len(label) > 63
                or label.startswith("-")
                or label.endswith("-")
                for label in host.split(".")
            )
        ):
            raise ValueError(f"{location}: missing or invalid enroll_host")
        names.add(name)
        relays.append({"name": name, "ip": address, "host": host})
    if not relays:
        raise ValueError(f"{environment}: empty relay inventory")
    return relays


def resolve_all(host):
    try:
        return sorted(
            {
                row[4][0]
                for row in socket.getaddrinfo(
                    host, PORT, socket.AF_INET, socket.SOCK_STREAM
                )
            }
        )
    except socket.gaierror:
        return []


def probe_tcp(address, timeout):
    try:
        with socket.create_connection((address, PORT), timeout=timeout):
            return "OPEN"
    except ConnectionRefusedError:
        return "REFUSED"
    except OSError:
        return "FILTERED"


def check_fleets(fleets, timeout, regions=None):
    checks = []
    for environment, relays in fleets.items():
        for relay in relays:
            if regions is not None and relay["name"] not in regions:
                continue
            checks.append(
                {
                    "tier": "relay",
                    "environment": environment,
                    "relay": relay["name"],
                    "target": relay["ip"],
                    "result": probe_tcp(relay["ip"], timeout),
                }
            )
        for host in sorted({relay["host"] for relay in relays}):
            addresses = resolve_all(host)
            if not addresses:
                checks.append(
                    {
                        "tier": "pool",
                        "environment": environment,
                        "target": host,
                        "result": "NODNS",
                    }
                )
            for address in addresses:
                checks.append(
                    {
                        "tier": "pool",
                        "environment": environment,
                        "target": f"{host}->{address}",
                        "result": probe_tcp(address, timeout),
                    }
                )
    return {
        "port": PORT,
        "healthy": bool(checks) and all(row["result"] == "OPEN" for row in checks),
        "checks": checks,
    }


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--environment", choices=["all", *FLEETS], default="all")
    parser.add_argument(
        "--regions",
        default=os.environ.get("REGIONS"),
        help="comma or space separated relay short names; pools are always checked in full",
    )
    parser.add_argument("--json", action="store_true")
    args = parser.parse_args(argv)
    try:
        timeout = float(os.environ.get("RELAY_HC_TIMEOUT", "6"))
        if not math.isfinite(timeout) or timeout <= 0:
            raise ValueError("RELAY_HC_TIMEOUT must be a positive finite number")
        environments = FLEETS if args.environment == "all" else [args.environment]
        fleets = {env: load_fleet(env) for env in environments}
        requested = None
        if args.regions is not None:
            requested = set(args.regions.replace(",", " ").split())
            known = {relay["name"] for relays in fleets.values() for relay in relays}
            if not requested or requested - known:
                raise ValueError(
                    "--regions must select known relay short names in the selected environment(s)"
                )
            if any(
                not any(relay["name"] in requested for relay in relays)
                for relays in fleets.values()
            ):
                raise ValueError(
                    "--regions leaves an environment empty; select --environment explicitly"
                )
        result = check_fleets(fleets, timeout, requested)
    except (OSError, ValueError, OverflowError) as exc:
        print(f"relay-fleet-healthcheck: {exc}", file=sys.stderr)
        return 2
    if args.json:
        print(json.dumps(result))
    else:
        print(f"{'ENVIRONMENT':<12} {'RELAY':<8} {'TIER':<6} {'TARGET':<60} :{PORT}")
        for row in result["checks"]:
            print(
                f"{row['environment']:<12} {row.get('relay', '-'):<8} {row['tier']:<6} {row['target']:<60} {row['result']}"
            )
        print(
            "RESULT: all relays + pool members serve :33073 — healthy."
            if result["healthy"]
            else "RESULT: FAIL — one or more relay listeners or DNS pools are unavailable."
        )
    return 0 if result["healthy"] else 1


if __name__ == "__main__":
    sys.exit(main())
