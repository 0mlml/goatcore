# compose-no-flows — willing to connect, not to exchange

`package-xab` carries `transport_offer` and **no `data_offer` and no `cut`**. Under presence-is-
the-declaration (§3), that is xab saying it will connect and will not exchange. It is a complete,
valid package, not a truncated one.

## What must come out

| | |
|---|---|
| `transport` | **Present**, identical to `compose-full` — both packages carry `transport_offer` |
| `flows` | **`[]`** |
| Digest | `sha256:4b62680ea3795682ec45675f4257d1f697c990cb4041c632f98e06bebb175fd3` |

Neither direction is emitted, and for different reasons — which is the point of the fixture:

- **xaa → xab** is dropped because the receiver, xab, declares no `accept_*` values. A flow here
  would carry limits nobody set.
- **xab → xaa** is dropped because the originator, xab, declares no `release` set. A flow here
  would carry no content and would still have to be enforced against.

`flows: []` is a valid agreement: a path that carries nothing (§8.1). It is not an error and must
not be reported as one.

## The failure this fixture exists to catch

Emitting one flow with null, zero or absent limits. The specification forbids a member without a
value (§4), and a `0` standing in for "not declared" is worse than an absent flow: it is a real
limit that both sides will agree on and neither party chose. Absent, not defaulted.
