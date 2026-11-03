#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# check-compose-memory-limits.sh — every container this repo declares carries a
# memory ceiling.
#
# WHY: a container with no memory limit can consume the whole host, and limits
# that exist on only some services are per-service guards, not a plan. An
# unbounded leak in any one service consumes all physical RAM before a cgroup
# intervenes, and takes the identity plane with it. This guard is so that the
# first failure is bounded.
#
# WHAT: for every `container_name:` in a compose file or template under ops/,
# the same service block must declare `deploy.resources.limits.memory` (or the
# older `mem_limit:`). Templates are Jinja, not YAML, so this is a text walk:
# a service block runs from a key at indent N to the next key at indent N.
#
# ALLOWLIST: files that predate the rule and have not been sized yet are listed
# in ops/ci/compose-memory-limits.allow, one path per line, each with a date.
# The list may only SHRINK. Add a service to it and the diff says so out loud.
#
# Run: ops/ci/check-compose-memory-limits.sh
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

ALLOW=ops/ci/compose-memory-limits.allow
python3 - "$ALLOW" <<'PY'
import re, sys, glob, os
allow_path = sys.argv[1]
allow = set()
if os.path.exists(allow_path):
    for line in open(allow_path):
        line = line.split('#', 1)[0].strip()
        if line: allow.add(line)

files = sorted(set(glob.glob('ops/compose/**/docker-compose*.yml', recursive=True)
                 + glob.glob('ops/ansible/roles/**/templates/docker-compose*.j2', recursive=True)
                 + glob.glob('ops/ansible/roles/**/templates/docker-compose*.yml', recursive=True)))
KEY = re.compile(r'^(\s+)([A-Za-z0-9_{}.\- ]+):\s*$')
missing = []   # (file, service)
for f in files:
    lines = open(f, encoding='utf-8', errors='replace').read().split('\n')
    # locate service blocks: a key line whose block contains container_name
    i = 0
    while i < len(lines):
        m = KEY.match(lines[i])
        if not m: i += 1; continue
        ind, key = len(m.group(1)), m.group(2).strip()
        j = i + 1; body = []
        while j < len(lines):
            mk = KEY.match(lines[j])
            if mk and len(mk.group(1)) <= ind: break
            body.append(lines[j]); j += 1
        text = '\n'.join(body)
        if re.search(r'^\s+container_name:', text, re.M):
            has = re.search(r'^\s+memory:\s', text, re.M) or re.search(r'^\s+mem_limit:', text, re.M)
            if not has: missing.append((f, key))
        i = j if j > i else i + 1

unallowed = [(f, k) for f, k in missing if f not in allow]
stale = sorted(a for a in allow if a not in {f for f, _ in missing})
for f, k in unallowed:
    print(f"FAIL: {f} service '{k}' declares a container and no memory ceiling.")
if stale:
    print("NOTE: these allowlist entries now carry limits and can be removed:")
    for a in stale: print(f"      {a}")
print(f"{len(files)} compose files, {len(missing)} services without a ceiling, {len(unallowed)} not allowlisted.")
if unallowed:
    print("Add deploy.resources.limits.memory (4x the measured peak, floor 512M), or")
    print(f"add the FILE to {allow_path} with a date and a reason. The list only shrinks.")
    raise SystemExit(1)
print("compose memory-ceiling check OK")
PY
