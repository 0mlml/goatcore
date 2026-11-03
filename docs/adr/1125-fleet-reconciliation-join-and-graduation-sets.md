# ADR 1125 — One row per declared fact, carrying sync and health separately; graduation promotes a set

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-08-12 |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | [ADR 1110](1110-typed-consistency-foundation-and-console.md) (the two fact kinds + the projection this extends), [ADR 1120](1120-release-graduation-ladder.md) (the ladder this changes the unit of), [ADR 1060](1060-universal-deploy-rollback-ledger.md) (the observed-ref source), [ADR 1036](1036-environment-health-surface.md) (the health axis), [ADR 1109](1109-fleet-management-dimension-and-pull-convergence.md) (convergence is pulled — this decides nothing), [ADR 1058](1058-relay-metrics-self-publish-ingest.md), [ADR 1016](1016-complexity-budget-substrate-encapsulation.md) (the budget this spends against) |

## TL;DR (for a layperson)

We can already answer two questions about our systems separately: *is each
machine running the software we said it should?* and *is that software
working?* The trouble is that those two answers live in different tools, keyed
differently, so nobody can line them up — and a third question, *is anything
even watching this?*, had no answer at all. That third gap is how a piece of
network monitoring came to be described in three files as running on a
production machine for months while it had never been installed anywhere. This
decision gives every single thing we declare one row that carries all three
answers side by side, so "nothing is watching this" is a visible state you can
act on rather than a silence. It also changes how a batch of software gets
promoted from the test environment to production: as one named set that moves
together, instead of piece by piece, because promoting pieces separately is how
one piece arrives ahead of the others by accident.

## Context

The consistency foundation (ADR 1110) already does most of this. IntentV1 is
the declared fact per `(component | capability, goatnet, member)`;
`project.rs` grades a node's reported component tips against it and is
scrupulously honest about what it cannot see, reporting `components_observed`
of `components_governed` rather than inventing a verdict. ADR 1120 made
"what should run" change along a committed ladder. ADR 1060 made every
deploying role write a ledger. ADR 1036 put health on the bus. None of these
is missing.

What is missing is that they do not meet. Five surfaces key five different
objects: the consistency console on `(subject, member)` from committed intent;
the deploy ledger on `(component, host, ref, ts)`; release records on
`(component, candidate_ref, rung)`; `EnvHealthV1` on `(domain, scope)`; the
relay-health dashboard on a relay's public address. A platform admin asking
"is zagros-prod where it should be" joins those by hand, across tabs, and the
join is exactly where the errors are.

Three failures made the gap concrete rather than theoretical.

**F-324
(2026-08-12).** The `net` domain was declared on zagros-prod and deployed
nowhere. Three files — a group_vars comment, a host_vars override, and a
`validation-state.md` paragraph — described a rollup topology no host carried,
and a fourth capability (`host_net_publish_enabled`) was declared group-wide
and realized on one member of the group. Every one of those was a claim about a
host that no surface ever checked against a host. Capabilities have **no
observed side at all**: `project.rs` grades components only, so a declared
capability is unfalsifiable by construction. The row sat as undeclared drift
from M-105
(2026-08-09) — where it was named as the single undeclared SKEW on the
aggregator's first day — until somebody read the tree three days later.

**A component promoted by descendant (2026-08-12).** Two concurrent sessions,
one console train. A fix merged directly on top of another session's release
candidate; the pin bump that carried it to prod also graduated the earlier
session's console train, ahead of its stated intent. The sessions coordinated
in prose on the session bus and got the right outcome. The structural cause was
untouched: **the graduation unit is one component's pin**, so nothing in the
model can express "these refs move together, or not at all."

**A page that could not date itself (2026-08-12).** The hosted aggregator was
rendering zagros intent serial 51 while the tree was on 67, with nothing on the
page to say so. Fixed as labelling in the same change as this ADR; the
underlying cause — the artifacts arrive baked into the console image — is this
ADR's.

The prior art worth copying is specific, and it is not the UI. **Argo CD** gives
every application two orthogonal statuses computed against the *same object*:
Sync (declared vs live) and Health (live vs working). The goat has both facts
and keys them differently, so neither can be read next to the other. **Kargo**
makes the promotion unit an immutable *set* of pinned artifacts that moves
through stages together, which is precisely the property the per-component pin
lacks. **Kubernetes** carries `observedGeneration` on every status, so a reader
can tell which spec revision a verdict was computed from. Each is one idea, and
each maps onto machinery this repo already has.

## Decision

**A. One derived row per declared fact — the reconciliation row.**

Key: `(subject, name, goatnet, member)` — the same key IntentV1 already uses,
extended with the goatnet so it spans envs. It is **derived, computed at read
time, stored nowhere**, and it introduces no new fact kind: it joins facts the
tree and the nodes already publish. It carries three fields:

| Field | Question | Source |
|---|---|---|
| `sync` | does what is running match what is declared? | `project.rs` (existing) for components; the capability probe below for capabilities |
| `health` | is the thing that is running working? | `EnvHealthV1` (ADR 1036) where a domain covers this row; relay-health (relay-health-dashboard.md, ADR 1058) for non-peers |
| `coverage` | is anything observing this at all? | this ADR — see C |

`sync` and `health` are **orthogonal and never collapsed into one verdict.** A
component on the right digest whose service is failing is `synced` +
`degraded`, and a single colour that averaged those would hide the more
actionable of the two.

**Coverage is orthogonal to the grade and NOT to the colour.** Building RJ-1
established the boundary the design had left implicit: a node's `grade` is the
sync verdict over real comparisons, but its `color` floors at ungrounded when
gaps exist, because a green node reporting one of twelve governed components is
the false green this surface exists to prevent. It is a floor and not a
worst-wins collapse — `indeterminate` outranks `unaccountable` in the grammar's
precedence, so collapsing gaps in by worst-wins would let an uninstrumented
component MASK a red. Detail and the tests that pin it:
[`fleet-reconciliation-join.md`](../design/fleet-reconciliation-join.md) §7.1. Both grade through the existing six-grade grammar
(`ops/consistency/grammar.yaml`); this ADR defines **no new grade and no new
sentence generator** — the ADR 1110 D2 rule that a projection may choose what
it sees and how it renders but may not grade is binding here.

It does add **two members to the closed remediation set** (`instrument`,
`enrol`), and the drafted version of this ADR claimed it would add none. That
was wrong and building it showed why: `indeterminate` at the `live` vantage
dispatches to `reconverge`, and re-running the play cannot create ledger
coverage for a component no role deploys. Reusing `reconverge` there would have
shipped a pointer that does not work, which is how a permanent gap gets read as
a transient. The set is closed, not frozen; it grows in one change with the
cases that pin it.

**B. Capabilities gain an observed side, or they are honestly marked
unobservable.**

Each capability governed in `ops/pins/parity-capabilities.yaml` may declare how
a node proves it:

```yaml
- var: node_net_health_enabled
  why: …
  observe:                      # optional; absent means "no instrument declared"
    kind: container             # container | unit | path | topic
    name: node-net-health
```

The node's converge record reports the probe result as a **fact** — the
CF-4 `RenderStateV1` pattern exactly: the node states what it sees, the
comparison happens at read time in the shared library, and the node issues no
verdict. A capability with no `observe:` block is not a failure; it is
`coverage: no-instrument`, which is a countable, remediable state (see C). This
is the piece that would have caught F-324 on the day it was written.

**C. `coverage` is a state, not a denominator.**

`project.rs` reports `components_observed` of `components_governed`, which is
honest and is a number. This ADR makes the missing rows *rows*, each with a
reason:

| `coverage` | Means | Remediation kind |
|---|---|---|
| `observed` | a node reported this fact | — |
| `no-instrument` | nothing declares how to see it (`ledger_component: null`, no `observe:` block) | `instrument` — declare the probe |
| `silent` | an instrument exists and the node has not reported | `reconverge` — the existing pointer |
| `unreported-scope` | the member exists in intent and no node claims that member | `enrol` — the node is not reporting at all |

`no-instrument` and `silent` are different problems with different fixes, and
today they are the same absence. **Coverage is never green by omission:** a
goatnet where nothing reports renders every row `coverage: silent`, never
`match`.

**D. The graduation unit becomes a set, without abolishing the pin.**

A **graduation set** is an immutable, named, committed collection of component
refs that graduate together:
`ops/release/sets/<name>-<date>.yaml`, naming its members, the rung it is on,
and one record reference per member. The per-component record (ADR 1120 §3)
survives unchanged and remains the evidence carrier; the set is the *unit of
movement*. `graduate.py` gains set-aware verbs, and its `check` gate refuses to
graduate a component that is a member of an open set unless the whole set
graduates — which is the specific refusal that would have turned the 2026-08-12
descendant promotion into a decision instead of a discovery. A component in no
set graduates exactly as it does today; sets are opt-in per train, not a new
tax on every bump. *(The term is `graduation set`, not "release set" — that
name is taken by ADR 1027's partner data governance and must not be
overloaded.)*

**E. Every verdict names the generation it was computed for.**

`observedGeneration`, applied. Any surface rendering a reconciliation row states
the intent serial + `content_hash` it graded against, and its age. Landed for
the cross-env panel already; extended to every row-bearing surface here.

**F. This decides nothing and actuates nothing.**

ADR 1109's "convergence is PULLED" is untouched: the join is a read-model. It
may render, alert, and point at a remediation; it may not deploy, and no node
may consult it to decide what to run. The ADR 1065 derived-read-only-projection
stance applies verbatim — a reconciliation row must never appear in an
enforcement path.

## Alternatives considered

**Another dashboard.** The cheapest-looking option and the one that caused the
problem: a sixth surface with a sixth key adds a place to look, not a place
where the facts meet. Rejected because the complaint that produced this ADR was
"it all seems loosely connected", and one more loosely-connected thing is a
strict worsening.

**Adopt Argo CD / Flux outright.** They solve the two-axis problem properly, and
for a Kubernetes fleet this would be the answer. This fleet is Ansible-converged
Docker Compose on Orins and VPSes, with a deliberately brokerless fabric and
island-mode requirements; adopting a Kubernetes control plane to get a status
model is far past the complexity budget (ADR 1016), and the reconciler would
have to be reachable, which ADR 1109 rejects. We take the *model* — two
orthogonal statuses on one object — and leave the machinery.

**Adopt Kargo outright.** Same shape of answer: the Freight concept is right,
the implementation assumes Kubernetes CRDs and a controller. A committed YAML
set plus a refusal in `graduate.py` gets the property (a promotion unit larger
than one component) at a fraction of the substrate.

**Store the join.** A materialized reconciliation table would make queries
cheap and history free. Rejected for the reason the whole consistency
foundation is read-time: a stored verdict is a verdict that can be stale
without saying so, and the F-324 class is *precisely* stored claims nobody
re-derived. Computing at read time means a wrong answer requires wrong inputs,
not merely old ones.

**Collapse sync and health into one status.** Tempting for the band, and it is
what a single traffic light wants. Rejected: the two failure modes have
different remediations (`reconverge` vs the health domain's own pointer), and
an averaged colour would let a healthy-but-wrong-version service look the same
as a right-version-but-broken one.

**Leave capabilities unobserved and rely on review.** The status quo. F-324 is
the evidence against it: three reviewed files agreed with each other and all
three were wrong, for months, about a service that did not exist.

## Consequences

**Positive.** "Declared and nothing is watching" becomes a countable state with
a remediation pointer instead of a silence — the single largest class of wrong
belief this repo has recorded. Sync and health become readable next to each
other without a human join. A promotion can no longer take a component out of
its train by accident. Existing surfaces become lenses over one object rather
than five systems, which is the elegance the design was reaching for, and the
per-goatnet console, the aggregator, `goat health` and the relay dashboard each
keep their own altitude.

**Negative.** The capability probe (B) is real work in the collector and touches
every governed capability that wants coverage; it is the largest piece here and
it lands last for that reason. Every `observe:` block is a new thing that can be
wrong — a probe naming the wrong container is
F-320's
shape (a check wired to something nobody sets), so probes MUST be pinned by a
test that reads the deploying role's own template rather than restating the
name. Coverage rows will make the fleet look worse on day one, because they
count gaps that were previously invisible; that is the point and must not be
softened by defaulting anything to green. The complexity budget spend is
declared in the design doc's leak-register call.

**Residual risk we accept.** The join is only as good as its inputs: a node that
lies in its converge record produces a confident wrong row, and nothing here
detects that. The existing mitigations (the ledger reconciles against
`docker inspect`; renders are hashed) are unchanged and not extended by this
ADR.

## Assessor view (SCA-R)

This is a configuration-management and monitoring decision, mapping to
NIST SP 800-53 **CM-2** (baseline configuration — IntentV1 is the baseline,
signed and verified at read), **CM-3** (change control — the graduation ladder
and its records), **CM-6** (configuration settings — the capability rows and
their declared deviations), and **SI-4** (system monitoring — the coverage
state, which makes *absence of monitoring* an auditable finding rather than an
absence of findings). The coverage state is the control-relevant novelty: an
assessor can be shown a count of declared settings for which no observation
mechanism exists, with a dated remediation, which is a stronger posture than a
report that silently omits them. No waiver is required. Consistent with a cATO
pathway, where continuous evidence of configuration conformance — including
honest declaration of coverage gaps — is the operative requirement. Residual
risk: the join trusts node self-reports; the compensating control is that the
reports are facts rather than verdicts, hashed where they are files
(`RenderStateV1`) and reconciled against the running container where they are
refs (ADR 1060), so a divergence between claim and reality grades
`unaccountable` rather than `match`.

## Trigger conditions for revisit

- A third goatnet joins the ladder and the cross-env row set stops fitting one
  page — the projection may need per-env collapse before it needs a new model.
- Any part of the fleet moves to Kubernetes, at which point Argo CD's real
  implementation beats this one and the model transfers unchanged.
- The coverage-gap count stops falling for two consecutive quarters, which
  would mean `no-instrument` has become a permanent excuse rather than a
  backlog — the same failure mode `until:` expiry exists to prevent for
  declared deviations.
- A reconciliation row is ever consulted by something that decides rather than
  renders. That is a violation, not a revisit, and the response is to remove
  the consumer.
