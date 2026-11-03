#!/usr/bin/env python3
"""Every oauth2-proxy fence's client id must be registered by something.

A fence presents OAUTH2_PROXY_CLIENT_ID to the site Hydra. When nothing in the
tree registers that client, the container, DNS, Traefik and TLS are all green
and every page load ends at Ory `invalid_client` (microscope on zagros-prod,
2026-07/08; consistency-console on zagros-dev, #2126). No health check sees it;
only loading the page does. This gate refuses the shape statically.

A fence is a role template that sets OAUTH2_PROXY_CLIENT_ID={{ <var> }}. It is
registered when a registration site names <var> (or the client id literal it
defaults to). A registration site is any role or goatnet-neutral playbook that
writes to Hydra's /admin/clients, together with that role's defaults. A
goatnet's own playbook (zagros-prod-oidc-clients.yml) does not count: it registers
the client on that goatnet only, and every other goatnet that enables the fence
is left with `invalid_client` (#2126 is exactly that). A fence whose
issuer is not the site Hydra (a public Dex broker) is listed in NOT_HYDRA with
its reason.

    python3 ops/ci/check-fence-client-registered.py           # exit 1 on a gap
    python3 ops/ci/check-fence-client-registered.py --self-test
"""
from __future__ import annotations

import re
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]

# Fences registered by one goatnet's own play because their Hydra is not the
# fence's host's. Each names that play; a goatnet that enables the fence
# elsewhere adds its registration there.
SITE_REGISTERED = {
    "consistency_aggregator_oauth2_proxy_client_id":
        "the hosted aggregator's fence runs on the cloud relay-health box, which has no Hydra; "
        "the hosting goatnet's Phase-12 play registers it (zagros-prod-oidc-clients.yml)",
    "compliance_proxy_oidc_client_id":
        "enabled on zagros-prod only (group_vars/zagros_prod_hosts/vars.yml); registered by "
        "zagros-prod-oidc-clients.yml",
}

# Fences whose client lives in an issuer other than the site Hydra.
NOT_HYDRA = {
    "consistency_aggregator_dex_client_id": "the public Dex broker's shared static client (dex-allowlist mode)",
    "relay_health_public_oidc_client_id": "the public relay-health door authenticates against the public Dex broker",
}

TEMPLATE_VAR = re.compile(r"OAUTH2_PROXY_CLIENT_ID=\{\{\s*([a-z0-9_]+)")


def fences(ansible: Path):
    """(var, template path, client id literal or '') per fence template line."""
    for tpl in sorted(ansible.glob("roles/*/templates/*.j2")):
        text = tpl.read_text(errors="replace")
        for var in TEMPLATE_VAR.findall(text):
            role = tpl.parents[1]
            literal = ""
            d = role / "defaults" / "main.yml"
            if d.is_file():
                m = re.search(rf"^{re.escape(var)}:\s*\"?([A-Za-z0-9._-]+)\"?\s*$", d.read_text(), re.M)
                literal = m.group(1) if m else ""
            yield var, tpl.relative_to(ansible.parent.parent), literal


def goatnet_prefixes(ansible: Path):
    gv = ansible / "group_vars"
    names = {p.name[: -len("_hosts")].replace("_", "-") for p in gv.glob("*_hosts")} if gv.is_dir() else set()
    return sorted(names, key=len, reverse=True)


def registration_text(ansible: Path) -> str:
    """Every role or goatnet-neutral play that writes to Hydra's /admin/clients,
    plus its role's defaults."""
    prefixes = goatnet_prefixes(ansible)
    parts = []
    for f in sorted(ansible.rglob("*.yml")):
        if "roles" not in f.relative_to(ansible).parts and any(
                f.name.startswith(p + "-") for p in prefixes):
            continue
        t = f.read_text(errors="replace")
        if "/admin/clients" not in t:
            continue
        parts.append(t)
        if "roles" in f.parts:
            role = f.parents[1]
            d = role / "defaults" / "main.yml"
            if d.is_file() and d != f:
                parts.append(d.read_text(errors="replace"))
    return "\n".join(parts)


def gaps(ansible: Path):
    reg = registration_text(ansible)
    out = []
    for var, tpl, literal in fences(ansible):
        if var in NOT_HYDRA or var in SITE_REGISTERED:
            continue
        if re.search(rf"\b{re.escape(var)}\b", reg):
            continue
        if literal and re.search(rf"[\"'\s:]{re.escape(literal)}[\"'\s]", reg):
            continue
        out.append((var, tpl, literal))
    return out


def self_test() -> int:
    with tempfile.TemporaryDirectory() as d:
        a = Path(d) / "ops" / "ansible"
        for role, var, cid in (("good", "good_oauth2_proxy_client_id", "good"),
                               ("bad", "bad_oauth2_proxy_client_id", "bad")):
            (a / "roles" / role / "templates").mkdir(parents=True)
            (a / "roles" / role / "defaults").mkdir(parents=True)
            (a / "roles" / role / "templates" / "p.env.j2").write_text(f"OAUTH2_PROXY_CLIENT_ID={{{{ {var} }}}}\n")
            (a / "roles" / role / "defaults" / "main.yml").write_text(f'{var}: "{cid}"\n')
        (a / "roles" / "good-oidc-client" / "tasks").mkdir(parents=True)
        (a / "roles" / "good-oidc-client" / "tasks" / "main.yml").write_text(
            "- uri: {url: 'http://h/admin/clients/{{ good_oauth2_proxy_client_id }}'}\n")
        (a / "group_vars" / "alpha_hosts").mkdir(parents=True)
        (a / "alpha-oidc-clients.yml").write_text(
            "- uri: {url: 'http://h/admin/clients'}  # bad_oauth2_proxy_client_id\n")
        got = [g[0] for g in gaps(a)]
    ok = got == ["bad_oauth2_proxy_client_id"]
    print(f"self-test: {'ok' if ok else 'FAIL'} (gaps={got}, want ['bad_oauth2_proxy_client_id'])")
    return 0 if ok else 1


def main() -> int:
    if "--self-test" in sys.argv:
        return self_test()
    found = gaps(ROOT / "ops" / "ansible")
    if not found:
        print("ok: every oauth2-proxy fence's Hydra client is registered by a role or playbook")
        return 0
    for var, tpl, literal in found:
        print(f"FAIL: {tpl} presents {{{{ {var} }}}}"
              f"{' (' + literal + ')' if literal else ''} to Hydra, and nothing registers it. "
              "Add a <surface>-oidc-client role (roles/microscope-oidc-client is the shape) "
              "and run it before the fence, or list the var in NOT_HYDRA with its issuer.")
    return 1


if __name__ == "__main__":
    sys.exit(main())
