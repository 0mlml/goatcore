# compose-full — the reference pair

Two packages that both offer transport and both offer data. This is the vector every other one
is a variation of, and the pair reproduced as the example in §8.2 of the specification.

## What each derivation exercises

| Agreement member | Value | Derived how |
|---|---|---|
| `expires` | `2026-11-20` | Earlier of `2026-12-05` (xaa) and `2026-11-20` (xab) |
| `parties[0]` / `parties[1]` | xaa, xab | Ascending by `sovereign_id`, byte-wise |
| `parties[n].peering_cn` | `fed.xaa.xab.01`, `fed.xab.xaa.01` | Derived; neither package declares one |
| `parties[n].fabric_fips_image` | `true`, `true` | Verbatim per party, never combined |
| `transport` | present | Both packages carry `transport_offer` |
| `transport.kind` | `ipsec-fips` | `["ipsec-fips","wireguard"]` ∩ `["ipsec-fips"]`, then the fixed preference order |
| `transport.mtu` | `1380` | `min(1380, 1420)` |
| `transport.link_subnet` | `169.254.10.0/31` | `parties[0]`'s proposal. xab proposed `169.254.20.0/31` and position decided |
| `transport.listener` | `xaa` | xaa `listener: true`, xab `listener: false` — exactly one |
| `flows[0]` | xaa → xab | `parties[0]` originates first |
| `flows[0].envelope` | hz 50, 262144 B, 2000 keys | The **receiver's** figures. xab's |
| `flows[1].envelope` | hz 100, 1048576 B, 5000 keys | xaa's |
| `flows[n].publish_under` | `from/xaa/**`, `from/xab/**` | Derived; neither package declares one |

## Digest

`sha256:b29bb23071a5c668d0d0f2a3aec0a1c85337ed8a9d4cf94feae1f052d34410df`

The two parties compare this value and nothing else.
