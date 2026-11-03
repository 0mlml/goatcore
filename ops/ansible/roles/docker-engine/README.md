# docker-engine

Install Docker CE + Compose plugin from Docker's official apt repository. Idempotent. Multi-arch (amd64 + arm64).

Used by [`mgmt-host-baseline.yml`](../../mgmt-host-baseline.yml) so any new mgmt-host site gets Docker as part of the baseline turnkey chain — before `mgmt-host-luks.yml` and the per-site bootstrap playbook run.

## What it does

1. Probe for existing Docker install (`docker --version`); skip the install steps if present.
2. Add Docker's apt signing key + source list (arch-aware: `amd64` or `arm64`).
3. Install `docker-ce` + `docker-ce-cli` + `containerd.io` + `docker-buildx-plugin` + `docker-compose-plugin`.
4. Enable + start `docker.service`.
5. Verify both `docker` CLI and Compose plugin respond.

## Why this is a separate role

The cloud envs (vega PoC, integdev, hadev) had Docker pre-installed via the AWS AMI. On-prem mgmt-hosts (Jetson Orin, future bare-metal x86) don't. Discovered during the vega Orin-A bringup when `vega-bootstrap.yml` Phase 2 hit `Unit docker.service not found` on the `mgmt-host-compose-watch` enable step. Codified here so future site bringups don't re-discover the gap.

## Convention

Docker is the canonical container orchestrator today per [ADR 0207](../../../../docs/adr/0207-docker-compose-orchestration.md). Podman + Quadlet is the active successor track (`docs/design/container-orchestration.md`, Phase 2D spike). When that lands, this role gets a `docker_engine_use_podman` switch — until then, `docker-ce` is the only supported path.

## Usage

```yaml
- hosts: mgmt_hosts
  become: true
  roles:
    - docker-engine
```

Or as part of the standard mgmt-host bring-up chain:

```bash
ops/scripts/seed-ssh-key.sh "$USER@$HOST" <PASS_ENV_VAR>
ansible-playbook -l <host> mgmt-host-baseline.yml   # includes docker-engine
ansible-playbook -l <host> mgmt-host-luks.yml
ansible-playbook -l <host> <site>-bootstrap.yml -e ...
```

## Docker group membership (root-equivalent, opt-in)

By default this role adds no one to the `docker` group (`docker_group_members: []`),
so accounts without socket access use `sudo docker ...`. This role adds membership
and does not remove it: existing grants remain, and clearing the list does not revoke
a previous grant. Membership in the `docker` group grants passwordless access to the daemon
socket, which is root-equivalent on the host: the daemon runs as root, so socket access
can start a container that mounts the host and returns a root shell.

To grant it deliberately, per environment:

```yaml
docker_group_members: ["{{ ansible_user }}"]
```

Listed names must already exist on the host. The role refuses a missing name (the
getent gate fails the play, fail-closed) rather than creating a fresh account inside
the group. This replaces the previously manual `usermod -aG docker` step from
`docs/operations/deploy-guide.md` with a visible, off-by-default, gated setting.

After adding an account, reconnect its login session so supplementary groups refresh.
Revoking an existing grant requires a deliberate host access change; clearing this
setting is not a revocation command.

## inotify ceiling

The role raises `fs.inotify.max_user_instances` to 1024 (and
`max_user_watches` to 524288) in `/etc/sysctl.d/99-goat-inotify.conf`. The
kernel default of 128 is per UID and shared by every container running as
root; a goatnet mgmt host runs over 100 of them. When the budget is spent, a
service that opens a file watcher at start (Traefik's file provider) comes up
with no routes and the whole edge answers 404. Override
`docker_engine_inotify_max_user_instances` per host group if a host needs more.
