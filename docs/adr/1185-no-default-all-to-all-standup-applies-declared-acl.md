# ADR 1185 — No goatnet carries a Default All→All policy; the standup applies the declared ACL before anything crosses the mesh

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-09-21 |
| Deciders | operator |
| Repo scope | goat core (`ops/ansible/templates/management.json.j2`, `ops/mgmt-host-bootstrap/`, `ops/ansible/goat-netbird-account-init.yml`, `ops/ansible/netbird-acl-cutover.yml`, `ops/scripts/goat-standup.sh`) |
| Supersedes | — |
| Superseded by | — |
| Amends | ADR 0807 (makes its default-deny enforceable); the `cutover.default_policy_action` vocabulary in `mesh-acl.md` (`delete` only, `mode=rollback` retired) |
| Related | ADR 0807 (microsegmentation), ADR 1084 (founder and joiner), ADR 1129 (warrant: the declared ACL), F-283 (groups on a flat mesh), issue #178 / PR #254 (`delete` the dormant Default) |

## TL;DR (for a layperson)

Every device on a goatnet can reach only what the written-down access rules allow. Until now a new goatnet started with a catch-all rule that let every device reach every other device on every port, and the rules were expected to replace it later. Three separate pieces of setup code created that catch-all, one of them silently, and one of them could put it back on a live goatnet. From now on no goatnet ever has a catch-all rule: the setup code does not create one, NetBird is told not to, and the standard standup applies the written rules first and then checks the live network for anything else.

## Context

ADR 0807 (Accepted 2026-04-25) decided that the mesh is default-deny: peers reach what a rule grants. Nothing enforced it. A goatnet acquired a Default All→All four ways:

| Source | How |
|---|---|
| NetBird itself | creates a `Default` All→All policy with every new account unless `DisableDefaultPolicy` is set (upstream #3970); goat never set it |
| `mgmt-host-bootstrap create-account` | the account seeder (`site-bootstrap.yml`, standup phase 6) inserted `pol_<site>_default` All→All with raw SQL on every new founder |
| `goat-netbird-account-init.yml` | standup phase 13 inserted the same row with raw SQL for any site whose matrix entry did not declare `delete`, including a site with no entry at all |
| `netbird-acl-cutover.yml mode=rollback` | the emergency undo re-enabled or recreated the Default and deleted every declared policy |

The standup never applied the declared ACL (`goat-standup.sh` ran neither `netbird-acl-bootstrap.yml` nor `netbird-acl-cutover.yml`), so every new goatnet was flat until someone applied the rules by hand. PR #254 made `delete` something a site could declare; it did not remove the seeders. A joiner with no matrix entry, whose NetBird store is replicated from its founder, running account-init puts an enabled All↔All into the live founder account, bypassing the API and its audit log.

## Decision

1. **No goatnet carries a policy that accepts from or to the `All` group**, at any stage of its life, for any reason, including an emergency. Reach comes only from the declared ACL (ADR 1129, `ops/warrant/`).
2. **Nothing creates one.** `management.json.j2` sets `"DisableDefaultPolicy": true` (not a variable). `mgmt-host-bootstrap create-account` and `goat-netbird-account-init.yml` write no policy. No file under `ops/` writes NetBird's `policies` or `policy_rules` tables with raw SQL, except five named soak, test and cleanup files.
3. **A joiner writes nothing into the NetBird account.** Its store is the founder's (ADR 1084). account-init refuses `goatnet_founder: false`; `goat-standup.sh` skips it for a joiner.
4. **The standup applies the declared ACL first.** Phase 0 refuses a goatnet whose ACL is not declared (`netbird-policy-posture.py --declared`). Phase 13 opens with `netbird-acl-bootstrap.yml`, cutover `mode=apply`, cutover `mode=enforce`, then gates on `netbird-policy-posture.py --url`, which reads the live account and fails on any `All`-group rule or any enabled policy the matrix does not declare. A joiner is gated against its founder's account. The cutover and bootstrap playbooks take `-e netbird_mgmt_url`, so the standup can target a goatnet they have no hard-coded entry for.
5. **`cutover.default_policy_action` is `delete` on every goatnet.** `disable` and `keep` are refused for a goatnet (`keep` survives only for exercise scopes, where it means "not this scope's to touch").
6. **`mode=rollback` is retired.** It refuses and prints the undo: `git revert` the rule PR, then `mode=apply -e reap_orphans=true`.

`ops/ci/check-no-default-allow.sh` (workflow `no-default-allow`) enforces 2 and 5 on every PR.

## Consequences

- A goatnet stood up from now on has no reach between peers until phase 13 applies its ACL. If a step in phases 10–12 needs peer-to-peer reach, the fix is to move the ACL phase earlier, never to seed a Default. A goatnet with no declared ACL cannot be stood up at all; every goatnet needs a matrix entry before its next rebuild.
- There is no one-command way back to a flat mesh. A bad rule is undone by reverting the rule.
- The posture check is a standup gate. A policy added in the dashboard after standup is found when the check is next run.

## Alternatives rejected

- **Keep the seed, key it on `site_id` correctly.** The guard already existed and failed open on a missing entry; a Default seeded "correctly" still makes every new goatnet flat until someone applies the rules.
- **Keep `mode=rollback` for emergencies.** An emergency is exactly when a flat mesh does the most harm, and it deleted every declared policy on the way.
- **Seed a disabled Default for convenience.** A disabled All→All is one dashboard toggle from a flat mesh; issue #178 removed it for that reason.

## Note on phase numbers

Phase numbers here are the standup numbering from before 2026-10-29; the
old-to-new table is [goatnet-standup.md §"Phase numbers"](../operations/goatnet-standup.md#phase-numbers).
