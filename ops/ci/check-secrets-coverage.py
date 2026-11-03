#!/usr/bin/env python3
"""check-secrets-coverage.py — every secret a deploy templates has a reference line.

For every component × goatnet in ops/pins/fleet-pins.yaml (declared, not absent),
the environment variables the component's play templates — derived exactly as
the drivers derive them (ops/scripts/deploy-secrets-needed.py), its roles' own
defaults included; a role default read as A|B|C is covered by any one — must each have a
line `NAME=op://vault/item/field` in the team's references file (goat-credentials
refs/goat.refs). A variable without one is what stopped the 2026-10-28 panoscope
candidate: the MinIO root username had a 1Password item but no reference line,
so the deployer had nothing to put in a .refs file.

usage: check-secrets-coverage.py [--repo DIR] [--refs FILE] [--json]
  --refs absent or unreadable: prints the full needed set and exits 0 with a
  WARNING (goat's CI cannot read the private repository); with --refs, exits 1
  on any missing line. goat-credentials' CI runs it with --refs against goat main.
"""
import argparse
import importlib.util
import os
import sys

try:
    import yaml
except ImportError:
    sys.exit("check-secrets-coverage: PyYAML is required")


def read_text(path):
    with open(path, encoding="utf-8") as fh:
        return fh.read()

HERE = os.path.dirname(os.path.abspath(__file__))


def load_needed_module(repo):
    p = os.path.join(repo, "ops/scripts/deploy-secrets-needed.py")
    spec = importlib.util.spec_from_file_location("deploy_secrets_needed", p)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def play_for(repo, name, comp):
    ledger = comp.get("ledger_component") or name
    cands = []
    if comp.get("deploy_playbook"):
        cands.append(os.path.join(repo, "ops/ansible", comp["deploy_playbook"]))
    cands += [os.path.join(repo, "ops/ansible", f"{ledger}-deploy.yml"), os.path.join(repo, "ops/ansible", f"{name}-deploy.yml")]
    for c in cands:
        if os.path.isfile(c):
            play = c
            for _ in range(3):
                txt = read_text(play)
                if any(l.lstrip().startswith("hosts:") for l in txt.splitlines()):
                    return play
                imp = None
                for l in txt.splitlines():
                    i = l.find("import_playbook:")
                    if i >= 0:
                        imp = l[i + len("import_playbook:"):].strip()
                        break
                if not imp:
                    return play
                play = os.path.join(os.path.dirname(play), imp)
            return play
    return None


def needed(repo):
    """{(component, goatnet): {ansible_var: ENV_NAME}} for every declared pair."""
    mod = load_needed_module(repo)
    pins = yaml.safe_load(read_text(os.path.join(repo, "ops/pins/fleet-pins.yaml")))
    out = {}
    for gname, g in (pins.get("goatnets") or {}).items():
        members = list(g.get("members") or [])
        for cname, comp in (pins.get("components") or {}).items():
            comp = comp or {}
            devs = comp.get("deviations") or {}
            if (devs.get(gname) or {}).get("absent"):
                continue
            mem = [m for m in members if not (devs.get(m) or {}).get("absent")]
            if not mem:
                continue
            play = play_for(repo, cname, comp)
            if not play:
                continue
            ledger = comp.get("ledger_component") or cname
            names = [cname] + ([ledger] if ledger != cname else [])
            # group_vars bindings and the roles' own defaults (any one name of an A|B|C suffices)
            vars_ = {var: "|".join(alts) for var, alts in mod.needed_for(repo, play, names, mem).items()}
            if vars_:
                out[(cname, gname)] = vars_
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", default=os.path.abspath(os.path.join(HERE, "..", "..")))
    ap.add_argument("--refs", default=os.environ.get("GOAT_CREDENTIALS_REFS", ""))
    ap.add_argument("--json", action="store_true")
    a = ap.parse_args()
    need = needed(a.repo)
    env_names = sorted({e for v in need.values() for e in v.values()})   # A|B|C: any one
    if a.json:
        import json
        print(json.dumps({f"{c}@{g}": v for (c, g), v in sorted(need.items())}, indent=2))
    refs = None
    if a.refs and os.path.isfile(a.refs):
        refs = set()
        for line in read_text(a.refs).splitlines(True):
            s = line.strip()
            if s and not s.startswith("#") and "=" in s:
                refs.add(s.split("=", 1)[0])
    print(f"secrets-coverage: {len(need)} component×goatnet pair(s) template {len(env_names)} distinct variable(s)")
    if refs is None:
        print("WARNING: no references file readable (--refs / GOAT_CREDENTIALS_REFS) — coverage not judged here; "
              "goat-credentials' CI judges it against goat main. Variables needed:")
        for e in env_names:
            print(f"  {e}")
        return 0
    missing = {}
    for (c, g), v in sorted(need.items()):
        for var, env in sorted(v.items()):
            if not any(e in refs for e in env.split("|")):
                missing.setdefault(env, []).append(f"{c}@{g} ({var})")
    if missing:
        print(f"FAIL: {len(missing)} variable(s) a deploy templates have NO reference line in {a.refs}:")
        for env, where in sorted(missing.items()):
            print(f"  {env}  <- {', '.join(where)}")
        print("  add `NAME=op://<vault>/<item>/<field>` for each (the 1Password item must carry the field).")
        return 1
    print(f"ok: every variable has a reference line in {os.path.basename(a.refs)} ({len(refs)} lines)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
