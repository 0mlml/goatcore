# Platform-admin lifecycle — one command to grant or revoke

**Status:** Built and live-fired; decision in
[ADR 1182](../adr/1182-platform-admin-lifecycle-one-command.md). Shipped in goat-cli 0.5.5.
`--apply` proven in both directions against a notional account:
a promotion set the Ory group, requested and approved an operator key, and set sponsorship
to platform-admin; a demotion revoked the key, cleared sponsorship and emptied the login
groups. The host-shell plane was proven by running its playbook against
a mgmt host — key placed and verified, then removed and verified.

## What it does

One command makes a person a platform admin on a goatnet, or stops them being one, on every
plane that grants the role, and then reads every plane back to show what the goatnet now holds.

```
goat --context <env>-op admin platform-admin status <email> --env <env>
goat --context <env>-op admin platform-admin grant  <email> --env <env> [--pubkey-file key.pub] --apply
goat --context <env>-op admin platform-admin revoke <email> --env <env> [--full] --apply
```

Without `--apply`, `grant` and `revoke` print each plane's current state and the change they
would make, and change nothing. Run once per goatnet; each goatnet has its own identity store,
mesh and hosts. Add `--no-1password` on the second and later goatnet of a sweep, because the
1Password plane is team-wide.

**Revoke is a demote by default.** The person keeps their login and fabric access at the
developer tier, and `grant` reverses it. `--full` also deactivates the login, blocks the NetBird
user and strips the person's peers from every group except `All`. Nothing is deleted in either
mode.

## The planes

A person's level is read from their Ory login groups after the change: `PLATFORM-ADMIN` is
platform-admin, `IAM-ADMIN` is iam-admin, anything else is developer/browse. These are the tiers
`ops/goat-id-console/web/static/admin/onboard-intake-form.json` grants at onboarding. Sponsorship
and the operator key follow the level, so a demoted person ends up with what everyone else at
that level has.

| Plane | Grant | Revoke | Mechanism | Recorded by |
|---|---|---|---|---|
| **Ory login groups** | add `PLATFORM-ADMIN` | remove `PLATFORM-ADMIN` and legacy `ADMINS`; revoke sessions | console `POST /api/users/groups` | console audit + OperatorActionV1. The console emails the person a role-change notice |
| **NetBird dashboard role** | `user` → `admin` if the user row exists | `admin` → `user` (`--full`: also blocked). Refuses the account owner | NetBird `PUT /api/users/{id}` | OperatorActionV1 |
| **Mesh groups** | the person's peers join `<PREFIX>-PLATFORM-ADMIN` | the person's peers join `<PREFIX>-OBSERVER`, verified, then leave every `*-PLATFORM-ADMIN` group | NetBird group PUTs, add-then-verify-then-remove | OperatorActionV1 |
| **Host SSH keys** | the person's console keys placed on the hosts PLATFORM-ADMIN is scoped to; rows marked installed | the person's console keys, `--pubkey-file` keys and the keys GitHub lists for their recorded GitHub account removed from every host in the goatnet's revoke union; rows revoked | `ops/ansible/platform-admin-ssh-keys.yml`, which fails unless every host reads back as requested | OperatorActionV1 + console audit |
| **Operator key** | platform-admin holds one: approve a pending request, or request and approve one. The console mails the person the one-time collect link | any level below platform-admin holds none: live keys revoked, pending requests rejected | console operator-key API | console audit |

**Host keys reach the outage path too.** A site that has a breakglass node declares a `breakglass-path` scope in `operator_ssh_scopes`: the breakglass node over its independent overlay (Tailscale, ADR 1114) plus every host reached through it on the site LAN, each entry carrying `ssh_jump`. `PLATFORM-ADMIN` includes that scope, so the same grant and revoke cover the path that still works when the mesh is down.

| **Sponsorship** | authorized up to `platform-admin` | iam-admin: up to `iam-admin`; developer/browse: not a sponsor | portal `POST /api/orgs/{handle}/sponsorship` | OperatorActionV1 |
| **1Password** | added to the `Platform` group | removed from it | `op group user grant/revoke` | OperatorActionV1 |
| **`site_admins`** | reported | reported | read of `group_vars/<env>_hosts/vars.yml` | never edited; change it by PR |

**Which peers are the person's.** A peer counts when its NetBird `user_id` is the person's Kratos
identity id, when its name starts `peer-<handle>-` for a handle the console holds for their email,
or when `--peer <selector>` names it. Setup-key enrolments carry no `user_id`, so the name match
is what finds most laptops. `status` lists the matched peers so a missing device is visible
before `--apply`.

**Which hosts.** Grant uses `tasks/operator-ssh-scope.yml` with the PLATFORM-ADMIN role, the same
resolution `goat admin ssh-key install` uses. Revoke uses `operator_ssh_hosts` (the union of every
scope) plus `operator_ssh_revoke_extra_hosts`, because removal is safe to over-apply and a key left
on one host is a live shell. Revoke removes matching lines from every `authorized_keys`
and `authorized_keys2` under `/home/*` and `/root`, from their `.bak*` copies, and from the dropbear
initramfs key file, rebuilding the initramfs when that file changed. Lines match on the key body,
never the comment.

**Reaching a goatnet the laptop is not meshed to.** Open its door (`ops/scripts/door-open.sh <env>`)
and export `GOAT_SSH_PROXY_<ENV>`, for example
`GOAT_SSH_PROXY_<ENV>="ssh -p <port> -W %h:%p <user>@127.0.0.1"`. The Kratos read and the SSH
play both use it.

## Order

**Revoke:** SSH keys → operator key → sponsorship → mesh groups → Ory → NetBird role → 1Password.
Access that works without a login (a shell, an API key) goes first. Ory goes before the NetBird
role because `netbird-admin-reconcile` promotes anyone Ory still names on its next tick. A revoke
keeps going when one plane fails, because a partial revoke still leaves less access, and exits
non-zero naming the failed planes.

**Grant:** Ory → NetBird role → mesh groups → sponsorship → SSH keys → operator key → 1Password.
Ory is the authority; if it fails, nothing else runs.

## NetBird role reconciler: promotes, never demotes, reports strays

**Role map ([ADR 1189](../adr/1189-netbird-read-only-dashboard-and-warrant-owned-membership.md)).** Which NetBird role an Ory group earns is `netbird_mgmt_role_map`, an ordered list of `{group, role}`; the highest role a person's groups map to wins. The default reproduces the behaviour below (`PLATFORM-ADMIN → admin`). On a goatnet under ADR 1189 it is `NETBIRD-ADMIN → admin`, `PLATFORM-ADMIN → auditor`, `IAM-ADMIN → auditor`, so platform admins get a read-only dashboard and only `NETBIRD-ADMIN` holders can write. Lowering existing roles to match is the one-time `netbird-role-realign.yml` play, never the timer. Design: [`mesh-change-control.md`](mesh-change-control.md).

`netbird-admin-reconcile` (a systemd timer on each goatnet's mgmt host) promotes a NetBird user
to `admin` within one interval of their first dashboard sign-in when Ory names them
PLATFORM-ADMIN. It never demotes: an automatic demotion fired by a mistyped group or a Kratos
outage would strip every admin on the goatnet at once. It now logs every human NetBird admin
without an entitling, active Kratos identity as `STRAY` and publishes the count as
`netbird_admin_reconcile_stray_admins`. The fix for a stray is `revoke`.

## CA operator is not part of this

Holding the CA private key (an age recipient of `ca.key.age`) and operating a CA signing host are
separate grants. Revoking CA-key recipiency needs a re-encrypt and a CA rotation, because the
person already had the key. Neither is bundled into platform admin, and this command neither
grants nor revokes them.

## Source of truth

Each goatnet's Ory `PLATFORM-ADMIN` group is the authority for who is a platform admin there.
Every other plane is brought in line with it by this command. `site_admins` in group_vars is only
the bootstrap seed for a new goatnet's first identities; it does not grant anything on a running
goatnet, and the command reports rather than edits it.

## Limits

- **Operator key on grant needs two people.** The console refuses an approval by the key's owner,
  so an admin cannot grant themselves.
- **Keys installed by hand under a comment only.** Revoke finds console rows, `--pubkey-file` keys
  and the person's GitHub keys. A hand-installed key that is none of these is not found; pass it
  with `--pubkey-file`.
- **Ory read needs SSH to the mgmt host** (the Kratos admin API is loopback-only). The writes go
  through the console API and do not.
- **The NetBird dashboard user exists only after first sign-in.** A grant to someone who never
  signed in leaves that plane to the reconciler.
- **The command cannot run the host-shell plane from a goatnet's own mgmt host.** That plane
  drives `ops/ansible/platform-admin-ssh-keys.yml`, which needs ansible and a repo checkout;
  a mgmt host has neither. Run the command from an operator laptop that can reach
  the goatnet's console, or run the playbook separately.
- **A slow access-change email still costs the caller its deadline.** The console applies the
  Kratos patch and then waits on the mailer before answering (F-534).
  The command no longer reports that as a failure — it re-reads the identity and says the write
  landed — but the call is slow, and the console-side ordering is unfixed.
- **Operator-action events need `GOAT_OPERATOR_AGE_RECIPIENTS` where the command runs.** Without
  it every event falls back to a local log with a warning, so the actions are recorded off the
  fabric audit surface.
