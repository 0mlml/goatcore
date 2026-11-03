# compose-crossed-extremes — `min` and `earlier`, on opposite sides

Two §7 rules select a value from a pair. In every other fixture here the selected value sits on
`parties[0]`, so "compute the minimum" and "take `parties[0]`'s" give the same answer and a
positional implementation passes.

## The fixture

| | `xaa` = `parties[0]` | `xab` = `parties[1]` | Expected |
|---|---|---|---|
| `measured_mtu` | 1420 | **1380** | `transport.mtu` = **1380** |
| `expires` | **2026-10-01** | 2026-12-31 | `expires` = **2026-10-01** |

The two extremes sit on **opposite** parties, which is the whole point: no single positional rule
explains both.

| Wrong rule | `mtu` | `expires` |
|---|---|---|
| Take `parties[0]`'s | 1420 | 2026-10-01 — right by accident |
| Take `parties[1]`'s | 1380 — right by accident | 2026-12-31 |
| `min` / earlier (§7) | 1380 | 2026-10-01 |

Everything else is `compose-full`: `transport.kind` `ipsec-fips`, `listener` `xaa`,
`link_subnet` `169.254.10.0/31`, two flows.

## The standing rule this fixture exists to enforce

**Cross the extremes.** For any rule that selects a minimum, maximum, earliest or latest, put the
selected value on `parties[1]` in at least one fixture. A fixture author who habitually puts the
winning value on the same side reproduces this gap for every such rule in the format, and the
suite passes an implementation that never computes anything.

## Digest

`sha256:a3926bc840cea86157f7833ea1b6fe17833d397f29722ab09b3296fad1b55f50`
