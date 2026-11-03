# Publisher Trust Model

**Scope:** the identity + attestation taxonomy for publishers in the goat system. Covers three roles (`publisher`, `unattested-publisher`, `subscriber`) and the dimensions of attestation (NTP, cert chain, enrollment, source provenance, supply-chain). Provides the vocabulary used across mgmt-plane and data-fabric enforcement.

**Status:** mainline. Role assignments are Gate 2; wire-format extensions for multi-dimensional attestation are Phase 3+.

**Relationship to other docs:** `universal-ntp-hygiene.md` is the NTP dimension specifically; this doc is the broader umbrella. `mgmt-server-security.md` §3.2 enforces role assignment. `zenoh-ntp-integration.md` implements the per-sample attachment. `zenoh-cds-data-conditioning.md` handles DMZ-edge transitions.

---

## Table of contents

1. [Three roles](#1-three-roles)
2. [Attestation dimensions](#2-attestation-dimensions)
3. [Unattested exemption reasons](#3-unattested-exemption-reasons)
4. [DMZ / external ingestion pattern](#4-dmz--external-ingestion-pattern)
5. [Wire format — current and proposed extension](#5-wire-format--current-and-proposed-extension)
6. [Enrollment flow per role](#6-enrollment-flow-per-role)
7. [Netbird group + policy mapping](#7-netbird-group--policy-mapping)
8. [Role transitions](#8-role-transitions)
9. [References](#9-references)

---

## 1. Three roles

| Role | Can publish? | Attestation required | Data tagged at publish | Data-plane policy |
|---|---|---|---|---|
| `publisher` | Yes | All dimensions (§2): NTP + cert chain + enrollment + mesh-native | Tagged with attested flags | Full: data-plane access + may reach mgmt-plane endpoints designated for publishers |
| `unattested-publisher` | Yes | Partial — at least ONE dimension missing (see §3 for specific exemption classes) | Tagged `trust_class=unattested` + `unattested_reason` | Restricted: may publish to designated topics only; denied reach to mgmt-plane admin endpoints |
| `subscriber` | No | N/A — no publish path | N/A | Receive only. Denied any publish or write |

The `unattested-publisher` role is **operator-shorthand "wild publisher"** in field speech; `unattested-publisher` is the formal name used in docs, code, and Netbird group definitions.

---

## 2. Attestation dimensions

A fully-attested `publisher` satisfies ALL five dimensions. An `unattested-publisher` is exempted from one or more; the specific exemption reason(s) are what downstream consumers need to understand.

| # | Dimension | What it asserts | Governing doc |
|---|---|---|---|
| 1 | **NTP attestation** | Producer's clock is correct; commit timestamps / sample timestamps are trustworthy | [`universal-ntp-hygiene.md`](universal-ntp-hygiene.md) |
| 2 | **Cert chain attestation** | Producer's TLS / identity cert chains to our CA; producer is not an impersonator | `mgmt-server-security.md`, [`offline-enrollment.md`](offline-enrollment.md) |
| 3 | **Enrollment attestation** | Producer was admitted via a gated setup-key flow with attestation; not a back-door enrollment | `mgmt-server-security.md` |
| 4 | **Source provenance** | Producer is mesh-native (enrolled in our mesh, runs our stack) vs. bridged from an external network (DMZ, partner) | This doc §4 |
| 5 | **Supply-chain attestation** | Producer's running binaries match our signed artifacts (Sigstore / SBOM / reproducible build) | `os-supply-chain-security.md` |

---

## 3. Unattested exemption reasons

Enumerated; extensible as new ingestion paths are added:

| Reason code | Meaning | Typical use case |
|---|---|---|
| `no_ntp_attestation` | Device cannot or does not meet NTP hygiene bar | Low-end sensor, constrained hardware, device without GNSS or reliable NTP |
| `external_dmz_source` | Data bridged from an external Zenoh or DMZ; not mesh-native | External vendor data feeds, unattributed public data streams |
| `partner_ingestion` | Data from a partner mesh / partner network with its own trust model | Partner C2 feeds, allied-system data, cross-agency data shares |
| `pending_vetting` | New device during onboarding window; attestation paperwork in progress | Newly-shipped Orins before their first-boot attestation completes |
| `legacy_source` | Imported historical data without current attestation | Data migration, archived feeds |

Reasons **combine** — a `external_dmz_source` sample also has `no_ntp_attestation` relative to our mesh (we don't trust the external producer's clock as ours). Tagging preserves the full set.

---

## 4. DMZ / external ingestion pattern

Problem: we have data sources we want to consume but don't want to give a direct path into the network. Examples include external vendor feeds, public / OSINT data streams, partner-network gateways where the partner's trust domain doesn't match ours.

Pattern:

1. **External producers** publish on an external Zenoh router that we do not control and is not in our mesh.
2. **A DMZ Zenoh router** sits at the boundary. It subscribes to the external feeds and is simultaneously reachable from our mesh (typically bridge VPSes per `docs/operations/bridge-peering.md`).
3. **Samples crossing the DMZ boundary** are re-stamped at ingress by a DMZ interceptor with:
   - `trust_class = unattested`
   - `unattested_reason = external_dmz_source` (plus any other applicable reasons)
   - `ntp_class = unknown` (we make no assertion about the external producer's clock)
4. **DMZ boundary interceptors** enforce:
   - Schema validation (drop or DLQ on fail — see [`zenoh-dlq.md`](zenoh-dlq.md))
   - Key-expression allowlist (topic ACL)
   - Rate-limit per source
   - Optionally: re-encode to internal schemas
5. **Consumers within our mesh** see the `unattested + external_dmz_source` tag and decide per-consumer whether to use, derate confidence, or ignore.

The DMZ interceptor chain is a concrete application of the `zenoh-cds-data-conditioning.md` interceptor pattern; the CDS doc is where the mechanism is specified.

### Why this is asymmetric

External publishers cannot influence our mesh state beyond what our DMZ interceptors allow. They:

- **Cannot** reach mgmt-plane endpoints (Netbird policy denies).
- **Cannot** become mesh peers unless explicitly re-enrolled with attestation.
- **Cannot** claim `trust_class=attested` — the DMZ interceptor overwrites trust flags at ingress regardless of what the source claims.

This is defense against adversarial external producers who might try to forge high-trust metadata.

---

## 5. Wire format — current and proposed extension

### Current (decision 2, 2026-04-20)

Zenoh sample attachment is 5 bytes:

```
ntp_class:       1 byte  enum { healthy=0, warning=1, degraded=2, unknown=3 }
producer_epoch:  4 bytes monotonically-incrementing counter from producer's daemon
```

Sufficient for the NTP dimension alone.

### Proposed extension (Phase 3+, when first multi-dimensional use case ships)

Add 1-byte `trust_flags` bitfield:

```
trust_flags:     1 byte
  bit 0: mesh_native          (peer enrolled in our mesh with full attestation)
  bit 1: cert_trusted          (cert chain validated against our CA at publish time)
  bit 2: ntp_attested          (NTP verdict was healthy OR warning at publish; ntp_class != unknown)
  bit 3: enrollment_attested   (enrolled via a gated setup-key flow)
  bit 4: external_source       (bridged from external; mutually exclusive with mesh_native)
  bits 5-7: reserved
ntp_class:       1 byte        (unchanged)
producer_epoch:  4 bytes       (unchanged)
```

Total wire format: 6 bytes = `trust_flags` (1) + `ntp_class` (1) + `producer_epoch` (4).

**Do not extend the wire format during Phase 2.** Ship the 5-byte format with just the NTP dimension. Add `trust_flags` when the first non-NTP exemption case (DMZ ingestion, partner feed) is actually being implemented — Phase 3+. The extension is additive; existing 5-byte consumers see a default `trust_flags = mesh_native | cert_trusted | ntp_attested | enrollment_attested` for samples that predate the extension.

### Subscriber resolution pattern

Subscribers evaluate the combination:

- `trust_flags & mesh_native` AND `ntp_class = healthy` → full-trust publication
- `trust_flags & external_source` → treat as DMZ source; consumer policy (pass/flag/drop) applies
- `!(trust_flags & ntp_attested)` → loose-time publication, consumer policy applies
- `trust_flags = 0` → unattested across all dimensions; strictest consumer policy

Detail lookup via the cache per [`zenoh-ntp-integration.md`](zenoh-ntp-integration.md) §3 still works; the cache holds the producer's attestation document which includes the full trust context.

---

## 6. Enrollment flow per role

| Role | Setup-key type | Enrollment pre-hook | Assigned Netbird group | Default topic access |
|---|---|---|---|---|
| `publisher` | `role=publisher` | Validates NTP attestation + cert chain + server-time RTT probe | `role.publisher` | Full data-plane + designated mgmt endpoints |
| `unattested-publisher` | `role=unattested-publisher` + required `unattested_reason` field | Skips NTP validation; records the exemption reason; validates cert chain IF provided | `role.unattested-publisher` | Designated "unattested-allowed" topics only |
| `subscriber` | `role=subscriber` | Neither NTP check nor cert validation required | `role.subscriber` | Receive-only |

DMZ-ingested data does NOT go through Netbird enrollment. The DMZ router is itself a `publisher` (fully attested) that RELAYS samples tagged as `unattested + external_dmz_source`. This preserves the separation: the DMZ router's identity is attested; the data it relays is not.

---

## 7. Netbird group + policy mapping

Three baseline groups at the mgmt-plane:

- `role.publisher` — ACL policy permits reach to designated data-plane services and mgmt-plane endpoints where applicable.
- `role.unattested-publisher` — ACL policy permits reach to unattested-allowed data-plane services; denies mgmt-plane admin endpoints; publishes may be subject to per-topic filters.
- `role.subscriber` — ACL policy denies all publish paths; allows receive on designated data-plane services.

Topic-level enforcement at the Zenoh router (for publishes):

- Strict topics — only `role.publisher` can publish.
- Mission-data topics — `role.publisher` AND `role.unattested-publisher` can publish; subscribers receive with `trust_class` visible.
- Unattested-only topics — designated for external/DMZ data; `role.unattested-publisher` can publish here exclusively.

---

## 8. Role transitions

Transitions are explicit operator actions, not automatic role escalations.

| From | To | Trigger | Mechanism |
|---|---|---|---|
| `publisher` | `unattested-publisher` | Sustained NTP degradation (per `universal-ntp-hygiene.md` role-demote job) | Mgmt API call: move peer between groups. Reversible if NTP recovers per operator decision. |
| `unattested-publisher` | `subscriber` | Misbehavior — publishing outside scope, rate-limit abuse, policy violation | Operator-initiated. Recorded with reason. |
| `subscriber` | `publisher` or `unattested-publisher` | Requires **re-enrollment** under a new setup key of the target role. | Cannot be done via group change alone — security boundary. |
| `unattested-publisher` | `publisher` | Producer gains capability to meet the missing attestation dimension (e.g., NTP hygiene restored AND cert chain validated). | Requires **re-enrollment** under a new setup key of the target role. |

The asymmetry: demotion is cheap (operator edits a group); promotion requires re-enrollment (attestation must be re-validated from scratch).

---

## 9. References

- [`docs/design/universal-ntp-hygiene.md`](universal-ntp-hygiene.md) — NTP attestation dimension (one of five)
- `docs/design/mgmt-server-security.md` — admit-class enforcement + role governance
- [`docs/design/zenoh-ntp-integration.md`](zenoh-ntp-integration.md) — per-sample wire format
- `docs/design/zenoh-cds-data-conditioning.md` — DMZ boundary interceptor pattern
- [`docs/design/zenoh-dlq.md`](zenoh-dlq.md) — dead-letter queue for validation failures
- `docs/design/federation-security.md` — nation-state threat model context
- `docs/design/os-supply-chain-security.md` — supply-chain attestation dimension (5)
- [`docs/design/offline-enrollment.md`](offline-enrollment.md) — enrollment attestation (dimension 3)
