#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
#
# served-payloads.py — what a serving surface should serve, and whether it does.
#
# goat-get and goat-docs serve release assets they download at deploy time (the
# goat CLI, goatbuilder, the SDK, the books). The release is chosen by a role
# var, not by the container image, so a ledger row naming the right nginx digest
# says nothing about which release is on the page: a site can serve an old goat
# CLI while main declares a newer one, and every check reads it as converged.
#
# The role declares its payloads once, in its meta stanza for rollback-ledger
# (rollback_served: name, tag_var, optional enabled_var and tag_role, file). At the end of a
# play the ledger row records `served`, read back from each file on the host.
# This module resolves the same declaration from the repository for one member
# group and judges a row against it. Both avenues call it, so the words and the
# verdict are one (ADR 1201): deploy-component.sh --verify-only, and goatbuilder
# deploy / verify / diff.
#
#   served-payloads.py declared --repo R --ledger L --group G   -> JSON {payload: tag|"absent"}
#   served-payloads.py check    --repo R --ledger L --group G   (the row JSON on stdin)
#       exit 0  every payload as declared (prints one line), or nothing declared (prints nothing)
#       exit 3  a payload differs, or the row does not record what it serves (prints one line)
#       exit 2  the declaration cannot be resolved from the repository (prints why)
#
# Resolution: member group_vars, then group_vars/all, then the role's defaults —
# the order Ansible applies for these plays. A value that is itself a template
# resolves only when it is a bare {{ var }}; anything else is reported, never guessed.

import argparse
import glob
import json
import os
import re
import sys

import yaml

BARE = re.compile(r"^\s*\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}\s*$")


class _AnsibleLoader(yaml.SafeLoader):
    """SafeLoader that reads Ansible's own tags (!unsafe, !vault, …) as plain values,
    so scanning every role's tasks never stops on one file that uses them."""


_AnsibleLoader.add_multi_constructor("!", lambda loader, suffix, node: (
    loader.construct_scalar(node) if isinstance(node, yaml.ScalarNode)
    else loader.construct_sequence(node) if isinstance(node, yaml.SequenceNode)
    else loader.construct_mapping(node)))


def load(path):
    try:
        with open(path) as fh:
            return yaml.load(fh, Loader=_AnsibleLoader) or {}
    except FileNotFoundError:
        return {}


def var_files(gv, name):
    out = []
    if os.path.isfile(os.path.join(gv, name + ".yml")):
        out.append(os.path.join(gv, name + ".yml"))
    if os.path.isfile(os.path.join(gv, name + ".yaml")):
        out.append(os.path.join(gv, name + ".yaml"))
    d = os.path.join(gv, name)
    if os.path.isdir(d):
        out += sorted(glob.glob(os.path.join(d, "*.yml")) + glob.glob(os.path.join(d, "*.yaml")))
    return out


def find_role(repo, ledger):
    """The role whose rollback-ledger stanza names this ledger and declares payloads:
    a meta dependency, or an include_role in its tasks (snitch-bundle includes the
    ledger on Linux peers only)."""
    for meta in sorted(glob.glob(os.path.join(repo, "ops/ansible/roles/*/meta/main.yml"))):
        doc = load(meta)
        for dep in doc.get("dependencies") or []:
            if not isinstance(dep, dict) or dep.get("role") != "rollback-ledger":
                continue
            v = dep.get("vars") or {}
            if v.get("rollback_component") == ledger and v.get("rollback_served"):
                return os.path.dirname(os.path.dirname(meta)), v["rollback_served"]
    for tf in sorted(glob.glob(os.path.join(repo, "ops/ansible/roles/*/tasks/*.yml"))):
        # Text first: parsing every task file of every role costs a second per call,
        # and both avenues call this once per member group.
        try:
            with open(tf) as fh:
                text = fh.read()
        except OSError:
            continue
        if "rollback_served" not in text or ledger not in text:
            continue
        doc = load(tf)
        for task in doc if isinstance(doc, list) else []:
            if not isinstance(task, dict):
                continue
            inc = task.get("ansible.builtin.include_role") or task.get("include_role") or {}
            if not isinstance(inc, dict) or inc.get("name") != "rollback-ledger" or inc.get("tasks_from"):
                continue
            v = task.get("vars") or {}
            if v.get("rollback_component") == ledger and v.get("rollback_served"):
                return os.path.dirname(os.path.dirname(tf)), v["rollback_served"]
    return None, []


def resolver(repo, group, role_dir):
    gv = os.path.join(repo, "ops/ansible/group_vars")
    layers = [load(f) for f in var_files(gv, group)]
    layers += [load(f) for f in var_files(gv, "all")]
    layers.append(load(os.path.join(role_dir, "defaults/main.yml")))

    def get(name, depth=0):
        if depth > 8:
            raise ValueError("%s: template nests too deep" % name)
        for layer in layers:
            if isinstance(layer, dict) and name in layer:
                v = layer[name]
                if isinstance(v, str) and "{{" in v:
                    m = BARE.match(v)
                    if not m:
                        raise ValueError("%s is a template (%s); cannot resolve it outside Ansible" % (name, v.strip()))
                    return get(m.group(1), depth + 1)
                return v
        raise ValueError("%s is not set in group_vars/%s, group_vars/all or %s/defaults" % (name, group, os.path.basename(role_dir)))

    return get


def truthy(v):
    if isinstance(v, bool):
        return v
    return str(v).strip().lower() in ("1", "true", "yes", "on")


def declared(repo, ledger, group):
    role_dir, spec = find_role(repo, ledger)
    if not spec:
        return {}
    get = resolver(repo, group, role_dir)
    out = {}
    for p in spec:
        on = truthy(get(p["enabled_var"])) if p.get("enabled_var") else True
        if not on:
            out[p["name"]] = "absent"
            continue
        # tag_role: the release is declared by another role (onboard-runner installs
        # the goat-cli goat-get declares); that role's defaults are the last layer.
        tget = resolver(repo, group, os.path.join(repo, "ops/ansible/roles", p["tag_role"])) if p.get("tag_role") else get
        out[p["name"]] = str(tget(p["tag_var"]))
    return out


def verdict(want, row):
    """(exit, line) for a row against the declared payloads."""
    if not want:
        return 0, ""
    got = row.get("served")
    if not isinstance(got, dict):
        return 3, ("served payloads unknown — the newest ledger row does not record what the host serves "
                   "(written before payload tracking, or the play did not finish); main declares %s; "
                   "deploy to record it" % ", ".join("%s %s" % kv for kv in sorted(want.items())))
    diffs = []
    for name, tag in sorted(want.items()):
        have = got.get(name, "unrecorded")
        if have != tag:
            diffs.append("%s: serves %s, main declares %s" % (name, have, tag))
    if diffs:
        return 3, "served payload DRIFT — " + "; ".join(diffs)
    return 0, "serves as declared: " + ", ".join("%s %s" % kv for kv in sorted(want.items()))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("cmd", choices=["declared", "check"])
    ap.add_argument("--repo", required=True)
    ap.add_argument("--ledger", required=True)
    ap.add_argument("--group", required=True)
    a = ap.parse_args()
    try:
        want = declared(a.repo, a.ledger, a.group)
    except ValueError as e:
        print("served payloads: cannot resolve the declaration — %s" % e)
        return 2
    if a.cmd == "declared":
        print(json.dumps(want, sort_keys=True))
        return 0
    text = sys.stdin.read()
    try:
        row = json.loads(text[text.index("{"):text.rindex("}") + 1])
    except ValueError:
        row = {}
    rc, line = verdict(want, row)
    if line:
        print(line)
    return rc


if __name__ == "__main__":
    raise SystemExit(main())
