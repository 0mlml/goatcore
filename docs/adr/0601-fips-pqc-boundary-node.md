# ADR 0601 — FIPS 140-3 boundary node — x86 strongSwan + wolfSSL/AWS-LC

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-04-25 |
| Repo scope | goat core |
| Related | [`0103-crypto-boundary-discipline.md`](0103-crypto-boundary-discipline.md), [`0600-two-pq-paths.md`](0600-two-pq-paths.md), [`0603-ml-kem-at-boundary.md`](0603-ml-kem-at-boundary.md), `../design/fips-pqc-path.md`, `../project/boundary-node-procurement.md` |

## TL;DR (for a layperson)

Where our network has to talk to a partner network that requires US-government-validated cryptography,
that traffic flows through a single dedicated x86 server we call
the boundary node. The boundary node uses strongSwan with a
FIPS 140-3 validated cryptographic library and post-quantum
key exchange (ML-KEM). Everything else in our system — the
mesh interior — uses faster non-validated crypto, because
traffic inside the boundary doesn't need the validated stack.

## Context

The crypto boundary discipline ([`0103`](0103-crypto-boundary-discipline.md))
concentrates FIPS validation at named interfaces. The
"cross-boundary network" is the highest-priority interface:
traffic crossing into or out of the authorization boundary
must use FIPS-validated cryptography.

x86 is required because the FIPS-validated libraries we depend
on (wolfSSL, AWS-LC) ship validated builds on x86 first and
on arm64 with a lag. The boundary node is a separate workload
class from the rest of the system.

## Decision

The boundary node is a dedicated x86 host running:

- **strongSwan** as the IPsec/IKEv2 layer.
- **wolfSSL** as the primary FIPS 140-3 validated cryptographic
  module — `fips-pqc-path.md` lines 208–216 cite wolfSSL as
  having "broadest CMVP coverage." **AWS-LC** is the documented
  alternate (Apache-licensed; lower CMVP coverage today; higher
  integration effort). Selection pinned at procurement time but
  defaults to wolfSSL absent a blocker. The original draft of
  this ADR presented the two as co-equal — the design doc has a
  preference.
- **ML-KEM (FIPS 203)** as the post-quantum key encapsulation
  mechanism ([`0603`](0603-ml-kem-at-boundary.md)).
- **Hybrid TLS** at the partner interface: classical (ECDHE) +
  PQ (ML-KEM) per NSA hybrid guidance.
- **One boundary node per site** that needs cross-boundary
  federation; not every site.

Hardware procurement is tracked in
`../project/boundary-node-procurement.md`.
Architecture is in
`../design/fips-pqc-path.md`.

The boundary node is *not* the same hardware as the management
host. Workload separation is deliberate: management plane
services run on the mgmt-host; boundary cryptography runs on
the boundary node.

## Alternatives considered

- **FIPS validation on the management host.** Couples
  management plane and boundary cryptography on the same
  hardware; complicates STIG hardening. Rejected.
- **No FIPS boundary; rely on intra-mesh crypto for
  cross-boundary.** Disqualifies federation with FIPS-required
  partners. Rejected.
- **Vendor-bundled boundary appliance.** Adds prime-capture
  risk per [`0104`](0104-no-prime-capture.md). Rejected.

## Consequences

**Positive.** Cross-boundary traffic uses FIPS-validated
cryptography. Mesh interior stays fast and agile. Workload
isolation is clear.

**Negative.** Additional hardware per site that needs cross-
boundary federation. Procurement and STIG hardening cost.
Acceptable; the value is the only path to FIPS-required
partner federation.

## Assessor view (SCA-R)

The boundary node serves SC-7 (boundary protection), SC-8
(transmission confidentiality and integrity — FIPS-validated),
SC-12 (key establishment — ML-KEM hybrid), SC-13
(cryptographic protection — FIPS 140-3), and CA-3 (system
interconnections). The wolfSSL / AWS-LC FIPS 140-3
validation certificate is the load-bearing artifact;
specific validated-module selection happens at procurement
time and is documented in the SSP.

CSfC Capability Package compliance for specific cross-
boundary tunnels can be obtained at the boundary node
without affecting the mesh interior.

## Trigger conditions for revisit

- A required FIPS-validated module is sunset by its vendor.
  Switch to an alternative (wolfSSL ↔ AWS-LC); re-validate.
- ML-KEM hybrid configuration is updated by NSA / NIST.
  Update parameter set; do not change the architecture.
- arm64 FIPS-validated builds become routinely available.
  Allow boundary node on arm64 as alternative; do not
  abandon x86 default.

---

*See `GLOSSARY.md` for terms and acronyms.*
