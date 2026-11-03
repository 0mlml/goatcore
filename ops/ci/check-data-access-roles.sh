#!/usr/bin/env bash
# ops/ci/check-data-access-roles.sh — the ADR 1074 grammar guard.
#
# Validates the data-access SoT (ops/permissions/data-access-roles.yaml)
# and the code/config/schema that speaks it:
#   1. the SoT parses; the grammar regex compiles; every role row's name
#      parses against the grammar (parametric placeholders substituted);
#   2. every role row declares at least one real enforcement point (G7 —
#      aspirational vocabulary is rejected, our F-132 lesson);
#   3. the implication graph is a DAG (G4 — no cycles);
#   4. no scope word outside the SoT's axes appears in a role name
#      (extension lands by ADR, not typo);
#   5. the concrete `data.*` string constants AND the boundary prefixes in
#      the panoscope evaluator (src/viewer.rs) match the SoT, so the
#      evaluator and the SoT can't drift silently;
#   6. the site Kratos identity schema's `groups` item pattern actually
#      ADMITS the grammar values (the transport reality: Kratos pattern-
#      validates every groups item, so a schema that only allows
#      ^[A-Z...]$ makes every data.* grant undeliverable — the first-pass
#      defect this check exists to prevent recurring);
#   7. the SoT's platform→data implication set equals the matrix's
#      unrestricted (`read`) panoscope grantors (the cross-axis edge and
#      the matrix row must move together).
set -euo pipefail

SOT="${1:-ops/permissions/data-access-roles.yaml}"
VIEWER="${2:-ops/panoscope/src/viewer.rs}"
KRATOS_SCHEMA="${3:-ops/ansible/roles/ory/templates/ory/identity-schema.json.j2}"
MATRIX="${4:-ops/permissions/group-permissions.yml}"

python3 - "$SOT" "$VIEWER" "$KRATOS_SCHEMA" "$MATRIX" <<'PY'
import json, re, sys, yaml

sot_path, viewer_path, kratos_path, matrix_path = sys.argv[1:5]
sot = yaml.safe_load(open(sot_path))

pattern = re.compile(sot["grammar"]["pattern"])
ok = True

SAMPLES = {"<org_prefix>": "sample-org", "<producer_key>": "sample-producer", "<anchor>": "0f" * 8}

def parses(value):
    # Parametric rows use <angle> placeholders; substitute a valid sample.
    for ph, sub in SAMPLES.items():
        value = value.replace(ph, sub)
    return bool(pattern.match(value))

# 1+2: role rows parse and declare real enforcement.
for row in sot.get("roles", []):
    name = row.get("role", "")
    if not parses(name):
        print(f"FAIL: role {name!r} does not parse against the grammar")
        ok = False
    enforced = row.get("enforced_at") or {}
    real = {k: v for k, v in enforced.items() if v and "not enforced" not in str(v) and "not applicable" not in str(v)}
    if not real:
        print(f"FAIL (G7): role {name!r} declares no enforcement point — aspirational vocabulary rejected")
        ok = False

# 3: implication graph is a DAG. Nodes = the first grammar-shaped token in
# each endpoint (parametrics collapse to their scope word), else the raw
# holder string; edges holder -> implies.
role_token = re.compile(r"data\.[a-z]+\.[a-z]+")
def node_of(s):
    m = role_token.search(str(s))
    return m.group(0) if m else str(s).strip()

edges = {}
for imp in sot.get("implications", []):
    a, b = node_of(imp.get("holder", "")), node_of(imp.get("implies", ""))
    edges.setdefault(a, set()).add(b)

WHITE, GRAY, BLACK = 0, 1, 2
color = {}
def cyclic(n, path):
    color[n] = GRAY
    path.append(n)
    for m in edges.get(n, ()):  # noqa: B007
        c = color.get(m, WHITE)
        if c == GRAY:
            print(f"FAIL (G4): implication cycle: {' -> '.join(path + [m])}")
            return True
        if c == WHITE and cyclic(m, path):
            return True
    path.pop()
    color[n] = BLACK
    return False

for n in list(edges):
    if color.get(n, WHITE) == WHITE and cyclic(n, []):
        ok = False
        break

# 4: scope words are closed.
allowed_scopes = set(sot["grammar"]["axes"]["scope"])
reserved = set()
for v in (sot.get("reserved_scopes") or {}).values():
    reserved |= set(v)
for row in sot.get("roles", []):
    scope_word = row["role"].split(".")[2].split(":")[0]
    if scope_word not in allowed_scopes:
        print(f"FAIL: role {row['role']!r} uses scope {scope_word!r} not in the ratified axes "
              f"(reserved-but-unratified: {sorted(reserved)})")
        ok = False

# 5a: code constants match the SoT role vocabulary EXACTLY (not merely
# parse — a constant edited to any other grammar-valid string must fail).
code = open(viewer_path, encoding="utf-8").read()
expected_consts = {
    "ROLE_VIEW_ALL": "data.view.all",
    "ROLE_VIEW_NOTIONAL": "data.view.notional",
    "ROLE_VIEW_ORG_PREFIX": "data.view.org:",
    "ROLE_VIEW_PRODUCER_PREFIX": "data.view.producer:",
}
found_consts = dict(re.findall(r'(ROLE_[A-Z_]+): &str = "([^"]+)"', code))
if found_consts != expected_consts:
    print(f"FAIL: viewer.rs ROLE_* constants {found_consts} != expected {expected_consts}")
    ok = False
for const in found_consts.values():
    probe = const + ("sample-key" if const.endswith(":") else "")
    if not pattern.match(probe):
        print(f"FAIL: code constant {const!r} (as {probe!r}) does not parse against the SoT grammar")
        ok = False

# 5b: boundary prefixes in code match the SoT boundaries.
bounds = sot.get("boundaries") or {}
m = re.search(r'COMMON_PREFIXES:\s*\[&str;\s*\d+\]\s*=\s*\[([^\]]*)\]', code)
code_common = set(re.findall(r'"([^"]+)"', m.group(1))) if m else set()
m = re.search(r'AUDIT_PREFIX:\s*&str\s*=\s*"([^"]+)"', code)
code_platform = {m.group(1)} if m else set()
if code_common != set(bounds.get("common_floor", [])):
    print(f"FAIL: viewer.rs COMMON_PREFIXES {sorted(code_common)} != SoT boundaries.common_floor {bounds.get('common_floor')}")
    ok = False
if code_platform != set(bounds.get("platform_only", [])):
    print(f"FAIL: viewer.rs AUDIT_PREFIX {sorted(code_platform)} != SoT boundaries.platform_only {bounds.get('platform_only')}")
    ok = False

# 6: the site Kratos identity schema admits the grammar (transport). The
# schema is a Jinja2 template but the groups block is literal JSON — find
# the `groups` property's items pattern and probe it.
kratos = open(kratos_path, encoding="utf-8").read()
gm = re.search(r'"groups"\s*:\s*\{.*?"pattern"\s*:\s*"((?:[^"\\]|\\.)*)"', kratos, re.S)
if not gm:
    print(f"FAIL: no groups item pattern found in {kratos_path}")
    ok = False
else:
    kpat = re.compile(json.loads(f'"{gm.group(1)}"'))
    must_admit = ["ADMINS", "IAM-ADMIN", "data.view.all", "data.view.notional",
                  "data.view.org:sample-org", "data.view.producer:0f0f0f0f",
                  "data.view.producer:sample-producer"]
    must_reject = ["random.thing", "DATA.VIEW.ALL", "data.view.everything", "org:acme"]
    for v in must_admit:
        if not kpat.fullmatch(v):
            print(f"FAIL: Kratos groups pattern rejects {v!r} — the grant is undeliverable ({kratos_path})")
            ok = False
    for v in must_reject:
        if kpat.fullmatch(v):
            print(f"FAIL: Kratos groups pattern admits non-grammar value {v!r} ({kratos_path})")
            ok = False
    # Every data.* value the Kratos schema admits must parse in the SoT
    # grammar too (probe the grammar's own samples through both).
    for v in ["data.view.all", "data.view.org:sample-org", "data.write.producer:0f0f0f0f"]:
        if kpat.fullmatch(v) and not pattern.match(v):
            print(f"FAIL: Kratos schema admits {v!r} but the SoT grammar does not — patterns drifted")
            ok = False
    # 6a: no over-admission — write pairs only with producer. Sentences
    # with no role row / no enforcement point (G7) must not even parse,
    # in either the grammar or the transport schema.
    for v in ["data.write.all", "data.write.notional", "data.write.org:acme"]:
        if pattern.match(v):
            print(f"FAIL: SoT grammar admits {v!r} — write scope is producer-only (no such role row)")
            ok = False
        if kpat.fullmatch(v):
            print(f"FAIL: Kratos groups pattern admits {v!r} — write scope is producer-only ({kratos_path})")
            ok = False
    # 6a2: structural lock-step — the schema must embed the SoT grammar
    # body VERBATIM as its data alternative (probe lists alone could miss
    # a drift that changes neither the probes nor the samples).
    if pattern.pattern[1:-1] not in kpat.pattern:
        print(f"FAIL: Kratos schema does not embed the SoT grammar verbatim — patterns drifted ({kratos_path})")
        ok = False

# 6b: data_labels lanes (ADR 1075) are well-formed closed enums with legal
# defaults — labels inherit the G1 discipline.
labels = sot.get("data_labels")
if labels:
    LANES = {"fidelity": {"notional", "simulated", "real"},
             "context": {"experiment", "exercise", "operational"},
             "access": {"community", "org", "controlled"}}
    lanes = labels.get("lanes") or {}
    if set(lanes) != set(LANES):
        print(f"FAIL (ADR 1075): data_labels.lanes must be exactly {sorted(LANES)}, got {sorted(lanes)}")
        ok = False
    for lane, want in LANES.items():
        got = set((lanes.get(lane) or {}).get("values") or [])
        if got != want:
            print(f"FAIL (ADR 1075): lane {lane!r} values {sorted(got)} != ratified {sorted(want)} — new values land by ADR")
            ok = False
    if not (labels.get("guards") or []):
        print("FAIL (ADR 1075): data_labels.guards is empty — the restriction-friction guards are load-bearing")
        ok = False

# 7: the SoT's platform->data implications equal the matrix's unrestricted
# panoscope grantors (`read`; `scoped` rides the grammar, not an edge).
matrix = yaml.safe_load(open(matrix_path))
matrix_full = {
    g["name"] for g in matrix["groups"]
    for gr in g.get("grants", [])
    if gr["service"] == "panoscope" and gr["access"] == "read"
}
sot_full = set()
for imp in sot.get("implications", []):
    hm = re.match(r"platform-group\s+(\S+)", str(imp.get("holder", "")))
    if hm and node_of(imp.get("implies", "")) == "data.view.all":
        sot_full.add(hm.group(1))
if sot_full != matrix_full:
    print(f"FAIL: SoT platform implications to data.view.all {sorted(sot_full)} != "
          f"matrix panoscope `read` grantors {sorted(matrix_full)} — these must move together")
    ok = False

# 6c: the grants row_shape (ADR 1075) declares the closed vocabulary the grant
# store + data-roles resolver enforce — grantee kinds (org: / principal:) and
# the lifecycle status set (active | revoked). Guard the SoT-code contract so an
# edit that drops one of these terms can't silently diverge from the portal's
# registry.Grantee*/registry.Grant* constants (grants.go).
grants = sot.get("grants")
if grants:
    shape = str(grants.get("row_shape", ""))
    for term in ["org:", "principal:", "active", "revoked"]:
        if term not in shape:
            print(f"FAIL (ADR 1075): grants.row_shape must declare {term!r} — the grant store + resolver enforce this closed vocabulary (grants.go)")
            ok = False
else:
    print("FAIL (ADR 1075): grants section is missing — the grant loop's row shape is the SoT law")
    ok = False

if not ok:
    sys.exit(1)
print(f"ok: {len(sot.get('roles', []))} role rows parse + enforce somewhere; implication DAG acyclic; "
      f"viewer constants/boundaries, Kratos schema pattern, matrix implications, and label/grant vocab all match {sot_path}")
PY
