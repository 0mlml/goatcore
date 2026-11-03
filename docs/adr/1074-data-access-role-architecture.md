# ADR 1074 — Data-access roles: a closed-grammar, two-plane, registry-derived role architecture

| Field | Value |
|---|---|
| Status | Accepted (direction ratified by the identity owner 2026-07-09; v1 lattice enforced in panoscope this track; build-out sequenced). Revised in place same day by the mandated fresh-context second pass — see §Second pass. |
| Date | 2026-07-09 |
| Repo scope | goat core |
| Supersedes | — (fills the dimension ADR 1073 / rbac-adherence.md §3.3b declared missing) |
| Superseded by | — |
| Related | [ADR 1073](1073-rbac-adherence-principle.md), ADR 1043, [ADR 1025](1025-zenoh-dynamic-tenant-authz-slot-pool.md), [ADR 1027](1027-partner-release-set-governance.md), [ADR 1051](1051-identity-descriptor-standard.md), [ADR 1067](1067-identity-grammar-generated-sot.md) |

## TL;DR (for a layperson)

Until now the system had no vocabulary for "which data may this person see" — only "is this person an administrator." This decision creates that vocabulary as a small, strict language of roles: see everything (admins, and anyone explicitly trusted with it), see your own organization's data (automatic when you join), see one specific producer's data, and see the practice/synthetic fleet (everyone, by default). Roles for each producer and organization spring into existence automatically the moment the producer is created — nobody hand-writes them, which is how big organizations end up with thousands of stale, contradictory roles. The same vocabulary describes rights for both people (who log in) and machines (which hold certificates), with a registry acting as the dictionary between the two.

## Context

The 2026-07-09 RBAC track (ADR 1073) made the declared permission matrix enforceable and made panoscope viewer-scoped — and in doing so exposed that the enforced group vocabulary describes *platform administration*, not *data access*: no group answered "what fabric data may this human see," live partner identities carry no groups at all, and the track briefly invented a data policy (obs groups → fabric visibility) to satisfy its own CI before reverting it. The identity owner has now ratified the direction: three v1 access modes (per real producer / per notional defaulted-on / admin-sees-all), orthogonal-but-linked to platform rights, hierarchical and composable, conversant across both identity planes, with bones for extension and explicit guards against role sprawl.

## Decision

**Data access is its own role axis, expressed in a closed grammar (`data.<action>.<scope>[:<param>]`), governed by one SoT (`ops/permissions/data-access-roles.yaml`), derived from the registry rather than stored, enforced per-plane at declared points, and linked to other axes only by explicit implication edges.**

1. **v1 lattice:** `data.view.all` ⊒ `data.view.org:<org>` ⊒ `data.view.producer:<key>`; `data.view.notional` (default-granted to every authenticated identity, per-goatnet flag); `data.write.producer:<key>` names the existing wire write slot in the same vocabulary. Producer keys are what the registry holds in **both deployed shapes** — seed-model kebab prefixes (the live ZAGROS population) and 32-hex slot-pool anchors — one param charset covers both.
2. **Parametric roles are virtual (G3):** producer/org roles exist *iff* the registry row exists — minted atomically with the producer, expired with it, never hand-created. The registry is the role store; the grammar is the lens.
3. **Orthogonality with declared coupling (G2/G4):** cross-axis links are implication edges in the SoT only — `ADMINS ⇒ data.view.all` (the ratified "any platform admin" requirement), org-membership ⇒ org view, producer-custody ⇒ producer view, cert write-anchor ⇒ producer write. **IAM-ADMIN carries no implication** — identity administration needs the access COP, not standing all-mission-data view; the matrix grants it panoscope `scoped` (see §Second pass). Registry-derived edges are marked `resolver-pending` until the resolver evaluates them. An observer may hold `data.view.all` with zero platform rights.
4. **Belt-and-suspenders apex (G5):** `data.view.all` evaluates as wildcard predicate AND as expansion of all sub-roles; both mechanisms run, each covers the other.
5. **Two-plane conversance (G7):** every role row declares its enforcement point per plane (human: app viewer-scope filters; wire: the router ACL slot for writes). The wire's flat-read reality is recorded in the SoT's dedicated `wire_read_posture` section — visible and machine-readable, but deliberately **not** a grant cell: the wire enforces no view-scoping, and modeling non-enforcement as an enactment would normalize what should stay alarming. The interchange logic is the registry hub: `roles_of(email)` and `roles_of(anchor)` resolve through the same org rows; no string translation between planes exists or is needed.
6. **Sprawl guards G1–G8** (SoT header; design doc §4): closed grammar, orthogonal axes, virtual parametrics, implication-not-enumeration, belt-and-suspenders, deprecate-never-delete, **no role without an enforcement point**, named ratifier per role. Reserved scope words (site/echelon/releaseset/coalition above org; topicclass/topic below producer) land by ADR only.
7. **Transport:** claims carry *explicit extra grants only* (lowercase `data.*` values namespaced apart from UPPERCASE platform groups in the same Ory claim); the automatic majority derives at evaluation from the registry. Anti-bloat by construction. The site Kratos identity schema admits the data grammar as an explicit second `groups`-item alternative (it originally admitted only `^[A-Z…]$`, which silently rejected every data grant — see §Second pass); CI holds schema and SoT patterns in lock-step.

## Alternatives considered

- **Central PDP (SpiceDB/OpenFGA/Keto) now.** The evaluation model (relation-object pairs, implication) is deliberately Zanzibar-shaped so a later migration is mechanical — but a central authz server is a broker-shaped island-mode liability; policy-as-data with in-process evaluation preserves edge degradation.
- **Store parametric role instances.** Creates orphan/drift/recertification burden — the enterprise sprawl disease. Derivation makes stale roles structurally impossible.
- **Per-human wire enforcement.** Humans aren't wire peers; F-181 excludes attribute ACLs without a zenohd fork. Rejected per ADR 1073's seam decision.
- **Reuse platform groups for data tiers.** The category error this ADR exists to end.

## Consequences

- **+** "What may this person see" finally has a vocabulary, a SoT, automatic org/producer roles, and a default (notional) that makes the system useful to partners on day one without exposing anyone's real data.
- **+** The wire plane's realities — write slots and flat reads — are declared in the same model (write role rows; the `wire_read_posture` section), so the ADR 1025 posture is auditable policy, not an asterisk.
- **+** Extension has bones (reserved scopes) and a gate (ADR per new scope/action), the anti-sprawl posture.
- **−** v1 org-scope enforcement covers the value's own prefix + explicit producer grants; on seed-model goatnets (ZAGROS) topics are keyed by *member* producer prefix, so an org value alone typically matches nothing there until the registry resolver lands (build item 1). Stated in the built-vs-next map; grant explicit `producer:` values in the interim.
- **−** Until the onboarding grant surface lands (build item 2), explicit grants (`data.view.all` to an observer) ride manual Kratos identity edits — deliverable now that the identity schema admits the grammar, and still blocked in the manifest pipeline by design (exact platform-enum).
- **−** Wire reads stay flat until release-set keying (reserved `releaseset` scope, ADR 1027 arc) — unchanged from ADR 1025/1073, now visible as the posture section.

## Second pass (2026-07-09, fresh-context re-derivation — mandated)

A fresh-context review independently re-derived the architecture from the requirements and then attacked the first pass. The shape survived: closed verb-first grammar, virtual registry-derived roles, declared-implication coupling, registry-as-interchange, belt-and-suspenders apex, no central PDP. Four substantive corrections landed in place:

1. **Transport was undeliverable.** The site Kratos schema pattern (`^[A-Z][A-Z0-9-]{0,40}$`) rejected every `data.*` value — no grant could ever reach a claim; the first pass never minted a test identity. Fixed: schema pattern widened with the grammar as an explicit alternative; CI now probes deliverability (`check-data-access-roles.sh` §6).
2. **The producer param could not name the live fleet.** `[a-f0-9]{8,32}` matched slot-pool anchors only; every deployed ZAGROS producer is a kebab prefix (checked against real roster rows). Fixed: one param charset for both shapes.
3. **`IAM-ADMIN ⇒ data.view.all` was inherited, not ratified.** Its basis — the 2026-06-29 matrix `read` row — predates viewer scoping and ratified surface *entry*, not unrestricted data view; reading it as view-all was retroactive reinterpretation (the invented-policy pattern in subtler form). The group's own charter says "NOT a full platform admin." Narrowed to matrix `scoped`; re-widening is a one-row identity-owner decision, recorded at all three sites (SoT, matrix, role default).
4. **The notional default was half-granted.** Only synthetic *nodes* rendered; their edges, live events, and watches were still denied — access mode 2 would have shown partners a fleet of dead nodes. Fixed in the evaluator + ws path, test-locked.

Also hardened: the DAG-acyclicity check the SoT claimed was implemented (it wasn't); boundary prefixes (`common_floor`/`platform_only`) moved into the SoT and drift-checked against the evaluator; the wire posture restructured from a grant cell to `wire_read_posture`; registry-derived implications annotated `resolver-pending` so the SoT never claims an auto-grant no code performs; stale pre-1074 tier vocabulary purged from the surrounding docs.

## Addendum (2026-07-09) — resolver landed (build item 1)

The registry data-roles resolver is built. It is the portal endpoint `GET /api/access/data-roles`, accepting exactly one of `identity=<uuid>`, `email=<addr>`, or `anchor=<producer-key>`, and returning the principal (id + kind person|machine), the derived role sentences, the expanded producer-key set, and a `derived_at` stamp with the roster's freshness provenance. It serves **only registry-derived roles** — org membership, producer custody, and the org roll-up; explicit claim-carried grants ride the Ory claim and the notional default is a per-goatnet flag, neither is returned here (design doc §5).

- **Principal-key discipline (ADR 1051/1069).** The resolver keys the principal on the **identity UUID**. `email=` is a login-email **alias**: it resolves to the identity UUID first (case-insensitive), and the roster join then uses the tag-independent email-hash (the F-234 key), never the raw email — no email-keyed join is stored. `anchor=` keys a machine principal on its producer/write-anchor key.
- **The org-hierarchy roll-up rule, as implemented.** Holding `data.view.org:<parent>` covers every child org's producers **transitively, through vertical `parent` containment ONLY** (ADR 1043 parent edges). Lateral/time-bounded relationship edges (`comrel`, `supports`, `data-authority`) confer nothing by inference — the reserved `data-authority` edge is the explicit cross-org hook (design doc §8; recognition ≠ authorization). **Multi-org membership is a union.** This landed as a new SoT implication row (`ops/permissions/data-access-roles.yaml`, the vertical parent-containment edge), phrased as the registry containment fact so the DAG-acyclicity check reads it as a registry-derived edge, not a `data.view.org` self-loop.
- **Statuses unchanged.** The registry-derived implication rows (membership, custody, and the new parent-containment row) stay `status: resolver-pending`. A status flips to `enforced` only when a surface *consumes* the resolver (panoscope/flashlight/stethoscope, follow-ons §2–§3); the resolver existing is not itself an enforcement point. The apex, ADMINS cross-axis, cert-write-anchor, and notional-default rows remain `enforced` as before.
- **Grant-row extension point (ADR 1075, scope-limited out).** Grant rows have **no registry store yet**, so the resolver returns membership/custody/roll-up roles only. When the grant store lands (follow-ons §0c.4), active grant rows within their effective window join at the marked extension point in the handler — grantee (org|principal) → granted producer — as additional derived roles, read from the registry with no parallel store (SoT `no_third_shim`).
