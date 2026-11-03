# compose-listener-second-party — the sole listener is `parties[1]`

`compose-full` has `xaa` (`parties[0]`) as the only listener, and
`../compose-preference-and-tiebreak/` has both listening and resolves to `parties[0]`. Both are
consistent with a wrong rule — *"`parties[0]` whenever anyone listens"* — that no fixture caught.

## The fixture

`compose-full` with the two `listener` booleans swapped:

| Party | `transport_offer.listener` |
|---|---|
| `xaa` | `false` |
| `xab` | **`true`** |

## Expected outcome

| Member | Value | Why |
|---|---|---|
| `transport.listener` | **`xab`** | Exactly one `true` → that party (§7), regardless of position |
| `transport.link_subnet` | `169.254.10.0/31` | Still `parties[0]`'s proposal — **position**, not the listener |

The second row is a second rule this file pins for free. In `compose-full` the listening party
*is* `parties[0]`, so "the link subnet comes from the listener" and "from `parties[0]`" agree
there. Here they do not, and §7 says position.

§6 step 4(b) does not fire: one party declares `listener: true`. Its fixture is
`../refuse-neither-listener/`.

## Digest

`sha256:fb6996e1dbecea7ef7ac9419afc8488e53c650cb195a15d9eed3901ccea26f10`
