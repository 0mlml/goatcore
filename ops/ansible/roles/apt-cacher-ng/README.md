# apt-cacher-ng (apt forward-proxy cache)

> Anchor: ADR 0826 Amendment  (Orin-C utility-host expansion).
> · Operator runbook: `docs/operations/orin-c-utility-host.md`.

## Scope

Run a caching forward proxy for apt repositories on Orin-C so the rest
of the fleet pulls Ubuntu / Debian / NVIDIA Tegra packages from the
Orin-C local mirror instead of upstream. Two leverage points:

1. **Bandwidth** — every Orin runs the same apt baseline; pulling each
   package once from upstream and N times from Orin-C saves the WAN.
2. **Island-mode resilience** — when the WAN is partitioned, fleet
   apt operations continue against Orin-C as long as the cache is hot.

Composes with — does not displace — the operator's own apt sources.
Each peer's `/etc/apt/apt.conf.d/00goat-cache.conf` (deployed via a
separate role-side patch or operator runbook) sets:

```conf
Acquire::http::Proxy "http://192.168.1.253:3142";
```

When Orin-C is unreachable, apt falls through to the upstream mirror
on its own (it tries the proxy first, fails fast, retries direct).

## Architecture choice

Native apt package, not Docker. apt-cacher-ng is small, well-packaged,
and one of the workloads where co-residency with the host's own apt
operations is a feature (you can prime the cache by running `apt
update` on the host once).

## Required vars

None — defaults work. Override per-host to bind the listener to
specific interfaces (recommended once wg-cp0 is up):

```yaml
apt_cacher_ng_bind_addresses:
  - "192.168.1.253"   # LAN
  - "198.18.0.3"      # wg-cp0 (when assigned)
```

## Self-test

```bash
sudo /usr/local/bin/apt-cacher-ng-self-test
```

## Prometheus visibility

Cache size, file count, and systemd unit state surface via
`/var/lib/node_exporter/textfile_collector/apt-cacher-ng.prom` (cron
every 5 min). Grafana dashboard is part of the obs-host follow-on.

## Composes with

- `obs-host` (same Orin-C; Prometheus scrapes node_exporter which
  exposes the textfile-collector metrics).
- `docker-registry-mirror` (sister utility role for container images;
  same caching shape, different protocol).
