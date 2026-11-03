# ADR 1183 — A site declares its break-glass and network-jump nodes, or declares why it has neither

- **Status:** Accepted
- **Date:** 2026-09-18
- **Deciders:** operator
- **Amends:** ADR 1141 (four-node host-site
  baseline — the core pair and obs pair stand; this adds the two rescue roles the
  baseline never named)
- **Related:** ADR 1114
  (the `BREAKGLASS` class and its two-path requirement),
  [ADR 1131](1131-network-jump-class-and-herd-gear-scope.md) (the `NETWORK-JUMP`
  class — reach to switches, routers and out-of-band gear management),
  F-533
  (a break-glass node dark, unnoticed),
  [`goatnet-standup.md`](../operations/goatnet-standup.md) (the driver this binds to),
  [`goatnet-deployment-doctrine.md`](../design/goatnet-deployment-doctrine.md)

## Context

Two node classes exist to be used on the worst day a site has, and neither was
part of the path that builds a site.

**The break-glass node** (ADR 1114)
is the way in when the mesh and the control plane are both down. Break-glass
nodes did not arrive through the standup driver: `ops/ansible/breakglass-nodes.yml`
is invoked by no phase, no gate and no timer, and a node can end up with no
configuration scope on the goatnet it serves at all — its only `group_vars` home
on a different goatnet, so a rebuild would not reproduce it.

**The network-jump host** ([ADR 1131](1131-network-jump-class-and-herd-gear-scope.md))
is how an operator reaches the switch, the router and the gear's own management
interfaces. The class is declared everywhere it needs to be — identity grammar,
four ACL templates, groups across goatnets — and had **no peers**. `ops/ansible/herd-connector-deploy.yml` deliberately has no
default target, and no inventory anywhere names a host for it.

The cost of leaving both outside the driver is not hypothetical.
F-533:
a break-glass node that was never added to the roster of peers whose absence is
graded can sit dark after a site power cut, and nothing says so. A rescue node that nobody converges, nobody grades and nobody
counts is a node the site believes it has.

`ops/bootstrap/hardware-classes.yaml` already calls a host site **six machines**,
and names both roles with their purposes. Nothing in ansible, the standup driver
or CI reads that file — it selects an image at first boot. So the six-machine
shape was written down and the four-node shape was the one the tooling built.

## Decision

**A site's configuration states, for each rescue class, either the node it has or
why it has none. The standup driver converges what is declared, and refuses to
proceed while a site has said nothing.**

Two keys per class in `group_vars/<site>_hosts/`, the same shape
`has_obs_host` already uses for the observability node:

| Key | Meaning |
|---|---|
| `has_breakglass_node` | `true` — this site has a `BREAKGLASS` node (ADR 1114), in inventory group `<site>_breakglass_hosts` |
| `breakglass_absent_reason` | required when `false`: one sentence naming what covers the site instead, or what is on order |
| `has_network_jump_node` | `true` — this site has a `NETWORK-JUMP` host (ADR 1131), in inventory group `<site>_network_jump_hosts` |
| `network_jump_absent_reason` | required when `false` |

Three consequences, each in one place:

1. **`standup-config-preflight.py` fails closed** on a site that sets neither the
   flag nor a reason. A new site cannot be stood up without someone answering the
   question, which is the point: the answer "none, because the site is a single
   rack and the switch is in the same cabinet as the operator" is a fine answer,
   and an unconsidered silence is not.
2. **`goat-standup.sh` phase 14 converges each declared node** —
   `breakglass-nodes.yml` for the first, `herd-connector-deploy.yml` for the
   second — and prints the recorded reason where a class is declared absent. A
   phase that skips says why it skipped.
3. **A declared break-glass node is a graded peer.** Its absence alerts through
   `mesh_health_peer_classes` (ADR 1042), which is what F-533 found missing.
4. **A node is converged only once it is adopted.** A node not yet adopted
   carries `breakglass_node_adopted=false` and `breakglass-nodes.yml` ends on it,
   so a stray inventory choice cannot converge a live production box nobody
   adopted.

**Declared-but-not-yet-built is a first-class state.** `has_obs_host: true` with
no host in inventory is already normal for a site whose third box has not
arrived: the leg skips with a note and the acceptance rows read N/A rather than
PASS. The rescue classes behave identically. A site that intends a break-glass
node and does not have one yet is in a different state from a site that has
decided it needs none, and the configuration distinguishes them.

**What this does not do.** It does not mandate that every site have either node.
Sites differ — a cloud-resident site has no switch to reach and its console is
the provider's. It does not enrol either node onto the mesh; enrolment stays
manual for the break-glass node by ADR 1114's own reasoning (an auth key is not a
declared fact). It does not change either class's ACL position.

## Consequences

**A rebuild reproduces the rescue nodes.** Without the declaration, a converged
node and an unconverged one look the same until somebody rebuilds one of them.

**Existing sites answer the question once.** The declaration was written for
every site group in the same change, from what the repository and the live
planes already showed — a cloud site's rescue path is the provider's SSM
session, a dev goatnet's is the door its own door registry declares, a LAN-only
lab has no mesh to lose. `ops/ci/check-site-rescue-nodes.py` is
therefore fail-closed rather than warn-level: there was no debt to grandfather,
and a site added later answers too. It derives the site list from `group_vars`,
so a new site cannot be missed by forgetting to add it to a list.

**The network-jump class stops being inert by accident.** A goatnet with no jump
peer may be in the right state — but it is currently the *undeclared*
state, indistinguishable from an oversight. After this, a site with no jump host
says so and names what covers gear access instead.

## Alternatives

**Leave both outside the driver and rely on the class documents.** This is the
status quo, and it produced a node with no config scope on its own goatnet, a
class with no peers anywhere, and an outage nobody saw. Documents that
describe a class do not build one.

**Mandate both nodes at every site.** Rejected: it would either be waived
routinely, which teaches that the baseline is advisory, or it would block sites
that genuinely do not need a jump host. A required *answer* is enforceable in a
way a required *node* is not.

**Put the roster in `ops/bootstrap/hardware-classes.yaml`**, which already names
both roles. Rejected: that file selects an image at first boot and has no per-site
instance dimension. The site's `group_vars` is where every other per-site fact
lives, including `has_obs_host`, and a second home for the same kind of fact is
the drift this repository keeps paying for.
