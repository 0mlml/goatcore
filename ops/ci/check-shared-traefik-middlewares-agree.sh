#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# check-shared-traefik-middlewares-agree.sh — assert that the middlewares
# defined in BOTH Traefik config paths carry identical definitions.
#
# WHY THIS EXISTS: two independent Traefik config paths exist and neither can
# reach the other (docs/design/surface-overload-protection.md §4):
#
#   ACME overlay  ops/ansible/templates/traefik-acme/mgmt-dynamic.yml.j2
#                 -> goatnets with traefik_services empty (the traefik role
#                    is skipped)
#   traefik role  ops/ansible/roles/traefik/templates/dynamic.yml.j2
#                 -> sites that populate traefik_services
#
# So a middleware that must apply everywhere has to be written out twice. That
# duplication is deliberate. What was NOT in place was anything checking the
# two copies still say the same thing — §4 asserted "what keeps them honest"
# and nothing did. The repo's own verdict on that arrangement, from
# check-stream-exclusion-matches-relabel.sh: "a comment saying 'keep these in
# sync' is not a mechanism."
#
# Drift here is silent and asymmetric: one goatnet gets a different rate
# ceiling, or compresses a content type another goatnet streams uncompressed,
# and nothing reports it because each file is independently valid.
#
# WHAT IT CHECKS: for every middleware name defined in both files, the parsed
# definitions must be equal. Middlewares defined in only one file are ignored
# — each path legitimately carries routing-specific middlewares the other has
# no use for (mesh-only-allowlist, the dashboard root rewrites).
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

ACME="ops/ansible/templates/traefik-acme/mgmt-dynamic.yml.j2"
ROLE="ops/ansible/roles/traefik/templates/dynamic.yml.j2"

for f in "$ACME" "$ROLE"; do
  [ -f "$f" ] || { echo "check-shared-traefik-middlewares: FAIL — $f missing"; exit 1; }
done

python3 - "$ACME" "$ROLE" <<'PY'
import re, sys, yaml

def middlewares(path):
    """Parse the http.middlewares block out of a Jinja-templated Traefik file.

    Rendering properly would need the full Ansible var set, which CI does not
    have. Instead: strip Jinja comments and tags, replace {{ expr }} with a
    scalar token that carries the expression's `default(...)` if it has one,
    and parse what is left. That is enough because the shared middleware
    definitions are plain unconditional YAML — the shape this guard compares.

    A `{% if %}/{% else %}` whose branches both define the same key would
    collapse to last-wins here. That is acceptable: the middlewares this guard
    requires are unconditional in both files, and anything malformed enough to
    matter fails the parse loudly rather than passing quietly.
    """
    text = open(path).read()
    text = re.sub(r'\{#.*?#\}', '', text, flags=re.S)   # Jinja comments
    text = re.sub(r'\{%.*?%\}', '', text, flags=re.S)   # Jinja control tags

    def sub(m):
        d = re.search(r'default\(([^)]*)\)', m.group(1))
        tok = d.group(1).strip().strip('\'"') if d else 'expr'
        # Bare alphanumeric token: the {{ }} is often already inside quotes
        # in the template, so adding our own would nest them.
        return 'JINJA_' + re.sub(r'[^A-Za-z0-9]', '_', tok)
    text = re.sub(r'\{\{(.*?)\}\}', sub, text)

    # A whole-block interpolation (`{{ traefik_middlewares | to_nice_yaml }}`)
    # sits alone on its line and collapses to a bare token at column 0, which
    # is not a valid mapping entry. Those lines carry site-declared extras,
    # never a shared definition, so dropping them is safe.
    text = '\n'.join(l for l in text.splitlines()
                     if not re.fullmatch(r'\s*JINJA_\w+\s*', l))

    doc = yaml.safe_load(text) or {}
    return (doc.get('http') or {}).get('middlewares') or {}

acme_p, role_p = sys.argv[1], sys.argv[2]
acme, role = middlewares(acme_p), middlewares(role_p)

shared = sorted(set(acme) & set(role))
if not shared:
    print("check-shared-traefik-middlewares: FAIL — no middleware is defined in "
          "both files. Expected at least goat-entrypoint-ceiling, goat-ratelimit "
          "and goat-compress; "
          "one path has probably lost its copy.")
    sys.exit(1)

fail = False
for name in shared:
    if acme[name] != role[name]:
        fail = True
        print(f"check-shared-traefik-middlewares: FAIL — `{name}` differs between the two config paths")
        print(f"    {acme_p}:")
        for l in yaml.safe_dump(acme[name], sort_keys=True).splitlines(): print(f"      {l}")
        print(f"    {role_p}:")
        for l in yaml.safe_dump(role[name], sort_keys=True).splitlines(): print(f"      {l}")

# These must exist in both. goat-entrypoint-ceiling and goat-compress are
# attached at the entrypoint and cover every router on :443; goat-ratelimit is
# referenced per router from role route files, and Traefik fails a router
# whose middleware is undefined. A path missing any of them breaks silently.
for required in ("goat-entrypoint-ceiling", "goat-ratelimit", "goat-compress"):
    if required not in shared:
        fail = True
        where = acme_p if required in role else role_p
        print(f"check-shared-traefik-middlewares: FAIL — `{required}` must be defined "
              f"in BOTH config paths; missing from {where}")

if fail:
    sys.exit(1)

print(f"check-shared-traefik-middlewares: OK — {len(shared)} shared middleware "
      f"definition(s) agree ({', '.join(shared)})")
PY
