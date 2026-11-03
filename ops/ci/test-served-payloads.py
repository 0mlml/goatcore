#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
#
# test-served-payloads.py — what a serving surface should serve, and the verdict
# on a ledger row (ops/scripts/served-payloads.py, called by both avenues).
#
# THE FAILURE THIS EXISTS FOR. goat-get serves the goat CLI release a role var
# names, not its nginx image; hosts can serve old releases while main declares a
# newer one, and every ledger check reads the hosts as converged because the image
# is right.
#
# Checks, on a scratch repository: the declaration resolves member group_vars
# over group_vars/all over the role's defaults; a disabled payload is `absent`;
# a template the module cannot evaluate is reported, never guessed; and the
# verdict calls a matching row as declared, a differing one DRIFT, and a row
# without `served` unknown. Then the real tree: a real goat-get scope resolves.
#
# Exit 0 clean, 1 on a failure.

import json
import os
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.dirname(os.path.dirname(HERE))
SCRIPT = os.path.join(REPO, "ops", "scripts", "served-payloads.py")
failures = []


def check(name, cond, detail=""):
    if cond:
        print("  ok    %s" % name)
    else:
        print("  FAIL  %s %s" % (name, detail))
        failures.append(name)


def run(repo, cmd, group, stdin=""):
    r = subprocess.run([sys.executable, SCRIPT, cmd, "--repo", repo, "--ledger", "svc", "--group", group],
                       input=stdin, capture_output=True, text=True, timeout=30)
    return r.returncode, (r.stdout + r.stderr).strip()


def write(path, text):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as fh:
        fh.write(text)


def main():
    with tempfile.TemporaryDirectory() as d:
        a = os.path.join(d, "ops", "ansible")
        write(os.path.join(a, "roles/svc/meta/main.yml"), """dependencies:
  - role: rollback-ledger
    vars:
      rollback_component: svc
      rollback_served_root: "{{ svc_www }}"
      rollback_served:
        - {name: cli, tag_var: svc_release_tag, file: version.json}
        - {name: extra, tag_var: svc_extra_tag, enabled_var: svc_extra_enabled, file: "{{ sub }}/version.json"}
        - {name: alias, tag_var: svc_alias_tag, file: alias.json}
""")
        write(os.path.join(a, "roles/svc/defaults/main.yml"), """svc_release_tag: cli-v1
svc_extra_tag: extra-v1
svc_extra_enabled: false
svc_alias_tag: "{{ svc_release_tag }}"
""")
        write(os.path.join(a, "group_vars/all.yml"), "svc_release_tag: cli-v2\n")
        write(os.path.join(a, "group_vars/on_hosts/vars.yml"), "svc_release_tag: cli-v3\nsvc_extra_enabled: true\n")
        write(os.path.join(a, "group_vars/bad_hosts/vars.yml"), "svc_release_tag: \"{{ x | upper }}\"\n")

        rc, out = run(d, "declared", "off_hosts")
        got = json.loads(out) if rc == 0 else {}
        check("group_vars/all wins over the role default", got.get("cli") == "cli-v2", out)
        check("a disabled payload is absent", got.get("extra") == "absent", out)
        check("a bare {{ var }} template resolves through the layers", got.get("alias") == "cli-v2", out)

        rc, out = run(d, "declared", "on_hosts")
        got = json.loads(out) if rc == 0 else {}
        check("member group_vars win over group_vars/all", got.get("cli") == "cli-v3", out)
        check("an enabled payload carries its tag", got.get("extra") == "extra-v1", out)

        rc, out = run(d, "declared", "bad_hosts")
        check("a template it cannot evaluate is refused, exit 2", rc == 2 and "cannot resolve" in out, (rc, out))

        want = {"cli": "cli-v3", "extra": "extra-v1", "alias": "cli-v3"}
        rc, out = run(d, "check", "on_hosts", json.dumps({"ref": "x", "served": want}))
        check("a row serving what main declares is as declared, exit 0", rc == 0 and out.startswith("serves as declared"), (rc, out))
        stale = dict(want, cli="cli-v1")
        rc, out = run(d, "check", "on_hosts", "host | CHANGED | rc=0 | (stdout) " + json.dumps({"ref": "x", "served": stale}))
        check("a differing payload is DRIFT naming both tags, exit 3",
              rc == 3 and "DRIFT" in out and "cli: serves cli-v1, main declares cli-v3" in out, (rc, out))
        rc, out = run(d, "check", "on_hosts", json.dumps({"ref": "x"}))
        check("a row that records no served is unknown, exit 3", rc == 3 and "unknown" in out, (rc, out))
        # tag_role: a role that installs a release another role declares (onboard-runner's
        # host goat is goat-get's goat-cli) resolves it from that role's defaults.
        write(os.path.join(a, "roles/runner/meta/main.yml"), """dependencies:
  - role: rollback-ledger
    vars:
      rollback_component: runner
      rollback_served:
        - {name: cli, tag_var: svc_release_tag, tag_role: svc, file: cli.version.json}
""")
        write(os.path.join(a, "roles/runner/defaults/main.yml"), "runner_x: 1\n")
        r = subprocess.run([sys.executable, SCRIPT, "declared", "--repo", d, "--ledger", "runner", "--group", "off_hosts"],
                           capture_output=True, text=True, timeout=30)
        got = json.loads(r.stdout) if r.returncode == 0 else {}
        check("tag_role: the tag resolves through the declaring role's defaults", got.get("cli") == "cli-v2", r.stdout + r.stderr)
        write(os.path.join(a, "group_vars/all.yml"), "other_var: 1\n")
        r = subprocess.run([sys.executable, SCRIPT, "declared", "--repo", d, "--ledger", "runner", "--group", "off_hosts"],
                           capture_output=True, text=True, timeout=30)
        got = json.loads(r.stdout) if r.returncode == 0 else {}
        check("tag_role: with no group setting it, the declaring role's default is the tag", got.get("cli") == "cli-v1", r.stdout + r.stderr)
        write(os.path.join(a, "group_vars/all.yml"), "svc_release_tag: cli-v2\n")
        # An include_role stanza in a role's tasks declares payloads too (snitch-bundle, Linux only).
        write(os.path.join(a, "roles/probe/tasks/Linux.yml"), """- name: ledger
  ansible.builtin.include_role:
    name: rollback-ledger
  vars:
    rollback_component: probe
    rollback_served:
      - {name: probe, tag_var: probe_release_tag, file: probe.version.json}
""")
        write(os.path.join(a, "roles/probe/defaults/main.yml"), "probe_release_tag: probe-v4\n")
        r = subprocess.run([sys.executable, SCRIPT, "declared", "--repo", d, "--ledger", "probe", "--group", "off_hosts"],
                           capture_output=True, text=True, timeout=30)
        got = json.loads(r.stdout) if r.returncode == 0 else {}
        check("an include_role ledger stanza in tasks declares its payloads", got.get("probe") == "probe-v4", r.stdout + r.stderr)

        r = subprocess.run([sys.executable, SCRIPT, "check", "--repo", d, "--ledger", "other", "--group", "on_hosts"],
                           input="{}", capture_output=True, text=True, timeout=30)
        check("a component that serves nothing prints nothing, exit 0", r.returncode == 0 and not r.stdout.strip(), (r.returncode, r.stdout))

    r = subprocess.run([sys.executable, SCRIPT, "declared", "--repo", REPO, "--ledger", "goat-get", "--group", "zagros_prod_hosts"],
                       capture_output=True, text=True, timeout=30)
    got = json.loads(r.stdout) if r.returncode == 0 else {}
    check("the tree: goat-get on zagros_prod_hosts resolves the goat CLI tag", str(got.get("goat-cli", "")).startswith("goat-cli-v"), r.stdout + r.stderr)
    gg = got.get("goat-cli")
    for ledger, name, want in (("onboard-runner", "goat-cli", gg), ("snitch-bundle", "snitch", None)):
        r = subprocess.run([sys.executable, SCRIPT, "declared", "--repo", REPO, "--ledger", ledger, "--group", "zagros_prod_hosts"],
                           capture_output=True, text=True, timeout=30)
        got2 = json.loads(r.stdout) if r.returncode == 0 else {}
        ok = (got2.get(name) == want) if want else str(got2.get(name, "")).startswith("snitch-v")
        check("the tree: %s on zagros_prod_hosts declares %s %s" % (ledger, name, want or "snitch-v*"), ok, r.stdout + r.stderr)

    if failures:
        print("\nFAIL: %d check(s) failed" % len(failures))
        return 1
    print("\nok: served payloads resolve and are judged the same way for both avenues")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
