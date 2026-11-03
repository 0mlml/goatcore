#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""check-netbird-url-container-pin.py — a container handed a NetBird
management URL must pin that URL's hostname with extra_hosts.

THE FAILURE THIS EXISTS FOR. On a mgmt host the `mesh-coredns` role pins the
enrollment apex (netbird.<zone>) to 127.0.0.1 in /etc/hosts so the host's own
NetBird client reaches local traefik. Docker's embedded DNS forwards to the
host resolver, so every container on that host resolves the same name through
it, and gets `CNAME localhost.` back, which glibc discards. Every NetBird read
from a container then fails at once with `error sending request`:

  * onboard-runner: every native mint fails at GET /api/groups.
  * netvis: /healthz 503 and a red surface canary. Any other container
    carrying the same unpinned URL fails the same way.

The fix each time is the same: `extra_hosts: <hostname>:host-gateway`. This
guard makes it structural instead of per-role memory.

WHAT IT CHECKS. For every role under ops/ansible/roles/ whose templates render
an env line `<NAME>=` with NETBIRD or MGMT and URL in the name from a single
variable (`FOO_NETBIRD_URL={{ foo_netbird_url }}`), and which ships a compose
template: some compose template in that role must pin
`{{ <that variable> | urlsplit('hostname') }}:`. A role with no compose
template reads the URL from a host process (peerview's systemd poller), where
/etc/hosts is the intended answer, and is skipped.

Usage:
  ops/ci/check-netbird-url-container-pin.py [repo-root]
  ops/ci/check-netbird-url-container-pin.py --self-test

Exit: 0 clean · 1 a container role lacks the pin · 2 self-test failure
"""
import pathlib
import re
import sys
import tempfile

ENV_LINE = re.compile(
    r"^\s*([A-Z0-9_]*(?:NETBIRD|MGMT)[A-Z0-9_]*URL)=\{\{\s*([a-z0-9_]+)\s*\}\}\s*$",
    re.M,
)


def check(root: pathlib.Path):
    problems = []
    for role in sorted((root / "ops/ansible/roles").iterdir()):
        tdir = role / "templates"
        if not tdir.is_dir():
            continue
        urls = {}
        for t in sorted(tdir.rglob("*.j2")):
            for name, var in ENV_LINE.findall(t.read_text(errors="replace")):
                urls.setdefault(var, (name, t))
        if not urls:
            continue
        composes = sorted(p for p in tdir.glob("*compose*.j2") if p.is_file())
        if not composes:
            continue
        text = "\n".join(c.read_text(errors="replace") for c in composes)
        for var, (name, t) in sorted(urls.items()):
            pin = re.compile(
                r"\{\{\s*" + re.escape(var)
                + r"\s*\|\s*urlsplit\(\s*['\"]hostname['\"]\s*\)\s*\}\}:"
            )
            if not pin.search(text):
                problems.append(
                    f"{t.relative_to(root)}: {name} is rendered from {var} into a "
                    f"container, but no compose template in {role.relative_to(root)} "
                    f"pins \"{{{{ {var} | urlsplit('hostname') }}}}:<addr>\" under extra_hosts"
                )
    return problems


def self_test():
    with tempfile.TemporaryDirectory() as d:
        root = pathlib.Path(d)

        def role(name, env, compose=None):
            t = root / "ops/ansible/roles" / name / "templates"
            t.mkdir(parents=True)
            (t / "env.j2").write_text(env)
            if compose is not None:
                (t / "docker-compose.yml.j2").write_text(compose)

        role("unpinned", "X_NETBIRD_URL={{ x_netbird_url }}\n",
             "services:\n  x:\n    image: x\n")
        role("pinned", "Y_MGMT_URL={{ y_mgmt_url }}\n",
             "services:\n  y:\n    extra_hosts:\n"
             "      - \"{{ y_mgmt_url | urlsplit('hostname') }}:{{ y_addr }}\"\n")
        role("hostproc", "Z_NETBIRD_URL={{ z_netbird_url }}\n")
        got = check(root)
        if len(got) != 1 or "roles/unpinned" not in got[0]:
            print("self-test FAILED: expected exactly the unpinned role refused, got:")
            for g in got:
                print("  " + g)
            return 2
    print("self-test ok: refused the unpinned container role, passed the pinned "
          "role and the host process")
    return 0


def main(argv):
    if argv[1:2] == ["--self-test"]:
        return self_test()
    root = pathlib.Path(argv[1] if len(argv) > 1 else ".").resolve()
    problems = check(root)
    if problems:
        print("NetBird management URL reaches a container with no hostname pin:")
        for p in problems:
            print("  " + p)
        print("\nFix: extra_hosts: - \"{{ <var> | urlsplit('hostname') }}:host-gateway\" "
              "(see netvis_netbird_host_addr in ops/ansible/roles/netvis/defaults/main.yml).")
        return 1
    print("ok: every container handed a NetBird management URL pins its hostname")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
