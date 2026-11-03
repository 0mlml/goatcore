# ADR 0844 — Zenoh router deployment topology v1 + ACL contract

| Field | Value |
|---|---|
| Status | Proposed (pilot) |
| Date | 2026-05-12 |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | [`0210-zenoh-universal-substrate.md`](0210-zenoh-universal-substrate.md), [`0500-zenoh-data-fabric.md`](0500-zenoh-data-fabric.md), [`0708-build-zenoh-from-source.md`](0708-build-zenoh-from-source.md), [`0216-goat-net-federation-and-site-planes.md`](0216-goat-net-federation-and-site-planes.md), [`0313-silent-control-plane-outer-wg-wrap.md`](0313-silent-control-plane-outer-wg-wrap.md), [`0407-internal-x509-ca-for-service-tls.md`](0407-internal-x509-ca-for-service-tls.md), [`0512-rapid-adaptation-with-ai.md`](0512-rapid-adaptation-with-ai.md), `../design/mesh-acl.md`, `../findings/F-099-netbird-daemon-not-installed-on-mgmt-hosts.md` |

## TL;DR (for a layperson)

The basic shape of goat is **Zenoh running inside a NetBird WireGuard peer-to-peer network.** NetBird is the underlay — every peer (sensor, effector, operator laptop, agent, mgmt-host) joins the mesh and gets an identity-attested encrypted tunnel to every other peer. Zenoh runs on top of that mesh as the universal interaction substrate: pub/sub, queryable storage, service discovery, command channels, configuration distribution, identity attestation, health, partner federation, all on one bus. Access control is two layers — NetBird gates which peers can reach which (network layer); Zenoh gates which peers can publish or subscribe on which topics (application layer). Only command roles can issue actuator commands; sensors cannot. Bulk sensor data stays local for bandwidth; small important signals (events, commands, replies, alerts) ride the federation between sites. We do not adopt any single vendor's data model as canonical — translation lives at the boundary.

## Context

Zenoh is the project's data fabric per [ADR 0500](0500-zenoh-data-fabric.md) and the universal peer-interaction substrate per [ADR 0210](0210-zenoh-universal-substrate.md). We need a v1 production deployment shape: where routers live, who's allowed to talk to whom, what crosses site boundaries, how identities are bound.

The decision composes with substantial existing canon. ADR 0216 splits the system into a small federation plane and a large site plane. ADR 0313 establishes wg-cp0 as the silent control plane between mgmt-hosts (mgmt-API, signal-server federation, Spock multi-master replication, peer-key attestation). ADR 0407 owns service-TLS certs via the offline-CA-rooted internal X.509 CA. ADR 0512 + the data-dictionary discipline mandate schema declaration on every emission. `docs/integrations/data-standards-review.md` locks the project at **schema-neutral substrate, translate at boundaries** — vendor SDKs get severable per-vendor adapter repos; open standards consolidate in `goat-open-standards`.

What was not yet decided: production topology, ACL contract, reserved namespace for command and effector traffic, cross-site command authority, latency posture, partition behavior, how Zenoh's transport relates to NetBird vs wg-cp0.

A subordinate question surfaced during this design: are mgmt-hosts NetBird mesh peers? The earlier 2026-05-12 closure of F-099 framed them as "not mesh peers by intent." That closure assumed no service required mesh-IP reachability on a mgmt-host. This ADR creates the first such service — Zenoh routers live on mgmt-hosts and must be reachable from NetBird mesh peers — so the F-099 direction is reversed here.

## Decision

### Scope of the "Zenoh inside NetBird" pattern

Two clarifications about what this pattern is and is not:

**Same-LAN peers ride the same substrate.** Many peers live inside a site LAN — sensor, effector, operator workstation, all on the same physical switch. They still join the NetBird mesh and reach Zenoh routers through it; the same identity / encryption / ACL stack applies. The substrate isn't just for crossing site boundaries. wg-cp0 enters when a peer is reaching the control plane from outside its site (operator laptop from off-LAN, federation between sites, partner peering); the data plane (NetBird mesh + Zenoh on top) is the substrate whether peers are physically adjacent or world-apart.

**Zenoh is the primary interaction substrate, not the exclusive one.** The case for Zenoh-by-default is operational: efficient transport on top of WireGuard, one identity model, one ACL stack, one schema registry, one audit trail, one consistent way to express formats / packets / interaction patterns across the fleet. Peers can still interact through other mechanisms (direct TCP / HTTP / gRPC / file-share / vendor-native protocols over the same NetBird mesh) when a specific case justifies it. Zenoh is the preferred pattern, not a hard requirement on every peer interaction.

### Topology — one logical Zenoh mesh, riding on NetBird

All production Zenoh routers + pilot routers join the same Zenoh session. Scale is not the load-bearing concern at small router counts (Zenoh is documented for thousands of peers).

**Routers live on mgmt-hosts.** Each router binds **one interface: the mgmt-host's NetBird mesh IP (100.x.x.x).** All Zenoh traffic rides NetBird:

- **Inter-site router-to-router federation** rides the NetBird mesh between mgmt-host peers.
- **Publisher and subscriber traffic** from sensors, effectors, operators, agents — any NetBird mesh peer — reaches the local site's router over the NetBird mesh.

**wg-cp0 stays for the silent control plane only.** Mgmt-API, signal-server federation, Spock multi-master replication, and peer-key attestation continue to ride wg-cp0 per ADR 0313. Zenoh does NOT ride wg-cp0. The silent-control-plane invariant is preserved.

**Mgmt-hosts join the NetBird mesh as peers.** This reverses the earlier same-day F-099 closure. The netbird daemon installs on all mgmt-hosts and they enroll via the same offline-CA-rooted bundle ceremony used for any other peer. Two planes preserved: control plane on wg-cp0 (mgmt-API, signal, Spock), data plane on NetBird mesh (Zenoh, peer-to-peer application traffic).

### Reserved top-level prefixes — three additions to existing canon

The design doc reserves nine top-level prefixes (`data/`, `services/`, `health/`, `config/`, `cmd/`, `attest/`, `ops/`, `agents/`, `release/`). This ADR adds three more:

- **`task/`** — mid-level effector tasking. Distinct from low-level commands; matches the tasking-vs-command split in TAK CoT and the SAPIENT ASM message family.
- **`ack/`** — command and task replies, keyed by the originating cmd-id.
- **`telemetry/<format>/`** — wire-faithful surfaces for non-canonical wire formats (`telemetry/cot/...`, `telemetry/sapient/...`, `telemetry/<vendor-format>/...`, etc.). Format-shaped, not source-shaped: any peer authorized to publish wire-faithful data in that format publishes here, regardless of where its code lives.

Total: 12 reserved top-level prefixes.

This ADR also reframes the existing `ops/` prefix from "operational events from in-tree services" to **"operational events emitted by any service running on the fabric"** — role-shaped, not source-tree-shaped. The Zenoh data plane is agnostic to where a publisher's code lives.

### Data-class structure under `data/`

Shape: `data/<class>/<site>/<id>`. Site segment is the **target** site (where the entity / sensor product / effector / alert resides); the issuing peer's identity comes from its cert.

| Class | Shape | Federation behavior |
|---|---|---|
| Bulk sensor product | `data/sensor/raw/<site>/<id>` | **Site-local only** (bandwidth) |
| Sensor detection | `data/sensor/detection/<site>/<id>` | Federated read |
| Persistent track | `data/entity/track/<site>/<id>` | Federated read |
| Asset (long-lived owned thing) | `data/entity/asset/<site>/<id>` | Federated read |
| Snapshot observation | `data/entity/observation/<site>/<id>` | Federated read |
| Point-in-time event | `data/event/<site>/<id>` | Federated read |
| Alert | `data/alert/<site>/<id>` | Federated read |

Subtree structure below `<class>/<site>/<id>` is the publisher's choice; not centrally governed.

### Cross-site behavior

- **Federated read** — events, detections, entities, alerts, status, services, health, ops cross site boundaries readable by any authorized subscriber.
- **Federated write, site-scoped** — `task/<site>/` and `cmd/effector/<site>/` are publishable only by peers whose cert identifies them as a command echelon AT THAT SITE. **No cross-site command authority in v1** — a command echelon at site A cannot publish `cmd/effector/B/...`. Cross-site command flow (e.g., a rear echelon as broker, or explicit per-relationship grants) is a future enhancement gated on operational demand.
- **Site-local only** — `data/sensor/raw/` does not cross. Forensics and replay use a separate storage layer (MinIO + Block 37 replay substrate), not Zenoh.

### Authority model — only command echelons publish `task/` and `cmd/`

Per `mesh-acl.md`, a small set of echelons carry command authority. Zenoh ACL allows publish on `task/...` and `cmd/effector/...` only for peers whose cert identifies them as one of those echelons. Sensors are deny-by-default on those prefixes.

Intra-command failover (a primary command echelon offline triggers a pre-authorized fallback) is configured per-effector at deploy time via group_vars, not in this ADR.

Effector self-judgment — whether an effector also acts on its own local conditions absent a command — is the effector's design problem, not a Zenoh ACL concern. Out of scope.

### ACL contract — default-deny + per-prefix allowlist, identity via offline-CA cert

Default-deny baseline. Each reserved prefix has an allowlist mapping cert-identity patterns to allowed actions on key-expression patterns. The audit-logger plugin subscribes to `**` and captures every publish for the audit trail per ADR 0508.

**Identity binding.** Each peer holds an X.509 client cert minted by the offline-CA-rooted internal CA (ADR 0407). The cert encodes **echelon**, **site**, **role** in a URI-SAN of the form `urn:goat:peer:site=<site>,role=<role>,echelon=<echelon>`. The cert-signing CLI (`cert-sign --peer ...`) takes those fields explicitly; the Ansible role rendering Zenoh's `access_control` config consumes them from group_vars. Detailed SAN grammar + rotation procedure lives in the design-doc extension.

**Two-layer ACL.** NetBird ACL groups (per `mesh-acl.md` v1 broad-read / narrow-write posture) gate IP-layer reachability — can this peer's packets even arrive at a router. Zenoh ACL then gates topic-level read/write. The two compose: NetBird is necessary but not sufficient; Zenoh is the fine-grained enforcement.

### Latency, partition, ack-timeout — defaults with per-effector override

- **Latency budget:** bounded best-effort, sub-second target. Publish + ack, no bus-side retry, caller-side timeout. Per-effector schema declaration may override (`cmd_budget_ms`) for hard-real-time effectors or relax for slow-path operations.
- **Partition behavior:** commands queued during a partition **expire** by default — they do not auto-replay on reconnect. The commanding peer reissues if the command is still desired after partition heals. Per-effector schema may override (`partition_behavior: queue-with-ttl`) when domain demands it.
- **Ack-timeout:** observational only. Logged to audit-logger + wide-event telemetry. Does not auto-alarm, auto-retry, or auto-failover. The commanding peer decides what timeout-exceeded means in its own loop. Per-effector schema may override.

### Schema discipline — inherit Block 23 / ADR 0512

Every publisher declares its schema before publishing per the data-dictionary discipline (`docs/design/data-dictionary.md`). Two states: `registered` (JSON Schema with project extensions for units, ranges, freshness, entity references) or `binary-declared` (vendor format with out-of-band reference + remediation plan).

Schemas live on the fabric at `attest/schema/<topic-pattern>` and `attest/schema-declaration/<topic-pattern>` — the fabric is the registry; no separate service. V1 ships the schema-enforcer interceptor in **audit mode** (counters + stamps, no rejections) per ADR 0512's rollout curve. Phase 3 entry promotes to **enforce mode** after first-site operational settle.

### Schema neutrality — translate at boundaries

Internal canonical schemas under `data/...` are informed by but not identical to any single vendor or open standard. Per `docs/integrations/data-standards-review.md`, the substrate is schema-neutral; translation lives in adapter components (in `goat-open-standards` for open standards, in per-vendor severable repos for vendor formats). Wire-faithful surfaces ride `telemetry/<format>/`; canonical-internal lives under `data/`. Equivalence tables in the data dictionary express the field-level mapping.

## Alternatives considered

- **Zenoh-on-wg-cp0-only.** Make every publisher and subscriber a wg-cp0 endpoint. Rejected — expands wg-cp0 from "small fixed silent control plane" to hundreds of peers, compromising the ADR 0313 invariant. wg-cp0 was deliberately the small surface; widening it to the full publisher/subscriber set is the wrong direction.
- **Site-LAN bind + NetBird exit-node forwarding.** Router binds wg-cp0 (federation) + site LAN IP (local clients). Off-site mesh peers reach via a NetBird exit-node-like forwarding mechanism. Rejected — operational complexity (exit-node setup, routing config, forwarding gateway on each mgmt-host) vs the simpler "mgmt-host is a mesh peer" answer.
- **Site-isolated Zenoh networks with explicit bridges.** Rejected — the use case is cross-site sensor → command → actuator flow; building bridges between site-isolated networks is more substrate for no benefit at today's router count.
- **Tree-per-region (regional hub broker pattern).** Rejected — premature at today's router count; Zenoh natively supports regional brokers as a config change later if scale demands.
- **Adopt TAK CoT (or SAPIENT, or a vendor format) as canonical internal schema.** Rejected — couples evolution cadence + roadmap risk to a single vendor or specification per data-standards-review recommendation #3. Translate at boundaries.
- **Defer ACL to v2.** Rejected — Zenoh's fan-out semantics mean any compromised peer with mesh reachability could subscribe to every site's data. ACL is the load-bearing constraint at v1, not optional polish.
- **Cross-site command authority in v1.** Rejected — adds ACL site-scoping complexity without an immediate operational demand. Deferred with explicit trigger conditions.

## Consequences

**Positive.** Cross-site sensor → command → actuator flow on one substrate. One transport for Zenoh (NetBird mesh), one identity model (offline-CA cert), one ACL gating model (default-deny + allowlist), one audit trail (audit-logger interceptor), one schema registry (data dictionary on `attest/`). Compromise blast radius bounded by ACL + cert identity. Schema neutrality preserves partner / vendor portability. F-099 closure direction reversal is structural — the mgmt-host gains its mesh-peer identity, which is also useful beyond Zenoh (operator paths gain a mesh-IP alternative; observability surfaces gain a uniform peer-status view).

**Negative.** Mgmt-hosts gain one more piece of substrate (the netbird daemon). Daemon install on every mgmt-host becomes a precondition for first-site Zenoh deploy. NetBird relay throughput now matters for cross-site Zenoh federation throughput — relevant at the upper-band of ADR 0215's scaling envelope, not at today's fleet size. Cert-issuance ceremony grows a new URI-SAN field; the cert-sign CLI gains site/role/echelon flags. Schema discipline is by policy at v1 (audit-mode enforcement only); enforce-mode lands in Phase 3.

**Residual risk.** Cert identity binding is operator-issued — a cert minted with the wrong site grants the wrong scope. Mitigated by cert-issuance review + audit-logger trail. Promotion to enforce-mode in Phase 3 is gated on operational settle of the first-site deploy; until then, undeclared publishes are logged-and-allowed, not rejected. Cross-site command authority is denied in v1; if an operationally-driven need arises before the trigger conditions are met, this ADR is the place to revisit.

## Assessor view (SCA-R)

Control mapping:

- **AC-3 (access enforcement)** — default-deny Zenoh ACL with per-prefix allowlist; cert-identity-bound via URI-SAN.
- **AC-4 (information flow enforcement)** — federation plane carries event + cmd + ack + alert flows; site plane carries bulk raw; cross-site bulk denied in v1.
- **AU-2 (audit events)** — audit-logger interceptor captures every publish at the substrate layer; ties to Block 17D tamper-evident audit.
- **IA-3 (device identification + authentication)** — X.509 client cert from offline-CA-rooted internal CA (ADR 0407); URI-SAN encodes site + role + echelon.
- **IA-5 (authenticator management)** — cert rotation per ADR 0407's existing ceremony; CRL pipeline carries revocation.
- **SC-7 (boundary protection)** — control plane bounded by wg-cp0 (ADR 0313); data-plane access bounded by NetBird ACL + Zenoh ACL composition.
- **SC-8 (transmission confidentiality + integrity)** — NetBird WireGuard for peer-to-peer; TLS 1.3 at the Zenoh router itself.

Residual risk acceptance: ACL is in audit-mode at v1; Phase 3 enforce-mode promotion is the trigger for hard rejection of undeclared publishes. Documented per ADR 0855 (accepted risks register) at first-site deploy.

## Trigger conditions for revisit

- **Cross-site command authority is operationally requested** (named effector + named operator workflow). Promote to ACL extension; specify whether broker-via-rear-echelon or direct-per-relationship.
- **Router count crosses 50 or partner federation joins the same logical mesh.** Revisit topology — tree-per-region or partner-isolated-mesh-with-bridge become non-trivial options.
- **Schema-enforcer audit-mode counters indicate substantial undeclared traffic post-Phase-3-entry.** Tighten the enforce-mode rollout curve or extend the audit window.
- **An effector class is identified for which "expire on partition" is structurally wrong.** Confirm per-effector schema override handles it; if not, revisit default.
- **A canonical alignment to a specific standard is operationally compelled** (program-of-record accreditation requires CoT or SAPIENT as the substrate schema). Revisit recommendation #3 in `data-standards-review.md`.
- **NetBird relay throughput becomes the cross-site Zenoh federation bottleneck.** Lever options: regional broker hierarchy, dedicated Zenoh transport on top of WireGuard, or a separate inter-site Zenoh path.

---

**See also:** `docs/adr/GLOSSARY.md` for acronyms (CoT, SAPIENT, ASTERIX, NetBird, wg-cp0, SAN, ACL).
