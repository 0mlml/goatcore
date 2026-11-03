# Fleet reconciliation join — one row per declared fact, three answers on it

| Field | Value |
|---|---|
| Status | **RJ-1 + RJ-2 BUILT (repo-side, 2026-08-12); RJ-3..RJ-6 designed, not built.** Anchor [ADR 1125](../adr/1125-fleet-reconciliation-join-and-graduation-sets.md) (Proposed). Coverage is a typed field on every projected row and on the wire, resolved from `ops/consistency/grammar.yaml`, rendered on the node grid and served in `aggregate-v1`; graduation sets refuse a lone member and are CI-gated (C7). 128 Rust tests + the readiness suite green. **Not deployed** — the meter and console images carry it only after a pin bump. Two prerequisites landed first in the same track: cross-env declaration scoping + `until:` expiry, and observed-generation labelling (`consistency-foundation.md` §7.8, amendment 2026-08-12). |
| Anchor ADR | [ADR 1125](../adr/1125-fleet-reconciliation-join-and-graduation-sets.md) |
| Composes with | `consistency-foundation.md` (ADR 1110 — the facts this joins) · [`release-graduation.md`](release-graduation.md) (ADR 1120 — the unit this changes) · [`universal-deploy-rollback.md`](universal-deploy-rollback.md) (ADR 1060 — observed refs) · [`environment-health-surface.md`](environment-health-surface.md) (ADR 1036 — the health axis) · `environment-parity.md` §5 · [`fleet-management.md`](fleet-management.md) (ADR 1109 — convergence is pulled) |
| Tracking | `fleet-reconciliation-join` in `in-flight.md` |
| Realised for service presence | [ADR 1206](../adr/1206-goatnet-declared-end-state.md), [`goatnet-end-state.md`](goatnet-end-state.md): one derived row per site service, graded deployed/active/healthy/functional with its edges, and coverage (UNOBSERVED) apart from sync and health; built 2026-10-29 |
| Origin | Operator, 2026-08-12: *"it all seems kind of loosely connected. is there some kind of example of excellence and best practices that we can compare ourselves to"* — asked while reading two undeclared-drift rows on `consistency.zagros-prod.goat.example`, one of which turned out to be F-324. |

> **60-second summary:** Five surfaces answer five parts of one question and key
> five different objects, so a human does the join. This doc defines the join as
> a **derived row** — `(subject, name, goatnet, member)` — carrying **sync**
> (declared vs running), **health** (running vs working) and **coverage** (is
> anything watching) as three separate answers, computed at read time through
> the existing grading library, stored nowhere, deciding nothing. Capabilities
> gain an observed side or are honestly marked `no-instrument`. Graduation gains
> a **graduation set** so a train of components moves together. The existing
> surfaces become lenses over the one row rather than five systems.

## 1. The feeling, made specific

The complaint was that the relay-health dashboard, the consistency console and
the cross-fleet CI/CD strategy are "loosely connected". They are. Here is the
join key each one uses:

| Surface | Object it keys on | The part of the question it answers |
|---|---|---|
| consistency console (ADR 1110) | `(subject, member)` from committed intent | what is *declared* |
| deploy ledger (ADR 1060) | `(component, host, ref, ts)` on the host | what actually *ran* |
| release records + ladder (ADR 1120) | `(component, candidate_ref, rung)` | what is *allowed* to run |
| `EnvHealthV1` (ADR 1036) | `(domain, scope)` on the fabric | whether it *works* |
| relay-health (doc) | a relay's public address | whether the relays work |

Every one is individually well built and individually honest. None of them
shares an identity with the others, so no surface can answer *"is zagros-prod
where it is supposed to be"* without a human opening five tabs. **The
looseness is not a UI problem and will not be fixed by a sixth dashboard.** It
is a missing join key and a missing state.

### 1.1 What the exemplars actually contribute

Three ideas, each one sentence, each already half-present here:

- **Argo CD / Flux.** Every application carries **two orthogonal statuses**
  computed against the *same object*: *Sync* (declared vs live) and *Health*
  (live vs working). The load-bearing part is "same object", not the UI. We have
  both facts on different keys.
- **Kargo.** The promotion unit is an **immutable set** of pinned artifacts that
  moves through stages together. Our promotion unit is one component's pin,
  which is why a descendant commit graduated another session's console train on
  2026-08-12 — structural, not careless.
- **Kubernetes.** `observedGeneration` on every status: a verdict is only
  readable next to the spec revision it was computed from. Landed for the
  cross-env panel 2026-08-12; generalized here.

What we deliberately do **not** take is the machinery. This is an
Ansible-converged Compose fleet with a brokerless fabric and island-mode
requirements; a Kubernetes control plane to obtain a status model is far past
the complexity budget, and a central reconciler contradicts ADR 1109. The
alternatives section of ADR 1125 records that call.

### 1.2 The state nobody had

Argo has *Synced/OutOfSync* and *Healthy/Degraded/Missing/Unknown*. We had
neither pair, and we also lacked a third thing Argo gets for free by having a
reconciler: **is anything watching this at all?**

F-324 is what that gap costs. `node_net_health_enabled` and
`host_net_publish_enabled` were declared in group_vars, described in three
files, and observed by nothing — `project.rs` grades components, not
capabilities, so a declared capability is unfalsifiable by construction. No
surface was wrong; no surface was *asked*.

`project.rs` already reports `components_observed` of `components_governed`,
which is honest. But a denominator is a number you read past. A **row** with a
remediation pointer is a thing you fix.

## 2. The object

One derived row. Key `(subject, name, goatnet, member)` — IntentV1's own key
plus the goatnet, so the row spans envs.

```
subject      component | capability
name         portal | node_net_health_enabled | …
goatnet      zagros | zagros_prod
member       zagros_hosts | zagros_b_hosts | zagros_prod_hosts
─────────────────────────────────────────────────────────────
declared     the IntentV1 cell, verbatim (value + state + declaration)
sync         GradedRow — declared vs observed        (six-grade grammar)
health       GradedRow — observed vs working         (six-grade grammar)
coverage     observed | no-instrument | silent | unreported-scope
generation   the intent serial + content_hash this row was graded against
```

Three properties are non-negotiable and are inherited, not invented:

- **Derived and read-time.** Stored nowhere. A stored verdict can be stale
  without saying so, and stale stored claims are the F-324 class exactly.
- **Grades through the shared library.** No new grade, no new remediation kind,
  no new sentence generator — ADR 1110 D2. A surface may choose what it can see
  and how it renders; it may not grade.
- **Decides nothing.** ADR 1109: convergence is pulled. The row may render,
  alert and point at a remediation. No node consults it. It never appears in an
  enforcement path (the ADR 1065 stance).

### 2.1 Sync and health stay apart

| | `health: ok` | `health: degraded` |
|---|---|---|
| **`sync: match`** | the boring, correct case | right version, broken service — the health domain's own pointer |
| **`sync: SKEW`** | wrong version, working fine — `reconverge` | wrong version AND broken — both pointers, and the order matters |

A single averaged colour would make the top-right and bottom-left cells look
alike. They have different remediations, so they get different fields. The band
may collapse them for display (worst-wins, as it already does), but the row
never loses the pair.

## 3. Capabilities gain an observed side

The largest piece, and the one that closes F-324's class.

`ops/pins/parity-capabilities.yaml` gains an optional `observe:` block per
capability, declaring how a node proves it:

```yaml
- var: node_net_health_enabled
  why: >-
    …existing reason…
  observe:
    kind: container          # container | unit | path | topic
    name: node-net-health

- var: host_net_publish_enabled
  why: …
  observe:
    kind: unit
    name: host-net-publish.timer
```

**The node reports a fact, never a verdict** — the CF-4 `RenderStateV1` pattern
reused whole. The collector answers "is there a container named X / is unit Y
active / does path Z exist / has topic T been published within the window", and
publishes that observation. The comparison happens at read time in the shared
library, where every other comparison happens.

Two rules that are not optional:

- **A probe MUST be pinned by a test that reads the deploying role's own
  template**, not one that restates the name. This is
  F-320's
  shape: a check wired to something nobody sets is indistinguishable from a
  working check, and a unit test written from the same wrong assumption passes.
  For `kind: container` the test reads the role's compose template; for
  `kind: unit`, the role's systemd unit name.
- **A probe that cannot run is `unreadable`, never `false`.** "I could not look"
  and "I looked and it is not there" are different facts, and the grammar
  already keeps them apart.

A capability with no `observe:` block is **not a failure**. It is
`coverage: no-instrument`, which is countable and has a remediation
(`instrument`). Most of the ~40 `*_enabled` keys in `zagros_hosts` are per-service
micro-toggles that will never earn a probe; the selection bar in
`parity-capabilities.yaml`'s own header already governs which keys are parity
questions at all, and `observe:` is a strictly smaller set again.

## 4. Coverage — the four states

| `coverage` | Means | Remediation kind | Today it looks like |
|---|---|---|---|
| `observed` | a node reported this fact | — | in the numerator |
| `no-instrument` | nothing declares how to see it — `ledger_component: null`, or no `observe:` block | `instrument` | missing from the numerator |
| `silent` | an instrument exists; the node has not reported | `reconverge` | missing from the numerator |
| `unreported-scope` | intent names this member and no node claims it | `enrol` | missing entirely |

`no-instrument` and `silent` are different problems with different fixes, and
today they are the same absence. `unreported-scope` is the one that shows a
member publishing nothing.

**Coverage is never green by omission.** A goatnet where nothing reports renders
every row `coverage: silent`. The number of coverage gaps will go UP on the day
this lands, because it counts what was previously invisible. That is the
deliverable, not a regression, and nothing may be defaulted to green to soften
it.

## 5. Graduation sets

The promotion unit becomes a set, without abolishing the pin.

```yaml
# ops/release/sets/console-train-20260812.yaml
name: console-train
opened: '2026-08-12'
rung: preprod
reason: >-
  The console + portal + onboard-runner refs that were validated together on
  zagros and must reach prod together — the guide copy in one depends on the
  flow the other serves.
members:
  - component: id-admin-console
    ref: ghcr.io/{{ ghcr_org }}/goat-id-console:sha-…@sha256:…
    record: records/id-admin-console/20260812-….yaml
  - component: portal
    ref: …
    record: records/portal/20260812-….yaml
state: open        # open | graduated | abandoned
```

**As built (2026-08-12).** `ops/release/sets/*.yaml` with its own
README; `graduate-set <name> --ack` moves
every member in one act or refuses naming **every** unready member (not the
first — fixing one blocker and rediscovering the next is the experience this
tool replaces); `graduate <component>` refuses inside an open set and prints
the set, its members and the way through; CI check **C7** fails closed on a
set that does not parse, has no name, carries an unknown state or no members,
names an unknown component, carries a ref that is not digest-pinned, has a
member with no record for its exact ref, or puts one component in **two** open
sets. Every negative path was fired before landing — a gate that cannot fail is
F-321's
shape. Not yet wired into the readiness view.

- The **per-component record survives unchanged** (ADR 1120 §3) and remains the
  evidence carrier. The set is the unit of *movement*, not a second evidence
  system.
- `graduate.py check` **refuses** to graduate a component that is a member of an
  open set unless the whole set graduates. That refusal is what turns the
  2026-08-12 descendant promotion from a discovery into a decision.
- **Opt-in per train.** A component in no set graduates exactly as it does
  today. Sets are for changes that were validated together; they are not a new
  tax on every pin bump.
- **Build once, promote the digest** is unchanged and now applies set-wide: the
  set names index digests, so what graduates is byte-identical to what soaked.

Naming note: **`graduation set`, never "release set"** — that name belongs to
ADR 1027's partner data governance (`release/<set>/**` on the fabric) and
overloading it would put two unrelated meanings on one term in one repo.

## 6. What renders it — lenses, not a new dashboard

Nothing new is hosted. Every existing surface becomes a lens on the same row:

| Surface | Lens |
|---|---|
| consistency console, per-goatnet grid | sync + coverage for one goatnet, the altitude it already serves |
| hosted aggregator (`consistency.zagros-prod.goat.example`) | the cross-env lens — and for the first time it can compare *observed*, not only intent-vs-intent |
| `goat health` | the node's own rows, island-safe, from local facts |
| panoscope band | worst-grounded collapse, unchanged |
| relay-health dashboard | health for the peers that are not mesh peers, joined by component where a relay component is governed |
| `graduate.py` readiness | the rung view, now set-aware |

The one new *page* is not a page: it is the coverage list, which belongs inside
the console as a section, because a coverage gap is a consistency finding.

## 7. Increments

Ordered so each lands a usable answer and the expensive piece is last. RJ-1 and
RJ-2 pay for themselves before any collector work.

| # | Increment | What it lands | Depends on |
|---|---|---|---|
| **RJ-1** | Coverage rows for components | **BUILT 2026-08-12.** `no-instrument` / `silent` / `unreported-scope` as ROWS with pointers, beside — not replacing — the `components_observed` denominator. Pure read-model over facts `project.rs` already had; see §7.1 for what building it changed about the design. | — |
| **RJ-2** | Graduation sets | **BUILT 2026-08-12.** `ops/release/sets/` + its README, `graduate-set` verb, the `graduate` refusal, and CI check C7. Readiness-view integration not done. | — |
| **RJ-3** | Health joined by component | `EnvHealthV1` domains carry the component key where the domain is component-scoped; the row gains its `health` field | RJ-1 |
| **RJ-4** | The cross-env observed lens | the aggregator compares observed state, not only committed intent | RJ-1, RJ-3 |
| **RJ-5** | Capability probes | the `observe:` block, the collector's probe facts, the role-template-pinned tests | RJ-1 |
| **RJ-6** | Coverage-gap burn-down | `until:`-style expiry on `no-instrument`, so an uninstrumented capability is a dated backlog item rather than a permanent excuse | RJ-5 |

### 7.1 What building RJ-1 changed about this design

Two things the design got wrong on paper, both found by running it.

**The grade and the colour split, and they had to.** The design said sync and
coverage are orthogonal and never collapse into one verdict. Applied literally
— excluding coverage rows from the node's collapse entirely — a node holding
the current generation while reporting tips for **one of twelve** governed
components rendered **green**. That is the false green the whole surface exists
to prevent, and F-324's exact shape. Applied the other way — worst-wins over
all rows — an uninstrumented component **masked a red**, because
`indeterminate` outranks `unaccountable` in the grammar's precedence, so a
coverage gap would hide a hand-edited render.

The resolution is two fields and a floor, and it is now pinned by tests:

- `grade` is the **sync verdict**, computed over rows that are real
  comparisons. A node with gaps can still read `match`, because its
  convergence genuinely is fine.
- `color` is that grade's colour with **one floor**: gaps may not let a node
  render green. A floor, not a worst-wins — ungrounded must never read as
  fine, and must never read as *quieter than a finding* either.
- `coverage_gaps` is the count, carried beside both.

So a node with gaps reads `grade: match` · `color: idle` · `coverage_gaps: N`.
**The operational consequence is real and was accepted:** goatnets with
uninstrumented components stop showing green. The CF-4 render recorder is
default-off fleet-wide, so that is most of the fleet on the day this deploys.
The gaps were always there; only their invisibility is gone.

**`unreported-scope` was half-built until it was checked.** The state was
declared in the grammar, computed as `unreported_members`, and pinned by a
library test — and rendered by nothing. A grammar state no surface can show is
the same shape as the finding that motivated this increment, one layer up, so
it is now on the wire (`ConsistencyDetailV1.unreported_members`, field 11), in
`aggregate-v1`, and named on the rollup panel with the member spelled out. `N
of M reporting` says how many are missing; only the name says which, and every
failure in the F-324 class was a specific host nobody looked at.

**The CF-4 render gaps were already this feature, unnamed.** A declared
watch-set with nothing recorded was emitting an `indeterminate` row with no way
to say why it was empty. Those rows now carry `coverage: silent`, which is what
they always were.

RJ-3 through RJ-6 are queued as a tee-up rather than left as a table here:
`NEXT-reconciliation-join-rj3-rj6.md`,
which also carries the two adjacent items owned by other sessions (the
`superseded_by` record fields, and fastlane's use of `open_set_for`).

**The two prerequisites already landed** (2026-08-12, this track): declarations
now cover only the columns they name and expire on their `until:` date, and the
cross-env panel states the generation it graded. Both were preconditions for
RJ-4 — comparing observed state across envs is meaningless if a declaration
about one env's standby can excuse a cross-env difference, or if the page cannot
say which generation it read.

## 8. Non-goals

- **Not a reconciler.** Nothing here converges anything. ADR 1109 stands: the
  node pulls. If a future change makes a reconciliation row an input to a deploy
  decision, that is a violation of ADR 1125 §F, not an evolution of it.
- **Not a mega-unification.** The row covers what IntentV1 *governs*. A node's
  ledger carries dozens of components nothing claims authority over; those ride
  as facts in the drill-down and get no verdict — the same boundary
  `project.rs` already holds (§project.rs note 1).
- **Not a stored history.** No materialized table, no time series. The deploy
  ledger is the history of what ran; the release records are the history of what
  was allowed. This is a read-time join over both.
- **Not a health system.** `EnvHealthV1` and relay-health own health. This joins
  to them by key; it does not re-measure anything.

## 9. Complexity-budget call (ADR 1016 D7)

This adds one **derived** object and one authoring field (`observe:`), and adds
`ops/release/sets/` to the release surface. It adds **no new vocabulary to the
troubleshooting path**: sync and health grade in the same six grades
L-001
already accounts for. `coverage` IS new operator-facing vocabulary — four state
names an operator must learn — and it is a leak-register candidate at the point
RJ-1 lands a surface that shows it. The offsetting argument, which the register
entry must make rather than assume: the four states replace an *absence* that
operators currently reason about with no vocabulary at all, and the F-324 cost
of that absence is on the record.

The four onboarding paths are unaffected: none of them reads this surface, and
none acquires a new substrate. If that stops being true, the bar in
[`complexity-budget-and-encapsulation.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/complexity-budget-and-encapsulation.md)
is re-tested before the increment lands.
