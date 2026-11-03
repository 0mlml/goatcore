# ADR 0407 — Internal X.509 CA for service TLS, single-tier, sharing the offline-enrollment Ed25519 root

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-04 |
| Repo scope | goat core |
| Related | `0406-offline-ca-yubikey-piv.md`, `0400-sovereign-per-site-pki.md`, [`0313-silent-control-plane-outer-wg-wrap.md`](0313-silent-control-plane-outer-wg-wrap.md), [`0314-no-public-dns-for-control-plane.md`](0314-no-public-dns-for-control-plane.md), [`../design/internal-tls-ca.md`](../design/internal-tls-ca.md), `../design/wg-cp0-prod.md`, [`../design/offline-enrollment.md`](../design/offline-enrollment.md) |

## TL;DR (for a layperson)

When peers connect to our internal management API at a sovereign
site, the connection uses TLS — the same encrypted-handshake
pattern your browser uses to reach a banking website. TLS needs
a "certificate authority" peers trust. Public authorities like
Let's Encrypt won't sign certificates for our internal-only
addresses, and we deliberately don't expose our management API
to the public internet. So we run our own small certificate
authority, rooted in the same hardware-protected key
(ADR 0406) we already use to
sign peer enrollment bundles. One trust root, one custody story,
no extra cloud dependency.

## Context

Block 22 silent control plane puts
our management API behind a WireGuard tunnel at internal
address `https://198.18.0.1:443`. There is **no public DNS** for
this endpoint per [ADR 0314](0314-no-public-dns-for-control-plane.md),
which rules out Let's Encrypt's HTTP-01 / DNS-01 / TLS-ALPN
challenges. We need TLS for two reasons:

1. **Defense-in-depth inside the WG tunnel.** WireGuard is the
   load-bearing crypto, but a misconfigured tunnel or kernel
   exploit shouldn't drop the inner-API to plaintext.
2. **Service multiplexing on `:443`.** The management API is the
   first inhabitant; future inhabitants (Ory Kratos/Hydra public
   endpoints, dashboard, db-policy-engine, snitch collector) all
   need TLS termination on the same Orin via host-routing through
   Traefik.

We already custody an Ed25519 signing root for offline-enrollment
bundles (ADR 0406) — an
age-encrypted file or a YubiKey-PIV slot. That root
has the right custody story (offline ceremony, hardware-protected
in production) but signs raw Ed25519 over CBOR, not X.509
certificates. The two artifact types serve different purposes.

The natural question: do we run **two separate roots** (one for
bundles, one for X.509 TLS) or **one root used two ways**? Two
separate roots double the custody story; one root keeps the trust
anchor count at one. The ceremony, the audit pipeline, and the
backup procedure are unchanged.

## Decision

Run **one Ed25519 root** (ADR 0406),
used two ways:

1. As today: signs CBOR enrollment bundles
   ([ADR 0308](0308-offline-enrollment-cbor-bundles.md)).
2. New under this ADR: signs X.509 leaf certificates for service
   TLS termination on internal endpoints.

**Single-tier CA** — root signs leaves directly, no intermediate.
At the scale this targets (a handful of sites, tens of services per site, annual rotation)
the operational cost of a two-tier hierarchy outweighs the benefit.
If issuance volume or rotation cadence pushes back, we add an
intermediate and re-issue leaves under it; trust anchor stays the
same.

**Algorithm — Ed25519 (RFC 8410).** FIPS 186-5 approved. Universal
client support in 2026 (Go `crypto/tls`, rustls, OpenSSL 1.1.1+,
BoringSSL FIPS, AWS-LC). Same algorithm as our bundle root, so the
custody tooling is reused unchanged.

**Issuance ceremony.** Operator-on-laptop runs
`ops/enrollment/cmd/cert-sign` against the age-encrypted root
file (dev mode) or YubiKey PIV (when ADR 0406
production backend lands). Inputs: SAN list, common name, validity
period. Output: PEM-encoded leaf cert + Ed25519 leaf private key.
Each issuance produces an audit-log entry on the air-gapped
laptop, joined to the bundle-issuance audit on next online sync.

**Distribution.**
- **Trust anchor** (root cert PEM, derived from existing root
  Ed25519 public key) ships in every offline-enrollment bundle as
  a new field `tls_trust_anchor`. Peers add it as a CA cert in
  their TLS verification context.
- **Leaf cert + key** push to the service host via Ansible from
  operator laptop, mounted into Traefik (or the consuming service)
  as read-only files. Mode `0400 root:root` for the key.

**Validity — 1 year leaves.** Industry split is 90 days (LE) vs
1+ year (internal CAs). Manual rotation makes 90 days punishing;
1 year matches the cadence of operator visits to a site.
Prometheus blackbox_exporter probe alerts at 30 days from expiry.

**Naming convention.**
- CN: service-fqdn (e.g., `mgmt.alpha.zagros.example`)
- SANs: every reachable address — `198.18.0.1` (wg-cp0 internal),
  `127.0.0.1` (local), the FQDN, the keepalived VIP mesh IP when
  applicable.
- Subject DN: `O=<organisation>, OU=<site_id>, CN=<service-fqdn>`.

**Revocation — manual re-issue, no OCSP/CRL day-1.** At our scale
a compromised cert is mitigated by re-issuing every cert under the
root and pushing the new bundle CRL field. CRL/OCSP infrastructure
is queued as a follow-on if issuance volume or compromise frequency
demands it; the bundle CRL surface
([offline-enrollment.md §4](../design/offline-enrollment.md))
already has the shape we'd extend.

## Alternatives considered

- **Let's Encrypt via DNS-01 / Cloudflare.** Won't work — by
  design [ADR 0314](0314-no-public-dns-for-control-plane.md)
  removes the public DNS we'd validate against. Rejected.
- **Self-signed cert per host with bundle pin.** Operationally
  fragile (every cert rotation rewrites every bundle's pin field;
  no recovery story for a single compromised cert short of
  revoking the bundle entirely). Rejected.
- **Smallstep step-ca.** Mature open-source CA with Ed25519
  support. Heavyweight import for what is, at our scale, a
  one-cert-per-host-per-year ceremony. Doesn't compose cleanly
  with our existing offline-CA custody model. Rejected.
- **Plain h2c inside the wg-cp0 tunnel (no TLS at all).**
  Considered. Defers TLS-in-depth indefinitely and forces a
  separate decision the moment any service expects HTTPS by
  default. Rejected.
- **Two-tier CA (root signs intermediate signs leaves).**
  Industry-standard for large fleets. Adds operational complexity
  (two key-custody stories, intermediate-rotation cadence) without
  proportionate benefit at our scale. Held as a future
  enhancement; trigger condition documented below.
- **Separate X.509 root distinct from the bundle Ed25519 root.**
  Doubles custody story. Two ceremonies, two backups, two audit
  trails. Rejected — same root used two ways is operationally
  cheaper and trust-equivalent.

## Consequences

**Positive.** Single trust anchor across the system. No new
custody ceremony. All TLS-terminated services on every Orin chain
back to one root we already control. Adding a service to `:443` is
a cert-issuance + Traefik-config change, not a new auth-system
integration. Trust anchor distribution rides the existing bundle
pipeline at no extra cost.

**Negative.** Manual rotation cadence (annual per cert). Compromise
of the root key forces re-issuance of every leaf — same pre-existing
exposure as bundle compromise; the blast radius hasn't grown.

**Operationally accepted carry-forwards** (do not block this
decision; document for visibility):
- Annual cert rotation is a manual operator step until automation
  lands. Acceptable at our cadence.
- No OCSP/CRL infrastructure for cert revocation. Acceptable until
  issuance volume demands it.
- Single-tier hierarchy. Add intermediate if scale demands.
- mTLS (client certs) for service-to-service is out of scope here;
  enables it cheaply by reusing the same CA + tooling when a
  service needs it.

## Assessor view (SCA-R)

Internal X.509 CA serves SC-8 (transmission confidentiality —
TLS in the inner control-plane path), SC-12 (key establishment),
SC-17 (PKI certificates), and SC-23 (session authenticity).
Single-tier with an Ed25519 root rooted in the same offline-CA
custody pattern as ADR 0406 keeps the cert-issuance audit trail
joinable to bundle issuance, simplifying continuous-monitoring
collection. Annual leaf rotation aligns with NIST SP 800-57 part 1
short-term-key guidance for operational TLS keys.

## Trigger conditions for revisit

- **Issuance volume** exceeds ~50 cert ceremonies per year. Adopt
  intermediate CA + automate leaf issuance from a Hot intermediate;
  keep root ceremony cadence unchanged.
- **Compromise frequency** demands a working revocation channel
  faster than re-issue-all-bundles can deliver. Build CRL or OCSP
  responder; reuse the bundle CRL pipeline shape.
- **mTLS for service-to-service** becomes load-bearing. Issue
  client certs from this CA; no algorithm change needed.
- **Block 43 FROST threshold signing** lands. Migrate root signing
  ceremony to K-of-N over distributed YubiKey custodians; trust
  anchor and issuance interface stay the same.
- **Block 41 SPIFFE/SPIRE workload identity** lands. SPIRE has its
  own SVID issuance; this ADR's scope is platform-service TLS,
  not workload identity. The two coexist.

---

*See `GLOSSARY.md` for terms and acronyms.*
