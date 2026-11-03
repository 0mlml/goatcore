# Refusal fixture — same `identity.trust_domain`

| | |
|---|---|
| Rule | **Refuse** if `identity.trust_domain` is equal. |
| Spec | §6 step 2 |
| Evaluated at | `now` = 2026-09-05, from `../now.txt` |


## The fixture

`package-xab.canonical.json` declares `"trust_domain":"spiffe://xaa.example"`, the same value
xaa declares. Its `sovereign_id` is still `xab`, so the parties are distinguishable by name and
only the trust-domain rule can fire. Signed with the xab key; step 0 passes.

## Expected outcome

Composition refuses at step 2. No agreement, no digest.

Distinct names over one trust domain is the more dangerous half of this rule: the names would
compose cleanly, and the workload identities behind them would be issued by one authority.
