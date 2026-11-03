# ADR 1138 — Pre-cut partner slot blocks: a restart per lane, not per partner

| Field | Value |
|---|---|
| Status | Proposed (2026-08-18) |
| Date | 2026-08-18 |
| Repo scope | goat core — Zenoh router ACL render, per-goatnet data-access group_vars |
| Supersedes | — |
| Superseded by | — |
| Related | [ADR 1025](1025-zenoh-dynamic-tenant-authz-slot-pool.md) (the pre-cut slot pool this generalizes) · [ADR 1027](1027-partner-release-set-governance.md) (release sets) · [ADR 1077](1077-affiliation-coalition-standing.md) (who is eligible for which lane) · F-156 (SIGHUP is a no-op — why an ACL edit costs a restart) · F-177 (denial is silent) · design: `partner-read-partition.md` |

## TL;DR (for a layperson)

Partners are supposed to see only the data deliberately released to them. Making that true for one partner currently means editing the network's access rules and restarting the component that routes all data — for every partner, one at a time. This decision reserves blocks of identity slots in advance, each already wired to a particular set of shareable lanes, so onboarding a partner becomes "give them one of the reserved slots" instead of "edit the rules and restart everything". The restart moves from once per person to once per new sharing lane, which is rare.

## Context

**The asymmetry.** Ordinary peers cost the router nothing at onboarding. ADR 1025 pre-renders a `slot-<anchor>` subject for all 1000 slots, so when the portal mints `CN=<uuid>` for slot *k*, the cert matches a subject that already exists — the template says it in as many words: *"no ACL edit, no restart."*

Partners break that pattern. The B2.1 read partition renders a `partner-<anchor>` subject and its `partner-r-*` / `partner-deliver-*` rules **per anchor**, driven by the `zenoh_partner_read_lanes` map. A new partner is a new map entry, a new render, and — because F-156 established that zenohd 1.9.0 silently ignores SIGHUP for ACL reload — a **router restart**. There is no live-reload path to fall back on.

**The arithmetic.** One map entry per partner, and a fabric-wide restart to make any of it true.

**Why this is not "just accept the restart".** A restart is a fabric-wide event on a brokerless fabric — every peer's session re-establishes. Coupling that to *person-joins-org* means the cost scales with the population, lands at the least predictable moment (whenever someone finishes a form), and creates a standing incentive to skip the carve. A partner who is not carved reads the whole fabric, and F-177 guarantees the failure is silent: nothing distinguishes "correctly partitioned" from "we never got round to it" without reading the rendered ACL.

## Decision

1. **Pre-cut partner slots by lane-set.** `zenoh_partner_slot_blocks` declares ranges of the existing slot pool, each bound to a lane-set: block 900–929 reads `REL-ALL`, block 930–949 reads `REL-ALL + REL-<PAIR>`. The router renders the partner subject and rules for **every slot in a block**, before anyone holds the corresponding cert. This is ADR 1025's mechanism applied to the partner dimension rather than a new one.

2. **Onboarding a partner becomes a mint-time slot choice.** Assigning a partner a slot from the correct block gives them a cert whose CN already matches a rendered subject — no ACL edit, no restart, exactly as for an ordinary peer.

3. **Unassigned pre-cut slots are inert.** No cert exists carrying that CN, so the subject matches nobody. Identical to the existing pre-cut pool: the ACL describes capacity, not population.

4. **The explicit per-anchor map survives and WINS on collision.** `zenoh_partner_read_lanes` remains for anchors minted outside a block — including everyone minted before this ADR. Where both name an anchor, the explicit entry wins: an operator's deliberate line about one anchor beats the block it happens to fall in. Both feed one merged map so every render site reads a single source.

5. **The restart moves from per-partner to per-lane-set.** Creating a new nation's bilateral lane still costs one restart. That is rare, plannable, and belongs to a governance act (declaring a release set) rather than to a person's onboarding.

6. **The lane-set is fixed at slot assignment.** Changing a partner's lanes later means moving them to a different block (a re-mint) or taking the restart. This is the deliberate price of pre-cutting, and it is stated rather than discovered.

7. **Blocks sit at the TOP of the pool** (900+) so they cannot collide with the sequential allocation the portal hands out from the bottom.

## Alternatives considered

- **Accept a restart per partner.** The status quo. Rejected — it scales with population, fires at unpredictable moments, and the pressure it creates is to skip the carve, whose failure mode is silent over-sharing.
- **Make the ACL live-reloadable.** Requires a zenohd fork or an upstream fix; F-156 is an upstream behaviour, and forking the router is the standing rejected option across this substrate.
- **A dynamic authorization service the router consults.** This is the broker the brokerless design exists to refuse — a central dependency in the data path, unavailable in island mode.
- **One pre-cut block per partner ORG.** Finer-grained but the block count then tracks the org population, which is the problem restated one level up. Lane-sets are few and change rarely; orgs are many and change often.
- **Per-slot lane-sets stored in the registry and rendered per mint.** Still an ACL edit per partner, so still a restart. Same cost, more machinery.

## Consequences

- Partner onboarding stops being a fabric event. The remaining restart is bound to declaring a sharing lane, which is a deliberate governance act with a natural change window.
- Blocks make capacity explicit: 30 partner slots on the broad lane, 20 on a bilateral pair. Exhausting a block is a visible, plannable event rather than a surprise.
- **Not retroactive.** Anchors minted before blocks are declared are not in a block, so they are carved through the explicit map and cost the one restart this ADR exists to avoid for their successors.
- The ACL grows by one subject and two rules per pre-cut slot. At 50 slots this is bounded and dwarfed by the 1000 pre-cut ordinary subjects already rendered.
- New obligation: block ranges must stay inside the pool and must not overlap each other — asserted in the render checks, not left to reading.

## Rollback

Entirely additive and independently revertible. Removing `zenoh_partner_slot_blocks` renders byte-identically to the previous template (asserted by a render check); the explicit map path is untouched. A goatnet that declares no blocks is unaffected. Reverting after partners have been assigned block slots returns them to the fabric-wide read — the pre-existing posture, not a lockout.
