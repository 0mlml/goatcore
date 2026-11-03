# compose-one-way — a send-only party and a receive-only one

`data_offer`'s two halves are **independently optional, each complete once present** (§3). This
vector is the pair that only reads correctly under that rule.

| Party | `data_offer` carries | So it is |
|---|---|---|
| `xaa` | `release` only | **Send-only** |
| `xab` | `accept_hz`, `accept_bytes_per_message`, `accept_distinct_keys` | **Receive-only** |

Both carry `transport_offer`, so the pair produces a link; both carry `cut`, which `data_offer`
requires whichever halves it holds.

## What this vector pins

| Agreement member | Value | Derived how |
|---|---|---|
| `expires` | `2026-11-20` | Earlier of `2026-12-05` (xaa) and `2026-11-20` (xab) |
| `transport` | present | Both packages carry `transport_offer` |
| `transport.kind` | `ipsec-fips` | First entry of `["ipsec-fips","wireguard"]` present in both |
| `transport.mtu` | `1380` | `min(1380, 1420)` |
| `transport.link_subnet` | `169.254.10.0/31` | `parties[0]`'s proposal |
| `transport.listener` | `xaa` | Exactly one `listener: true` |
| `flows` | **one entry**, xaa → xab | xaa carries `release`; xab carries all three `accept_*` |
| `flows[0].envelope` | hz 50, 262144 B, 2000 keys | The receiver's figures — xab's |
| — | xab → xaa **not emitted** | xab carries no `release`, so that direction is not fully specified (§7) |

The single flow is the one originating from `parties[0]`. §8.1's ordering rule still fixes which
entry it is when only one direction is emitted, and this is the vector where that matters.

## Why the rule has to be per-half

Were all four members required together, a receive-only partner would have to declare a release
set it will never publish under — and an empty one is malformed (§3), so it would have to invent
a key expression. That fiction would then sit in a signed document and be read, later, as an
intention. One honest half is the better failure.

## Digest

`sha256:16d898d9496b4d043b487a922a386aaee7a611eefcfd6e19e80229423c16257e`
