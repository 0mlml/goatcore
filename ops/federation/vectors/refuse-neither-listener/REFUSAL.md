# Refusal fixture — neither listener

| | |
|---|---|
| Rule | **Refuse**, when both packages carry `transport_offer`, if neither declares `listener: true`. |
| Spec | §6 step 4(b), §7 `transport.listener` |
| Evaluated at | `now` = 2026-09-05, from `../now.txt` |


## The fixture

Both packages carry `"listener":false` in `transport_offer`. Their transport lists do intersect
(`ipsec-fips`), so the overlap rule does not fire and this fixture tests one rule.

Both are validly signed.

## Expected outcome

Composition refuses at step 4. No agreement, no digest.

There is no link to build: each party has said it will dial and neither will answer. This is the
value the deleted three-value `reachability` enum could not express cleanly — `both` was a
listener state that read like a reachability state, and the composed member was called
`initiator` while holding the party that listens.
