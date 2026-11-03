# ADR 1160 — The zenoh topology surface is a passive reader of self-published router state

- **Status:** Accepted
- **Date:** 2026-09-04
- **Deciders:** platform
- **Related:** [ADR 1123](1123-client-demand-must-not-amplify.md) (client demand must not amplify into fabric load), [ADR 1036](1036-environment-health-surface.md) (self-publish + roll-up), [ADR 1041](1041-zenoh-router-zid-pinning-and-source-provenance.md) / [ADR 0844](0844-zenoh-deployment-topology-and-acl-v1.md) (a ZID is a label, not a credential), M-111, M-112, design at [`zenoh-topology-surface.md`](https://github.com/usareur-af/goatstuff/blob/main/docs/design/zenoh-topology-surface.md)

## Context

`gateway.south` sets the size and blast radius of a goatnet's routing domain,
and M-111
found it running on its `Preset(Auto)` default **fleet-wide, because no
configuration sets it anywhere**. That is now documented in
`zenoh-data-fabric.md`. A design doc answers *what the primitive does*; it
cannot answer *what is my fabric doing right now, and is any of it wrong* — and
during an incident that is the question.

Three constraints bound any answer:

1. **[ADR 1123](1123-client-demand-must-not-amplify.md)** — panoscope is the
   only surface whose client demand reaches the fabric, and router wedge is not
   hypothetical here. A new surface must not join it.
2. **The adminspace is per-host loopback** (`8000/tcp -> 127.0.0.1:8000`,
   verified on zagros and zagros-prod). No central reader can reach it without
   opening a new network path.
3. **The adminspace does not expose the gateway config at all** — 737 keys on
   zagros, none of them config. The declared posture can only come from the
   router host's own config file.

## Decision

**1. Router topology state is self-published, and the surface is passive.**
Each router host runs a publisher that reads *its own* router and publishes
`RouterTopologyV1` to `ops/fabric/topology/<site>/<node>`. The surface
subscribes once, holds the last snapshot per router in memory, and answers every
HTTP request from that memory. **A page view costs the fabric nothing**; the
only fabric traffic is one small publish per router per interval, independent of
how many people have the page open. This is [ADR 1036](1036-environment-health-surface.md)'s
pattern, and it is also what survives partition.

**2. The declared mode is read from the config file, never inferred from
observed regions.** `Preset(Auto)` and a `Custom` list whose filters match
nothing produce *the same observable regions*, and the second is a real
misconfiguration. Inferring would report a fault as healthy. Where the file
cannot be read the mode is **`unreadable`** — a first-class state, never
silently `auto`.

**3. The publisher extracts the `gateway` block and nothing else.** That config
file is ~900 KB and overwhelmingly the goatnet's live ACL. None of it is read
into the envelope, logged, or published. A meter that shipped ACL content onto
the bus would be an identity leak dressed as observability.

**4. The findings engine may never recommend scoping by `region_names` or
`zids`.** Both are peer-asserted ([ADR 1041](1041-zenoh-router-zid-pinning-and-source-provenance.md)
citing [ADR 0844](0844-zenoh-deployment-topology-and-acl-v1.md)). The engine
flags their *use* as a critical hazard (`T-001`) and points at certificate
identity in the ACL instead. A test asserts no remediation string proposes a
region as a scoping mechanism.

**5. Scope boundary is fixed at the layer, not by feature.** netvis owns mesh/IP
reachability; topology owns zenoh routing domains. Neither grows into the other.

**6. It reads and renders; it never writes.** No control path to any router.
Remediation is text an operator acts on.

## Consequences

**Good.** The question "what is my routing plane doing" becomes answerable
without a shell on a router host. The `T-001` hazard — a filter list that looks
exactly like an access control while two of its five axes are peer-asserted —
gets named at the moment someone is looking at the config, which is when it can
still be cheap to fix. And the passive-reader property is easy to verify in
review: any handler that reaches the fabric is the defect.

**Cost.** One more component, one more image, two more role applications. That
is a real complexity-budget charge ([ADR 1016](1016-complexity-budget-substrate-encapsulation.md))
and it buys one thing: the routing plane stops being invisible between
incidents.

**A model correction this decision surfaced.** Running the publisher against the
live zagros router showed the M-112 successor formula needs an edge condition — a
region with `E=0` contributes nothing however many nodes it lists. Without it
`T-005` would have fired a false warning on a healthy production router every
cycle. Recorded in `zenoh-topology-surface.md` §4.1; the rig could not have
found it because every region in the rig was well-formed.

**What this does not do.** It is not alerting — findings are rendered, not
paged. It is not evidence about scale; the claims it can make are bounded by
M-111 and M-112. And it changes no goatnet's configuration: every goatnet has
one populated region and `V ≤ 3`, so there is no routing-state problem to solve
today. This is preparation for a scale we have not reached, and the surface says
so on its face.

## Alternatives considered

**A tab in panoscope.** Rejected: panoscope is the one surface whose demand
reaches the fabric, and putting a must-never-query surface inside it makes that
property hard to keep true.

**Query each router from the surface on demand.** Rejected on all three counts
in Context: it is the shape ADR 1123 forbids, the adminspace is not centrally
reachable, and it would not survive partition.

**Extend the existing `fabric-health` meter instead of a new component.**
Tempting — it already runs on router hosts with certs — but it publishes a
site-level health envelope, whereas this needs a per-router one, and the
surface half has no home there at all. Rejected as the wrong seam, not on cost.

**Infer the gateway mode from observed regions and skip reading the config.**
Rejected: it cannot distinguish `Auto` from an inert `Custom` list, so it would
report the one real misconfiguration in this space as healthy.
