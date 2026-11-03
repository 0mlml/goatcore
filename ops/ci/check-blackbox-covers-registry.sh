#!/usr/bin/env bash
# check-blackbox-covers-registry.sh — every probe-able declared service is
# actually probed.
#
# THE DEFECT THIS PREVENTS. A goatnet declares its hosted services in
# `isitup_services` (group_vars/<site>_hosts). Blackbox probe targets live in a
# DIFFERENT group's vars (group_vars/<site>_obs_hosts), because the obs host is
# a separate machine in a separate host group, and Ansible group_vars cannot
# read across groups. So the two lists are maintained by hand, and nothing made
# them agree.
#
# The failure is silent in the direction that matters. TLS certificate
# observability is PER TARGET: `probe_ssl_earliest_cert_expiry` exists for the
# names blackbox probes and for no others. Add a service to the registry,
# forget the probe target, and its certificate is unwatched — while
# TlsCertExpiryUnobserved stays quiet, because absent() is false as long as ANY
# target still reports a certificate. A dashboard full of green, and one vhost
# whose renewal can stop unnoticed until the day it expires.
#
# Same class as F-197 (declared-but-unresolvable deploys dark): a service that
# is declared and unobserved is declared and dark, just later.
#
# Scope: only goatnets that set `obs_blackbox_enabled: true`. A goatnet with
# probing off is not in violation, it has made a different choice.
#
# LIFECYCLE (#1518). A registry row marked `lifecycle: planned` / `retiring`
# or `listed: false` is not required to be probed — the obs-host role drops its
# target at render time, reading the registry through
# `obs_blackbox_registry_group`. So that group must be named, and must be this
# goatnet's own mgmt group: without it a retired service keeps paging until
# someone edits a second list, which is the defect #1518 removed.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

python3 - <<'PY'
import glob, os, re, sys

try:
    import yaml
except ImportError:
    print("check-blackbox-covers-registry: SKIP — pyyaml not available")
    sys.exit(0)


def load(path):
    try:
        with open(path, encoding="utf-8") as fh:
            return yaml.safe_load(fh) or {}
    except Exception as exc:                      # noqa: BLE001
        print("  cannot parse %s: %s" % (path, exc))
        return {}


def expand(value, site_id, zone):
    """Resolve the two Jinja refs the registry actually uses.

    Deliberately not a Jinja render: these files are full of lookups and
    env vars that cannot resolve outside a play, and a real render would
    fail on all of them. Any fqdn carrying an unresolved `{{ ... }}` after
    this is reported rather than guessed at.
    """
    return (value or "").replace("{{ site_id }}", site_id).replace(
        "{{ site_dns_zone }}", zone)


failures = []
checked = 0

for obs_vars in sorted(glob.glob("ops/ansible/group_vars/*_obs_hosts/vars.yml")):
    obs = load(obs_vars)
    if not obs.get("obs_blackbox_enabled"):
        continue

    group = os.path.basename(os.path.dirname(obs_vars))          # <site>_obs_hosts
    site_group = group[: -len("_obs_hosts")] + "_hosts"
    site_vars = "ops/ansible/group_vars/%s/vars.yml" % site_group
    if not os.path.exists(site_vars):
        failures.append("%s enables blackbox but %s does not exist, so the "
                        "declared-service list cannot be checked" % (group, site_vars))
        continue

    site = load(site_vars)
    site_id = site.get("site_id", "")
    zone = site.get("site_dns_zone", "")
    if not site_id or not zone:
        failures.append("%s: site_id / site_dns_zone missing, cannot expand fqdns" % site_vars)
        continue

    reg_group = obs.get("obs_blackbox_registry_group", "")
    if reg_group != site_group:
        failures.append("%s: obs_blackbox_registry_group is %r, must be %r — the "
                        "role reads that group's isitup_services to stop probing "
                        "rows marked planned, retiring or listed: false"
                        % (obs_vars, reg_group, site_group))

    probed = set()
    for t in obs.get("obs_blackbox_targets") or []:
        host = re.sub(r"^https?://", "", str(t))
        probed.add(re.split(r"[:/]", host, maxsplit=1)[0])

    declared = {}
    for svc in site.get("isitup_services") or []:
        if not svc.get("probe_gate", True):
            continue                                   # opted out on purpose
        if svc.get("lifecycle", "live") != "live" or svc.get("listed", True) is False:
            continue                                   # not live: the role drops it
        fq = expand(svc.get("fqdn"), site_id, zone)
        if not fq:
            continue
        if "{{" in fq:
            failures.append("%s: service %r fqdn %r still carries an unresolved "
                            "template ref; this guard cannot verify it"
                            % (site_vars, svc.get("id"), fq))
            continue
        declared.setdefault(fq, []).append(svc.get("id"))

    checked += 1
    missing = sorted(set(declared) - probed)
    if missing:
        for fq in missing:
            failures.append(
                "%s: %s is declared (%s) and probe_gate is true, but no entry in "
                "%s obs_blackbox_targets — its certificate is unwatched"
                % (site_group, fq, ", ".join(declared[fq]), group))

    # The other direction is a warning, not a failure: probing something the
    # registry does not declare is over-coverage, which is harmless, and a few
    # goatnets legitimately probe an apex that is not an isitup row.
    extra = sorted(probed - set(declared))
    if extra:
        print("  note: %s probes %d name(s) not declared in %s: %s"
              % (group, len(extra), site_group, ", ".join(extra)))

if failures:
    print("check-blackbox-covers-registry: FAIL")
    for f in failures:
        print("  - %s" % f)
    print()
    print("  Every probe_gate service needs a target in the obs group's")
    print("  obs_blackbox_targets, or its TLS certificate renews unobserved.")
    print("  Coverage is per-target: TlsCertExpiryUnobserved stays quiet while")
    print("  any one target still reports a cert, so a gap here is silent.")
    sys.exit(1)

if checked == 0:
    print("check-blackbox-covers-registry: OK — no goatnet sets obs_blackbox_enabled")
else:
    print("check-blackbox-covers-registry: PASS — %d goatnet(s) probe every "
          "declared probe_gate service" % checked)
PY
