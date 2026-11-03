# ADR 0835 — Secure messaging substrate (tuwunel) — supersedes ADR 0812

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-05-07 |
| Repo scope | goat core |
| Supersedes | [ADR 0812](0812-per-site-matrix.md) (per-site Dendrite for SRE pager) |
| Related | [ADR 0411](0411-ory-idp-mvp.md) (Ory MVP — OIDC SSO target), [ADR 0407](0407-internal-x509-ca-for-service-tls.md) (internal X.509 CA — Element web TLS leaf), [ADR 0313](0313-silent-control-plane-outer-wg-wrap.md) + [ADR 0314](0314-no-public-dns-for-control-plane.md) (silent control plane — federation rides wg-cp0), [ADR 0110](0110-license-posture-agplv3-forked-components.md) + [ADR 0111](0111-vendor-friendly-license-posture.md) (license tiers — Apache-2.0 passes), [ADR 0112](0112-tech-strategy-workload-shape-language-boundary.md) (workload-shape rule — buy-not-build language posture), [ADR 0808](0808-pam.md) (egress proxy — homeserver SNI allowlist), [ADR 0836](0836-matrix-push-gateway-hedwig.md) (push gateway — sibling decision), [`../design/secure-messaging.md`](https://github.com/usareur-af/goatstuff/blob/main/docs/design/secure-messaging.md) |

## TL;DR (for a layperson)

ADR 0812 picked a small Matrix homeserver (Dendrite) as a chat
layer for SRE pager notifications. New scope: **secure messaging
for every peer in the mesh, up to 1000 users per site, across
several sites.** That invalidates the "lightweight pager"
framing. Three candidates were re-compared: Synapse (Python — out
per the no-Python-for-load-bearing rule), Dendrite (Go), and
**tuwunel** (Rust, official successor to conduwuit, sponsored by
the government of Switzerland for citizen-facing deployment). We
pick **tuwunel**, with a federation soak gate before committing
beyond the first site, and a clean fallback to Dendrite or
ejabberd named in advance if the gate fails.

## Context

ADR 0812 was sized for the policy-engine notification adapter use
case (~10 paged operators per site, near-zero sustained chat
traffic). That ADR's stated rationale — "Dendrite is lighter than
Synapse" — was true and load-bearing for that scope. New
requirements have changed the scope:

1. **All peers in the netbird mesh need a secure messaging
   service.** Not just SREs.
2. **Up to 1000 users per site.** Per-site envelope under ADR 0215
   (floor 100 / target 500 / **stretch 1000**) — so
   1000-per-site is the stretch target.
3. **Mobile clients matter** (Android + iOS) — operators carry
   issued devices.
4. **Sovereign per-site authority + opt-in inter-site federation**
   per ADR 0812's existing posture; preserved.
5. **No Python for load-bearing applications and services.**
   Standing project rule from ADR 0112's predecessor language
   discipline — Python is acceptable for tooling and Ansible
   helpers but not for services that hold state, handle volume,
   or need careful scaling attention.

The pager-tier framing in ADR 0812 does not survive these
requirements. A 1000-user-per-site chat substrate is a real
service with real ops weight: identity (OIDC SSO via Ory), media
(MinIO bucket on Block 27 substrate), federation (across
sites over wg-cp0), mobile push (sovereign push gateway path —
see ADR 0836), backups, monitoring, and a real audit
relationship with the Block 17D / Block 27 substrate.

The Synapse vs Dendrite vs Rust-alternative comparison must be
re-run against this scope.

## Decision

**Adopt tuwunel as the secure messaging substrate. One
homeserver per site. Federation between sites over wg-cp0
silent control plane.**

### Server selection

| Candidate | Language | Production reference at scale | License | Verdict |
|---|---|---|---|---|
| Synapse | Python | matrix.org public + KDE + Mozilla + Element Cloud + ~every >100-user Matrix deploy | Apache-2.0 | **Out** — violates no-Python-for-load-bearing rule |
| Dendrite | Go | Smaller deployments; no major sovereign-gov reference; Element investment trajectory slowing as attention shifts to Element X / Matrix 2.0 | Apache-2.0 | **Fallback** — in-language but weakest production reference at our scale; named as the explicit fallback if tuwunel fails the federation soak gate |
| **tuwunel** | **Rust** | **Official successor to conduwuit (declared stable); sponsored by the government of Switzerland and currently deployed for citizens; multiple committed companies with full-time staff** | **Apache-2.0** | **Selected** |
| conduwuit | Rust | Pre-tuwunel; succeeded by tuwunel | Apache-2.0 | Out — superseded by tuwunel per project's own docs |
| Conduit | Rust | Predecessor of conduwuit; smaller scale | Apache-2.0 | Out — succeeded |
| ejabberd | Erlang/OTP | Massive — WhatsApp historical, mobile carriers, Nintendo network | Apache-2.0 (community) | **Off-Matrix fallback** — named as the protocol-pivot fallback if both tuwunel AND Dendrite fail the soak; loses Matrix client ecosystem |

The tuwunel selection rests on three load-bearing claims, each
with a verification checkpoint named in this ADR:

1. **Pure Rust** — passes the no-Python rule cleanly. Verified
   by the project README + repo language stats. Done.
2. **Apache-2.0** — Tier-1 license per [ADR 0111](0111-vendor-friendly-license-posture.md);
   passes our deployment-and-build surface license audit. Done.
3. **Production reference at scale** — Swiss-government
   citizen-facing deployment is a strong claim but cannot be
   relied on from a README alone. The verification spike (Block 70.1)
   converts the claim to evidence: scope (how many users, what
   shape), reachability (can we talk to a Swiss operator),
   operational hours (how long has the deployment been live),
   and incident posture (any public RCAs).

### Database backend

**Tuwunel uses RocksDB** as its embedded backend (per project
documentation). Implications:

- The chat substrate is **not** on the Spock-replicated postgres
  cluster. This is by design — chat is high-volume,
  latency-sensitive, append-heavy at the message-event level,
  and benefits from a local embedded store.
- **No multi-master replication for the chat database.** Per-site
  Matrix federation handles cross-site availability at the
  Matrix-protocol layer, not the storage layer.
- **Backups are RocksDB filesystem snapshots** plus media files,
  not pgBackRest. New `chat-backup` cadence: nightly snapshot to
  the per-site MinIO `chat-archive` bucket; 30-day retention;
  restore tested annually as part of Block 56 site-loss drill.
- **Per-site RocksDB lives on the site's primary host NVMe** at
  `/opt/docker-data/tuwunel/db/`. Sized for ~1 GB per active
  user as upper-band working estimate (1 TB NVMe ÷ 1000 users ≈
  1 GB/user — fits with headroom; tighter monitoring landed via
  Block 27 disk-pressure alerts).

### Identity and authentication

**OIDC SSO via Ory Kratos** ([ADR 0411](0411-ory-idp-mvp.md))
is mandatory for tuwunel — no local-account creation in
production. Tuwunel speaks the OIDC client side; Ory Hydra is
the OIDC OP; Kratos is the user-management surface. This:

- Eliminates manual provisioning at 1000 users per site
- Uses the same audit trail as every other Ory-fronted service
- Inherits Block 21F IdP-down recovery posture
- Is a **hard dependency on Block 21D Ory wire-up landing first**

User identifiers in Matrix are `@<kratos-sub>:<site>.<mesh-zone>`
where `<kratos-sub>` is the Kratos-issued stable subject claim.

### Network surface

| Endpoint | Listener | Network | Purpose |
|---|---|---|---|
| `chat.<site>.<mesh-zone>:443` | Caddy → tuwunel HTTP | Mesh-only (no public DNS) | Client-server API + Element web hosting |
| `chat.<site>.<mesh-zone>:8448` | Caddy → tuwunel HTTPS | wg-cp0 between sites only | Server-server (federation) |
| Element web static assets | Caddy file_server | Same vhost | Browser client, served from `/var/www/element/` |

**Federation is wg-cp0-internal-only.** Listener is gated by
nftables / Caddy IP allowlist to the wg-cp0 subnet
(`198.18.0.0/24`). No public DNS for `chat.<site>...`; mesh
DNS only per the silent-control-plane invariant. This composes
with [ADR 0313](0313-silent-control-plane-outer-wg-wrap.md) /
[ADR 0314](0314-no-public-dns-for-control-plane.md) — the chat
substrate inherits the same no-public-internet-on-secure-plane
invariant as the management API.

**External federation to partner Matrix homeservers is out of
scope for v1**; queued for v3 under Block 60 moon-pod federation
posture, gated on Block 28 WASM conditioning sandbox availability
for inbound message classification.

### Encryption

**E2EE on by default for all rooms** via Matrix's Megolm
protocol (the Matrix client-side standard). Server cannot read
message content; metadata (who-talks-to-whom, when, room
membership) is server-visible by Matrix protocol design.

**Cross-signing + key backup** are mandatory for v2 — without
them, device loss equals message-history loss for the user.
Server-side encrypted-key-backup is a Matrix protocol feature;
tuwunel supports it.

**Outer transport is TLS 1.3** terminated by Caddy with a leaf
cert from the Block 22J / [ADR 0407](0407-internal-x509-ca-for-service-tls.md)
internal X.509 CA. Federation port 8448 uses the same cert;
tuwunel's federation client trusts the offline-CA root delivered
via the Block 22K bundle's `tls_trust_anchor` field. **No
Let's Encrypt** for the chat substrate — the homeserver hostname
is mesh-only.

### Mobile push

See sibling [ADR 0836](0836-matrix-push-gateway-hedwig.md). v1
ships UnifiedPush (Android-only, sovereign via self-hosted ntfy);
v2 adds the hedwig FCM-routed path for stock-Element on
Play-Store-Android + iOS-via-FCM-proxied-APNs. The iOS
metadata trade-off is decided in that ADR, not this one.

### Federation soak gate (the load-bearing verification)

Tuwunel is selected on a **promote-on-evidence** basis. The
v2 cutover (single-site → multi-site federation) is **gated** on
a 30-day federation soak between three sites at
synthetic 1000-user load. Pass criteria:

- Zero room-state-divergence events across the three
  homeservers as observed by a synthetic auditor peer
- Federation lag p99 < 5 s for room events
- Zero unplanned tuwunel restarts attributable to federation
  bugs
- Zero data-corruption RocksDB incidents
- Memory + CPU within 70% of provisioned headroom on the
  primary host under sustained synthetic load

**Failure paths (named in advance):**

- Failure mode A — federation bugs at scale: pivot to **Dendrite**.
  Same Matrix protocol, different implementation. Existing
  client compatibility preserved. ~1 week pivot work assuming
  v1 deployed. Note: cannot directly migrate the RocksDB
  database to Dendrite's Postgres backend — users re-register;
  message history is lost. Operator-fired event with explicit
  user notice.
- Failure mode B — both tuwunel AND Dendrite fail: pivot to
  **ejabberd** (XMPP). Different protocol; loses Element ecosystem.
  Reasoned about in this ADR but full pivot path designed only
  if A fails.
- Failure mode C — soak exposes a fixable upstream bug: file
  upstream issue + carry a small fork until resolved. Acceptable
  if the fix is bounded.

## Alternatives considered

- **Synapse (Python).** Strongest production track record but
  excluded by the no-Python-for-load-bearing rule. Acknowledged
  as the conservative production answer in any other context.
- **Dendrite (Go).** Was the prior pick under ADR 0812. Still
  in-language; loses on production-reference evidence at the
  1000-user scale; Element investment trajectory is the
  ongoing concern. Named as the v1.5 / fallback path in this
  ADR; not an architecture rebuild if we have to pivot.
- **conduwuit / Conduit (Rust).** Conduwuit is succeeded by
  tuwunel per the tuwunel README; Conduit is older and smaller-
  scale. Excluded.
- **ejabberd (Erlang/OTP) — protocol pivot.** Strongest chat-
  at-scale evidence in existence; runs WhatsApp-historical-shape
  workloads; loses Matrix client ecosystem. Named as the
  protocol-pivot fallback if both Matrix-implementation choices
  fail. Erlang is a buy-not-build language so the no-Python
  rule does not bind on it (same logic as tuwunel-is-Rust /
  Synapse-would-be-Python — language matters for ops only when
  the code is ours; here it's upstream).
- **Mattermost (Go).** Mature in Go; deployed at scale across
  US gov + DoD. Loses Matrix federation entirely (Mattermost
  Connect is the closest match and is enterprise-tier paid).
  Excluded — federation across our sites is load-bearing.
- **Rocket.Chat (Node.js + MongoDB).** Bad stack fit (MongoDB
  not in our substrate; would require new ops surface). Out.
- **Build our own homeserver.** Engineering ROI rejection.
  ~years of work to reach feature parity; this is not our
  value-add.

## Consequences

### Positive

- **No Python in our stack** for the chat substrate — passes
  the standing rule cleanly.
- **Single binary deploy** + RocksDB embedded — operationally
  simple, fits the primary-host footprint.
- **Apache-2.0** — Tier-1 license, no AGPL-at-process-boundary
  concerns.
- **Active full-time-staff development** with sovereign-gov
  sponsor — better long-term continuity bet than a dwindling
  Element investment in Dendrite.
- **Element web + standard Matrix client ecosystem** preserved
  for users — Element web in browser, Element X / FluffyChat
  on mobile.
- **Federation across sites** is native to Matrix; no DIY
  cross-site coordination layer needed.
- **Aligns with the Block 70 plan** for first-class secure
  messaging without re-opening protocol selection.

### Negative

- **Smaller operational community** than Synapse. When weird
  bugs hit at month 6, fewer Stack-Overflow answers.
- **15 months as a named project** (tuwunel-specifically) —
  the lineage is older but the named-fork is younger than
  Dendrite.
- **Conduit lineage's database lock-in** — "Never switch
  between forks of Conduit or you will corrupt your database."
  Once committed, we cannot bail back to conduwuit. Acceptable
  but worth naming.
- **Federation across several sites is the empirical question** — Conduit
  family was historically weak here; tuwunel's claims need
  soak validation per the gate above.
- **Hard dependency on Block 21D Ory wire-up landing** before
  v1 ship.

## Assessor view (SCA-R)

This ADR serves SC-8 (transmission confidentiality — TLS 1.3 +
Megolm E2EE), SC-13 (cryptographic protection — TLS leaf from
internal X.509 CA per ADR 0407), AC-2 (account management — via
Ory Kratos OIDC), AU-2 (auditable events — Matrix events ride
the Block 27 OTLP wide-event surface), and IR-6 (incident
reporting — chat as the operator coordination channel).

The federation-soak gate provides the cited evidence chain
required by [ADR 1006](1006-phase-gates.md) phase-gate
discipline; promotion of this ADR Proposed → Accepted requires
soak-evidence citation.

## Trigger conditions for revisit

- **Tuwunel project changes substantially** (sponsor withdrawal,
  abandonment, license change away from Apache-2.0). Then the
  Dendrite fallback or a re-evaluation of Synapse-as-exception
  applies.
- **Federation soak fails per the gate.** Pivot to Dendrite per
  the named failure path. ADR amendment, not a new ADR.
- **Scale envelope changes materially upward** (e.g. 10K users
  per site instead of 1000). The architecture survives at 5K
  per site by reasonable extrapolation; beyond that, re-test
  RocksDB single-host capacity and federation lag.
- **External federation to partner homeservers becomes a v1
  requirement** (e.g. a partner's Matrix homeserver). Re-evaluate the public-
  internet listener question — partner federation requires
  reachability. Today this is a wg-cp0-only deployment.
- **iOS mobile push policy changes** (e.g. Apple opens
  alternative push paths in EU per DMA). Re-evaluate the
  hedwig + FCM-proxy posture in ADR 0836; this ADR's federation
  + identity decisions stand independent of push.

---

*See `GLOSSARY.md` for terms and acronyms.*
