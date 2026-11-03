# ADR 1136 — The peer and demand surface, and the access log as its attribution source

- **Status:** Accepted
- **Date:** 2026-08-15
- **Deciders:** platform
- **Related:** ADR 1128 (mesh bandwidth attribution: two planes, never summed),
  ADR 1123 (client demand must not amplify), ADR 1085 (relay ingress dispatch),
  ADR 1122 (scale envelope: systems vs peers), ADR 1043 (identity grammar),
  ADR 1116 (gear classes), F-333 (signal federation forwarding),
  [peer-demand-surface.md](../design/peer-demand-surface.md),
  [mesh-bandwidth-attribution.md](../design/mesh-bandwidth-attribution.md),
  mesh-and-ory-roles.md

## Context

ADR 1128 answered *how full is this host's pipe and what category of work is
filling it*. It deliberately did not answer **who**. The population and demand
half was unanswered by anything on the fleet, and the specific questions have
operational owners: how many peers exist and how many are online; which roles
and scopes; what clients and operating systems; direct or relayed; who is
applying the most pressure to which service; whether browsing surfaces compete
with signal, management and auth; what a peer costs by merely existing versus
by being used; and how any of that moves across a day or a month.

Four facts, measured 2026-08-15 rather than assumed, shaped every decision
below.

**1. Traefik's metrics carry no client identity at all.** The Prometheus output
has a `service` label and nothing about who asked. "Which peer is leaning on
panoscope" is not a hard query against those metrics — it is an impossible one.

**2. The management API carries no peer→person edge.** `user_id` is empty on
**every** peer on both goatnets measured, because every peer
is enrolled with a setup key rather than by a user login. Any design assuming
the API can group peers by person is designing against a field that is
uniformly blank.

**3. The two goatnets carry different telemetry and neither is a subset of the
other.** One had Traefik metrics, `goat_net_*` attribution and blackbox, and no
`goat_mesh_*` path quality. The other had `goat_mesh_*` and none of the other
three — and its Prometheus is on a separate observability host, not on the
mgmt host's loopback. A surface that assumes one shape lies on the
other goatnet.

**4. Prometheus retains 15 days.** A month-over-month trajectory drawn from it
is an extrapolation wearing a chart's clothes.

## Decision

**D1 — The Traefik JSON access log is a first-class attribution source, joined
on the mesh address.** `ClientHost` on the mesh entrypoint is the peer's own
`100.64/10` address, which is the `ip` field on the management API's peer
record. One join, and per-peer × per-service demand exists. This is the only
source that works on **both** goatnets, which is why the design rests on it
rather than on metrics.

**D2 — The unattributable fraction is printed, never divided away.** Requests
arriving through the relay ingress tunnel (ADR 1085) reach Traefik from the
container bridge and share one source address. Measured over five minutes,
that was the large majority of requests on both goatnets. The traceable
slice is the browsing plane — which is the plane the questions are about — but
the share appears beside every figure, because a per-peer chart computed over
a few percent of traffic and presented without that number is a chart about something
else.

**D3 — Synthetic traffic and stream lifetimes are removed before anything is
counted, by declaration.** Both would otherwise produce numbers that look
measured and are artefacts:
- Health probes: netvis, microscope, flashlight, stethoscope, compliance and
  grafana each logged exactly 60 requests per 15 minutes, every one a
  blackbox `GET /healthz`. Counting them made every unused surface read as
  evenly popular.
- Stream lifetimes: Traefik times a connection open-to-close, so one held-open
  gRPC stream is a single enormous "request". Before the exclusion existed, the
  contention test reported a core-service p95 of **over 675,000 ms** —
  eleven minutes of one signal stream, reported as a response time. This is the
  same exclusion netview's percentiles carry; the two lists are one fact.

Both live in `ops/peerview/peerview-service-classes.yaml` with their reasoning,
because "is panoscope a browsing surface" is a policy question with an owner and
must be answerable by reading one file rather than by reading a regex in a
poller.

**D4 — A poller writing one document, not a queryable surface.** Identical
reasoning to netview and to ADR 1123: the pages are static files behind
oauth2-proxy in the isitup `data/` mount; a browser fetch would put
admin-token demand on a path per viewer and would mean exposing a loopback-only
Prometheus. One timer, one document, every viewer reads the same bytes. The
unit runs `Nice=10` / `IOSchedulingClass=idle` — a reading surface must never be
why the thing it reads gets slower.

**D5 — Every source degrades independently and says what goes dark.** Four
sources, each recording whether it answered, rendered as a table on both pages.
`reachable` and `has_data` are separate facts and must not be collapsed: a
Prometheus can be healthy and carry none of the series the transport section
needs, and a source reported green with every panel under it null is a lie. A source that could not be reached blanks
its panels; a source that answered with nothing says exactly that.

**D6 — Role and scope resolve against the declared taxonomy, copied to the host
rather than re-declared.** Longest-prefix match, because splitting on the first
dash yields scope `ZAGROS` and role `PROD-PLATFORM-ADMIN` on a goatnet named
`zagros-prod` — a scope that exists and a role that does not. Both taxonomy keys
are read, because `netbird_exercises` holds exercise scopes and dropping it
loses every peer at a scope that may be the one carrying gear. One role per peer, most privileged held,
since DEVELOPER + OBSERVER is the standard pair and counting memberships would
total more than the population. `P2P` is counted as a capability, never a role.
**A scope is an authorization boundary and not a place**, and the page says so
under the chart.

**D7 — The person axis is the mint handle, used as an opaque key and nothing
more.** Given (2), the name is what is left: `peer-<handle>-<env>-<date>-<host>`.
It groups peers by person and is rendered verbatim. Nothing reads an
organisation, employer or place out of it. **Affiliation is reported absent with
its reason**, because affiliation is an identity-plane attribute and
deliberately never a mesh group (ADR 1043) — inferring one from a handle would
be the exact failure the identity grammar exists to prevent.

**D8 — Cost is split into baseline and active by declared class, and the
projection is labelled a floor.** Baseline is what a connected peer costs by
existing and scales with connected headcount; active is what a person costs by
using something. First light: baseline was well over nine-tenths of ingress
bytes on both goatnets, at tens to hundreds of kbit/s per connected peer. The
projection to larger fleets is linear in connected peers,
which is what the baseline definition licenses and no more — signal federation
forwarding is super-linear and is explicitly outside the line, so the
projection is a floor and says so.

**D9 — Contention is answered as a within-window association, with guards that
let it refuse.** Rank the window's minutes by browsing load; compare
core-service p95 in the busiest third against the quietest. It does not
randomise and controls for nothing, and the page states that beside the result.
It refuses to produce a verdict when there are fewer than six minutes carrying a
timed core request, when either side holds fewer than thirty timed core requests
(a first run compared hundreds against three), or when the difference is small
on a sub-50 ms baseline — a sub-millisecond separation on a five-minute sample
is scheduling noise, and announcing it as contention is how a panel teaches
people to ignore it. Settling the question is a load test, and the page says so.

**D10 — Day and month shape come from the poller's own ledger, never from
Prometheus.** Retention is 15 days. Population growth is the exception and needs
no ledger at all — every peer carries `created_at`, so enrolments-by-month is
complete from the first run (and counts peers *still enrolled*, a standing
population history rather than a gross intake history). Connected count and
demand rate have no such record, so the poller appends one row per run, folds to
hourly after 8 days and daily after 40, bounded at 4,000 rows, in
`/var/lib/peerview/` mode 0600 — outside the served directory, because it is
state and not a page asset. The pages print how much history exists and hatch
hours with no sample rather than drawing them at zero.

## Consequences

- Per-peer service demand exists on both goatnets for the first time, over the
  browsing plane, with its coverage fraction stated.
- **An admin-scoped NetBird token now lives on each mgmt host**, in a 0600 env
  file, never on a command line. Listing peers has no read-only role on this
  build; that is a real cost of the surface and is stated rather than
  discovered when someone tries to narrow it.
- The surface reads the Traefik container log every interval. Volume is bounded
  and a truncated window is reported as truncated.
- The stream-service exclusion is duplicated in two files by necessity
  (netview's poller and peerview's class map). They are one fact and must change
  together; both say so at the definition.
- The first two runs surfaced two facts worth their own attention: baseline
  dominates ingress, and the per-connected-peer baseline can differ severalfold
  between goatnets.

## Alternatives rejected

- **Ship it on Traefik metrics alone.** They carry no client identity, so the
  central question is unanswerable. This is not a preference.
- **Ship the access log to a queryable store and build Grafana panels.** No such
  store exists on either goatnet, and adding one to answer a question a bounded
  file read already answers is a new data path for no gain. A Grafana view stays
  possible if a store ever lands.
- **Group peers by `user_id`.** Empty on 100% of peers on both goatnets.
- **Derive affiliation from the handle.** The handles look as though they encode
  one. Doing this would put inferred organisation on an operator surface and
  contradict ADR 1043 outright.
- **A queryable service instead of a poller.** ADR 1123 bounded exactly this;
  panoscope remains the only surface that queries on request.
- **One page instead of two.** The census and the demand questions have
  different audiences and different refresh needs, and a single page carrying
  both scrolled past the point where anything is scannable.
- **Draw a month-over-month line from Prometheus.** Fifteen days of retention
  cannot produce one honestly.
- **Report a contention verdict on whatever the window holds.** The refusal
  guards exist because a first run produced a confident number from
  three requests and an eleven-minute stream.
