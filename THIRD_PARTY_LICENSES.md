# Third-Party Licenses and Attributions

goat is licensed under the Apache License 2.0 (see [LICENSE](LICENSE)
and [NOTICE](NOTICE)). This document catalogs the third-party software components
that goat depends on and runs alongside, with their licenses and the
links required to satisfy each component's attribution and source-availability
obligations.

**Posture.** Per [ADR 0111](docs/adr/0111-vendor-friendly-license-posture.md),
goat is vendor-friendly open-core: our own code is Apache 2.0
(maximum vendor flexibility, including patent grant); third-party dependencies
are tier-preferred (Permissive > Weak copyleft > Strong copyleft > Network
copyleft); and anti-commercial licenses (SSPL, Commons Clause, Elastic v2,
restrictive BSL, "non-commercial" custom licenses) are **disqualified by
default**. Tier-3 (GPL) and Tier-4 (AGPL) components are accepted only at
**process boundary** — they run as separate processes / containers, never linked
as libraries into our own binaries.

**Audit basis.** Component classification reflects the
license audit. The
recurring CI re-check is specified at
docs/project/license-audit-ci-spec.md.
Updates to this file land in the same PR as any dependency change that adds or
upgrades a tracked component.

---

## Tier 1 — Permissive licenses

Components below are under Apache-2.0, MIT, BSD-2/3-Clause, ISC, or the
PostgreSQL License. No source-redistribution obligation; copyright notices
preserved per their respective licenses.

### Apache-2.0

| Component | Upstream |
|---|---|
| Caddy | https://github.com/caddyserver/caddy |
| CoreDNS | https://github.com/coredns/coredns |
| Authelia | https://github.com/authelia/authelia |
| Prometheus | https://github.com/prometheus/prometheus |
| Alertmanager | https://github.com/prometheus/alertmanager |
| Prometheus node_exporter | https://github.com/prometheus/node_exporter |
| Prometheus postgres_exporter | https://github.com/prometheus-community/postgres_exporter |
| OpenTelemetry Collector | https://github.com/open-telemetry/opentelemetry-collector |
| Falco | https://github.com/falcosecurity/falco |
| osquery (dual-licensed Apache-2.0 / GPL-2.0; we elect Apache-2.0) | https://github.com/osquery/osquery |
| eclipse-zenoh (dual-licensed Apache-2.0 / EPL-2.0; we elect Apache-2.0) | https://github.com/eclipse-zenoh/zenoh |
| Zenoh DDS bridge | https://github.com/eclipse-zenoh/zenoh-plugin-dds |
| Trivy | https://github.com/aquasecurity/trivy |
| Grype | https://github.com/anchore/grype |
| rathole | https://github.com/rapiz1/rathole |
| Rosenpass | https://github.com/rosenpass/rosenpass |
| Dendrite (Matrix) | https://github.com/matrix-org/dendrite |
| Ory Kratos | https://github.com/ory/kratos |
| Ory Hydra | https://github.com/ory/hydra |
| Puppet | https://github.com/puppetlabs/puppet |
| AWS-LC | https://github.com/aws/aws-lc |

### MIT

| Component | Upstream |
|---|---|
| Traefik | https://github.com/traefik/traefik |
| pgBackRest | https://github.com/pgbackrest/pgbackrest |
| liboqs (Open Quantum Safe) | https://github.com/open-quantum-safe/liboqs |

### BSD-2-Clause

| Component | Upstream |
|---|---|
| nginx | https://github.com/nginx/nginx |

### BSD-3-Clause

| Component | Upstream |
|---|---|
| netbird (client/ component; Tier-4 components in same repo are AGPL-3.0 — see below) | https://github.com/netbirdio/netbird |
| Coturn | https://github.com/coturn/coturn |
| dante (SOCKS5, used in Block 17 egress) | https://www.inet.no/dante/ |

### PostgreSQL License (BSD-derivative)

| Component | Upstream |
|---|---|
| PostgreSQL | https://www.postgresql.org/ |
| pgEdge Spock multi-master replication extension | https://github.com/pgedge/spock |

### ISC / 0BSD / Unlicense — none currently in our direct stack

---

## Tier 2 — Weak copyleft

Components below are under LGPL, MPL, or EPL. Modifications to the licensed
*libraries themselves* must be released under the same terms; vendors can build
proprietary work on top via dynamic linking (LGPL) or per-file separation
(MPL) or process boundary.

### EPL-2.0 / BSD-3-Clause (dual)

| Component | Upstream | Notes |
|---|---|---|
| (none currently in our direct stack) | | |

(No LGPL or MPL components currently in our direct stack.)

---

## Tier 3 — Strong copyleft (GPL)

Components below are under GPL-2.0 or GPL-3.0. Each runs as a **separate
process** and is **not linked** into any goat binary. Per ADR 0111
D4 process-boundary discipline, this aggregation pattern keeps the GPL
obligation cleanly attached to the GPL binary.

### GPL-2.0

| Component | Upstream | Use shape |
|---|---|---|
| WireGuard tools (`wg`, `wg-quick`); kernel module is dual GPL-2.0 / BSD-3-Clause | https://git.zx2c4.com/wireguard-tools/ | Daemon / kernel module on every mesh peer |
| Squid (egress proxy, Block 17) | https://github.com/squid-cache/squid | Daemon process on per-site router |
| Keepalived (intra-site VRRP, ADR 0205) | https://github.com/acassen/keepalived | Daemon process on Router-A/B |
| auditd (Linux audit subsystem, host-level) | https://github.com/linux-audit/audit-userspace | Host-level — boundary is OS distribution, not goat |

### GPL-3.0

| Component | Upstream | Use shape | Notes |
|---|---|---|---|
| wolfSSL (named alternative to AWS-LC for FIPS PQC boundary; AWS-LC preferred) | https://github.com/wolfSSL/wolfssl | Boundary-node daemon | Confirmed GPL-3.0 (not GPL-2.0 as previously documented) per license audit . AWS-LC (Apache-2.0) is the cleaner Tier-1 alternative; wolfSSL is on standby for any FIPS-PQC requirement that forces it. |
| stunnel (not currently deployed; named in some boundary-path discussions) | https://github.com/mtrojnar/stunnel | Daemon process if adopted |

---

## Tier 4 — Network copyleft (AGPL-3.0)

AGPLv3 §13 (the Affero clause) requires that we make **corresponding source
available to network users** of the modified-and-served Work. The links below
satisfy that obligation. Per ADR 0110 D4, all AGPLv3 components run at process
boundary — they are containers / daemons reached via API or socket; no AGPLv3
code is linked into any goat binary.

| Component | Upstream source | Our pin / fork | Modified by us? |
|---|---|---|---|
| netbird `management/` (we run our patched build per [ADR 0301](docs/adr/0301-patched-netbird-management-image.md)) | https://github.com/netbirdio/netbird | Pinned ref `53b04e512af0d94f619e870467115c43d200830b` (see [ops/netbird/Dockerfile](ops/netbird/Dockerfile)). Patches in [ops/netbird/patches/](ops/netbird/patches/). | **Yes** — see F-010 and [ADR 0110](docs/adr/0110-license-posture-agplv3-forked-components.md). §13 advertised on the management API status response (in flight, Block 22 D5). |
| netbird `signal/` | https://github.com/netbirdio/netbird | Unmodified upstream binary. | No. |
| netbird `relay/` | https://github.com/netbirdio/netbird | Unmodified upstream binary. | No. |
| netbird `combined/` (not used) | https://github.com/netbirdio/netbird | Not deployed. | No. |
| MinIO (audit-log integrity store per [ADR 0811](docs/adr/0811-audit-log-integrity.md), and substrate for Block 27 wide-event observability per [ADR 0822](docs/adr/0822-wide-event-observability-otlp-openobserve.md)) | https://github.com/minio/minio | Upstream pinned digest. | No. |
| OpenObserve (wide-event observability per [ADR 0822](docs/adr/0822-wide-event-observability-otlp-openobserve.md); not yet shipped) | https://github.com/openobserve/openobserve | TBD pinned digest at first deploy. | No. |
| Grafana (dev-machine only; not in production deployment) | https://github.com/grafana/grafana | Upstream. | No. |
| Zabbix (vendor-server-only; relicensed AGPL-3.0 in 2024) | https://github.com/zabbix/zabbix | Vendor-server-only; outside our deployment surface. | No. |

---

## Tier 5 — Disqualified by default (vendor-hostile licenses)

The following license classes are **not allowed** as direct dependencies in
goat without an explicit per-component ADR exception. Per the
audit, none are currently in our deployment-and-build surface.

- **SSPL** (MongoDB, Elastic-pre-relicense) — disqualified.
- **Commons Clause** (rider on otherwise-permissive licenses) — disqualified.
- **Elastic License v2 (ELv2)** — not OSI-approved; disqualified.
- **Business Source License (BSL)** — case-by-case per [ADR 0106](docs/adr/0106-sovereign-tooling-preference.md); disqualified when grant restrictions block vendor profit.
- **PolyForm Shield / Strict / Internal-Use / Noncommercial** — disqualified.
- **Custom "non-commercial" / "research-only" licenses** — disqualified.
- **No-license / "all rights reserved"** — disqualified for distributed components.

---


## How to update this file

- **New direct dependency** → add to the appropriate Tier section in the same
  PR. Cite the upstream URL and license SPDX identifier from the audit.
- **Upstream relicense event** → file a finding under
  `docs/findings/` following the F-NNN convention (F-010 is
  the netbird AGPLv3 reference) and update the table here.
- **CI hook** (per license-audit-ci-spec.md)
  catches license-tier shifts on every dependency bump and fails the PR if the
  shift would create a Tier-5 entry.
- **Source-availability obligation** for AGPL-3.0 entries: ensure the upstream
  link is reachable + the modified-by-us flag is accurate. Block 22 D5 work
  advertises these links on the management-API status response so network users
  of the modified components have direct access.

---

## Cross-references

- [LICENSE](LICENSE) — Apache License 2.0 (goat's own license)
- [NOTICE](NOTICE) — Apache 2.0 NOTICE convention
- [ADR 0111](docs/adr/0111-vendor-friendly-license-posture.md) — vendor-friendly license posture decision
- [ADR 0110](docs/adr/0110-license-posture-agplv3-forked-components.md) — AGPLv3 acceptance for forked components
- [ADR 0106](docs/adr/0106-sovereign-tooling-preference.md) — sovereign tooling preference
- F-010 — netbird AGPLv3 relicense finding
- License audit  — direct-component audit findings
- License-audit CI spec — recurring CI service spec
