# Examples

Working demos that a stranger can run with `git clone && make demo` — no AWS account, no hardware tokens, no setup keys.

This directory is a skeleton. Concrete examples land under Block 29A and Block 29C of `../docs/project/implementation-plan.md`. Strategy context: `../docs/project/oss-strategy.md`.

## Planned examples

| Path | What it demonstrates | Block |
|---|---|---|
| `quickstart/` | 3-node mesh + Zenoh router pair + synthetic telemetry + browser dashboard. The 5-minute introduction. | 29A |
| `industrial-ot/` | Sovereign mesh + queryable telemetry for an industrial / OT-security scenario. | 29C |
| `disaster-response/` | Partition-tolerant mesh that survives intermittent backhaul; identity-bound peers; offline enrollment. | 29C |
| `autonomy-fleet/` | Mobile peers as phoenix-native nodes; lineage-tracked perception data. | 29C |

## Discipline

Each example:

- Has a `README.md` explaining the scenario in one paragraph and the substrate features it exercises.
- Has a `docker-compose.yml` (or `Makefile` target) that brings the scenario up locally without external credentials.
- Cites the design docs and ADRs the example depends on.
- Survives a `make clean` round-trip without leaving artifacts.

If an example needs external credentials to run, it does not belong in this directory; it belongs in `docs/deployments/<slug>/` as a deployment runbook.
