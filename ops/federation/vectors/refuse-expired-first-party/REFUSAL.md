# Refusal fixture — the expired package is `parties[0]`

| | |
|---|---|
| Rule | **Refuse** if **either** package's `expires` is before `now`. |
| Spec | §6 step 3 |
| Evaluated at | `now` = 2026-09-05, from `../now.txt` |

## The fixture

The mirror of `../refuse-expired/`. There, `xab` expires `2026-08-01`; here, `xaa` does, and
`xab` is valid through `2026-11-20`.

## Why the mirror is needed

In `../refuse-expired/` the expired package is `xab`, which is **both `parties[1]` and the second
file passed**. Two wrong implementations pass it:

| Wrong rule | `refuse-expired` | This fixture |
|---|---|---|
| Check only `parties[1]` | Refuses — right answer, wrong reason | **Composes.** Caught |
| Check only the second argument | Refuses — right answer, wrong reason | **Composes.** Caught |

"Either" is the word in step 3, and a one-sided check that happens to look at the right side is
indistinguishable from it until the expiry moves to the other party.

This is the same discipline as `../compose-crossed-extremes/`, applied to a refusal instead of a
derivation: the decisive value must sit on `parties[0]` in one fixture and `parties[1]` in
another, or position and rule stay coincident.

## Expected outcome

Composition refuses at step 3, naming `xaa`. No agreement, no digest.
