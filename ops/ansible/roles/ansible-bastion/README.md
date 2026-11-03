# ansible-bastion (on-site Ansible operator workspace)

> Anchor: ADR 0826 Amendment  (Orin-C utility-host expansion).
> · Operator runbook: `docs/operations/orin-c-utility-host.md`.

## Scope

Stand up Orin-C as a place an operator at the site can:

1. SSH into (per-operator UNIX accounts, no shared service account)
2. Pull the trunk repo (any HTTPS or SSH git remote — bring-your-own-auth)
3. Run Ansible plays against the local fleet

Composes with — does not displace — workstation-driven Ansible. Each
operator should still be able to run plays from their own laptop. The
bastion exists for the case where laptops are unavailable, off-LAN, or
the operator wants long-running plays to survive their SSH drop (tmux
ships in the package list).

## What this role installs

- Apt baseline: `ansible`, `ansible-lint`, `git`, `direnv`, `jq`,
  `tmux`, `sshpass`, `golang-go`, `shellcheck`, plus shell hygiene tools.
- UNIX group `goat-ops` (configurable) with workspace at
  `/opt/ansible-workspace` (NVMe-backed via `/opt/docker-data` per ADR
  0826 — same Orin-C disk, different mount path; setgid bit means new
  files inherit the group).
- Per-operator UNIX accounts, SSH `authorized_keys`, and `~/.gitconfig`.
- Optional pre-clone of trunk + pre-population of fleet `known_hosts`.

## Required vars

None — defaults work. Override the operator roster in
`group_vars/<host>/vars.yml`:

```yaml
ansible_bastion_operators:
  - name: alice
    email: alice@example.org
    ssh_pubkey: "ssh-ed25519 AAAA... alice@laptop"
    comment: "operator 1 — onboarded 2026-05-06"
```

Optional pre-clone:

```yaml
ansible_bastion_trunk_repo_url: "https://github.com/usareur-af/goatcore.git"
ansible_bastion_trunk_repo_dir: "goat"
ansible_bastion_trunk_repo_branch: main
```

## Self-test

After the role runs, operators can verify the bastion is ready:

```bash
/opt/ansible-workspace/self-test.sh
```

Exits 0 when every required tool is present and the workspace is
writable; non-zero with the failing check named on stderr otherwise.

## Why on Orin-C, not Orin-A/B

Per ADR 0826, Orin-A + Orin-B are the mgmt-plane pair (postgres-spock +
signal + netbird-mgmt + Traefik). They are deliberately quiet — a
foreground SSH session running tmux + ansible would compete with that
workload for IO and CPU. Orin-C is the obs/storage Orin and runs
aggregator + caching workloads that tolerate co-residency with operator
shells.

## Composes with

- `ca-signing-host` (same Orin-C, parallel `goat-ca-operators` group;
  CA-signing access is a strict subset of bastion access — same humans,
  different group membership, different access surface).
- `ansible-bastion` is the runtime *for* operating the rest of the
  cluster, including running this very role against new sites.
