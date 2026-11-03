# ADR 1129 — warrant: scoped, evidenced, expiring network authorizations on two planes

**Status:** Proposed (2026-08-13, platform) · **Design:** [warrant.md](../design/warrant.md) · **Track:** `warrant`

**The name.** A **warrant** is a narrowly-scoped authorization, issued quickly by
a named authority, that **expires**. That is not a metaphor for what this
decides — it is the shape of it: one source class reaching one destination class
on one named port-set, approved by a decider chosen by blast radius, carrying an
owner and an expiry. Chosen over `bouncer` (PgBouncer, F-020), `marshal` (serde)
and `deputy` (a real job title in the on-duty docs), all of which collide.

## Context

`ops/ansible/netbird_acl_matrix.yml` says which group may reach which group, on
which protocol, on which ports. Two measurements taken 2026-08-13 describe the
state of that file and of the goatnet it produces.

**The port vocabulary is already closed, and already named — in prose.** Across
the declaration there are **118 rule-shapes carrying a protocol, and 13 distinct
`(protocol, ports)` combinations**. Eleven of those carry ports; the other two
are `all` (48 uses) and `icmp` (12 uses, which has no ports and is correct as
written).

| uses | protocol | ports | the name already in the rule |
|---|---|---|---|
| 8 | tcp | 443, 53, 5353, 7447 | `(web/dns/fabric tcp)` |
| 8 | udp | 53, 5353, 7448 | `(dns/fabric udp)` |
| 7 | tcp | 443, 53, 5353 | `(web/dns only)` |
| 7 | udp | 53, 5353 | `(dns udp)` |
| 7 | tcp | 22 | `(ssh only)` |
| 4 | tcp | 7447 | `(fabric tcp)` / `Zenoh router :7447` |
| 4 | tcp | 443, 53, 49, 6514 | `(management-plane tcp)` |
| 4 | udp | 53, 5353, 123, 514, 162 | `(management-plane udp)` |
| 4 | udp | 5353 | `(mesh dns only)` |
| 3 | tcp | 443 | `(HTTPS :443)` |
| 2 | udp | 7448 | `(fabric udp)` |

The right-hand column is not proposed vocabulary. It is copied out of the
`name:` field of the rules that already use each set. **The declaration names
its port-sets 118 times and stores them zero times.**

The cost is not hypothetical. Changing the Zenoh TCP port means finding eight
identical literals and being sure there was no ninth. And the rationale has
already drifted between copies of one set: the three rules carrying
`tcp 443, 53, 49, 6514` describe it as *"Bearer gear reaches the substrate's
management services only — mesh web/mgmt (443), DNS (53), TACACS+ (49),
syslog-over-TLS (6514). NO fabric ports…"* in one place and *"Bearer gear
reaches management services only — 443 / DNS 53 / TACACS+ 49 / syslog-TLS 6514.
No fabric ports."* in another. Same fact, two texts, no mechanism keeping them
equal.

**Many enabled rules name no protocol.** A share of enabled rules are
`protocol: all` and declare no ports at all. Some are
deliberate and documented as directional decisions (ADR 1124's
`HOST:PLATFORM-ADMIN` downward grant states its reasoning in the rule). Others
are `all` because **nobody could prove what narrowing them would break.**

That second group is the real problem, and it is an information problem rather
than a discipline problem. The knowledge of *which ports a service needs* sits
with whoever runs the service. The ACL is written by whoever administers the
network. Nothing carries the first fact to the second person, so `all` is the
honest expression of "I do not know, and I will not risk an outage guessing."

**A count-based ratchet makes this worse, not better.** The obvious control —
baseline today's count and fail CI on one more — is rejected. It
applies pressure while supplying no information, and the cheapest way to satisfy
it is to *widen an existing rule* rather than add a new one. That is strictly
worse than the behaviour it replaces and invisible to the counter measuring it.

## Decision

A **warrant** is the unit: one source class, one destination class, one named
port-set, one owner, one **expiry**. Eight decisions follow. D0 is the one the rest rest on; then vocabulary,
evidence, authority, the plane that governs authority, what mediates it, and
what keeps it honest.

### D0 — Git is the system of record for network policy decisions

Stated first because every other decision follows from it. **The decision and
the artifact are the same object**: the reviewed diff is what generates the ACL,
so there is no gap between what was agreed and what is deployed, and no second
system to reconcile against.

| property | because |
|---|---|
| **attribution, permanently** | every rule traces to a person, a date, a review and a stated reason — `git log -p` on one file answers "who opened this port, when, and why" |
| **no decision/deployment drift** | a decision cannot be recorded and then not applied, nor applied and not recorded — they are one act |
| **review before effect** | CI runs the [ADR 1126](1126-acl-assertions-rechecked-when-nothing-is-written.md) assertions on the proposal, so a change breaching a declared invariant fails *before* it reaches a goatnet |
| **rollback is ordinary** | `git revert` + fastlane. The undo path is the same path, not a special one |
| **offline-legible** | the whole policy history clones to a laptop; at a denied edge you can still answer what is permitted and why |

**Rejected: a bespoke approval service holding decisions in its own store.** It
creates a second source of truth for authorization, and the failure mode is
familiar on this substrate — a record that says one thing while the goatnet does
another, with nothing forcing them to agree.

Consequently **a request is a pull request, routing is `CODEOWNERS`, a decision
is a review, and the audit is the history** — leaving nothing for a bespoke
approval service to do.

### D1 — Port-sets are named, stored once, and generate the rules

A registry file declares each port-set with a name, its `(protocol, ports)`
members, a purpose sentence and an owner; rules reference the name. The seven
sets are extracted from the declaration's own `name:` fields, not designed. A set
spans protocols, because the measurement shows the tcp and udp halves are never
used apart — which removes the class of change where one half is updated and the
other forgotten.

**The registry GENERATES the rules; it does not sit beside them.** Same shape as
`ops/serve/services.yaml` → Caddyfile and `isitup_services` → mesh DNS zones. An
advisory registry drifts, and F-324
is this month's proof. The generated output stays fully expanded, so the
indirection lives in the source and never in what is applied or reviewed.

### D2 — The listening surface is evidence, and it is a configuration fact

Each node publishes what a machine of its class actually listens on. **This is
not traffic telemetry** — it is what `ss -ltnp` reports, observable without
watching a packet, which is why it does not touch the brokerless position. Same
reveal-only shape as `EnvHealthV1` ([ADR 1036](1036-environment-health-surface.md)):
nodes self-publish, a passive reader rolls up, an unreported class reads
`unknown` and cannot license a narrowing.

**Evidence, never authority.** It never issues a grant — a node that opened a
socket would otherwise be authoring its own permissions.

### D3 — Authority is a ladder, and five of its six rungs reach no queue

Joining the declared grant against the observed surface answers both directions:
*what would narrowing cost* and *what else does opening grant*. From that:

| the change | who decides |
|---|---|
| **narrowing** anything | nobody — applied and logged; it cannot create reach |
| an opening **already permitted** | nobody — diagnosed and returned, no warrant created |
| an opening **matching a standing template** | nobody — decided once, over the class |
| an opening needing **relief now**, own scope | the site's own admin, provisionally (24h) |
| a **novel** opening, own scope, existing port-set | the site's own admin |
| new port-set · new class pair · cross-scope · `all` · `NETWORK`/`BREAKGLASS` | platform admin — the only queued rung |

Three properties do the work. **Narrowing is free**, so the mesh gets stricter by
default rather than by campaign. **Most openings are not ACL problems** — the
entry point is a diagnosis computable from the reach resolver netvis already
serves, and a request path whose first act is to rule itself out removes most
traffic before a human sees it. **Standing templates** pre-authorize a *shape*
once, over a class, so novelty escalates and volume does not; **provisional
warrants** let a site's own admin unblock immediately, bounded to 24h by
construction rather than by anyone remembering to revoke.

When a request is needed, the system proposes **the narrowest port-set
satisfying the stated need, not the one asked for** — so the path of least
resistance produces the smallest grant.

### D4 — Governance changes only through git, and never through the fast path

D1–D3 change *warrants*. The things that govern warrants — the vocabulary, the
templates, decider resolution, maximum durations — are a **separate plane that
only a pull request can change.** `goat warrant` has no `template create`; the
console has no template editor; no endpoint writes a port-set.

**The instance plane is fast because the governance plane is slow**, and the
trade only holds if they never share a path. The reason is specific: a standing
template is a **delegation of authority, not a grant of access** — a warrant
lets one class reach another until it expires, while a template lets unbounded
future warrants issue with no human in the loop, indefinitely. If the fast path
could create templates, anyone able to use it could promote themselves to
auto-issue permanently.

Kubernetes RBAC's **escalation prevention** is adopted directly: a template may
not authorize reach broader than the authority approving it holds, and
`approved_by` is verified **at generation**, so a template whose approver has
lost that standing stops issuing rather than running on a stale approval.

Two consequences. **Vocabulary edits go through review in both directions** —
this is the one place narrowing-is-free does not apply, because narrowing one
warrant affects one pair while narrowing a port-set affects all sixteen rules
referencing it; blast radius, not direction, is what makes them dangerous. And
**there is no emergency governance change**: no 24-hour template, no expedited
vocabulary edit. The emergency path is a provisional *warrant*, which expires on
its own — because if "emergency" could reach the governance plane it would become
the route by which authority is permanently widened under time pressure.

### D5 — Git is the mediator; no new service is built

The request, decision and audit machinery already exists and is not worth
re-inventing: **a pull request is the proposal, `CODEOWNERS` is the routing, a
review is the decision, the history is the audit, CI is the gate, and
[ADR 1127](1127-fastlane-urgent-fleet-wide-delivery.md) is the delivery** at ~30
seconds to every goatnet.

So the build is four things and **no new services**: `port-sets.yaml` plus a
generator; `goat warrant why-blocked` as a read over the resolver netvis already
serves; an `expires:` field with a CI check and a scheduled removal PR; and
`CODEOWNERS` over the registry paths. The narrowing-is-free asymmetry becomes
one CI check — the generator can *prove* a change is a reduction by diffing its
own output — rather than a queue.

A console store, a machine API, a `warrant-runner` service, an approval queue, a
provisional-warrant state machine and a standing-template engine are all
**deferred behind named, observed failures of the simple path** — each would
duplicate machinery git already provides, and together they would breach
[ADR 1016](1016-complexity-budget-substrate-encapsulation.md) for a concept that
is three sentences long. The console surface is first among them, being the one
thing git cannot serve: an admin who cannot open a PR.

**Consequence for the delegated tier:** `CODEOWNERS` routes to named people
today and is fail-closed when no owner resolves, so the delegation this design
exists to enable does **not** wait on ADR 1073's console enforcement. The
console is an accessibility improvement, not the mechanism.

### D6 — Everything expires, and the assertions guard both planes

Every warrant carries an owner and an expiry. One that cannot be narrowed today
is still issuable; on expiry it neither lapses nor renews automatically but
surfaces **with the join already attached**, so narrow-or-extend is a two-minute
read. This makes rule recertification continuous and targeted rather than
periodic and dreaded.

Each port-set declares `required_by`, generated into `netbird_acl_tests`
([ADR 1126](1126-acl-assertions-rechecked-when-nothing-is-written.md)), so a
narrowing that would break a declared dependency fails the gate instead of the
site — including across membership changes, which write no file and escape every
write-time gate. **The governance plane gets its own assertions too** (no
template may target `NETWORK`/`BREAKGLASS`, carry `protocol: all`, or cross a
scope; no orphan port-sets; bounded `max_duration`), so the meta-layer is gated
by machinery rather than by review alone.


### D7 — Mechanism is public, policy is private, and the seam goes in first

goat goes open (oss-strategy.md, Block 29M: public
upstream `goat-maintainer/goat` + private downstream overlay), and **the governance
record D0 establishes is the most sensitive artifact this design creates** —
`git log -p` on a rule-intents file is at once a map of what reaches what and a
list of who authorizes it, hitting the deployment-specific and
attacker-useful categories of the cleansing taxonomy simultaneously.

**Public:** the port-set schema, the generator, `goat warrant why-blocked`,
every CI check, this ADR and its docs, and the **reference vocabulary** — the
seven port-set names are generic infrastructure (`shell` is tcp/22), so shipping
them gives an adopter a working default rather than a blank file.
**Private overlay:** the rule-intents, `CODEOWNERS` naming real people,
deployment-specific assertions, scope/site/class names, and **the decision
history for all of it**.

**The seam goes in before the split, not after.** History cannot be relocated,
and history is the point of the record; separating it later needs `filter-repo`
surgery on exactly the file whose history is load-bearing, and a leaked commit
message is as exploitable as a leaked tree. So policy files live in an
overlay-only path from their first commit — even while one repo still holds
both — making the split a path that does not travel rather than an excision. The
two guards already planned (`public-surface-guard.yml`,
`overlay-content-guard.yml`) need only to learn these paths.

**The generator takes policy as an argument**, never a vendored directory, so
the public project is runnable against any operator's policy — including an
adopter who has never seen a goatnet. Consequently **an adopter's governance
record is theirs**: goat ships the mechanism for a policy record and never
becomes the place someone else's network policy accumulates.

## Consequences

- The eleven port-sets become editable in one place; the eight-copy Zenoh port
  change becomes one line.
- The rationale drift shown above is resolved by construction — one set, one
  purpose sentence.
- `protocol: all` becomes a *state with an owner and an expiry* rather than a
  permanent property of a rule.
- The generated rules must be byte-comparable to today's hand-written ones on
  first run, or the migration is not safe. This is a hard gate on phase 2, not
  an aspiration.
- New work: a listening-surface publisher per node class. The gear and Jetson
  classes are where it matters most and where it is hardest, and they are
  explicitly last in the plan rather than first.
- The registry adds one indirection between reading a rule and knowing its
  ports. Mitigated by the generated output remaining a fully-expanded,
  human-readable file — the indirection exists in the source, never in what is
  applied or reviewed.

## Rejected

**A count-based ratchet on its own** (baseline today's count, fail at one more). Applies pressure
without information; the cheapest way to satisfy it is to widen an existing rule
instead of adding one. A ratchet is fine *downstream* of D3, once the evidence
exists to act on — not as the primary control.

**Generating rules from the observed listening surface.** Encodes today's
accidents as tomorrow's policy, and lets a compromised node author its own
grant by opening a socket. The listening surface is evidence for a human
narrowing a rule; it is never an authority that widens one.

**A solver or intent-based engine (Batfish//NAA-shaped).** [ADR 1126](1126-acl-assertions-rechecked-when-nothing-is-written.md)
already rejected a solver for this problem: group-to-group reach with ports is
set intersection over bounded membership, not a routing fixed-point. Adding one
here would spend the [ADR 1016](1016-complexity-budget-substrate-encapsulation.md)
complexity budget on a problem that does not need it.

**Per-workload sidecar policy (Istio-shaped).** Requires a proxy in the data
path, which re-introduces the broker this architecture is built to avoid — the
same objection recorded in [zenoh-brokerless-consequences.md](../design/zenoh-brokerless-consequences.md).

**Service-keyed rather than purpose-keyed port-sets** (`netvis → 9760`). The
measurement says the real unit is the purpose: `substrate` is used by eight
different rules spanning many services, and a service-keyed registry would
re-fragment a vocabulary that is already closed at eleven.

**A console or CLI path that creates standing templates.** The obvious
convenience, and the design's largest latent escalation vector: a template
delegates issuing authority indefinitely, so a fast path that could create one
would let anyone able to use it promote themselves to auto-issue permanently.
Governance changes through git or not at all.

**An expedited or provisional governance change.** Rejected because "emergency"
would become the route by which authority is permanently widened under time
pressure, which is how every exception process decays. Urgency is served at the
instance layer, where blast radius is one pair and the clock runs out by itself.

**Making the registry advisory** — a document that describes what the rules
should contain. That is the F-324 failure mode by construction. It generates or
it is not worth building.
