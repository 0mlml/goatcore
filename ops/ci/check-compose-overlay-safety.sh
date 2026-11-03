#!/usr/bin/env bash
# F-312 — an explicit `-f` set silently drops the ACME overlay, and traefik
# comes back without its certificate resolver.
#
# THE TRAP, in the words of the fix that half-closed it. traefik-acme.yml
# pins COMPOSE_FILE in /opt/mgmt-host/.env so a BARE `docker compose` keeps the
# overlay, and its own comment says:
#
#     "Explicit -f flags still override it, so every scoped call in the roles
#      (`-f docker-compose.yml -f <overlay> up -d <service>`) behaves exactly
#      as before."
#
# That rests on an assumption nothing else enforces: that a scoped call names
# its service. A call that passes an explicit -f set — which overrides the pin —
# and then runs a BARE `up -d` rebuilds the whole stack including traefik,
# without the resolver.
#
# So the pin is not a general safety net: it protects the bare command and is
# defeated by the very calls the comment assumed were safe.
#
# THE PRECISE RULE. A compose call is dangerous only if it can RECREATE TRAEFIK:
#
#   it is an `up`, AND it passes an explicit -f set including docker-compose.yml
#   (which overrides the pin), AND it either names no service at all or names
#   traefik itself.
#
# A call that names its own services — `up -d spire-server` — cannot touch
# traefik and needs no overlay. A `pull` recreates nothing. Neither is flagged:
# a check that fires on things that are not bugs trains people to skim it, and
# then the real one goes past unread.
#
# Usage:  ops/ci/check-compose-overlay-safety.sh [repo-root]
# Rollback: rm this file + its workflow step.
set -euo pipefail

ROOT="${1:-$(cd "$(dirname "$0")/../.." && pwd)}"

python3 - "$ROOT" <<'PY'
import re, sys, pathlib

root = pathlib.Path(sys.argv[1])
ans = root / "ops" / "ansible"
if not ans.is_dir():
    print(f"FAIL: {ans} not found", file=sys.stderr)
    sys.exit(2)

# Calls that legitimately manage the overlay itself, or that run somewhere other
# than the mgmt-host compose dir. Each needs a reason, not just a path.
ALLOW = {
    # renders and applies the overlay; naming it is the whole point
    "ops/ansible/traefik-acme.yml": "owns the ACME overlay",
    # LAN-only TLS path with its own overlay, documented in-file
    "ops/ansible/lan-traefik-routes.yml": "lan-tls overlay, LAN-only vhosts",
}

CMD = re.compile(
    r"cmd:\s*>-?\s*\n((?:[ \t]+\S.*\n|[ \t]*\n)+?)(?=[ \t]*(?:chdir|register|changed_when|when|args|environment|creates|removes)\s*:|[ \t]*-\s|\Z)"
)

problems = []
for p in sorted(ans.rglob("*.yml")):
    rel = str(p.relative_to(root))
    if rel in ALLOW:
        continue
    try:
        text = p.read_text()
    except Exception:
        continue
    for m in CMD.finditer(text):
        block = m.group(1)
        if "docker compose" not in block:
            continue
        flat = " ".join(x.strip() for x in block.splitlines() if x.strip())
        if "-f docker-compose.yml" not in flat:
            continue  # bare call — the COMPOSE_FILE pin protects it
        up = re.search(r"\bup\b((?:\s+-{1,2}[\w-]+)*)(.*)$", flat)
        if not up:
            continue  # pull / config / logs — recreates nothing
        named = [w for w in up.group(2).split() if w and not w.startswith("-")
                 and not w.startswith("{%") and not w.startswith("{{")]
        touches_traefik = (not named) or ("traefik" in named)
        if not touches_traefik:
            continue  # scoped to services that are not traefik
        if "docker-compose.acme.yml" in flat:
            continue  # carries the overlay, so traefik keeps its resolver
        line = text[: m.start()].count("\n") + 1
        why = ("`up -d` names no service, so it rebuilds traefik too, and the -f set "
               "drops the ACME overlay") if not named else \
              "recreates traefik with an -f set that drops the ACME overlay"
        problems.append((rel, line, why, flat))

if problems:
    print("compose overlay safety: FAIL\n", file=sys.stderr)
    for rel, line, why, flat in problems:
        print(f"  {rel}:{line}", file=sys.stderr)
        print(f"      {why}", file=sys.stderr)
        print(f"      {flat[:150]}", file=sys.stderr)
        print(file=sys.stderr)
    print(
        "  An explicit -f set OVERRIDES the COMPOSE_FILE pin in .env. If the call does not\n"
        "  name docker-compose.acme.yml, traefik is recreated WITHOUT its certificate\n"
        "  resolver and every vhost falls to TRAEFIK DEFAULT CERT — which takes the goatnet\n"
        "  off management, including the mgmt hosts themselves.\n\n"
        "  Fix: stat the overlay and add `{% if <var>.stat.exists %}-f docker-compose.acme.yml{% endif %}`,\n"
        "  and name the services the role owns instead of a bare `up -d`.\n"
        "  See docs/findings/F-312-bare-docker-compose-drops-the-acme-overlay.md.",
        file=sys.stderr,
    )
    sys.exit(1)

print("compose overlay safety: PASS — every explicit -f set carries the ACME overlay")
print("  and names its services.")
PY
