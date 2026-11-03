# ADR 1117 — Per-env doors: cross-env management without switching the laptop mesh

**Status:** Accepted (2026-08-08, ops); Tier 2 first lane LIVE 2026-08-09 (zenoh-door — deployed + verified, M-105, track `aggregator-golive`; SSH lanes still designed-not-built) · **Design:** [env-doors.md](../design/env-doors.md) · **Track:** `env-doors`

## Context

Deploys and host administration across goatnets (dev, preprod, prod, …) have used the operator laptop's NetBird membership as the
transport. The profile is a machine-global singleton, and all goatnets
allocate peers from the same `100.64.0.0/16`, so a laptop can only ever be
on one mesh at a time. Consequences observed: env work serializes on
profile flips, flips disrupt concurrent sessions,
and profile drift wedges sessions.

## Decision

1. **The laptop's mesh membership is not the deploy transport.** Env
   selection happens per-connection, via a per-env **door**: a
   mesh-independent path that lands on one goatnet's mgmt host. Fleet
   reach inside the env is `ProxyJump` through that mgmt host over the
   env's own mesh.
2. **Tier 1 (live):** SSM port-forward through the env's cloud mesh peer
   (`AWS-StartPortForwardingSessionToRemoteHost` → mgmt host mesh `:22`).
   Zero server-side changes; SSH end-to-end with the laptop's key; stable
   local port per env (registry in env-doors.md);
   `ops/scripts/env-door.sh` is the entry point. Proven 2026-08-08:
   concurrent SSH + one Ansible run spanning two goatnets with the
   NetBird profile parked and untouched.
3. **Tier 2:** an mTLS-gated TCP lane through the existing relay ingress
   dispatch (SNI-pinned at the relay haproxy, Traefik TCP router on the
   enrollment entrypoint, `RequireAndVerifyClientCert` against the
   offline CA — the ADR 0843 hard-gate applied to one TCP route). Removes
   the AWS API from the connect path and covers goatnets with no cloud
   mesh peer. **First lane as-built 2026-08-09:** the
   `zenoh-door` lane carrying the CF-7 hosted aggregator's subscribe-only
   zenoh session (env-doors.md §"As-built"); the SSH lane variant remains
   designed-not-built.
4. Profile switching stays as break-glass, bus-announced, unchanged.

Isolation is unaffected: every door touches exactly one goatnet's
infrastructure; no cross-env path exists.

## Alternatives rejected

- **Concurrent NetBird daemons, one per mesh** — the meshes' `100.64.0.0/16`
  ranges overlap; routes collide. Renumbering live goatnets is a per-env
  flag day across pinned mesh IPs, `/etc/hosts` mgmt pins, DNS and ACLs.
- **A shared admin overlay spanning envs** — deliberately punctures the
  env isolation the account/domain separation exists to provide.
- **Do nothing (keep flipping profiles)** — the cost lands on every
  session, and grows with session concurrency.

## Consequences

- Concurrent sessions target different goatnets independently.
- Tier 1 inherits an AWS dependency at connect time and a per-env door
  peer; mesh/LAN paths remain as fallbacks, Tier 2 removes the AWS
  dependency when built.
- Inventories may reference `127.0.0.1:<door-port>` transports; the
  registry in env-doors.md is the one place door ports and peers live.
