# ADR 1139 — Response compression at the entrypoint, and goatnet-generic names for shared surfaces

- **Status:** Accepted
- **Date:** 2026-08-18
- **Deciders:** platform
- **Related:** ADR 1123 (client demand must never amplify into fabric load),
  ADR 1128 (mesh bandwidth attribution: two planes, never summed),
  ADR 1135 (peer and demand surface — the file is 1135, its own header
  says 1136; a known duplicate the id-uniqueness guard already tracks), ADR 1052 (vendor→org machine rename —
  the rename pattern this follows), ADR 1060 (universal deploy-rollback ledger),
  ADR 1110 (consistency foundation — signed IntentV1),
  surface-overload-protection.md §4/§4.1,
  nova-naming-generalization.md

## Context

Two problems surfaced from one screenshot. An operator opened the ZAGROS demand
page (ADR 1135), saw one peer accounting for the top row, and asked why. The
answer produced both decisions here.

**Nothing on the fleet compressed anything.** Measured on ZAGROS 2026-08-18 over
a 15-minute Traefik access-log window: **23,961 of 23,961 responses carried no
`Content-Encoding`**. In that window Traefik shipped 69.9 MB — 621 kbit/s — of
payloads that are almost entirely text. Compression ratios, each measured
against the live container rather than estimated:

| Surface | Payload | Raw | gzip | Ratio |
|---|---|---:|---:|---:|
| panoscope | `/api/snapshot?window_hours=6&aggressive=true` | 60,173 B | 5,928 B | **10.1×** |
| isitup | `/data/peerview.json` | 53,795 B | 10,559 B | 5.0× |
| gym | `/` | 57,019 B | 13,634 B | 4.1× |
| goat-docs | `/` | 22,535 B | 6,698 B | 3.3× |
| portal | `/` | 10,588 B | 3,679 B | 2.8× |

panoscope alone was 380 kbit/s of the 621, from a single browser tab: its
frontend polls `/api/snapshot` on a tier-gated cadence (LIVE 1500 ms / SLOW
3000 ms), so an open tab is a standing load for as long as it stays open. That
is the same query-on-request property ADR 1123 singles panoscope out for. Over
ten minutes one peer pulled 21.7 MB — roughly 70% of the whole attributable
browsing plane.

**The second problem was the name the operator read.** The row said
`nova-panoscope@file` on ZAGROS, a goatnet that is not nova. nova-a was the
first goatnet; everything was built there, named `nova-*`, then
generalized to other goatnets without renaming. By 2026-08-14 that had produced
three live conventions on one goatnet (`nova-<name>`, `<name>`, `<name>-zagros`),
and a decision doc had been sitting unanswered since.

## Decision

### 1. Response compression is universal, declared once, attached at the entrypoint

`goat-compress` is defined beside `goat-ratelimit` in both Traefik config paths
and attached to the `websecure` entrypoint as
`goat-ratelimit@file,goat-compress@file` (since the 2026-09-11 amendment of
ADR 1123: `goat-entrypoint-ceiling@file,goat-compress@file`). Ordering is
deliberate: a rejected request is never compressed.

Entrypoint rather than per-router, for the reason ADR 1123 already gives for
the rate limit — per-router attachment must be remembered once per service, and
the service everybody forgets is the one that needed it. It also covers
surfaces that do not exist yet.

**Exclusions are load-bearing, not tuning.** `text/event-stream` is excluded
because a compressor buffers to fill a window, and buffering an event stream
delays exactly the events it exists to deliver; the portal's SSE route is the
majority of what Traefik counts against `portal`. `application/grpc` is
Traefik's own default exclusion, named explicitly so a later edit to the list
cannot silently drop it — the signal server holds a gRPC stream per peer.
Already-compressed media types are excluded because re-compressing a PNG spends
CPU to add bytes.

Compression is negotiated, so a client whose `Accept-Encoding` Traefik cannot
satisfy receives the identity encoding. This cannot break a consumer.

### 2. Shared surfaces carry goatnet-generic names; instances carry instance names

The Traefik router, service and dynamic-config filename for any surface
deployed to more than one goatnet is the bare service name — no goatnet prefix,
no `-<site>` suffix. The goatnet is already carried by the router's `Host()`
rule and by the host it runs on; putting it in the name as well is what put a
nova name on ZAGROS.

The same applies to fleet-generic Ansible roles and target-parameterized
playbooks. Playbooks keep a deprecation shim at the old name, because operators
type them; roles do not, because playbooks reference roles and every in-repo
reference moves with the rename.

**Names that identify a real instance keep their prefix** — `nova-lan-*`,
`nova-twin`, `mgmt-nova-01-thor`, `terraform-nova/`, the `nova-bootstrap`
/ `-baseline` / `-pre-flight` instance playbooks, and all of `docs/adr`,
`docs/measurements`, `docs/findings`, which are the record of what happened.

### 3. A name that is a join key to signed or on-host state is not renameable in the repo

This is the operative constraint and the one that cost the most to learn. Three
classes must not move in a rename:

| Class | Example | Why |
|---|---|---|
| Signed declarations | `nova_zenoh_image` in `fleet-pins.yaml` and inside signed IntentV1 | Changing it makes the committed intent fail its own signature; re-signing needs the operator holding that goatnet's offline-CA age key |
| On-host directories | `zenoh_stack` → `/opt/<zenoh_stack>/` | The repo half moves; the directory on the host does not |
| Ledger / manifest keys | `ledger_component: nova-zenoh` → `/var/lib/goat/nova-zenoh/` | Renaming orphans every rollback record for that component (ADR 1060) |

Renaming any of these is a live-state migration with a cutover, not a rename.
Each is verified against the running host, not reasoned about: on ZAGROS,
`/opt/zagros-sandbox-zenoh` and `/var/lib/goat/nova-zenoh/` both exist.

## Consequences

**Load — deployed to ZAGROS 2026-08-18. Real client traffic dropped 71.4%.**

`traefik_service_responses_bytes_total` does **not** see compression: it counts
what the backend produced, not what Traefik shipped. This ADR named it as the
verification before that was checked. The instrument that sees compression is
the access log's `OriginContentSize` vs `DownstreamContentSize` — the field the
peer-demand page (ADR 1135) already reads.

Twenty minutes on ZAGROS, split by client, which is the whole story:

| Client | Requests | Origin | Wire | Saved |
|---|---:|---:|---:|---:|
| Mesh peers `100.64/10` — real users | 4,153 | 5.97 MB | 1.71 MB | **71.4%** |
| Container gateway `172.18.0.1` — our own probes | 27,769 | 21.53 MB | 21.87 MB | −1.6% |
| Combined | 31,922 | 27.51 MB | 23.58 MB | 14.3% |

The non-negotiating 87% is `zagros-obs-blackbox` running the `http_2xx` module,
which sets `method: GET` with no headers and therefore never sends
`Accept-Encoding` — Prometheus's blackbox exporter does not request compression.
It probes `/` rather than `/healthz` deliberately (§3: chasing a 302 graded
Ory's login page while wearing panoscope's label), so this is correct behaviour
for the canary and simply cannot benefit from the middleware.

Two consequences, and only one is about compression. The user-facing saving is
**71.4%**, and any fleet average understates it because the canary dominates
the request count. Separately, **the canary pulls whole page bodies to observe
a status code** — roughly 4.5 MB of gym and 3.6 MB of docs per twenty minutes —
a cost unaffected by compression and worth its own look.

**No metrics seam from the rename.** The goat-obs Prometheus config already
strips a `^nova-` prefix from `service` and `router` at ingest, so live ZAGROS was
already reporting bare labels. After the rename the label arrives bare instead
of being stripped — identical value either side, no gap. The relabel becomes a
no-op and can be dropped once no `nova-` series remain in the retention window.

**Two guards added, because two classes of this were caught by nothing.**
`check-shared-traefik-middlewares-agree.sh` requires that every middleware
defined in both config paths carries an identical definition — §4 of
surface-overload-protection.md previously claimed a mechanism that did not
exist, and `goat-compress` made it two such pairs.
`check-role-template-refs-resolve.sh` requires every `src: <x>.j2` in a role to
name a template that exists; a rename moved one half and left the other, which
parsed cleanly and would have failed on a mgmt host mid-deploy.

**A stale-matcher class remains uncovered.** `service-response-pressure.json`
matched `nova-signal@file|nova-portal@file` in 22 expressions against labels the
ingest relabel had already made unmatchable, so its stream panels matched
nothing and its percentile panels excluded nothing. Fixed here. No guard reads
Grafana dashboard JSON for labels a relabel has invalidated;
`check-stream-exclusion-matches-relabel.sh` covers netview's declared lists only.

**Deploy is not a config-only change.** The entrypoint flag lives in the Traefik
command set, so applying compression recreates Traefik once per mgmt host. The
route renames each remove their predecessor file in the same play that writes
the replacement, because Traefik's file provider reads the whole directory and
an orphan is a second router on the same `Host()` rule — a condition already
live on ZAGROS, where `netvis.yml` and `netvis-deploy.yml` both declared router
`netvis`.

## Alternatives considered

**Compress in each application.** Rejected: it is per-service work repeated
once per surface, with no single place to state the SSE exclusion, and it does
nothing for a surface whose author forgets.

**Per-router compress middleware.** Rejected for the reason ADR 1123 gives for
the rate limit, and because a route-local copy drifts from the shared
definition.

**Leave the names and relabel forever (option C of the decision doc).** The
relabel is already deployed and was the right bridge, but it solves the
dashboard problem without solving the naming problem: the metric reads `portal`
while the router is still `nova-portal`, so the next person debugging a route
finds no `portal` router anywhere.

**Dual-emit both router names through a transition (option B).** Rejected:
two routers for one service double-counts request totals until every consumer
picks one, and the consumers are the thing being fixed. A visible gap beats a
plausible wrong number — the same principle as ADR 1128's two planes.
