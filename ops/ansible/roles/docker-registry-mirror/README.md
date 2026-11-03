# docker-registry-mirror (Docker / OCI pull-through cache)

> Anchor: ADR 0826 Amendment  (Orin-C utility-host expansion).
> · Operator runbook: `docs/operations/orin-c-utility-host.md`.
> · Sister role: [`apt-cacher-ng`](../apt-cacher-ng/) (same caching shape, apt protocol).

## Scope

Run pull-through caches for upstream container registries on Orin-C.
One `registry:2` instance per upstream — `docker.io`, `ghcr.io`,
`quay.io` by default, extensible. Same two leverage points as
apt-cacher-ng:

1. **Bandwidth** — fleet pulls each L4T base layer / Chainguard image
   once from upstream + N times from Orin-C.
2. **Island-mode resilience** — when WAN is down, fleet container
   pulls keep working as long as the cache is hot.

## Client routing

**For docker.io (the dominant case):** clients add `registry-mirrors`
to `/etc/docker/daemon.json` (out of scope for this role; deployed
fleet-wide via separate operator runbook):

```json
{
  "registry-mirrors": ["http://192.168.1.253:5000"]
}
```

Docker Engine treats `registry-mirrors` as a list it falls back through
on any pull from `docker.io` — when Orin-C is unreachable, pulls
silently route direct.

**For ghcr.io / quay.io / etc.:** Docker Engine has no per-registry
mirror knob (only docker.io). Two options:

- **containerd hosts.toml** — modern Docker / k3s / Nomad runtimes;
  drop a `/etc/containerd/certs.d/<registry>/hosts.toml` per upstream.
- **Image-tag rewrite** — pull `192.168.1.253:5001/owner/repo:tag`
  directly. Best for batch jobs / CI; not user-friendly for ad-hoc.

The mirror works regardless — the routing is the client-side detail.

## Required vars

None — defaults work.

Override per-host to add registries (`docker_registry_mirror_upstreams`)
or to change image pin / storage root / GC schedule.

## Self-test

```bash
sudo /usr/local/bin/docker-registry-mirror-self-test
```

## Garbage collection

Default schedule: weekly Sunday 04:00 UTC. registry:2's `garbage-collect`
subcommand drops blobs no longer referenced by any cached tag. Set
`docker_registry_mirror_gc_schedule: ""` to disable.

## Prometheus visibility

Per-upstream cache size + container-running gauge surface via
node_exporter's textfile collector at
`/var/lib/node_exporter/textfile_collector/docker-registry-mirror.prom`.
registry:2 also exposes its native `/metrics` on port 5001 of each
container; wire it into the obs-host scrape config later if desired.

## Composes with

- `obs-host` (Prometheus scrapes via node_exporter textfile collector).
- `apt-cacher-ng` (sister utility role for apt; same caching shape).
- `forgejo` (separately self-hostable container registry; this role
  caches **upstream** registries; forgejo serves goat's **own** images).
