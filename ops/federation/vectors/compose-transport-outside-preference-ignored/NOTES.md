# compose-transport-outside-preference-ignored — the preference order, separated at last

Both parties declare `"transports":["quic","wireguard"]`. They share both entries; only
`wireguard` is in §7's preference order `["ipsec-fips","wireguard"]`.

## Expected outcome

`transport.kind` is **`wireguard`** — the first entry of the preference order present in both.
`quic` is shared, and ignored, because §7 selects from the preference order and not from the
intersection.

This is also the only fixture here whose `kind` is `wireguard`, so it is the one that shows the
preference order is walked rather than assumed.

## What it kills, and why the earlier attempt could not

`../compose-preference-and-tiebreak/` declares both preference-order transports in opposite
orders and expects `ipsec-fips`. That kills a reversed order and "first of the counterparty's
declared list", but **not** "first of the sorted intersection" — `ipsec-fips` sorts before
`wireguard`, so the wrong rule gives the right answer there.

Separating them does not need a third preference-order transport. It needs the **open list**,
which §6 step 4(a) and §7 already make explicit:

| Rule | Selects here |
|---|---|
| **Preference order (§7)** | `wireguard` |
| First of the sorted intersection | `quic` |
| First of either party's declared list | `quic` |

`quic` is not one of the two values §8.1 permits for `transport.kind`, so the wrong rule does not
merely disagree — it emits an agreement the schema forbids, from two packages that are both
entirely valid. That is a sharper failure than a digest mismatch, and it was reachable the whole
time.

§6 step 4(a) does not refuse: the parties share `wireguard`, which the preference order names.
Its sibling `../refuse-transport-outside-preference/` is the case where the *only* shared entry
is `quic`, and that one does refuse.

## The sentence this pins

§7: *"Entries either party names that are not in the preference order are ignored."* Nothing
exercised it before this file. An open list is not decoration — it is what lets a party offer a
transport a later version adds, and it is the reason the selection rule and the refusal rule must
both read the same ordered list.

## What a harness must assert, beyond the digest

Two things, and the second is the one that decays:

1. `transport.kind` is `wireguard`, and the agreement matches the committed bytes.
2. **The separation still holds** — that the shared out-of-preference entry (`quic`) sorts
   *before* the preference-order winner (`wireguard`), so the correct rule and the wrong one
   genuinely select differently.

Without the second, editing this fixture to a shared name that sorts *after* `wireguard` would
make the two rules agree again. The expected output would still match, the fixture would still
pass, and it would be testing nothing. **A fixture that depends on a property should assert that
property, not just its expected output** — the expected value cannot tell you the file is still a
discriminator.

This fixture was also, for a time, committed and absent from the test table: it ran nowhere while
looking like coverage. Whatever enumerates these directories should fail on one it does not know
about rather than skipping it.

## Digest

`sha256:09325573ce801219f61ac0f8d5d8378fc6dab8601e6465b4f600e78932d2cb61`
