# Identity grammar — worked examples

Five illustrative principals showing how the vocabulary in [`grammar.yaml`](grammar.yaml)
*composes* on real-ish identities. These are **grammar excerpts** (the identity +
affiliation + system fields), not full onboarding manifests (which also carry
netbird / ory / zenoh blocks) — the point is to make the dimensions concrete.
Values are illustrative.

The whole vocabulary in one read: `python3 ops/identity/generate.py --catalog`.

---

## 1 · Organic developer (person)

An engineer in the operating organisation, building on the fabric.

```yaml
identity:
  type: person
  handle: fabric-dev-01
affiliation:
  id: org-1
  kind: organic            # native — the operator's own organisation
  nation: XAA
# role DEVELOPER (netbird.auto_groups: [DEVELOPER])
```

**Exercises:** `identity.type=person` · `affiliation.kind=organic` (native) · role `DEVELOPER`.
No `unit_code` — a developer isn't a codified unit. Capability (`DEVELOPER`) is
named by role, never by affiliation.

---

## 2 · Partner tactical operator (person)

A partner operator, in a unit with an alliance-codified identity.

```yaml
identity:
  type: person
  handle: partner-op-01
affiliation:
  id: partner-unit-1
  kind: partner            # native bucket…
  nation: XAB              # …nation crosswalks to doctrine (STANAG 1059)
  unit_code:
    scheme: nato-logfas    # doctrinal — codified in LOGFAS, nato-scoped
    value: "UNIT-XAB-0007"
    source: authoritative  # matched an ingested official registry
# role OPERATOR · echelon COMMAND-POST
```

**Exercises:** `partner` affiliation + `nation` + a **doctrinal** `unit_code`
(`nato-logfas`) with an `authoritative` provenance grade · tactical role
`OPERATOR` · `echelon=COMMAND-POST`. The native UUID anchor is still the key —
the LOGFAS code rides as a graded, mapped attribute.

---

## 3 · Civil academic researcher (person)

A university researcher granted read access — no government or commercial affiliation.

```yaml
identity:
  type: person
  handle: univ-researcher
affiliation:
  id: example-university
  kind: civil              # native — the non-gov civil-society kind
  nation: XAA
# role OBSERVER (read-only)
```

**Exercises:** `affiliation.kind=civil` (the gap closed no home in
vendor/organic/partner) · role `OBSERVER`. No `unit_code` (civil orgs aren't
codified units).

---

## 4 · Vendor data-fusion process (system)

A commercial vendor's headless analytics process publishing on the fabric.

```yaml
identity:
  type: system            # a headless principal (uuid / Zenoh slot)
  handle: acme-fusion-svc
  autonomy: supervised    # autonomous with a human on-the-loop
  enrolled_by: acme-lead@example.com   # custody edge (not ownership)
affiliation:
  id: acme-analytics
  kind: vendor            # native — a commercial company
# node-class PROCESSOR (netbird.auto_groups: [PROCESSOR])
```

**Exercises:** `identity.type=system` · `vendor` affiliation · node-class
`PROCESSOR` (non-C2 data process) · `autonomy=supervised` · the person→system
`enrolled_by` custody edge. Autonomy is orthogonal to node-class.

---

## 5 · Allied autonomous sensor (system)

A partner-nation uncrewed sensor asset, self-directing, in a nationally-coded unit.

```yaml
identity:
  type: system
  handle: svk-sensor-14
  autonomy: autonomous    # fully self-directing
affiliation:
  id: svk-recon-coy
  kind: partner
  nation: XAC
  unit_code:
    scheme: sovereign     # doctrinal — the nation's own scheme
    value: "XAC-RC-014"
    source: operator-verified
# node-class SENSOR · echelon MFT
```

**Exercises:** `system` + `partner`/`nation=XAC` + node-class `SENSOR` +
`autonomy=autonomous` + a **sovereign** (nation-scoped doctrinal) `unit_code`
with an `operator-verified` grade + `echelon=MFT`. Shows a doctrinal code that is
*national-scope only* — meaningless to another nation, which is exactly why the
native anchor, not the code, is the identity key.

---

### Coverage

These five span: both principal types (person/system); all four affiliation kinds
(vendor/organic/partner/civil); roles DEVELOPER/OPERATOR/OBSERVER + node-classes
PROCESSOR/SENSOR; echelons COMMAND-POST/MFT; autonomy supervised/autonomous; both
`unit_code` schemes (nato-logfas/sovereign) with authoritative/operator-verified
provenance; and the doctrinal-code-as-graded-attribute vs native-anchor-as-key split.
