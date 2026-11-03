# ADR 1182 — Platform admin is granted and revoked by one command on every plane

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-09-15 |
| Repo scope | goat core (goat-cli, `ops/ansible`) |
| Supersedes | — |
| Superseded by | — |
| Related | ADR 1031 (revocation, mesh-first and reversible), ADR 1071 (sponsorship authorization; operator keys are the platform-admin tier's credential), ADR 1088 (membership transfer, mirror-first), F-207 (operator-action audit), F-238 (NetBird admin reconciler); docs/design/platform-admin-lifecycle.md, docs/design/ssh-key-self-service.md, docs/design/mesh-and-ory-roles.md |

## TL;DR (for a layperson)

Making someone a platform admin, or taking it away, touches seven separate systems. Doing that by hand leaves some of them behind. One command now does all seven, shows what it will change before it changes anything, and checks every system afterwards. Taking the role away keeps the person as an ordinary developer unless the operator asks for a full offboard.

## Context

A platform admin's access lives in seven places that know nothing about each other: the Ory login group, the NetBird dashboard role, the NetBird groups on the person's laptops, SSH keys in `authorized_keys` on the management hosts, a portal operator key, the right to sponsor new people, and the team password manager's platform group.

A demotion by hand fails in predictable ways. A raw Kratos write skips the operator-action audit and can write `DEVELOPER`, a mesh role name the console refuses as a login group. The demotion is reported complete while grants on other planes stay live: an SSH key with passwordless sudo on the primary management host, an active operator key, sponsorship up to platform-admin, and a laptop still in the goatnet's platform-admin mesh group. The NetBird reconciler, assumed to propagate a demotion, only ever promotes, so a NetBird dashboard admin whose Ory groups were reduced to IAM-ADMIN stays a dashboard admin.

## Decision

1. **One command.** `goat admin platform-admin status|grant|revoke <email> --env <goatnet>` reads every plane, prints the change per plane, and executes only with `--apply`. After executing it reads every plane again and reports what the goatnet now holds.
2. **Revoke demotes by default.** The person keeps login and fabric access at the developer tier. `--full` also deactivates the login, blocks the NetBird user and strips the person's peers from every group but `All`. Nothing is deleted.
3. **Sponsorship and operator key follow the resulting level.** Level is read from the Ory login groups after the change: platform-admin sponsors up to platform-admin and holds an operator key; iam-admin sponsors up to iam-admin and holds no operator key; developer/browse neither. A grant requests and approves the key on the person's behalf; the console mails them the one-time collect link.
4. **Every write goes through a recorded path.** Ory, operator keys, SSH key rows and sponsorship go through the console or portal APIs, which audit server-side. NetBird, mesh-group, SSH-host and 1Password writes emit an OperatorActionV1 from the CLI. No raw Kratos writes.
5. **Revoke removes keys everywhere a login or a restore could use them.** Every `authorized_keys` file under `/home/*` and `/root`, their `.bak*` copies, and the dropbear initramfs key file, on every host in the goatnet's revoke union. Keys come from the console rows, `--pubkey-file`, and the keys GitHub lists for the person's recorded GitHub account. The play fails unless every host reads back clean.
6. **Order is load-bearing.** Revoke cuts access that works without a login first (shell, operator key), and changes Ory before the NetBird role, because the reconciler re-promotes anyone Ory still names. A revoke continues past a failed plane and exits non-zero; a grant stops if Ory fails.
7. **The NetBird reconciler stays promote-only and reports strays.** It logs every human NetBird admin without an entitling active Kratos identity and publishes `netbird_admin_reconcile_stray_admins`. Demotion is the command's job.
8. **CA operator is not part of platform admin.** Holding the CA key and operating a signing host are separate grants with their own revocation (re-encrypt and rotate); this command touches neither.

## Alternatives considered

- **A runbook listing the seven steps.** That is what failed: the person following it stopped when the planes they knew about were done.
- **Make the NetBird reconciler demote.** Rejected: a mistyped group or a Kratos outage would demote every admin on the goatnet in one tick.
- **A committed or gitignored roster file as the source of truth.** Rejected: each goatnet's Ory PLATFORM-ADMIN group already is the authority, and a second list would drift from it. `site_admins` stays the bootstrap seed only, reported by the command, changed by PR.

## Consequences

- A demotion is one command per goatnet and cannot silently leave a plane behind; the post-apply read shows every plane.
- A grant to someone who never signed in to the NetBird dashboard leaves that plane to the reconciler's next tick after their first sign-in.
- A hand-installed key that is neither in the console, nor supplied, nor on the person's GitHub account is not found by revoke.
- An admin cannot grant themselves an operator key; the console's two-person rule refuses it.
