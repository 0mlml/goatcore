# Refusal fixture — a shared transport that the preference order does not name

| | |
|---|---|
| Rule | **Refuse** if both carry `transport_offer` and their `capabilities.transports` share no entry **that appears in the preference order of §7**. |
| Spec | §6 step 4(a) |
| Evaluated at | `now` = 2026-09-05, from `../now.txt` |

## The fixture

Both packages declare `"transports":["quic"]`, and both carry `transport_offer`. `xaa` declares
`listener: true`, so step 4(b) does not fire and this rule is unambiguously the one that does.

## Why the sibling fixture is not enough

`../refuse-no-transport-overlap/` has **disjoint** sets, so a raw intersection test catches it.
This pair's intersection is `{"quic"}` — non-empty. An implementation testing the raw
intersection passes step 4, reaches §7's selection with a shared entry the preference order does
not name, and has nothing to choose. What it does then is undefined, which is the defect: some
implementations emit `transport` with an empty `kind`, some panic, some fall through to the first
preference entry and assert a transport neither party offered.

`capabilities.transports` is an **open** list on purpose — a party may offer a transport this
version has never heard of, and a later version adds one by extending the preference order. That
openness is exactly what makes this case reachable, so the check is stated against the same
ordered list selection walks.

## Expected outcome

Composition refuses at step 4(a). No agreement, no digest.

An implementation that emits a `transport` member here, with any `kind`, has failed. So has one
that refuses with a finding naming the listener rule.
