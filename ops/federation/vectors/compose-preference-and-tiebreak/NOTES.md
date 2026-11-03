# compose-preference-and-tiebreak — three §7 rules that coincide everywhere else

A variant of `compose-full` differing in exactly three things. Diff the two agreements and every
difference is one of the rules below.

## 1. The preference order, declared in opposite orders

| Party | `capabilities.transports` as declared |
|---|---|
| `xaa` | `["ipsec-fips","wireguard"]` |
| `xab` | `["wireguard","ipsec-fips"]` |

Expected `transport.kind` is **`wireguard`** — the first entry of §7's fixed order
`["wireguard","ipsec-fips"]` present in both. The order was reversed 2026-09-06; this
vector is the only one in the suite whose result changed, because everywhere else the
intersection has one member and the order is never reached.

This kills three wrong rules that `compose-full` cannot distinguish, because there the
intersection has one member:

| Wrong rule | Gives here |
|---|---|
| Reversed preference order | `ipsec-fips` |
| First of the *sorted* intersection | `ipsec-fips` |
| First entry of the counterparty's declared list | `wireguard` — right answer, wrong reason; see below |

`xab`'s array is **deliberately unsorted**, which is what makes the third rule visible at all. As
in `../compose-unsorted-release/`, that makes `xab` non-conforming as a signer's emission (§4)
and does not stop composition: the composer normalises. Selection reads §7's order, so neither
the declared nor the normalised order affects the result. An implementation that refuses the
package fails this fixture for a reason §4 does not support.

**Which rule this fixture kills by coincidence changed with the order, and it swapped ends.**
Under the old `["ipsec-fips","wireguard"]` order the expected answer was `ipsec-fips`, and it was
*"first of the sorted intersection"* that agreed with the right answer for the wrong reason —
`ipsec-fips` sorts before `wireguard`. Reversing the order to `["wireguard","ipsec-fips"]`
makes the sorted rule genuinely wrong here, so this fixture now does kill it;
what survives by coincidence instead is *"first entry of the counterparty's declared list"*,
because `xab` happens to declare `wireguard` first.

`../compose-transport-outside-preference-ignored/` still kills the sorted rule independently, and
should stay the fixture cited for it — a rule pinned by two fixtures for different reasons is
pinned; a rule pinned only by a coincidence that a future reordering can flip is not.

I first recorded the sorted rule as unpinnable in v1, reasoning that separating it would need a
third preference-order transport. That was wrong. It needs the **open list**, not a third
transport: two parties both declaring `["quic","wireguard"]` share `wireguard`, so the
intersection check passes, and then the preference order selects `wireguard` while the sorted
intersection selects `quic`. Worth leaving on the record, because the general form of the error is
common — *"no fixture can distinguish these"* is a claim about the fixtures that exist, not a
property of the format, and it is settled by attempting the construction rather than by reasoning
about it.

## 2. Both parties listening

Both declare `listener: true`. Expected `transport.listener` is **`xaa`** — `parties[0]`, per
§7's "both `true` → `parties[0]`".

**Compose in both argument orders and assert the same agreement.** That is what pins the
tie-break to position rather than to whichever package was passed first, and it is the only way
this fixture tests what it is for. Its complement is `../compose-listener-second-party/`, where
the sole listener is `parties[1]`.

## 3. Validated build, one each

| Party | `capabilities.fabric_fips_image` |
|---|---|
| `xaa` | `true` |
| `xab` | `false` |

Expected: `parties[0].fabric_fips_image` `true`, `parties[1].fabric_fips_image` `false`.

**This is the exact case §7 deleted the combined boolean for.** Every other fixture here carries
`true` on both sides, where per-party, per-party-swapped, and a single combined `f0 && f1` are
all indistinguishable. The specification removed a member to make a distinction visible, and
until this file no fixture made it visible.

Note what the agreement does *not* carry: no top-level `fips`, and no sentence anywhere saying
the parties disagree. What to do about the difference is each party's own policy, applied and
rendered locally (§7).

## Digest

`sha256:49ee55a8f1b0e8d4cd2cd3bde4f699315a8c976b30c2c0ad82e42964a2dd7bca`
