#!/usr/bin/env bash
# check-partition-render-consistency.sh — partner-read partition flag vs the
# rendered ACL (coupled-fact guard #1, redteam remediation).
#
# THE COUPLED FACTS: `zenoh_partner_read_partition: true` in a goatnet's
# group_vars says the partner read carve is ON; the router template only
# RENDERS the carve when the PARTNER LANE MAP is ALSO non-empty. The gate
# (roles/goat-zenoh/templates/config.json5.j2, §subjects trial-peer-read):
#
#   {% if zenoh_partner_read_partition | default(false)
#         and _partner_lane_map | length > 0 %}
#
# `_partner_lane_map` IS A MERGE OF TWO SOURCES (ADR 1138), and this guard has
# to read both or it inverts:
#
#   zenoh_partner_slot_blocks — pre-cut ranges of the slot pool, each bound to
#                               a lane-set; expands to one anchor per slot.
#   zenoh_partner_read_lanes  — the explicit per-anchor map, applied last.
#
# Before ADR 1138 only the explicit map existed and this guard read only that.
# A goatnet that carves partners the RECOMMENDED way — pre-cut blocks, explicit
# map deliberately empty — renders the carve correctly, and the old guard called
# that a lying flag. Reading only the explicit map does not just miss failures,
# it manufactures them, which is the worse direction for a guard to be wrong in.
#
# So flag-true + lanes-empty renders the byte-stable link_protocols:["tls"]
# UNIVERSAL read — the flag reads "partition on" while every partner anchor
# still reads the whole fabric, and nothing anywhere errors.
#
# This guard mirrors the template gate EXACTLY: effective flag truthy AND
# effective lanes empty ⇒ hard FAIL. Scopes that never set the flag ride the
# role default false (roles/goat-zenoh/defaults/main.yml:300) and render
# the carve off — flag reads off, behavior is off, coherent, out of scope.
# Fix on failure: populate the lanes from the goatnet's own registry, or set
# the flag false until a partner is carved — never ship the lying flag.
#
# --self-test: proves the guard RED against a broken fixture and GREEN against
# fixed ones before its verdict on the real tree is trusted (doctrine P6 — a
# checker that greens on its own excuse is worse than none). CI runs the
# self-test before every real run.
set -euo pipefail

if [ "${1:-}" = "--self-test" ]; then
  self="$(cd "$(dirname "$0")" && pwd)/$(basename "$0")"
  tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
  # RED: flag true, lanes {} — the defect, reconstructed.
  mkdir -p "$tmp/red/group_vars/broken_hosts"
  cat > "$tmp/red/group_vars/broken_hosts/data-access.yml" <<'EOF'
zenoh_partner_read_partition: true
zenoh_partner_read_lanes: {}
EOF
  if KNOWN_OPEN_FILE=/dev/null "$self" "$tmp/red" >/dev/null 2>&1; then
    echo "SELF-TEST FAIL: guard passed a flag-true/lanes-empty fixture — it never goes red"
    exit 1
  fi
  echo "self-test: RED proven — flag-true + lanes-empty fixture fails"
  # GREEN: flag true with a carved lane; flag false with empty lanes.
  mkdir -p "$tmp/green/group_vars/carved_hosts" "$tmp/green/group_vars/off_hosts"
  cat > "$tmp/green/group_vars/carved_hosts/data-access.yml" <<'EOF'
zenoh_partner_read_partition: true
zenoh_partner_read_lanes:
  "24d7fbd06eb1f4148a1a4d634b9f4df7": ["ZAGROS"]
EOF
  cat > "$tmp/green/group_vars/off_hosts/data-access.yml" <<'EOF'
zenoh_partner_read_partition: false
zenoh_partner_read_lanes: {}
EOF
  # GREEN, ADR 1138 shape: pre-cut BLOCKS carve the partners, explicit map
  # deliberately empty. This is the recommended configuration and the old
  # explicit-map-only guard failed it. Regression-locked here.
  mkdir -p "$tmp/green/group_vars/blocks_hosts"
  cat > "$tmp/green/group_vars/blocks_hosts/data-access.yml" <<'EOF'
zenoh_partner_read_partition: true
zenoh_partner_read_lanes: {}
zenoh_partner_slot_blocks:
  - lanes: ["REL-ALL"]
    from: 900
    to: 929
EOF
  KNOWN_OPEN_FILE=/dev/null "$self" "$tmp/green" >/dev/null
  echo "self-test: GREEN proven — carved-lane, pre-cut-block and flag-off fixtures pass"
  # RED: flag true with BOTH sources empty — still the lying flag.
  mkdir -p "$tmp/red2/group_vars/broken2_hosts"
  cat > "$tmp/red2/group_vars/broken2_hosts/data-access.yml" <<'EOF'
zenoh_partner_read_partition: true
zenoh_partner_read_lanes: {}
zenoh_partner_slot_blocks: []
EOF
  if KNOWN_OPEN_FILE=/dev/null "$self" "$tmp/red2" >/dev/null 2>&1; then
    echo "SELF-TEST FAIL: guard passed flag-true with an EMPTY block list too"
    exit 1
  fi
  echo "self-test: RED proven — flag-true with both lane sources empty fails"
  echo "OK: partition-render-consistency self-test — red-first proof holds"
  exit 0
fi

[ $# -eq 0 ] && cd "$(git rev-parse --show-toplevel)"
ANSIBLE_DIR="${1:-ops/ansible}"
KNOWN_OPEN="${KNOWN_OPEN_FILE:-ops/ci/coupled-fact-known-open.txt}"

python3 - "$ANSIBLE_DIR" "$KNOWN_OPEN" <<'PY'
import glob, os, re, sys

ansible_dir = sys.argv[1]
known_open_path = sys.argv[2] if len(sys.argv) > 2 else ""

# Shrink-only baseline (ops/ci/coupled-fact-known-open.txt): scopes already
# tracked as open are grandfathered to a NOTE so the guard ships green and
# blocks NEW introductions; a grandfathered scope that no longer reproduces
# FAILS ("you fixed it — remove the line"), so the set can only shrink.
KNOWN_OPEN = set()
for _line in (open(known_open_path, encoding="utf-8") if known_open_path and os.path.exists(known_open_path) else []):
    _line = _line.strip()
    if not _line or _line.startswith("#"):
        continue
    parts = _line.split("|")
    if len(parts) >= 2 and parts[0].strip() == "partition":
        KNOWN_OPEN.add(parts[1].strip())
known_open_hit = set()

TRUTHY = {"true", "yes", "on", "1"}
FALSY = {"false", "no", "off", "0"}
FLAG = "zenoh_partner_read_partition"
LANES = "zenoh_partner_read_lanes"
BLOCKS = "zenoh_partner_slot_blocks"


def strip_value(raw):
    """Drop a trailing comment and one layer of matching quotes."""
    v = raw.strip()
    if v.startswith(("'", '"')):
        q = v[0]
        end = v.find(q, 1)
        if end != -1:
            return v[1:end]
    return re.split(r"\s+#", v, maxsplit=1)[0].strip()


def scan(path):
    """Top-level assignments in one group_vars file, block-mapping-aware:
    key -> (line, inline_value, indented_entry_count). Regex, not
    yaml.safe_load — these files carry Jinja values that must be read as
    written (mirrors check-group-vars-parity.sh)."""
    out = {}
    cur = None
    with open(path, encoding="utf-8") as fh:
        for n, line in enumerate(fh, 1):
            raw = line.rstrip("\n")
            if raw.strip() == "" or raw.lstrip().startswith("#"):
                continue  # blanks/comments neither open nor close a block
            m = re.match(r"^([a-z][a-z0-9_]*)\s*:\s*(.*)$", raw)
            if m:
                key, inline = m.group(1), strip_value(m.group(2))
                out[key] = [n, inline, 0]
                cur = key
                continue
            if raw.startswith((" ", "\t")) and cur is not None:
                out[cur][2] += 1  # an entry (or nested line) under the open key
                continue
            cur = None  # '---', a col-0 list item, etc.
    return out


def scopes():
    """Deployment scope -> {key: (path, line, inline, entries)}. A scope is one
    group_vars directory, one flat group_vars file, or one inventory
    group_vars file; later files in a dir win (YAML last-wins)."""
    found = {}
    for d in sorted(glob.glob(f"{ansible_dir}/group_vars/*/")):
        name = os.path.basename(d.rstrip("/"))
        if name == "_examples":
            continue
        for p in sorted(glob.glob(f"{d}*.yml")):
            for k, v in scan(p).items():
                found.setdefault(name, {})[k] = (p, v[0], v[1], v[2])
    for p in sorted(glob.glob(f"{ansible_dir}/group_vars/*.yml")):
        name = os.path.basename(p)[:-4]
        for k, v in scan(p).items():
            found.setdefault(name, {})[k] = (p, v[0], v[1], v[2])
    for p in sorted(glob.glob(f"{ansible_dir}/inventory/group_vars/*.yml")):
        name = f"inventory:{os.path.basename(p)[:-4]}"
        for k, v in scan(p).items():
            found.setdefault(name, {})[k] = (p, v[0], v[1], v[2])
    return found


fail = 0
checked = 0
for scope, keys in sorted(scopes().items()):
    f = keys.get(FLAG)
    if f is None:
        continue  # rides the role default false — coherent, out of scope
    val = f[2].strip().lower()
    if val in FALSY:
        continue  # flag reads off, render is off — coherent
    if val not in TRUTHY:
        print(
            f"note: group_vars/{scope} sets {FLAG} to a non-literal value "
            f"({f[2]!r}) — the render gate cannot be evaluated statically; skipping"
        )
        continue
    checked += 1
    l = keys.get(LANES)
    lanes_empty = l is None or (l[2] in ("", "{}") and l[3] == 0)
    # ADR 1138: pre-cut slot blocks feed the SAME _partner_lane_map, so a
    # non-empty block list renders the carve on its own. An empty explicit map
    # is the RECOMMENDED shape when blocks are doing the carving.
    b = keys.get(BLOCKS)
    blocks_empty = b is None or (b[2] in ("", "[]", "{}") and b[3] == 0)
    if lanes_empty and blocks_empty:
        lanes_where = "<absent>" if l is None else f"{l[0]}:{l[1]} = {l[2] or '(block with no entries)'}"
        blocks_where = "<absent>" if b is None else f"{b[0]}:{b[1]} = {b[2] or '(block with no entries)'}"
        if scope in KNOWN_OPEN:
            known_open_hit.add(scope)
            print(f"KNOWN-OPEN (tracked, shrink-only): group_vars/{scope}: {FLAG} true while {LANES} empty "
                  f"({f[0]}:{f[1]}). Grandfathered in {known_open_path}; remove that line when the fix lands.")
            continue
        print(f"FAIL: group_vars/{scope}: {FLAG}: true ({f[0]}:{f[1]}) while BOTH partner lane")
        print(f"      sources are empty — {LANES} ({lanes_where})")
        print(f"      and {BLOCKS} ({blocks_where}).")
        print( "      The render gate (roles/goat-zenoh/templates/config.json5.j2 §trial-peer-read)")
        print( "      requires BOTH flag-true AND a non-empty merged lane map, so this renders the flat")
        print( '      link_protocols:["tls"] UNIVERSAL read: the flag says the partner carve is ON')
        print( "      while every partner anchor still reads the whole fabric — silently.")
        print( "      Fix, in preference order: (1) declare zenoh_partner_slot_blocks so partners are")
        print( "      pre-cut and onboarding costs no restart (ADR 1138) — this is the recommended")
        print( "      shape and leaves the explicit map empty; (2) carve a named anchor from THIS")
        print( "      goatnet's own registry; (3) set the flag false until one exists (the documented")
        print( "      rollback valve). Never ship the lying flag.")
        print(f"      (If this scope is a tracked known-open, add it to {known_open_path} — but only if it")
        print( "       is genuinely sequenced; a NEW scope must be fixed, not grandfathered.)")
        fail = 1

# Shrink-only: a grandfathered scope that no longer reproduces must be removed.
for scope in sorted(KNOWN_OPEN - known_open_hit):
    print(f"FAIL: {scope} is grandfathered in {known_open_path} for the partition defect, but it no longer")
    print( "      reproduces — the fix landed. Remove its line from the baseline (the set only shrinks).")
    fail = 1

if fail:
    sys.exit(1)
print(f"OK: partition flag and rendered read carve agree ({checked} partition-on scope(s) checked; "
      f"{len(known_open_hit)} grandfathered)")
PY
