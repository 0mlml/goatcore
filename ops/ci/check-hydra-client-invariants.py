#!/usr/bin/env python3
"""
Codex (ADR 1014) lint rule: every Ansible task that creates/updates a
Hydra OAuth2 client (POST /admin/clients or PUT /admin/clients/<id>)
MUST set both `skip_consent` AND `access_token_strategy` in the body.

Why this is a Codex rule (per ADR 1014 §3.4 retrofit pattern):

- Hydra's defaults (`skip_consent: false` + opaque access tokens) are
  exactly the two settings that 2026-05-07 → 2026-05-08 first-dogfood
  found load-bearing-broken for our stack:
    * F-077 — `skip_consent: false` + selfservice-ui-node = silent
      0-scope token issuance.
    * F-078 — opaque access tokens fail JWKS-only resource servers
      (netbird-mgmt + future 21D consumers).
- The fix in both cases is **per-client**, set in the Ansible body.
  Without enforcement, a future Hydra-client provisioning task (e.g.
  Block 70 tuwunel or any other 21D consumer) ships without these
  fields and silently breaks the same way.
- Phoenix-native discipline (ADR 0208) makes the rule's deployment
  free: once it lands as block-strength, every future provisioning
  task carries the discipline. No separate rollout step.

Detection: parses YAML files under `ops/ansible/roles/**/tasks/*.yml`
+ `ops/ansible/playbooks/*.yml` for `ansible.builtin.uri` tasks
whose URL matches `/admin/clients` and whose method is POST or PUT.
For each match, asserts `skip_consent` AND `access_token_strategy`
are both keys in the `body:` dict — or in the `vars:` dict that the
body references via `body: "{{ <var> }}"`. The vars-indirection path
is the canonical pattern used across grafana-oidc-client +
isitup-oidc-client + forgejo-oidc-client + minio-oidc-client +
openobserve-oidc-client roles, where the client config is hoisted
into a per-task `vars:` block for shared reuse between POST + PUT.
The lint resolves the var name back to its assignment in the same
task or the file's preceding `set_fact`/`vars:` declarations.

Out-of-scope (intentional):
- GET tasks (read-only; no body required).
- Client sub-resources below /admin/clients/<id>/ (e.g. /lifespans),
  whose bodies follow their own schema, not the client's.
- DELETE tasks (no client config to assert on).
- Tasks that target Hydra admin paths other than /admin/clients
  (e.g. /admin/oauth2/auth/requests/consent/accept — operator-fired
  flow, not client provisioning).

Exit code 0 on all-pass, 1 on any-fail.

CI: runs in `.github/workflows/ansible-lint.yml` (or equivalent) on
PRs touching `ops/ansible/**`.
"""

import argparse
import re
import sys
from pathlib import Path
from typing import Any, Iterable

import yaml

REQUIRED_FIELDS = ("skip_consent", "access_token_strategy")
HYDRA_ADMIN_CLIENTS_PATH = "/admin/clients"

# `body: "{{ some_var }}"` Jinja2 reference. Captures the var name.
_VAR_REF_RE = re.compile(r"^\s*\{\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*\}\}\s*$")


class AnsibleLintLoader(yaml.SafeLoader):
    """Read Ansible literal strings without enabling arbitrary YAML tags."""


AnsibleLintLoader.add_constructor("!unsafe", AnsibleLintLoader.construct_scalar)


def is_hydra_client_provisioning_task(task: dict) -> bool:
    """Detect ansible.builtin.uri tasks targeting Hydra's /admin/clients."""
    uri_block = task.get("ansible.builtin.uri") or task.get("uri")
    if not isinstance(uri_block, dict):
        return False
    method = (uri_block.get("method") or "GET").upper()
    if method not in ("POST", "PUT"):
        return False
    url = str(uri_block.get("url") or "")
    if HYDRA_ADMIN_CLIENTS_PATH not in url:
        return False
    # Only the client resource itself carries a client body:
    # /admin/clients (POST) and /admin/clients/<id> (PUT). A further path
    # segment names a sub-resource with its own body schema, e.g.
    # PUT /admin/clients/<id>/lifespans takes only the *_lifespan fields,
    # so skip_consent / access_token_strategy do not belong there.
    rest = url.split(HYDRA_ADMIN_CLIENTS_PATH, 1)[1].split("?", 1)[0]
    segments = [seg for seg in rest.split("/") if seg]
    if len(segments) > 1:
        return False
    return True


def _resolve_var_keys(var_name: str, task: dict, file_vars: dict) -> set[str]:
    """Resolve a Jinja2 var reference back to the dict it names.

    Looks in the task's own `vars:` first (closest scope), then file-level
    `vars:` / `set_fact:` declarations collected by collect_file_vars().
    """
    task_vars = task.get("vars") or {}
    candidate = task_vars.get(var_name)
    if isinstance(candidate, dict):
        return set(candidate.keys())
    candidate = file_vars.get(var_name)
    if isinstance(candidate, dict):
        return set(candidate.keys())
    return set()


def task_body_keys(task: dict, file_vars: dict) -> set[str]:
    uri_block = task.get("ansible.builtin.uri") or task.get("uri") or {}
    body = uri_block.get("body")
    if isinstance(body, dict):
        return set(body.keys())
    if isinstance(body, str):
        # Common Ansible pattern: `body: "{{ client_config }}"` where the
        # actual fields live in a per-task `vars:` block (shared between
        # the POST-when-missing + PUT-on-drift tasks). Resolve the var
        # reference; emptied set means "we couldn't resolve" → upstream
        # treats this as missing, same as a literal empty body.
        match = _VAR_REF_RE.match(body)
        if match:
            return _resolve_var_keys(match.group(1), task, file_vars)
    return set()


def collect_file_vars(loaded: Any) -> dict:
    """Walk the file's task list and collect every set_fact / vars
    assignment into a flat name → value map.

    Last-write-wins per the Ansible execution order — Codex doesn't try
    to model conditional branches; this is a structural lint, not a
    semantic interpreter. For the OIDC-client roles' actual pattern
    (one `set_fact` building `<role>_client_config` near the top of
    tasks/main.yml + that fact consumed by POST + PUT below), this
    captures the var contents accurately.
    """
    file_vars: dict[str, Any] = {}
    if loaded is None:
        return file_vars

    def visit(items: Any) -> None:
        if not isinstance(items, list):
            return
        for item in items:
            if not isinstance(item, dict):
                continue
            if "hosts" in item:
                visit(item.get("tasks") or [])
                visit(item.get("handlers") or [])
                continue
            # Top-level vars: block (rare in tasks/main.yml but supported).
            block_vars = item.get("vars")
            if isinstance(block_vars, dict):
                for k, v in block_vars.items():
                    if isinstance(v, dict):
                        file_vars[k] = v
            # ansible.builtin.set_fact (or set_fact) — capture every dict-
            # valued fact for later var-resolution.
            fact_block = item.get("ansible.builtin.set_fact") or item.get("set_fact")
            if isinstance(fact_block, dict):
                for k, v in fact_block.items():
                    if isinstance(v, dict):
                        file_vars[k] = v

    visit(loaded)
    return file_vars


def iter_tasks(loaded: Any) -> Iterable[tuple[str, dict]]:
    """Yield (task-name, task-dict) pairs from a parsed YAML file.

    Handles both top-level task lists (most role tasks/main.yml) and
    playbook structure (list of plays each with a `tasks:` block).
    """
    if loaded is None:
        return
    if isinstance(loaded, list):
        # Could be a flat task list (role tasks/main.yml), or a list of
        # plays. Distinguish by the presence of `hosts:` (play) vs not
        # (task list).
        for item in loaded:
            if not isinstance(item, dict):
                continue
            if "hosts" in item:
                for sub in (item.get("tasks") or []):
                    if isinstance(sub, dict):
                        yield (sub.get("name") or "<unnamed>", sub)
                for sub in (item.get("handlers") or []):
                    if isinstance(sub, dict):
                        yield (sub.get("name") or "<unnamed>", sub)
            else:
                yield (item.get("name") or "<unnamed>", item)


def lint_file(path: Path) -> list[str]:
    """Return list of failure messages for the file (empty if clean)."""
    try:
        loaded = yaml.load(path.read_text(), Loader=AnsibleLintLoader)
    except yaml.YAMLError as exc:
        return [f"{path}: YAML parse error: {exc}"]

    file_vars = collect_file_vars(loaded)
    failures: list[str] = []
    for name, task in iter_tasks(loaded):
        if not is_hydra_client_provisioning_task(task):
            continue
        body_keys = task_body_keys(task, file_vars)
        missing = [f for f in REQUIRED_FIELDS if f not in body_keys]
        if missing:
            failures.append(
                f"{path}: task {name!r} provisions a Hydra OAuth2 client "
                f"but is missing required body field(s): {', '.join(missing)}. "
                f"See ADR 1014 §3.4 + F-077/F-078."
            )
    return failures


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(
        description="Codex rule: Hydra OAuth2 client provisioning must set skip_consent + access_token_strategy."
    )
    parser.add_argument(
        "paths",
        nargs="*",
        default=["ops/ansible"],
        help="Files or directories to scan (default: ops/ansible).",
    )
    args = parser.parse_args(argv)

    yaml_files: list[Path] = []
    for arg in args.paths:
        p = Path(arg)
        if p.is_file():
            yaml_files.append(p)
        elif p.is_dir():
            for sub in p.rglob("*.yml"):
                yaml_files.append(sub)
            for sub in p.rglob("*.yaml"):
                yaml_files.append(sub)

    failures: list[str] = []
    scanned = 0
    for f in yaml_files:
        scanned += 1
        failures.extend(lint_file(f))

    print(f"Scanned {scanned} YAML files under {args.paths}")
    if failures:
        print(f"FAIL: {len(failures)} Hydra-client invariant violation(s):")
        for msg in failures:
            print(f"  - {msg}")
        return 1
    print("PASS: every Hydra OAuth2 client provisioning task has skip_consent + access_token_strategy.")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
