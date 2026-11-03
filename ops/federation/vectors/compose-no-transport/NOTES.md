# compose-no-transport — willing to exchange, not to connect

Neither package carries `transport_offer`. Both carry `data_offer` and `cut`.

## What must come out

| | |
|---|---|
| `transport` | **Absent.** Not `null`, not `{}` — the member is not emitted |
| `flows` | Both directions, identical to `compose-full` |
| Digest | `sha256:974778f7eece5bbb65dc33755c12cb99a35e4dbe180fbe16eaeba87ff06c4ce8` |

## The clause this pins

`capabilities.transports` here are `["wireguard"]` and `["ipsec-fips"]` — **they do not
intersect**. Composition still succeeds, because step 4 applies only when both packages carry
`transport_offer` (§6). Neither did, so no link was asked for and there is nothing to fail.

`../refuse-no-transport-overlap/` is the same non-intersecting pair *with* both offers present,
and it refuses. The two fixtures differ in exactly that one thing.

## What this agreement is

Agreed data terms and no path: valid, and not connectable (§8.1). Something else has to carry the
traffic, or one party has to publish a `transport_offer` and both recompose.
