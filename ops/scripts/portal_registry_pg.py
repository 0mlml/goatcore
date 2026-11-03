"""Read the portal registry on Postgres (ADR 1165 step A).

A goatnet whose portal runs on Postgres (`portal_store: postgres`) has no
registry.db for host-side scripts to open. They read the same tables through
the Postgres container instead:

    docker exec -i postgres-a psql -U portal -d portal_registry

Inside the container pg_hba trusts the local socket, so this needs no password:
root on the host reads the registry exactly as it read registry.db before. The
session is read-only (default_transaction_read_only), so a script can never
write a table through it.

Each query is wrapped in json_agg so values come back typed (NULL stays None,
integers stay integers) and parameters are bound as psql variables
(`:'name'`), which psql quotes as literals; nothing is formatted into the SQL.

Used by reap-orphans.py, sync-slot-anchors.py and
ops/goat-id-console/scripts/slot-identity-backfill.py. budget-notify.py carries
its own copy because its role deploys it as a single file.
"""
from __future__ import annotations

import json
import shlex
import subprocess

DEFAULT_CONTAINER = "postgres-a"
DEFAULT_DB = "portal_registry"
DEFAULT_USER = "portal"


class RegistryUnreadable(RuntimeError):
    """The registry could not be read at all (container down, wrong database)."""


def query(sql: str, params: dict[str, str] | None = None, *,
          container: str = DEFAULT_CONTAINER, db: str = DEFAULT_DB,
          user: str = DEFAULT_USER, ssh: str = "", timeout: int = 60) -> list[dict]:
    """Rows of `sql` as dicts. `sql` is one SELECT with no trailing semicolon;
    reference a parameter as :'name' and pass it in `params`.

    `ssh` runs the same command on that host (`ssh <host> sudo -n docker exec
    ...`) for a script run from an operator checkout, the Postgres-era
    equivalent of copying registry.db off the host."""
    argv = ["docker", "exec", "-i",
            "-e", "PGOPTIONS=-c default_transaction_read_only=on",
            container, "psql", "-X", "-q", "-A", "-t",
            "-v", "ON_ERROR_STOP=1", "-U", user, "-d", db]
    for k, v in (params or {}).items():
        argv += ["-v", f"{k}={v}"]
    if ssh:
        argv = ["ssh", ssh, "sudo -n " + shlex.join(argv)]
    stmt = f"SELECT coalesce(json_agg(t), '[]'::json) FROM ({sql}) t;\n"
    try:
        out = subprocess.run(argv, input=stmt, capture_output=True, text=True,
                             timeout=timeout, check=False)
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise RegistryUnreadable(f"{container}/{db}: {exc}") from exc
    if out.returncode != 0:
        raise RegistryUnreadable(f"{container}/{db}: psql exit {out.returncode}: {out.stderr.strip()}")
    try:
        rows = json.loads(out.stdout.strip() or "[]")
    except json.JSONDecodeError as exc:
        raise RegistryUnreadable(f"{container}/{db}: unparseable psql output: {exc}") from exc
    if not isinstance(rows, list):
        raise RegistryUnreadable(f"{container}/{db}: expected a JSON array")
    return rows


def add_arguments(ap, path_flag: str) -> None:
    """The flags every reader takes to choose the store. `path_flag` is the
    script's existing SQLite path flag, which the postgres store ignores."""
    ap.add_argument("--registry-store", choices=("sqlite", "postgres"), default="sqlite",
                    help=f"where the portal registry lives: sqlite ({path_flag}) or "
                         "postgres (read through `docker exec <container> psql`; the "
                         "goatnet's portal_store group_var)")
    ap.add_argument("--pg-container", default=DEFAULT_CONTAINER,
                    help="Postgres container (portal_db_container)")
    ap.add_argument("--pg-db", default=DEFAULT_DB, help="registry database (portal_db_name)")
    ap.add_argument("--pg-user", default=DEFAULT_USER, help="login role (portal_db_user)")
    ap.add_argument("--pg-ssh", default="",
                    help="run the read on this management host over ssh (needs passwordless "
                         "sudo for docker there); omit when running on the host itself")


def kwargs(a) -> dict[str, str]:
    """The connection keywords for query() from parsed arguments."""
    return {"container": a.pg_container, "db": a.pg_db, "user": a.pg_user, "ssh": a.pg_ssh}
