# Peer and demand surface — who is on the goatnet, and what they are asking of it

| Field | Value |
|---|---|
| Status | **Live on ZAGROS and zagros-prod** (2026-08-15). Two static pages + one host poller. |
| Scope | Population census, per-peer demand, browse-vs-core contention, baseline-vs-active cost, day and month shape. Per-goatnet, one vantage. |
| SoTs | Poller `ops/peerview/peerview-poll.py`; class declaration `ops/peerview/peerview-service-classes.yaml`; role vocabulary `ops/ansible/netbird_acl_taxonomy.yml` (read, never re-declared); role `ops/ansible/roles/peerview/`; play `ops/ansible/peerview-deploy.yml` |
| Decision | [ADR 1135](../adr/1135-peer-demand-surface-and-access-log-attribution.md) |
| Siblings | [mesh-bandwidth-attribution.md](mesh-bandwidth-attribution.md) (ADR 1128) — the same host from the pipe's side. surface-overload-protection.md (ADR 1123) — why a poller and not a browser fetch. mesh-and-ory-roles.md — the role vocabulary. identity-grammar.md — why affiliation is absent here. |

## The question

The bandwidth surfaces answer *how full is the pipe and what is filling it*.
They deliberately do not answer *who*. This pair answers the population and
demand half:

1. How many peers are there, how many are online, in which roles and scopes?
2. What are they running — client version, operating system, direct or relayed?
3. What are they asking of which services, and is anyone an outlier?
4. Is peer usage of the browsing surfaces competing with the core services?
5. What does a peer cost by merely existing, versus by being used?
6. How does all of that move over a day, and over months?

## Two pages

| Page | Answers | Served at |
|---|---|---|
| **Peers & People** (`peers.html`) | 1, 2 — population, roles, scopes, people-vs-machines, client and OS spread, path class, per-peer posture | `https://isitup.<site>.<zone>/data/peers.html` |
| **Demand & Contention** (`demand.html`) | 3, 4, 5, 6 — per-peer service demand, outliers, the contention test, cost split, day and month shape | `https://isitup.<site>.<zone>/data/demand.html` |

Both read one document, `peerview.json`, written by a host timer. Neither page
queries anything: the browser never touches the NetBird API, the access log or
Prometheus, so viewers cost nothing regardless of how many there are
(ADR 1123). They are files in the isitup nginx `data/` mount — the same seam
netview's pages use, chosen because isitup bind-mounts individual files for the
rest of its docroot and a new file beside them would never appear inside the
container.

## Four sources, each able to fail on its own

This matters more than it sounds, because **the two goatnets do not carry the
same telemetry and neither is a subset of the other** (measured 2026-08-15):

| Source | What it gives | ZAGROS | zagros-prod |
|---|---|---|---|
| NetBird management API `/api/peers`, `/api/groups` | The whole census: totals, connected, roles, scopes, OS, client version, enrolment dates, per-peer local flags | yes | yes |
| Traefik JSON access log, read from the container | Per-peer × per-service demand, joined on the mesh address | yes | yes |
| Prometheus | Per-peer transport class and wire bytes (`goat_net_peer_*`), path quality (`goat_mesh_*`) | `goat_net_*` only | `goat_mesh_*` only — and on the observability host at `:9090`, not loopback `:9192` |
| The poller's own ledger | Hour-of-day and month-over-month shape | yes | yes |

Every source records whether it answered, and what goes dark without it. Both
pages carry that table. A source that could not be reached blanks its panels
and says so — it never renders as zero.

## The join that makes per-peer demand possible

Traefik's Prometheus output carries a `service` label and **no client identity
whatsoever**, so from metrics alone "which peer is leaning on panoscope" is
unanswerable. Traefik's JSON access log carries `ClientHost`, and on the mesh
entrypoint that is the peer's own `100.64/10` address, which is the `ip` field
on the management API's peer record. One join, and per-peer service demand
exists.

**What the join cannot see, stated on the page rather than left to be
rediscovered:** traffic arriving through the relay ingress tunnel
(relay-ingress-dispatch.md, ADR 1085) reaches
Traefik from the container bridge, so every such request shares one source
address and is unattributable at this vantage. Measured 2026-08-15 over a
five-minute window: on ZAGROS 12,783 of 13,160 logged requests came from the
gateway address; on zagros-prod 1,826 of 1,828. That is nearly all of the signal
and management plane. The attributable slice is the *browsing* plane — which is
the plane these pages are about — and the fraction is printed beside every
figure rather than quietly divided away.

## Three filters applied before anything is counted

**Synthetic probes are removed.** Health checks hit the same services real
users do and only the request path separates them. Measured on ZAGROS: netvis,
microscope, flashlight, stethoscope, compliance and grafana each logged exactly
60 requests per 15 minutes, and every one was `GET /healthz` from the blackbox
exporter. Four probes a minute forever is a monitoring artefact, and counting
it made every unused surface read as evenly popular. Probe paths are declared
in `peerview-service-classes.yaml`.

**Stream lifetimes are excluded from every duration statistic.** Traefik times
a connection open-to-close, so one held-open gRPC stream records as a single
enormous "request". Before this exclusion existed, the contention test on
zagros-prod reported a core-service p95 of **675,687 ms** — eleven minutes — and
it was one signal stream, not anything anybody waited for. Stream-carrying
services are declared in the same file; their requests and bytes are still
counted everywhere, only their durations are dropped. This is the same
exclusion netview's percentiles carry, and **the two lists are one fact and
must be changed together**.

**Requests with no backend get their own bucket.** A record Traefik handled
without routing to a service is real traffic and is not a service. Pooling it
with services whose class is merely undeclared would hide both.

## Roles, scopes, and the things that are neither

Role and scope are resolved against `netbird_acl_taxonomy.yml`, copied to the
host at deploy rather than re-declared, so the roles this page counts are the
roles the ACL declares.

- **Longest-prefix match, not split-on-first-dash.** `ZAGROS-PROD-PLATFORM-ADMIN`
  split naively yields scope `ZAGROS` and role `PROD-PLATFORM-ADMIN` — a scope
  that exists and a role that does not, on the production goatnet specifically.
- **Both taxonomy keys are read.** `netbird_acl_taxonomy` holds the persistent
  goatnets; `netbird_exercises` holds activity and forward-site scopes stamped
  onto a host goatnet. Reading only
  the first loses every peer in those scopes.
- **One role per peer, the most privileged held.** Peers routinely hold several
  groups (DEVELOPER + OBSERVER is the standard full-participation pair), so a
  chart counting memberships would total more than the population.
- **`P2P` is a capability, not a role** (mesh-and-ory-roles.md)
  and is counted separately, so graduation is visible without polluting the
  role census.
- **A scope is not a place.** It says which admin set may act on the peer and
  nothing about where the machine is. The page says so under the chart.

## The person axis, and what is deliberately absent

`user_id` is **empty on 100% of peers on both goatnets**, because every peer is enrolled with a
setup key rather than by a user login. The management API therefore carries no
peer→person edge at all.

What is left is the mint name, `peer-<handle>-<env>-<YYYY-MM-DD>-<HOSTNAME>`.
A peer whose name matches carries a person's handle; one that does not is a
machine. The handle is used **only as an opaque key for "these peers belong to
the same person"**, and is rendered verbatim. Nothing reads an organisation, an
employer or a place out of it — handles frequently look as though they encode
one, and they do not. Carrying a handle is **not** the same as having a declared
owner; that is a separate resolution, below.

**Affiliation is not shown, and the panel says why.** Affiliation is an
identity-plane attribute and is deliberately never a mesh group
(identity-grammar.md), so the mesh census cannot carry
it, and the peer→person link that would resolve it does not exist. Reported
absent rather than inferred.

## Who answers for a peer

Both pages carry an **Owner** column, and every owner is one click from the
identity it resolves to. The operator ask this closes: a row on a dashboard is
only actionable if you can get from it to whoever is answerable — whether the
peer is a person's laptop, a machine, or a piece of substrate.

**This surface does not invent its own answer.** peer → person is a solved,
deployed problem with one resolver — the portal's access graph, `POST
/api/access/peers` ([peer-identity-resolution.md](peer-identity-resolution.md),
Option B) — which `goat who`, panoscope `/who`, netvis and the relay-health
card already consume. A fifth re-derivation here would be a fifth chance to
disagree with the other four about who owns a machine. The call goes through the
shared Python client [`ops/identity/peerowner.py`](../../ops/identity/peerowner.py),
once per poll for the whole population (the batch endpoint exists for exactly
that), and this surface adopts the resolver's four-state vocabulary verbatim:

| Kind | Means | Renders as |
|---|---|---|
| `registry` | roster handle matched **and** a live Ory identity shares its email-hash | the owner, linked to that person in the id-admin-console |
| `name` | roster handle matched; no live identity did. Org known, person not | a **lead**, chipped as such |
| `team` | infra-class role — the scope's platform admins answer for it | the mesh group, chipped `team` |
| `custody` | a machine the portal enrolled (`machine-<handle>`); the resolver names the custodian who enrolled it | the custodian, chipped `custodian` |
| `unmatched` | no declared identity matched. Split by what the name says (`shape`): `person` — the onboarding grammar with a handle the roster lacks, typically a person onboarded after the roster was last regenerated; `machine` — the portal's machine form with no custodian found; `none` — no principal in the name at all | "not in roster" / "machine, no custodian" / "not onboarded", never a name |
| `unread` | we could not ask | "not checked" — a statement about this host, not about the peer |

**Three things the shared resolver does not answer**, supplied here and filed as
gaps in its own doc rather than treated as this surface's private business:

- **`custody`** — a peer minted for a machine resolves to the *machine's* handle,
  which cannot answer for anything. The person who can is the custodian who
  enrolled it ([ADR 1096](../adr/1096-delegated-machine-enrollment.md)). For a
  machine the portal enrolled, the resolver now names the custodian itself and
  this surface takes that answer. For a system declared in an operator manifest
  the custody edge is in the registry file, and a row for it — a `peer-` name,
  or a `machine-<handle>` name when the resolver cannot be asked — is re-pointed
  here. Either way the row names the custodian and says so.
- **`team`** — infrastructure answers to a team, and the mesh already says which
  peers those are. A peer holding an operator-owned node class (`INFRA`,
  `BACKEND-INFRA`, `EXTERNAL-INFRA`, `BREAKGLASS`) is attributed to the
  platform-admin group
  of **its own scope**, so an exercise node names that exercise's admins rather
  than the host goatnet's. **Derived from the role, never a list of hostnames**
  — a hand-kept glob list goes stale the day someone adds a host, while the role
  is something an admin must set for the peer to work at all. The group is named
  only if the management plane declares it; membership is deliberately not
  required, because an empty scope-local `PLATFORM-ADMIN` is the normal state at
  standup and means nobody has been appointed yet.

  Only infra-class roles qualify. Peers in the same
  no-principal-in-the-name set that hold a non-infra role are left unmatched:
  handing them to the platform admins would manufacture an owner rather than
  find one. **The surface does not say what any of those machines are** — see
  the caveat below.

  `BREAKGLASS` qualifies on accountability rather than on being
  "infrastructure": the taxonomy makes it non-delegable — *the entitlement that
  lets a site admin enroll a switch must not let them enroll their own way into
  the rescue path* (ADR 1114)
  — so an operator-enrolled, non-delegable node is the platform admins' by
  construction. **`NETWORK` is deliberately absent**: bearer gear IS delegable,
  driven by a site-scoped `network_admin` entitlement
  ([ADR 1113](../adr/1113-network-node-class-and-delegated-network-admin.md)),
  so its accountable group is not the platform admins. Gear classes are
  custodian-owned ([ADR 1096](../adr/1096-delegated-machine-enrollment.md)) and
  stay out for the same reason.

  **A group says what a peer is AUTHORIZED for, never what it is.** That is the
  whole basis of this tier and also its limit, and the limit is not theoretical:
  a host that gets repurposed keeps the groups it was enrolled with, so its
  labels describe the job it used to do. The attribution is still the right one
  to publish — the group is a live authorization edge, so whoever administers
  that scope genuinely can act on the peer — but it is only as current as the
  ACL, and a stale group means the wrong team holds admin over the machine,
  which is a finding rather than an answer. Nothing here should be read as, or
  written as, a claim about a machine's purpose or provenance. `ZAGROS-MOON` on a
  peer means the moon-pod scope may act on it; it does not mean the box is a
  moon pod, and prose in this repo has made exactly that error.
- **`declared`** — substrate (the mgmt host, a relay, a kiosk box) carries no
  onboarding name and never will, so `unmatched` is correct and permanent.
  `peerview_declared_owners` maps `<peer-name-or-glob>: <handle>`. A **fallback
  only**: it never overrides a resolved answer. Empty by default, because an
  undeclared substrate peer reading "not onboarded" is true and filling it in is
  a decision somebody makes.

**When the resolver cannot be asked**, the same registry file the resolver reads
is on disk here, so the handle join still runs — the loop is `belongs_to` from
the peer-name grammar SoT ([`ops/identity/peer-name-grammar.yaml`](../../ops/identity/peer-name-grammar.yaml)),
handle + org tag + goatnet all anchored. **A local answer can only ever claim
`name`.** `registry` requires reconciling against a *live* identity, which only
the resolver does; a local answer claiming it would be the exact
recorded-vs-inferred indistinguishability [ADR 1051](../adr/1051-identity-descriptor-standard.md)
exists to prevent.

Two counts are published, not one: `resolved` (reaches a named identity by any
route) and `recorded` (the resolver reconciled it against a live identity). The
difference between them is how much of the column is a record and how much is a
lead.

**Link targets.** A `registry` row links to that person in the id-admin-console
(`<base>/users/<ory-id>`) — the same deep-link the relay-health card uses, so
two surfaces land an operator in the same place. Everything else links to the
identity roster by handle (panoscope `/who?identity=<handle>`), which is all a
handle can honestly address. Each is drawn only where its base URL is
configured; a goatnet with neither still gets the column as plain text rather
than links to nowhere.

**No PII lands in the served document.** The resolver is operator-scoped and
returns the person's email and display name; the client drops both by default,
so these pages keep rendering handles.

## Baseline versus active cost

The split an operator plans capacity with:

- **Baseline** — what a connected peer costs by existing: signal, management,
  auth. It scales with how many peers are *connected*.
- **Active** — what a person costs by using something. It scales with usage,
  not headcount.

They are separated by declared service class, not by guesswork, and the
declaration carries its reasoning in the same file the matcher reads.

## The contention test, and its limits

The question the surface exists for — *does browsing degrade the core?* — is
answered with the only test the data supports:

> Rank this window's minutes by browsing request count. Compare core-service
> p95 in the busiest third against the quietest third.

It is a **within-window association**. It does not randomise, it controls for
nothing, and both thirds come from the same few minutes. The page says that in
the same panel as the result. Guards that stop it producing a number that means
nothing:

- at least six minutes carrying a *timed* core request;
- at least thirty timed core requests **on each side** — zagros-prod's first run
  compared 635 against 3 and printed the result;
- a verdict is refused outright when the difference is small on a sub-50 ms
  baseline, because a sub-millisecond separation on a five-minute sample is
  scheduling noise, and announcing it as contention trains people to ignore the
  panel;
- a note fires when the two thirds barely differ in browsing load, because a
  window with no contrast could not have detected contention whatever the
  verdict says.

Settling the question properly is a load test ([zenoh-loadtest.md](zenoh-loadtest.md)),
and the page says so.

## Day and month shape

**Prometheus retains 15 days** (`--storage.tsdb.retention.time=15d`). A
month-over-month line drawn from it would be an extrapolation wearing a chart's
clothes, so none is drawn from it.

- **Population growth needs no history at all.** Every peer carries
  `created_at`, so enrolments-by-month is complete from the poller's first run.
  It counts peers *still enrolled* — a standing-population history, not a gross
  intake history — and says so.
- **Connected count and demand rate have no such record**, so the poller keeps
  its own ledger: one row per run, folded to hourly after 8 days and daily
  after 40, bounded at 4,000 rows. The pages print how much history the ledger
  holds, and hours with no sample are hatched rather than drawn at zero.

The ledger lives in `/var/lib/peerview/`, mode 0600 — outside the served
directory, because it is the poller's state and not a page asset.

## Costs and constraints

- **The token is admin-scoped.** Listing peers has no read-only role on this
  NetBird build. It is written to a 0600 env file on the host and never onto a
  command line where `ps` would show it.
- **One vantage.** Transport class is what netbird reports *from the host
  running the poller*. Two hosts would legitimately disagree and neither would
  be wrong; the page says so under the section heading.
- **Log volume.** ZAGROS logs ~13 MB per five-minute window, zagros-prod ~4 MB. The
  read is capped at 48 MiB and the document reports a truncated window rather
  than presenting a partial count as a total. The unit runs `Nice=10` and
  `IOSchedulingClass=idle` — a reading surface must never be the reason the
  thing it reads gets slower.
- **The surfaces are admin-gated** by the same oauth2-proxy fence as the rest of
  isitup. Peer names and mint handles appear on them, exactly as they do on
  netvis and panoscope.

## Known gaps

| Gap | Why it is open |
|---|---|
| Per-peer attribution stops at the ingress tunnel | A property of the relay ingress path (ADR 1085), not something this surface can fix. The fraction is reported. |
| Affiliation | No peer→person link exists (`user_id` empty fleet-wide). Closing it means either user-login enrolment or an identity-plane join. |
| `goat_net_*` on zagros-prod, `goat_mesh_*` on ZAGROS | Measurement parity between the goatnets, not a defect in this surface. |
| Contention is an association, never a cause | Only a load test settles it. |
| The outlier rule ranks baseline traffic beside browsing | A peer's signal exchange is baseline by this document's own class table, and it dominates any browsing figure, so a machine negotiating connections and a person using a surface compete for the same "heavy" flag. Measured 2026-09-11: an externally-hosted service peer was flagged heavy on 896 signal requests, and a panoscope tab whose session had expired was flagged on 352 refused ones (F-523). Ranking on browse-class requests only, and on requests that were served rather than refused, is the fix. |
| Grafana panels | These are static pages, deliberately (ADR 1123). A Grafana view over the same access log would need the log in a queryable store, which does not exist on either goatnet. |

## Running it

```bash
ops/scripts/env-door.sh zagros &
cd ops/ansible
OBJC_DISABLE_INITIALIZE_FORK_SAFETY=YES ../scripts/ansible-preflight.sh \
  -i inventory/mgmt-hosts.ini -i inventory/doors-groupvars.ini \
  peerview-deploy.yml -e target=zagros_hosts
```

Unit tests: `python3 ops/peerview/test_peerview.py` (30 cases, covering every
derivation where a wrong answer would be plausible rather than obviously
broken).
