#!/usr/bin/env python3
"""Explicit root/privileged-SSH reinstatement; no console database credential."""
import argparse
import json
from pathlib import Path
import re
import subprocess
import uuid


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--identity", required=True)
    parser.add_argument("--actor", required=True)
    parser.add_argument("--reason", required=True)
    parser.add_argument("--config", default="/etc/goat/reinstatement.json")
    args = parser.parse_args()
    identity = str(uuid.UUID(args.identity))
    for value, limit in ((args.actor, 256), (args.reason, 512)):
        if not value.strip() or len(value.encode()) > limit or "\n" in value or "\r" in value:
            parser.error("actor and reason must be bounded, nonempty single lines")
    config = json.loads(Path(args.config).read_text())
    container = config["postgres_container"]
    if not isinstance(container, str) or not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]*", container):
        parser.error("invalid configured PostgreSQL container")
    command = ["docker", "exec", "-i", "-u", "postgres", container, "psql", "-X",
               "-U", "postgres", "-d", "kratos", "-v", "ON_ERROR_STOP=1", "-tAXq",
               "--set=identity=" + identity, "--set=actor=" + args.actor, "--set=reason=" + args.reason]
    result = subprocess.run(command, input="""
BEGIN;
SET LOCAL lock_timeout = '5s';
SELECT json_build_object('id', :'identity', 'actor', :'actor', 'reinstated',
  goat_reinstate_identity(:'identity'::uuid, :'actor' || ' reason=' || :'reason'));
COMMIT;
""", text=True, capture_output=True, timeout=30)
    if result.returncode:
        raise SystemExit("reinstatement refused; inspect the privileged database audit/log")
    receipt = json.loads(result.stdout.strip())
    if receipt != {"id": identity, "actor": args.actor, "reinstated": True}:
        raise SystemExit("reinstatement receipt mismatch; activation must not proceed")
    print(json.dumps(receipt))


if __name__ == "__main__":
    main()
