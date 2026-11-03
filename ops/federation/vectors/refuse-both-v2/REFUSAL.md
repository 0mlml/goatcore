# Refusal fixture — both packages on a version that is not this one

| | |
|---|---|
| Rule | **Refuse** unless each package's `spec` is exactly `federation-package/v1`. |
| Spec | §6 step 1 |
| Evaluated at | `now` = 2026-09-05, from `../now.txt` |

## The fixture

Both packages are the `compose-full` pair with `"spec":"federation-package/v2"`, re-signed under
v1 rules so that step 0 passes and the version rule is unambiguously the one that fires.

**The two packages agree with each other.** That is the entire point of this fixture. Its sibling
`../refuse-version-mismatch/` pairs a v1 package with a v2 one and is caught by any
implementation that compares the two `spec` values. This pair is not: an implementation reading
step 1 as "both carry the same value" composes it, under v1 rules, and emits a v1 agreement.

## Why that is an attack and not a curiosity

An implementation that composes this pair has verified two signatures under Ed25519 over packages
written for a version that may specify some other algorithm — and it did so with nothing anywhere
looking irregular, because the attack moves **both** sides rather than downgrading one. That is
the case §5's downgrade paragraph is written against.

## Expected outcome

Composition refuses at step 1. No agreement, no digest.

An implementation that refuses this pair somewhere in structural validation instead — as an
unrecognised `spec` string, before composition begins — has the right verdict for the wrong
reason and fails the "assert which rule fired" requirement (§10). The pin is step 1 so that the
mismatched pair and this one produce the same named refusal.
