# Refusal fixture — same `sovereign_id`

| | |
|---|---|
| Rule | **Refuse** if `sovereign_id` is equal. |
| Spec | §6 step 2 |
| Evaluated at | `now` = 2026-09-05, from `../now.txt` |


## The fixture

Both packages declare `"sovereign_id":"xaa"`. The second (`package-xaa-second.*`) is otherwise
the xab package, so it still declares `"trust_domain":"spiffe://xab.example"`. The two
parties therefore differ at every other layer, and only the `sovereign_id` rule can fire.

It is signed with the **xaa** test key, because step 0 verifies each package under the key bound
to the `sovereign_id` that package declares. Signing it with the xab key would have made this a
second signature fixture.

## Expected outcome

Composition refuses at step 2. No agreement, no digest.

The two parties would otherwise sort into the same position, both derive `fed.xaa.xaa.01`, and
both publish under `from/xaa/**` — a link whose two ends are indistinguishable to the access
rules that are the entire enforcement mechanism.
