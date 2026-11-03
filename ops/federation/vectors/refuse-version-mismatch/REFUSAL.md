# Refusal fixture — version mismatch

| | |
|---|---|
| Rule | **Refuse** unless both packages carry the same `spec` value. |
| Spec | §6 step 1 |
| Evaluated at | `now` = 2026-09-05, from `../now.txt` |


## The fixture

`package-xab.canonical.json` declares `"spec":"federation-package/v2"`. Everything else is the
`compose-full` xab package.

It is **signed validly under v1 rules** — canonical form per §4, Ed25519 per §5 — so that step 0
passes and the version rule is unambiguously the one that fires. This is deliberate: a fixture
that also failed the signature check would not test the rule it is named for.

That is a property of the fixture, not a claim about v2. A real v2 package would be signed by
whatever v2 specifies, and a v1 implementation that cannot verify a package under its own rules
refuses at step 0 instead. Either way the pair does not compose, which is the point.

## Expected outcome

Composition refuses at step 1. No agreement, no digest. Never compose across versions: two
implementations producing two digests is a false alarm, and a false alarm here is fatal.
