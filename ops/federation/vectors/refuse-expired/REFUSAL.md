# Refusal fixture — expired package

| | |
|---|---|
| Rule | **Refuse** if either package's `expires` is before `now`. |
| Spec | §6 step 3 |
| Evaluated at | `now` = 2026-09-05, from `../now.txt` |


## The fixture

`package-xab.canonical.json` carries `"issued":"2026-05-01"` and `"expires":"2026-08-01"`. Both
are before `now` = 2026-09-05. The package is internally consistent — `issued` precedes
`expires` — and validly signed, so the only rule it breaks is the one this fixture names.

`now` is a vector input, not a wall clock. An implementation testing against these vectors must
evaluate at the date in `../now.txt` or this fixture will pass or fail for the wrong reason as
time moves.

## Expected outcome

Composition refuses at step 3. No agreement, no digest.

Note what does **not** happen: composition does not succeed with `expires` set to the earlier of
the two (§7). The earlier-of-two rule gives a live link its term; it does not resurrect a dead
package.
