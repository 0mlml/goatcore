# "Is it me or the network?" — a public reachability surface + cloud canary peers

Design proposal. Evidence/inference labeled. Minimal moving parts. READ-ONLY (no deploys).

## Problem

When a user can't get on the mesh, **every surface that could tell them whether
the mesh is up is itself mesh-only** — isitup, the portal, relay-health are all
served behind the mesh (EVIDENCE: relay-health is `relay-health.<site>.<zone>`
mesh-CoreDNS + Ory-admin-gated, `relay-health-dashboard.md` §"Hosted
architecture"; isitup services are `*.netbird.<goatnet>.net`, CLAUDE.md). So a
locked-out user cannot distinguish "my laptop / my link is broken" from "the
mesh front door is down." Two layers answer that, from outside the mesh.

---

## (a) Public reachability surface — "the front door is up"

**Reuse, don't build.** The relay-health prober already blackbox-probes relay
**public IPs** and the non-flapping health signals are `:2333` rathole control
(TCP) + `:33073 ws_tcp` (a *direct*, non-hairpin TCP connect to the goat-relay),
with `:443` enroll-TLS ok-ratio (EVIDENCE: `relay-health-probe.sh` lines
452/459 + `relay-health-dashboard.md` §Vantage). A cloud-resident, **off-mesh**
prober vantage already exists for the external view and to survive a Thor/mesh
outage (EVIDENCE: ADR 1066 + `relay-health-cloud-deploy.yml`).

**The new piece is small: a public, read-only, coarse SUBSET render** served
from that off-mesh cloud box:

- **Content = one verdict + a per-relay up/down row, nothing else:** "Enrollment
  front door: UP — N/M relays answering (`:2333`+`:33073 ws_tcp`), `:443`
  ok-ratio X%, checked Xs ago." Derived from the non-flapping signals (so it
  does NOT inherit the client STUN/TURN whipsaw, and it does NOT inherit the
  Thor-vantage `:443`/STUN hairpin ambiguity — the cloud vantage is external).
- **What is safe to expose:** relay **public IPs / count / up-down** only —
  these already front `netbird.<goatnet>-backbone.net` round-robin and are
  discoverable by any enrollment attempt or `dig` (EVIDENCE: `relays.conf`
  public IPs are the enrollment endpoints). So this leaks nothing a would-be
  enroller can't already see.
- **What must NEVER appear here:** peer lists/counts, throughput/disk/CPU, the
  host-net band, tunnel topology, instance IDs, identity, region internals, the
  `--deep` block. The public page is a strict projection of `{name, ip,
  up/down, ok_ratio, generated}` — a whitelist, not a redaction.
- **Anti-abuse / anti-enumeration:**
  - **Serve a pre-rendered cached artifact, never probe-on-request.** The
    existing timer regenerates the JSON every 15s (EVIDENCE: doc §Hosted). A
    public request reads a static file — it can't be used to trigger or
    amplify probes, and can't be turned into a scanning pivot.
  - **Coarse + rate-limited.** The boolean + per-relay dot is the whole payload;
    add a simple per-IP rate limit at the fronting proxy.
  - **Gating:** the coarse boolean is arguably safe **ungated** (it reveals only
    what an enrollment attempt reveals). If any per-relay detail is shown,
    reuse the **existing public email-allowlist gate**
    (`OAUTH2_PROXY_AUTHENTICATED_EMAILS_FILE`, fail-closed on empty) already
    built for the public dashboard (EVIDENCE: doc §"Public-gate authorization
    2026-07-20"). Recommend: ungated one-line verdict; gated per-relay breakdown.
  - **No mesh dependency:** the cloud box is off-mesh and off-site, so it is
    reachable **precisely during** the outage it reports (EVIDENCE: ADR 1066
    rationale; doc §"Site network hosts" notes the cloud box is "reachable
    precisely when the site path is not").

**Limit (INFERENCE):** this answers "the front door TCP-answers from the
internet." It does **not** prove a peer can actually enroll, get signal, and
move around. That is (b).

---

## (b) Cloud canary peers — "a newcomer can actually get in and move around"

**What:** 2–3 always-on **enrolled** peers running in the cloud, each in a
**distinct region/provider**, **none co-located with a Thor or a site LAN** — so
they exercise the *same public enrollment path a locked-out laptop takes*, not a
privileged intra-site shortcut, and so a single cloud-region blip doesn't read
as a mesh outage. Count is a monitoring-companion call: **2 for
quorum-of-disagreement, 3 to break ties** (INFERENCE; matches the doc's
"deliberate non-HA is correct for a monitoring companion" stance, EVIDENCE).

**What each canary continuously proves (the "newcomer journey"), on a timer:**

1. **Management reachable** — `mgmClient.IsHealthy()` (EVIDENCE: exists,
   `engine.go` RunHealthProbes).
2. **Signal reachable** — `signal.IsHealthy()` (EVIDENCE: same).
3. **WireGuard actually up + a data-plane round-trip** to a known always-on
   hosted service (HTTP 200 over the mesh IP) — proves the tunnel carries
   traffic, not just that control-plane answered.
4. **DNS resolves a mesh name** via CoreDNS (a known `*.netbird.<goatnet>.net`).
5. *(optional)* **relay-fallback path** exercised (force-relay one connection)
   to prove the relayed data path independently of P2P.

goat-client already has enroll + a connectivity check to build on (EVIDENCE:
CLAUDE.md goat-client — "connectivity check + goatdrop built").

**How the verdict reaches the PUBLIC surface WITHOUT a mesh dependency (the
crux):** the canary must NOT publish its heartbeat over the mesh — a mesh outage
would then black out the very signal we need. Instead it **POSTs a coarse
journey verdict over the public internet to the cloud box's token-gated
ingest**, reusing the existing pattern verbatim:

- The ingest is already a token-gated public-write endpoint (64-char bearer +
  name allowlist + size cap + JSON-validate + fail-closed, writes `<name>.json`)
  and already has a second route precedent, `/host-net-ingest/<host>`
  (EVIDENCE: `relay-metrics-ingest.py`; doc §"Site network hosts"). **Add a
  mirror `/canary-ingest/<name>` route + a `CANARY_ALLOWED_NAMES` allowlist** —
  same trust domain, same token, ~30 lines.
- Target the **cloud box** ingest (off-mesh), not the Thor ingest — both the
  canaries and the cloud box are off-mesh and talk over the public internet, so
  the whole loop survives a site/mesh outage.
- The public page renders: **"N/N canaries enrolled & moving · last checked Xs
  ago"** → *it's the mesh, not you* is FALSE (mesh up); **"canaries DOWN"** →
  *it's the mesh (or front door), not your laptop*. Staleness-graded: past 3×
  the interval → **NOT REPORTING · last N ago**, grey, never shown as current
  truth (EVIDENCE: reuse the honesty discipline the host-net band already
  implements, doc §"Site network hosts" UI rules).
- **Anti-abuse:** canaries hold real enrollment credentials, so scope them
  tightly — own group, no admin, ACL limited to the journey targets; a
  compromised canary must reveal nothing beyond a normal low-priv peer
  (INFERENCE). The public render is aggregate only: **never** publish canary
  mesh IPs, which service was round-tripped, or per-step internals.

**Composition with EnvHealthV1 (no duplication):**

- Once a canary IS in, its *internal* view is exactly an **EnvHealthV1
  producer** — it self-publishes mesh/net/fabric-domain health on
  `ops/<domain>/health/<scope>` like any node (EVIDENCE: env-health is
  reveal-only self-publish; mesh+net domains built + ZAGROS-live, CLAUDE.md +
  `environment-health-surface.md`). So the "am I healthy inside" half **reuses
  EnvHealthV1 — no new schema.**
- The **net-new** half is the *outside-in, off-mesh* publication of the coarse
  "did the newcomer journey succeed" verdict. EnvHealthV1 is on-bus and thus
  **mesh-dependent — unreadable when locked out**, which is precisely the gap.
  So: **one envelope shape, two transports** — the bus (for on-mesh readers,
  unchanged) AND a token-POST to the off-mesh cloud ingest (for the public
  surface). The external verdict is a projection/summary of the same journey,
  reusing EnvHealthV1's grading vocabulary + freshness discipline; it is **not a
  second health schema.**

---

## Existing vs new-build

**Existing (reuse as-is):**
- Blackbox prober of relay public IPs `:2333`/`:33073 ws_tcp` — `relay-health-probe.sh`. (EVIDENCE)
- Off-mesh cloud-resident prober/dashboard box, external vantage, outage-surviving — ADR 1066, `relay-health-cloud-deploy.yml`. (EVIDENCE)
- Token-gated ingest (bearer + name allowlist + per-name JSON) with a multi-route precedent (`/host-net-ingest`) — `relay-metrics-ingest.py`. (EVIDENCE)
- Public email-allowlist gate, fail-closed — doc §2026-07-20. (EVIDENCE)
- EnvHealthV1 self-publish + freshness/honesty grading — `environment-health-surface.md`. (EVIDENCE)
- netbird health primitives (`mgmClient.IsHealthy`, `signal.IsHealthy`) + goat-client enroll/connectivity-check. (EVIDENCE)

**New-build (all small):**
- Public read-only coarse "front-door" render (whitelist subset of existing prober JSON) on the cloud box.
- 2–3 off-site cloud canary peers running the newcomer-journey loop.
- `/canary-ingest/<name>` route + `CANARY_ALLOWED_NAMES` on the cloud ingest (mirror of `/host-net-ingest`).
- Off-mesh heartbeat emission from each canary (reuse token-POST).
- The combined "is it me or the network?" public page (front-door verdict + canary verdict).

**Net:** no new listener on the relays, no new mesh dependency, no new secret
class (reuses the ingest bearer token), no new health schema (reuses
EnvHealthV1). The only genuinely new standing assets are the 2–3 canary peers
and one public read-only render.
