# Goat doctor battle drills

These drills expand the remedies printed by `goat doctor`. The command output
must remain sufficient when the mesh and its hosted documentation are down.
Run doctor again after each repair. Keep observed evidence distinct from a
suspected cause; a failed router TCP probe, for example, does not prove that a
setup key or certificate is wrong.

| Check or cause | Drill |
|---|---|
| `profile.context`, `profile.material` | [01 — CLI identity setup](01-cli-identity.md) |
| `mesh.client`, `mesh.tunnel` | [02 — mesh client](02-mesh-client.md) |
| `mesh.management`, `management.reachability`, `clock.skew` | [03 — management and time](03-management-time.md) |
| `ztol-membership` | [04 — router path](04-router-path.md) |
| `zenoh-tls-material` | [05 — TLS material](05-tls-material.md) |
| `zenoh-mtls-session` | [06 — session admission](06-session-admission.md) |
| `portal-api-orgs` | [07 — portal access](07-portal-access.md) |

Every drill has a symptom, entry conditions, steps, a pass condition,
escalation, and evidence. These pages contain no credential values; redact
tokens, private keys, and bundle contents from any escalation report.
