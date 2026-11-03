# ADR 1131 — `NETWORK-JUMP` node-class + group tier, and Herd scoped to bearer gear

**Status:** accepted
**Date:** 2026-08-14 (ratified by the operator on merge of #2564, with the scoping,
the shared-key choice and the entitlement gate all decided in review)
**Amends:** ADR 1043 §1.4 (node-class enum)
**Related:** [ADR 1113](1113-network-node-class-and-delegated-network-admin.md) (the `NETWORK`
class this serves, and the entitlement it reuses),
ADR 1114 (the outbound shape this copies),
[ADR 1109](1109-fleet-management-dimension-and-pull-convergence.md) (convergence is pulled),
[ADR 1125](1125-fleet-reconciliation-join-and-graduation-sets.md) (the join, whose surface is the
consistency console — see *Alternatives considered*),
[ADR 1129](1129-warrant-scoped-expiring-network-authorizations.md) (where the rules are declared),
[ssh-key-self-service.md](../design/ssh-key-self-service.md) (one door per device class),
mesh-acl.md, `ops/ansible/netbird_acl_taxonomy.yml`

## Context

`herd` is a web console for mapping mesh nodes and opening a browser session on them
— SSH, the vendor CLI hop, guestshell, RDP and VNC. Four PRs aligned its code to goat in
early August (identity seams, an Ory provider reading only `X-Forwarded-*`, SSH host-key pinning,
Secrets Manager credentials); the branch has been frozen at `36d89a6` since 2026-08-04 and is
deployed nowhere. The capability we do not otherwise have is **interactive access to bearer
gear** — a site network admin needs a console on a router or a switch, and that equipment carries
an operational support tail no amount of convergence on our side removes.

Three standing decisions were in the way, and all three postdate the branch.

**One door per device class.** [`ssh-key-self-service.md`](../design/ssh-key-self-service.md)
decided 2026-08-08 that a device class gets exactly one shell mechanism, never two, because two
grant surfaces and two revoke surfaces make revocation lie. Host nodes get `authorized_keys` and
keep `ssh_enabled` off; bearer gear gets NetBird SSH. Herd, holding its own per-node passwords in
`prisma/schema.prisma` and granting on Ory group membership alone, was a third door onto host
nodes: cutting a person's key left their Herd session working.

**Password authentication does not exist on goat hosts.** The fleet baseline is key-only SSH, so
Herd's stored-password path was not merely non-compliant, it could not open a goat host at all.
Of its three connection modes, none worked against one.

**A central controller is disqualified, not merely discouraged.**
[ADR 1109](1109-fleet-management-dimension-and-pull-convergence.md) D2 settled that
convergence is pulled, because [ADR 0206](0206-island-mode-by-construction.md) requires a site cut
off for thirty days to still manage its own nodes. A console that reaches out and drives N nodes
is the architecture that constraint exists to refuse. Herd could not have been that anyway — its
execution path is `runScriptOnNode(node, script)` with a single caller, one node at a time.

One mechanical finding shaped the rest. **A mgmt host *is* `INFRA`**, `INFRA` is omni-reaching and
omni-reachable, and `infra → network (any port)` already exists — so a console hosted there can
already reach every switch on every port, with no ACL change at all. **NetBird groups cannot nest
and extra membership unions reach**, which the taxonomy states directly. There is therefore no
group assignment that narrows an `INFRA` peer. The only variable that bounds Herd's reach is
*which peer its SSH egress leaves from*, which is what this ADR is mostly about.

## Decision

### 1. Herd is the bearer-gear console, and nothing else

Interactive access to `NETWORK`-class assets only — routers, switches, radios and other bearer
gear — one device at a time. **It may never open a session to a goat host node, and may never hold
a credential for anything outside the `NETWORK` class.**

Herd's map and monitoring half is **dropped**, not adopted and not re-pointed: it re-answers what
netvis, panoscope, snitch and stethoscope already answer, and fleet state has a live surface in the
consistency console. Herd deep-links those rather than reproducing them.

Herd therefore declares nothing and drives nothing, which settles the pulled-convergence question
([ADR 1109](1109-fleet-management-dimension-and-pull-convergence.md) D2) by construction rather
than by discipline.

### 2. Add `NETWORK-JUMP` to the node-class enum, and a matching group tier

The two lanes run on two peers. The **console** stays an ordinary hosted service on the mgmt host,
reached on 443 by the existing `all → infra` rule — no new permit, nothing novel. The process that
**opens SSH to gear** moves to its own minimal peer, which holds the key and is classed
`NETWORK-JUMP`.

Class and group carry the same name, per the F-213 name-match rule, and are added the way
[ADR 1113](1113-network-node-class-and-delegated-network-admin.md) added `NETWORK`: a value in
`ops/identity/grammar.yaml`, membership of `infraNodeClasses` in
`ops/goat-zenoh-portal/internal/registry/machines.go`, and a group suffix in
`ops/ansible/netbird_acl_taxonomy.yml`. Rules are declared as intents in
`ops/warrant/rule-intents/` and generated into the matrix
([ADR 1129](1129-warrant-scoped-expiring-network-authorizations.md));
`check-warrant-narrowing.py` proves the change grants nothing broader than stated. ADR 1129 and its
build both merged on 2026-08-14, so `ops/warrant/` is on `main` and the matrix is generated — the
sequencing caveat this paragraph originally carried is discharged.

Its reach, deliberately asymmetric and strictly narrower than `INFRA`:

| | |
|---|---|
| Reachable by | **Nothing.** The connector dials out, so no peer initiates toward it and there is no inbound rule |
| Initiates to | `NETWORK` on **tcp 22 only** — the entire purpose |
| Also initiates to | `INFRA` for DNS (53/5353), NTP (123), and 443 to submit its audit records |
| Never | fabric ports in either direction, lateral reach, `P2P`, or a destination of any `all → *` rule |

This is the `BREAKGLASS` outbound shape (ADR 1114)
without `BREAKGLASS` semantics. `NETWORK-JUMP` is **not** added to `delegableInfraNodeClasses`: the
people who use it hold a delegated entitlement, but the peer itself is operator-enrolled.

### 3. Interactive gear access gates on Ory identity **and** the delegated entitlement

Two checks, both required, in order:

1. The person authenticates through oauth2-proxy; Herd reads only the unforgeable
   `X-Forwarded-*` family and fails closed on absence. Already built and CI-guarded.
2. Herd checks `machine_grants.network_admin` on the person's custodian record — the site-scoped,
   fail-closed entitlement [ADR 1113](1113-network-node-class-and-delegated-network-admin.md) §3
   already created, which refuses a custodian with no grant and refuses a grant with no site.

**No new Ory group is created.** `NETWORK-ADMIN` was rejected by
[ADR 1113](1113-network-node-class-and-delegated-network-admin.md) and stays rejected; `HERD-ADMIN`
is unnecessary because the entitlement is the gate; and `ADMINS` in Herd's current map is the F-213
conflation the enum guard exists to catch. Herd's group map keeps `PLATFORM-ADMIN` and drops the
other three. A `herd` **service** row is added to `ops/permissions/group-permissions.yml` so the
door is documented where reviewers look.

The property this buys is the one the one-door rule demands: **a person's Herd access and their
entitlement are the same record**, so there is no second thing to remember to cut. This composes
with the revoke asymmetry already designed — install is role-scoped, revoke is the union of every
scope.

### 4. One shared service key, with the trigger to re-open it written down now

Herd holds a single service key rather than brokering each person's own. Attribution is per-person
inside Herd, emitting one `OperatorActionV1` naming the person, the device and the command; the
host sees Herd's account, the record does not.

This concentrates gear access in one box, and is acceptable **because of** the scoping rather than
despite it: the key opens gear, and gear cannot pivot — `NETWORK` initiates only to management-plane
services, with no fabric ports and never `P2P`.

> **If Herd's key ever gains reach to any class other than `NETWORK`, this decision is void and
> must be re-taken.** Scope creep here arrives one reasonable request at a time.

### 5. Keys to the device; device-local secrets only for the second hop

Hop one — mesh to the device's Linux side, over NetBird SSH as `NETWORK-JUMP`'s own identity. No
stored password; the first-hop password columns are **deleted**, not left unused, so no goat host
can be addressed by password even by misconfiguration.

Hop two — inside the device, to the vendor CLI, guestshell or RDP. These credentials are
device-local and have no key equivalent. They resolve from Secrets Manager, one secret per device,
never written to Herd's database and never displayed. This is the only place a username and
password legitimately exists.

## Consequences

- Two closed enums widen by one value each. Anything enumerating node classes or group suffixes
  must accept `NETWORK-JUMP`; the parity guards will say so if it does not.
- ~~The rule intents sequence behind PR #2550~~ — **discharged.** Warrant merged 2026-08-14, and the
  intents landed through `ops/warrant/` in #2566 rather than by hand-editing the generated matrix.
- Herd stops being a second answer to reachability, health and inventory. Those questions route to
  netvis, `goat warrant why-blocked`, the consistency console and the registry.
- One more peer per goatnet to enroll, patch and account for. That is the price of the containment;
  co-tenanting the egress on the mgmt host is what it buys out of.
- ZAGROS first. zagros-prod only after the gear console has run on ZAGROS and been re-probed. SPIFFE mTLS
  on the connector leg is ZAGROS-only today (ADR 0412), which
  suits that order and is a thing to solve before zagros-prod, not a reason to skip it.

## Alternatives considered

- **Run the egress on the mgmt host.** No new class, no new group, works today. Rejected for
  zagros-prod: a service driven by delegated non-platform admins, holding a shared gear key, would
  sit on the omni-reaching peer, and the ACL would contribute nothing to containment. Acceptable
  only as a sandbox expedient, and then only said out loud.
- **Put the peer in `NETWORK`.** Wrong on both sides: membership would expose it inbound any-port
  from `INFRA` and platform admins, and `NETWORK` has no lateral reach, so it still could not
  reach the gear it exists to reach.
- **Put the peer in `BREAKGLASS`.** Rejected — that class is deliberately not delegable, precisely
  so the entitlement that lets a site admin enroll a switch cannot enroll them into the rescue
  path, and a `BREAKGLASS` node is undefined without its bad-day path
  (ADR 1114 D3). Herd is a normal-day console.
- **Reuse `SERVICES` with a narrow setup key.** Reach follows the group, not the class, so this
  would work mechanically. Rejected because `node_class` is `closed: true` and
  `rides: [netbird-group, cert-uri-san]` — a group with no matching class name is the drift F-213's
  guards exist to catch.
- **Name it `HERD-JUMP`.** Rejected: the value is stamped into cert URI-SANs in a closed enum, the
  function outlives the software, and nothing in the mesh-group namespace is product-named
  (`ZENOH` and `SNITCH` are node classes, not groups). The vendor→org rename
  ([ADR 1052](1052-vendor-to-org-machine-rename.md)) is the standing lesson on that cost.
- **Name it `NETWORK-GEAR-*`.** Rejected: `gear` already names the ADR 1116
  mission-gear classes, where the class *is* the group and each is a rule-A fabric source — the
  opposite of what this peer may ever be.
- **Herd as a fleet console over the reconciliation join.** Specified in the first draft of this
  ADR and withdrawn the same day, before anything was built. The premise was false: the join
  already has a surface — RJ-1's coverage is "rendered on the node grid and served in
  `aggregate-v1`", and that node grid belongs to the **consistency console**, live on both
  goatnets. The draft had conflated a *deploy* with a *missing surface*: RJ-1/RJ-2 are built and
  awaiting a pin bump, while what the join genuinely lacks is RJ-3..RJ-6, all collector and
  read-model work. Building it would have put a second consistency console inside Herd. The need it
  was meant to serve — one place to see a fleet too large to manage box by box — is answered and
  under-deployed, and the work that serves it (bump the console pin, build RJ-3) does not belong in
  Herd.
- **An inbound control port on the connector.** The first draft left "which port does the console
  reach the connector on" as an open decision. Rejected by removing the question: the connector
  **dials out**. `onboard-runner` — the closest precedent, a privileged *mint-anything* capability
  driven by a console — polls rather than listens and is default-OFF; relays already reverse-tunnel
  `:443` over rathole; and pulled convergence says the privileged peer should dial home. So
  `NETWORK-JUMP` is outbound-only, no eighth port-set is needed (tcp 443 to the console is already
  in `netadmin`), and nothing on the mesh can initiate toward it. The cost is that the console must
  multiplex terminal sessions over a connection the connector opened.
- **Broker each person's own key instead of one service key.** Better host-side attribution, and
  the honest long-term answer. Deferred rather than rejected: it needs per-person key material on
  the jump peer, which is a larger design than the containment problem this ADR is solving.
- **Drop remote access and close Herd out.** Considered seriously, since the map and monitoring
  half duplicates existing surfaces. Rejected because the gear support tail is real and nothing
  else addresses it.

## Open items

- **Rotation interval and a named owner** for the second-hop device credentials. An unrotated
  device credential is the long tail of every network estate; leaving this implicit is how it
  becomes one.

## Revision history

- **2026-08-14** — accepted, then corrected twice the same day before any of it was built: the
  fleet-console lane was withdrawn and the connector's inbound control port was removed. Both
  corrections came from checking a premise this ADR had already asserted, and both rejected options
  are recorded under *Alternatives considered* so they are not re-proposed. Nothing had been built
  on the superseded text.
