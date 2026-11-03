# ADR 0856 — Partner-peering capability is an offline-CA-rooted X.509 cert with `urn:goat:partner-peering:...` URI-SAN, not a net-new capability token

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-05-15 |
| Repo scope | goat core (new `ops/goat-partner-peering-portal/`; extends `ops/enrollment/cmd/cert-sign/` URI-SAN grammar) |
| Supersedes | — |
| Superseded by | — |
| Related | [`0821-mission-partner-sharing-pattern.md`](0821-mission-partner-sharing-pattern.md), [`0844-zenoh-deployment-topology-and-acl-v1.md`](0844-zenoh-deployment-topology-and-acl-v1.md), [`0407-internal-x509-ca-for-service-tls.md`](0407-internal-x509-ca-for-service-tls.md), `0406-offline-ca-yubikey-piv.md`, [`0805-policy-engine-active-passive.md`](0805-policy-engine-active-passive.md), [`0822-wide-event-observability-otlp-openobserve.md`](0822-wide-event-observability-otlp-openobserve.md), [`0811-audit-log-integrity.md`](0811-audit-log-integrity.md), `../design/partner-peering-portal.md`, [`../design/mission-partner-sharing.md`](../design/mission-partner-sharing.md) |

## TL;DR (for a layperson)

When we let a partner organization's data fabric talk to ours, we have to hand them a credential that proves "this peering was approved." This ADR picks the **shape** of that credential: a regular signed certificate from our existing offline certificate authority — the same kind we already issue to every peer in the mesh — with one extra field that encodes which partner it's for, what data scopes it covers, and when it expires. We rejected the alternative of inventing a new credential format (lightweight tokens like macaroons or JWTs) because we already have a mature, audited, hardware-rooted credential ceremony for every other peer in the system; reusing it gets us revocation, audit, and key custody for free; the partner-side software (Zenoh routers) already speaks this format natively. The cost is a few minutes of operator time per approval (because the credential ceremony requires a YubiKey touch), which is fine — partner peering is rare and planned, not hot-path.

## Context

Block 84 (partner peering portal — `../design/partner-peering-portal.md`) needs to issue a *capability* per approved peering: a credential that the partner installs on their Zenoh router so our routers can recognize and authorize the peering. The credential must:

1. **Bind to a specific partner identity** — not transferable; not reusable across partners.
2. **Encode the scope** — which `release/<partner>/...` key-expression prefixes this peering covers.
3. **Be time-bounded** — explicit TTL after which the peering is no longer accepted.
4. **Be revocable** — operator-fired revocation before TTL expiry.
5. **Generate audit trail** — issuance, use, and revocation are all auditable.
6. **Speak a protocol our Zenoh routers natively accept** — Zenoh mTLS already validates X.509 client certs per [ADR 0844](0844-zenoh-deployment-topology-and-acl-v1.md) §"Identity binding," which uses `urn:goat:peer:site=<site>,role=<role>,echelon=<echelon>` URI-SAN.

The Zenoh ACL contract in ADR 0844 keys access decisions on the URI-SAN of the connecting peer's cert. Partner peering is structurally just another caller of that same ACL contract, with a different URI-SAN namespace and different allowlist prefixes.

The decision point: do we mint these capabilities through the existing offline-CA ceremony (one more cert, with a new URI-SAN sub-namespace) or do we invent a parallel capability-token substrate (macaroons / per-peering JWTs / capability tokens signed by a portal-held key)?

This ADR resolves that. The portal's other mechanism choices (approval workflow, ACL-emit path, audit trail target) are in the design doc.

## Decision

### D1 — Capability is an offline-CA-rooted X.509 cert

Each approved peering mints **one X.509 client cert per direction** signed by the offline CA via the same ceremony as any other peer cert ([ADR 0407](0407-internal-x509-ca-for-service-tls.md), ADR 0406). For a one-directional outbound peering (partner subscribes from `release/goat/...`), one cert is minted: an identity for the partner's Zenoh router to present to ours. For a bilateral peering (reciprocity per [`../design/mission-partner-sharing.md`](../design/mission-partner-sharing.md) §"Reciprocity"), two certs are minted under one request-id and one charter reference: an identity for the partner's router and an identity for ours.

The certs are delivered to the partner via the same one-time tarball-download mechanism the nova vendor portal already uses for vendor cert delivery (`/api/vendor-bundle/{vendor}/{token}.tar.gz` shape at [`../../ops/goat-zenoh-portal/`](../../ops/goat-zenoh-portal/)). The partner installs the cert into their Zenoh router's mTLS-client config.

### D2 — New URI-SAN sub-namespace: `urn:goat:partner-peering:...`

ADR 0844 reserved `urn:goat:peer:site=...,role=...,echelon=...` for mesh peers. Partner peering capabilities are a structurally distinct identity class — they are not a mesh-peer role, they are a fabric-to-fabric peering grant. This ADR reserves a sibling URI-SAN namespace:

```
urn:goat:partner-peering:partner=<slug>,scope=<keyexpr>,ttl=<rfc3339>,nonce=<8-hex>
```

Where:

- `partner=<slug>` — partner identifier (matches `partner.slug` in portal's Postgres `partner` table; e.g. `exemplar-corps`)
- `scope=<keyexpr>` — Zenoh key-expression prefix this cert authorizes (e.g. `release/exemplar-corps/data/event/*`); for bilateral, the *direction* is implicit in the prefix
- `ttl=<rfc3339>` — expiry timestamp, RFC 3339 UTC (`2026-08-15T00:00:00Z`)
- `nonce=<8-hex>` — random nonce to disambiguate re-issuance after revoke (avoids replay-by-name confusion when a re-mint reuses the same partner + scope + ttl shape)

The Zenoh router's ACL config reads URI-SAN matching against `urn:goat:partner-peering:partner=<slug>,*` patterns; the `scope=` field is informational for audit (the actual policy gate is the ACL config rendered by the portal — see `../design/partner-peering-portal.md` §Q4).

**TTL semantics:** the X.509 `Not After` field carries the same expiry. The TTL field in the URI-SAN is duplicative-but-explicit so audit trails don't have to re-parse the cert envelope. Cert and URI-SAN TTL **must match** at mint time; mismatch is a mint-side validation error.

### D3 — Mint requires YubiKey-PIV ceremony (no portal-private fast-path)

Partner-peering certs go through the **same** YubiKey-PIV custody ceremony as any other offline-CA-issued cert per ADR 0406. The portal **shells out** to `cert-sign --uri-san urn:goat:partner-peering:...` exactly the way the nova vendor portal does for vendor certs, and the ceremony requires operator presence (YubiKey touch) just like every other cert.

No portal-private intermediate CA. No "fast mint" path that bypasses the offline CA. This is the load-bearing security claim of this ADR — partner peerings have the same custody floor as any other mesh identity.

The latency cost (minutes-to-hours from approval to operator-availability-for-touch) is acceptable for partner peering because:

- Partner peering is **low cardinality** (single-digit partners over the foreseeable horizon per `partner-standalone-goatnet.md` "corps-level operational unit" persona).
- Partner peering is **planned, not hot-path** — the partner waits days for charter signature; minutes-to-hours for cert mint is in the noise.
- The portal's workflow (`approved` → `mint-pending` → `active`) makes the mint latency operationally visible — operators can batch touches; partners know the request is post-approval and pre-mint.

### D4 — Revocation via existing CRL pipeline

Cert revocation uses the same CRL pipeline as any other offline-CA-issued cert ([ADR 0407](0407-internal-x509-ca-for-service-tls.md) §"CRL Distribution"). Portal-fired revocation: operator triggers `revoke` on a `peering_active` row → portal calls the existing `cert-sign --revoke <serial>` CLI → CRL re-publishes → Zenoh routers' OCSP/CRL check picks up the revocation on next handshake. The portal additionally removes the partner's allowlist entries from the rendered `access_control` config, so revocation is enforced at both the cert layer (CRL) and the ACL layer (allowlist absence).

The portal does **not** maintain a parallel revocation list. All revocation evidence lives in the CRL pipeline.

### D5 — Mint event lands in the existing audit-integrity bucket

Cert-mint events for partner peering certs piggyback on the existing offline-CA mint audit pipeline ([ADR 0811](0811-audit-log-integrity.md), `audit-integrity` MinIO bucket, Object-Lock COMPLIANCE 1y). No new audit substrate. The portal additionally writes a wide event via OTLP per [ADR 0822](0822-wide-event-observability-otlp-openobserve.md) for operational visibility — see `../design/partner-peering-portal.md` §Q5.

## Alternatives considered

### Capability tokens — macaroons signed by a portal-held key

A macaroon ([Birgisson et al. 2014](https://research.google/pubs/pub41892/)) per peering would carry caveats (partner, scope, TTL) in a tamper-evident token, signed by a key held by the portal. The partner presents the token to our Zenoh routers on every publish; our routers validate the signature + caveats.

**Rejected because:** (1) Zenoh routers do not natively validate macaroons; we'd need a Zenoh interceptor plugin to do it — net-new substrate. (2) The portal's signing key becomes a high-value target — single compromise mints arbitrary peerings — and lacks the YubiKey-PIV hardware-root floor. (3) Macaroon revocation is awkward (either short-TTL + re-issue, or a separate revocation list — we'd reinvent the CRL pipeline). (4) Audit trail would be parallel to the existing cert-mint audit — diverging trust evidence.

The macaroon shape is excellent for high-cardinality short-lived authorizations (cloud-service per-request capabilities); it is the wrong fit for low-cardinality, long-lived, partner-level federation grants.

### Per-peering JWTs signed by Hydra

Use the existing Hydra OIDC infrastructure ([ADR 0411](0411-ory-idp-mvp.md)) to issue per-peering JWTs as bearer tokens. Hydra has a JWKS endpoint; our Zenoh routers could validate signatures.

**Rejected because:** (1) Hydra is a user-OIDC IdP, not a peer-authentication substrate — repurposing it crosses an identity-domain boundary (per `../design/identity-architecture.md` four-domain split, machines authenticate via WireGuard / certs, not OIDC). (2) JWT bearer-token model is wrong for long-lived router-to-router peering — bearer tokens get stolen; cert + private-key + TLS handshake is the cert-bound stronger property. (3) Hydra's signing key rotation cadence is wrong for peering (rotation per signing-key TTL would break long-lived peerings unless we rebuild every peering on every Hydra rotation).

### Per-peering capability stored in Spock-replicated table only (no cert)

Authoritative capability lives in the `peering_capability_mint` Postgres table, replicated cluster-wide via Spock. Zenoh routers query the table (or a portal-side service) on every handshake.

**Rejected because:** (1) Adds a portal-or-database dependency to the data plane — partition between a site's Zenoh router and the portal would block partner peering enforcement. (2) Defeats the offline-CA-rooted identity contract from ADR 0844 — partner-peering becomes a different security model from every other peer, increasing cognitive surface. (3) Adds a hot-path query per Zenoh handshake — performance footgun.

### Net-new partner-peering CA, not the offline CA

Issue peering certs from a dedicated `partner-peering-ca` rooted under the offline CA. Operationally separate ceremony; same custody floor.

**Rejected because:** insufficient benefit. The offline CA already covers mesh-peer certs and service-TLS certs; adding a partner-peering tier is more ceremony surface area for no security gain. Partner peering is differentiated by **URI-SAN namespace**, not by **CA identity**, and that's the right axis for differentiation (audit logs filter by URI-SAN prefix; CA identity is the trust root for the whole substrate).

## Consequences

**Positive:**

- **Zero new credential substrate.** Cert issuance, distribution, validation, revocation, and audit all ride existing rails. The portal is workflow + UX on top of substrate that already passes the audit-and-CSfC bar.
- **Hardware-rooted custody floor.** Every mint requires YubiKey-PIV touch — the same floor as every other peer identity in the system.
- **Native Zenoh acceptance.** Routers validate via standard mTLS; URI-SAN read via standard X.509 extension parsing already implemented in the audit-logger plugin.
- **CRL revocation for free.** Compromise response is identical to any other compromised peer cert.
- **Audit trail unification.** Partner-peering mint events land in the same `audit-integrity` bucket as all other mints; SIEM / wide-event queries / compliance evidence are uniform.

**Negative:**

- **Mint latency.** Operator-availability-for-touch can add minutes-to-hours between approval and active peering. Mitigated by the workflow's `approved` → `mint-pending` → `active` visibility.
- **One more URI-SAN sub-namespace** — the `cert-sign` CLI grows a flag for partner-peering URI-SAN grammar; the Zenoh router's `access_control` config grammar gains pattern-matching against `urn:goat:partner-peering:*`. Net surface increase is contained (one CLI flag, one config schema field).
- **YubiKey throughput as bottleneck if partner cardinality grows.** v1 persona is corps-level (single-digit partners over years); if cardinality scales to dozens, this ADR may need revisit to batch-mint or introduce a delegated intermediate. Trigger condition documented below.

**Residual risk:**

- **Operator/reviewer collusion to mint over-scoped peering.** Mitigated by portal-side scope-vs-charter-Appendix-A diff alerting (any mint emitting a `scope=` outside the partner's charter triggers `audit_anomaly: true` wide event); detected post-hoc, not prevented in-band. Acceptable per `partner-peering-portal.md` §"Threat model."
- **Partner-side cert custody compromise.** If a partner's Zenoh router host is compromised, the cert is exfiltrable. Mitigated by mandatory TTL (max 90d per portal default) + revocation-on-incident-notification (partner's four-facet SLA per `mission-partner-sharing.md` §"Security"). Partner-side custody is **partner's responsibility**, per the sovereignty floor.
- **CRL propagation lag.** Revocation reaches Zenoh routers on next handshake's OCSP/CRL check; latency is bounded by the existing CRL cache TTL. Documented operationally; not different from the existing per-peer cert revocation latency.

## Assessor view (SCA-R)

Control mapping:

- **AC-3 (access enforcement)** — Zenoh ACL gate on URI-SAN; URI-SAN bound to partner via cert subject.
- **AC-24 (access control decisions)** — decision encoded in two layers: cert (identity + TTL) + ACL config (per-prefix allowlist).
- **AU-2 (audit events)** — cert-mint event in `audit-integrity` bucket; peering state-transition events on wide-event substrate.
- **IA-3 (device identification + authentication)** — X.509 client cert from offline-CA-rooted CA; URI-SAN encodes partner + scope.
- **IA-5 (authenticator management)** — cert rotation via re-mint + revocation via CRL; same ceremony as every other peer cert.
- **SC-12 (cryptographic key management)** — offline-CA root in YubiKey-PIV per ADR 0406.
- **SC-17 (PKI certificates)** — offline-CA-rooted X.509; CRL maintained.

Residual risk acceptance: TTL upper bound at 90d default (portal-configurable per partner up to 1y for stable long-term partnerships); approval-side collusion detected post-hoc via charter-diff alert.

## Trigger conditions for revisit

- **Partner cardinality grows past ~10 active peerings.** YubiKey throughput becomes felt; consider batch-mint operator workflow or delegated intermediate CA.
- **Heterogeneous pub/sub federation arrives** (per Block 84 follow-on). Non-Zenoh partners' routers may not natively validate X.509 URI-SAN the way Zenoh does; revisit whether the URI-SAN-bound cert remains the right shape, or whether a translation layer wraps it.
- **Operator/reviewer collusion is detected (real or simulated drill).** Tighten in-band prevention (e.g., require a second YubiKey touch from a third identity for above-charter scopes), not just post-hoc alerting.
- **Real-time push-based partner revocation becomes operationally compelled.** CRL-based revocation latency is bounded but not real-time; if a partner-side compromise scenario requires sub-minute global revocation, the substrate revisit is broader than this ADR (touches the CRL pipeline itself).
- **Block 17A.5 Policy Engine lands and the ACL-emit path migrates to PE.** The mint side stays unchanged (PE consumes URI-SANs the same way the portal-direct render does); but the operational coupling between portal and PE may surface mint-flow simplifications.

---

**See also:** `docs/adr/GLOSSARY.md` for acronyms (CA, CRL, SAN, URI-SAN, mTLS, PIV, YubiKey, OIDC).
