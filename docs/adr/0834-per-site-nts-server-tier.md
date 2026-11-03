# ADR 0834 — Per-site NTS server tier on each mgmt-host (interim stratum-2, future stratum-1)

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-05-06 |
| Repo scope | goat core |
| Related | [ADR 0804](0804-three-tier-ntp.md) (three-tier NTP — extended, not superseded), [ADR 0407](0407-internal-x509-ca-for-service-tls.md) (internal X.509 CA — leaf certs for NTS-KE), [ADR 0313](0313-silent-control-plane-outer-wg-wrap.md) + [ADR 0314](0314-no-public-dns-for-control-plane.md) (silent control plane — wg-cp0 traffic-class addition), [`../design/ntp-strategy.md`](../design/ntp-strategy.md), [`../design/universal-ntp-hygiene.md`](../design/universal-ntp-hygiene.md), `../design/canonical-lan.md` |

## TL;DR (for a layperson)

Today every mgmt-host *consumes* time from public NTS but does
not *serve* time to anyone. Goat-net peers (operator laptops,
edge nodes, sensors, future workloads) have no first-class
authoritative time source on the mesh — they fall through to
public NTP/NTS over their own egress, which both widens the
trust surface and undermines the "no public-internet-resident
secure plane" invariant. This ADR adds a *server* role on each
mgmt-host: chrony continues to consume upstream NTS (today public,
future GNSS hardware) and additionally serves NTS to nearby
goat-net clients. Architecture is identical pre- and
post-GNSS — only the upstream changes; the server tier, cert
chain, client config, and FIPS posture stay constant. The
canonical-LAN switch is recorded as a third portfolio
option, available when reachable, never a dependency.

## Context

[ADR 0804](0804-three-tier-ntp.md) is about **upstream source
acquisition** — three tiers (sovereign hardware / public NTS /
adaptive drift). It does not address the **downstream
fan-out** question: when an edge node or sensor on the
canonical LAN needs the time, where does it sync to?

Today's `ops/ansible/roles/ntp-hygiene/` chrony config has
`port 0` and `cmdport 0` — every mgmt-host is a pure NTS
client. There is no in-mesh authoritative server. Implicit
behavior is that each goat-net peer picks its own upstream
(typically public NTS), which:

1. forces every peer to have its own internet egress for time,
2. widens the per-peer trust surface (each peer trusts its own
   upstream selection),
3. duplicates the per-site NTS handshake load N times across
   N peers,
4. subtly violates the "no part of the secure plane resident
   on the public internet" invariant — every mesh peer that
   pulls time from `time.cloudflare.com` is making a public
   handshake on the secure plane's behalf.

Separately, the canonical-LAN switch (per
`canonical-lan.md`, planned
but not yet deployed at any site) is planned to host an NTS
server with a claimed stratum-1 backing. Useful when it
arrives, but not yet present, and should be treated as one
of N independent options — never a dependency.

## Decision

**Each mgmt-host
runs chrony as an NTS server in addition to its existing NTS
client role.** Same chrony binary, same `ntp-hygiene` role,
gated by a new per-site var `chrony_serve_nts: true`
(default `false` for hosts where serving is not desired —
e.g., relay VPSes, dev hosts, validation envs).

Server-side chrony directives added to
`ops/ansible/roles/ntp-hygiene/templates/chrony.conf.j2`
under the `chrony_serve_nts` gate:

```
{% if chrony_serve_nts %}
# -- Server tier (ADR 0834) -------------------------------------------------
# Serve NTS to goat-net peers. Reachability is gated at the network layer
# (LAN + wg-cp0 only); chrony's allow-list is defense-in-depth, not the
# primary enforcement.
ntsserverkey  {{ chrony_nts_server_key }}     # from ADR 0407 cert-sign --eku serverAuth
ntsservercert {{ chrony_nts_server_cert }}
ntsdumpdir    {{ chrony_nts_dumpdir }}
ntsport       4460                            # standard NTS-KE port
{% for cidr in chrony_serve_nts_allow %}
allow {{ cidr }}
{% endfor %}
# Drop the existing port 0 / cmdport 0 lines under this gate; serve NTP on 123.
{% endif %}
```

`chrony_serve_nts_allow` defaults to:
- `<site-lan-cidr>` — same-site canonical LAN (sub-ms RTT — primary client path)
- `<wg-cp0-cidr>` — wg-cp0 silent control plane (cross-site only when no nearer mgmt-host reachable; cross-site RTT of hundreds of ms, sloppy precision but better than nothing)

Operator may add additional CIDRs per site (e.g., a per-VLAN
sensor subnet) without ADR change.

**Upstream architecture is unchanged.** Today: four public
NTS providers per ADR 0804 / `chrony_nts_sources`.
Post-GNSS-procurement: a GNSS receiver `refclock SOCK` + a
GNSS time-server `server` line (per ADR 0804 / Block 12C). The server tier
serves whatever chrony has selected as its best upstream;
clients see consistent stratum-2 today and stratum-2 (still,
because clients are one hop down from a stratum-1 server)
post-GNSS — wait, **let's be careful with the math**:
post-GNSS the mgmt-host becomes stratum-1 (refclock at stratum-0;
mgmt-host is stratum-1 directly). LAN clients then become
stratum-2. The architecture *role* doesn't change; the
*stratum number* of the mgmt-host shifts down by 1.

### Cert chain

NTS-KE leaves are issued by the Block 22J / [ADR 0407](0407-internal-x509-ca-for-service-tls.md)
internal X.509 CA. New `cert-sign` invocation:

```
cert-sign \
  --site-id <site> \
  --cn ntp.<site>.<mesh-zone> \
  --san <wg-cp0 mesh ip> \
  --san <canonical-lan ip> \
  --san ntp.<site>.<mesh-zone> \
  --validity 8760h \
  --eku serverAuth \
  --out-cert /etc/chrony/server-cert.pem \
  --out-key  /etc/chrony/server-key.pem
```

No new `--kind` variant required — `--eku serverAuth` covers
NTS-KE per RFC 8915. Clients trust the offline-CA root
delivered via the offline-enrollment bundle's
`tls_trust_anchor` field — same trust path as Traefik
service TLS. Rotation is the same operator ceremony as any
other internal-CA leaf; chrony reloads on SIGHUP.

### Cross-check signals — two layers, complementary

- **chrony `peer` directive** (NTP-layer): symmetric peering
  between sibling mgmt-hosts, RTT-measured pairwise offset,
  automatic, no signature. *Not* configured as a primary
  source — used only to detect when a sibling's clock has
  drifted relative to the others. `local stratum 10` orphan
  remains the last-resort anchor.
- **`ntp-health-agent` peer attestation** (Zenoh-layer,
  Block 12D): signed JSON, CA-rooted, app-level. Different
  trust model (offline-CA-signed key vs. NTP RTT geometry)
  and different cadence (~30 s heartbeat vs. each polling
  interval).

The two are complementary, not redundant. NTP-layer catches
mechanical clock drift; Zenoh-layer catches "agent says
healthy but agent is lying" via cross-site signature
verification. The new ADR adds the chrony peer directives;
Block 12D's attestation layer is unchanged by this ADR.

### Switch as portfolio option (not dependency)

The canonical-LAN switch (per `canonical-lan.md`)
is planned to host a stratum-1 NTS server (GNSS module
attached). Recorded as one entry in the portfolio:

- **Configured as `server <switch-ip> nts noselect prefer`** —
  `noselect` keeps it visible in `chronyc sources` without
  ever being chosen unless the operator explicitly promotes
  it; `prefer` makes the promotion a one-line config edit.
- Two open verifications before relying on it: (1) does the
  switch platform's OS actually speak NTS (RFC 8915), or
  only NTP+symmetric-key auth? (2) is the stratum-1 claim
  backed by a real refclock or chained from a public NTS
  upstream (in which case the switch is stratum-2)?
- Same architectural slot regardless of (1)/(2) outcome —
  another independent same-LAN low-RTT source. Plain-NTP
  fallback is acceptable per the LAN-trusted carve-out
  amended into [`universal-ntp-hygiene.md`](../design/universal-ntp-hygiene.md)
  §3.2 (the switch is on the same trust substrate that
  already mediates VRRP failover; trust posture is
  consistent).

### FIPS posture (Option A — internal-mesh)

NTS server lives on the internal mesh; cert chain rooted at
the offline-CA Ed25519 root per ADR 0406 /
[ADR 0407](0407-internal-x509-ca-for-service-tls.md). NTS
TLS handshake uses Ubuntu Pro FIPS OpenSSL 3.0.x (CMVP-validated
per ADR 0103). Ed25519 sits inside FIPS 186-5
(2023) but CMVP-validated implementations in production
OpenSSL are still landing — same posture as Postgres / Ory /
signal-server / Block 22J Traefik. Cross-boundary FIPS
strictness is at Block 19 strongSwan + AWS-LC, not at every
internal service.

### wg-cp0 traffic-class addition

This ADR adds a third class of traffic to the wg-cp0
overlay: NTS-KE (TCP 4460) + NTP-over-NTS (UDP 123). Today
wg-cp0 carries (1) mgmt-API on the overlay address and
(2) Spock/PostgreSQL streaming replication. Adding NTS is
explicit, not silent — [ADR 0313](0313-silent-control-plane-outer-wg-wrap.md)
and [ADR 0314](0314-no-public-dns-for-control-plane.md) name
the silent-control-plane invariant; this ADR records that
the invariant explicitly accommodates control-plane time
distribution as a third sanctioned class. wg-cp0 NTS use is
opt-in per-site (the wg-cp0 allow line is on by
default but can be removed if site-LAN-only fan-out is
desired); LAN-only fan-out is the recommended primary path
because precision over wg-cp0 (hundreds of ms RTT) is sloppy.

## Alternatives considered

- **Mesh peers each pull from public NTS directly (today's
  implicit behavior).** Widens trust surface, duplicates
  handshake load, violates the "no public-internet-resident
  secure plane" invariant for time. Rejected.
- **Centralize NTS at the relay VPSes (per [`ntp-strategy.md`](../design/ntp-strategy.md) §3.2 Option 2).**
  Already named as a fallback for sites where ZTOL policy
  forbids arbitrary mgmt-host WAN egress. Not the primary
  path because it routes time through cloud relays for sites
  that have a perfectly good per-site mgmt-host. Kept available;
  not promoted.
- **Wait for GNSS hardware (Block 12C) before standing up
  the server tier.** Conflates source acquisition with
  fan-out role. The server tier is useful today against
  public NTS upstream; the upstream improvement is
  orthogonal. Rejected — gates value behind procurement
  delay.
- **mgmt-host → siblings as primary source via wg-cp0
  cross-mgmt-host sync.** Rejected — cross-site wg-cp0 latency
  destroys precision, and `peer` directives at primary
  position can settle into a local consensus loop with no
  external anchor under sustained external-source loss.
  Used only as cross-check (not primary source).

## Consequences

**Positive.** Goat-net peers get a first-class
authoritative same-LAN time source. Trust surface narrows
(N peers ÷ public-NTS handshakes → 1 mgmt-host
public-NTS handshake fanned out to N peers). Architecture
is GNSS-ready by construction — Block 12C lands a
one-line upstream config change with no client-side
disruption. Composes with the planned switch NTS
server as a portfolio entry, not a dependency.

**Negative.** Operationally adds:
- a new failure mode (chrony-as-server crash blocks LAN
  clients from syncing — but `local stratum 10` orphan
  keeps degraded service), classified per
  [`universal-ntp-hygiene.md`](../design/universal-ntp-hygiene.md) §7.2 as
  **load-bearing for new admits**, not core-load-bearing
  (existing peers' WireGuard tunnels and replication are
  unaffected by NTS server failure);
- per-site cert rotation discipline (every mgmt-host has an
  NTS-KE leaf cert that rotates per Block 22J / ADR 0407
  cadence; folds into the existing operator ceremony);
- a third sanctioned traffic class on wg-cp0 (NTS-KE +
  NTP-over-NTS) that needs explicit recording in [ADR 0313](0313-silent-control-plane-outer-wg-wrap.md)
  / [ADR 0314](0314-no-public-dns-for-control-plane.md)
  text on next revision.

**Open verifications.**
1. Stratum math under `local stratum 10` + server mode —
   confirm chrony advertises stratum 10 to clients when
   no upstream reachable (clients can then choose to
   refuse). Smoke-test in next-session implementation.
2. Switch-side: NTS support and refclock backing
   (see "Switch as portfolio option" §).
3. NTS-server CPU/memory cost on mgmt-host under N=100+
   client load — measure during Block 12 implementation
   pass. Fan-out cost is per-handshake (NTS-KE TCP
   4460), not per-NTP-packet, so soak should be
   inexpensive.

## Assessor view (SCA-R)

Extends [ADR 0804](0804-three-tier-ntp.md)'s AU-8 / SC-45 /
SI-4 controls by closing a previously-implicit gap: today's
mesh peers had no first-class in-mesh time source, so the
"sovereign time discipline" claim was load-bearing only
for mgmt-hosts, not for the mesh peers depending on
them. With the server tier, every goat-net peer's time
source can be cryptographically traced to the same
offline-CA root that authorizes its enrollment.

## Trigger conditions for revisit

- Switch deployment lands with verified stratum-1 backing
  and NTS support — promote `noselect` → primary in the
  portfolio. Or confirm plain-NTP-only and amend the
  LAN-trusted carve-out language.
- NTS-KE handshake load on mgmt-host becomes measurable under
  fleet load — consider the relay-side NTS bridge option
  (see Alternatives) for sites with very high client
  counts.
- FIPS strictness on internal-mesh becomes load-bearing
  in a compliance review — re-evaluate Ed25519 cert
  chain (Option A) vs. provisioning a separate
  ECDSA-P-256 chain just for NTS (Option B in the
  pre-ADR analysis).

---

*See `GLOSSARY.md` for terms and acronyms.*
