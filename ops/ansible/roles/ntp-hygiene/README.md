# ntp-hygiene role

NTP hygiene MVP (v1) for a production mgmt-host pair. Deploys
**chrony with NTS sources only**, the upstream SuperQ/chrony_exporter
systemd unit, a preflight gate, and a smoke-test against the local
exporter.

Policy: [`docs/design/universal-ntp-hygiene.md`](../../../../docs/design/universal-ntp-hygiene.md)
Mechanics: [`docs/design/mgmt-plane-threat-mitigations.md`](../../../../docs/design/mgmt-plane-threat-mitigations.md) §1.1 (items N1-N5)
Debt retirement: `docs/design/ha-multi-site-validation.md` item 18 (chrony + NTS + 5 alerts + preflight + smoke-test) and item 22 v1 (this role is the stand-in for the first iteration of the `ntp-health-agent` daemon — see §§"Deferred" below).

## MVP scope (v1)

| What | Where |
|---|---|
| chrony 4.2 installed (Ubuntu 22.04 apt default) | `tasks/main.yml` |
| `/etc/chrony/chrony.conf` rendered — NTS sources only, no plain NTP | `templates/chrony.conf.j2` |
| 4 NTS sources: `time.cloudflare.com`, `nts.ntp.rnl.se`, `ptbtime1.ptb.de`, `ohio.time.system76.com` | `defaults/main.yml` (`chrony_nts_sources`) |
| SuperQ/chrony_exporter v0.13.3 as a systemd unit, bound to `127.0.0.1:9123`, reading `/run/chrony/chronyd.sock` | `tasks/main.yml` + `templates/chrony_exporter.service.j2` |
| Scrape job `chrony` in `ops/compose/mgmt-host/monitoring/prometheus.yml` reaches the exporter via `host.docker.internal:9123` (host-gateway alias in `docker-compose.obs.yml`) | `../../../compose/mgmt-host/` |
| 5 alert rules | `ops/compose/mgmt-host/monitoring/rules/ntp.yml` (vega copy) + `ops/monitoring/prometheus/rules/ntp.yml` (skeleton) |
| Preflight gate — refuses to proceed if no selected source OR offset > 200 ms | `tasks/preflight.yml` |
| Smoke-test — offset < 100 ms and source_count > 0 via `chrony_exporter` metrics | `tasks/smoketest.yml` |
| Reusable preflight import for any playbook | `ops/ansible/tasks/ntp-preflight.yml` |

## Alert rules → chrony_exporter metric mapping

From [upstream collectors](https://github.com/SuperQ/chrony_exporter/tree/main/collector):

| Alert | Metric(s) | Source |
|---|---|---|
| `ChronyClockOffsetHigh` (>500 ms for 2m) | `chrony_tracking_last_offset_seconds` | `collector/tracking.go` |
| `ChronyNtsSourceUnreachable` (any source unreachable 5m) | `chrony_sources_state_info{source_state=~"(?i)unreachable\|not.?reachable"}` | `collector/sources.go` |
| `ChronyDaemonDown` | `up{job="chrony"}`, `absent(chrony_tracking_last_offset_seconds)` | Prometheus built-in + `collector/tracking.go` |
| `ChronyNoReachableSource` (no source 5m) | `chrony_sources_reachability_success` | `collector/sources.go` |
| `ChronyDriftRateHigh` (>50 ppm for 10m) | `chrony_tracking_frequency_ppms` | `collector/tracking.go` |

## Running

```bash
cd ops/ansible
OBJC_DISABLE_INITIALIZE_FORK_SAFETY=YES aws-vault exec legacy --no-session -- \
  ansible-playbook \
    -i inventory/hosts.ini \
    -i inventory/mgmt-hosts.ini \
    vega-ntp-hygiene.yml
```

Run `vega-ntp-hygiene.yml` **before** `vega-observability.yml` so
Prometheus starts with the `chrony` scrape target + rules already in
place.

## Reusing the preflight

Any playbook that depends on LWW-correct timestamps (most importantly
`coordinated-upgrade.yml`, and the forthcoming `vega-bootstrap.yml`)
should call the preflight at the top:

```yaml
- name: Preflight — NTP must be healthy before proceeding
  ansible.builtin.import_tasks: tasks/ntp-preflight.yml
```

The shared task file at `ops/ansible/tasks/ntp-preflight.yml`
delegates into the same `preflight.yml` this role uses, so the
threshold (`chrony_preflight_offset_ms`, default 200 ms) stays single-
sourced.

## Block 12G — NTS server tier (per ADR 0834)

The role optionally turns each mgmt-host into an NTS *server* in
addition to its existing NTS-client role. Same chrony binary, same
role; gated by `chrony_serve_nts: true` per host.

### When to flip the flag

- ✅ Each mgmt-host (Orin-A / Orin-B / Orin-C per ADR 0826) at every
  production site that should fan out time to in-mesh peers.
- ❌ Relay VPSes, integdev, validation envs — these stay as pure NTS
  clients (default).

### Pre-flight (operator-fired BEFORE flipping the flag)

Issue an internal-CA leaf cert against each Orin via `cert-sign --eku
serverAuth` (per ADR 0407):

```bash
cert-sign \
  --site-id vega \
  --cn ntp.vega.netbird.legacy.example \
  --san <wg-cp0-ip> --san <lan-ip> \
  --san ntp.vega.netbird.legacy.example \
  --validity 8760h \
  --eku serverAuth \
  --out-cert /etc/chrony/server-cert.pem \
  --out-key  /etc/chrony/server-key.pem
```

The role asserts both files exist before rendering chrony.conf —
forgetting this step yields a clear pre-flight failure (not a
confusing chrony-side log line).

### What changes in chrony.conf

| Flag value | `port` | `ntsserverkey` | `allow` lines | `local stratum 10` |
|---|---|---|---|---|
| false (default) | 0 (closed) | absent | absent | absent |
| true | 123 (NTP) | from `chrony_serve_nts_cert_path` | per `chrony_serve_nts_allow_cidrs` | present (orphan-mode last-resort) |

NTS-KE handshake terminates on `chrony_serve_nts_port` (default 4460).

### Defaults

- `chrony_serve_nts_allow_cidrs`: `["192.168.1.0/24", "198.18.0.0/24"]`
  — same-site canonical LAN + wg-cp0 silent control plane.
- `chrony_serve_nts_cert_path`: `/etc/chrony/server-cert.pem`
- `chrony_serve_nts_key_path`: `/etc/chrony/server-key.pem`

### Test fixture

```
ops/ansible/roles/ntp-hygiene/tests/test-block-12g.sh
```

Two tests: `--syntax-check` on both flag shapes; jinja-render
verification that client-only mode emits `port 0` (server hardened
off) and server-tier mode emits `port 123` + `ntsserverkey` + `allow
<cidr>` + `local stratum 10`. PASS 4/4.

## Expected steady-state metrics

After a clean run, the exporter should show:

```
chrony_tracking_last_offset_seconds          < 0.01   (typically  ~100 µs)
chrony_tracking_rms_offset_seconds           < 0.02
chrony_tracking_frequency_ppms               abs < 10  (healthy hardware)
chrony_sources_reachability_success          1 for 3+ of the 4 configured sources
count by (site) (chrony_sources_state_info)  >= 4
```

Any of the 5 alerts firing against these steady-state values means
something has actually changed — not a tuning issue.

## Deferred items (NOT in v1) — explicit retirement paths

| Deferred | Why deferred | Retirement path |
|---|---|---|
| **GNSS receiver integration** (N6–N7 in mgmt-plane-threat-mitigations.md §1.2) | Hardware integration is separate from this role | Re-render `chrony.conf.j2` to add `refclock PPS /dev/pps0` + `refclock SHM 0 refid GPS` + `allow` for the gpsd bridge. |
| **`ntp-health-agent` daemon** (debt item 22, universal-ntp-hygiene.md §4) | v1 "mainline" daemon is Phase 2 scope (~3–5 days); this Ansible role covers the chrony-+-exporter subset of what the daemon will eventually do | Build the Go daemon per §4.4; retire the direct chrony_exporter scrape in favour of scraping the daemon's `:9753/metrics`; local consumers move from reading the `/metrics` HTTP endpoint to the Unix socket at `/run/ntp-health.sock`. |
| **Cross-site peer attestation** (debt item 23, universal-ntp-hygiene.md §5) | Resolves the self-diagnosis paradox; requires Zenoh data fabric + signed attestation publishing; v2 daemon work | Phase 3. Once daemon v2 lands, each mgmt-plane host subscribes to `@/ntp-health/**` and computes pairwise offsets; a divergence-detected alert fires alongside the existing 5 rules. |
| **Enrollment-time NTP health check** (debt item 20) | Setup-key activation currently admits any peer regardless of local clock state | Phase 3 pre-hook on the management API; peer-provided attestation validated against server time for RTT sanity. Fail-closed per universal-ntp-hygiene.md §7. |
| **Auto-demote on sustained NTP degradation** (debt item 20) | Requires role.publisher / role.subscriber Netbird groups + a cron that reads the Prometheus rule state and calls the mgmt API to flip group membership | Phase 3 — covered by the same work that retires debt item 20. |
| **Client-side NTS-KE failure metric** (upstream gap) | `chrony_exporter` v0.13 does not emit a client-side NTS authentication-failure counter (only serverstats NTS metrics) | Raise upstream issue, or surface via the `ntp-health-agent` daemon v1 which has direct access to chrony's log stream. Until then `ChronyNtsSourceUnreachable` is the best available proxy. |

## Known limitations

- **Alert 2 (NTS auth failure) is a proxy.** See "Client-side NTS-KE failure metric" above — it fires on source unreachability, not true auth failure. An authentication-specific failure (bad cert, expired key) and a simple UDP-blocked source look identical at this layer. The runbook should direct the operator to `journalctl -u chrony | grep -i nts` for definitive root cause.
- **Preflight runs on the same host being checked.** Self-diagnosis paradox unresolved until peer attestation lands (v2). For v1 we accept this: the 5 alerts cover the obvious failure modes, and a silently-broken chrony with good-looking local numbers is what v2 is specifically designed to catch.
- **Prometheus scrapes chrony_exporter over `host.docker.internal`.** Works on Docker ≥ 20.10 with `host-gateway` alias support. Older Docker engines would need `network_mode: host` on the Prometheus container — not an issue on Ubuntu 22.04 + current engine.
