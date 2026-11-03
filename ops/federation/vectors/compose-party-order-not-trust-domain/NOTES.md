# compose-party-order-not-trust-domain — position is fixed by `sovereign_id`

§6 step 5 sorts the two parties by **`sovereign_id`**. Every other fixture here pairs `xaa` with
`spiffe://xaa.example` and `xab` with `spiffe://xab.example`, so sorting by `sovereign_id` and
sorting by `trust_domain` give the same order and are indistinguishable.

## The fixture

The two are deliberately crossed:

| `sovereign_id` | `identity.trust_domain` |
|---|---|
| `xaa` | `spiffe://zzz.example` |
| `xab` | `spiffe://aaa.example` |

| Sorted by | `parties[0]` |
|---|---|
| **`sovereign_id` — §6 step 5** | **`xaa`** |
| `trust_domain` | `xab` |

Nothing in the specification requires a `trust_domain` to correspond to its party's
`sovereign_id`, and this fixture pins that too: an implementation that derives, validates or
sorts on a relationship between the two fails here. §6 step 2 only requires the two parties'
trust domains to **differ** from each other.

## What changes if the order is wrong

Everything positional, at once: `parties[0]`/`parties[1]`, both `peering_cn` values,
`transport.link_subnet` (`parties[0]`'s proposal), the listener tie-break, and the order of
`flows`. A wrong sort is not a subtle difference in one member — it is a different agreement, and
the digest says so.

## Expected outcome

`parties[0]` is `xaa`, carrying `trust_domain` `spiffe://zzz.example` and `peering_cn`
`fed.xaa.xab.01`. `transport.link_subnet` is `169.254.10.0/31`, `xaa`'s proposal.

## Digest

`sha256:a8d25d0295ad7dfc28c4ea31f1c0dc84dfb99a8f0ab7592cb5697d7e6ec513ab`
