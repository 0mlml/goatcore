# Refusal fixture — no transport overlap

| | |
|---|---|
| Rule | **Refuse**, when both packages carry `transport_offer`, if their `capabilities.transports` do not intersect. |
| Spec | §6 step 4(a), §7 `transport.kind` |
| Evaluated at | `now` = 2026-09-05, from `../now.txt` |


## The fixture

`package-xaa.canonical.json` offers `["wireguard"]`; `package-xab.canonical.json` offers
`["ipsec-fips"]`. The intersection is empty. **Both carry `transport_offer`**, which is what
arms this check.

Both are validly signed and neither breaks any other rule.

## Expected outcome

Composition refuses at step 4. No agreement, no digest.

Contrast `../compose-no-transport/`, where the transport lists also do not intersect and
composition **succeeds** with no `transport` member — because neither package carries
`transport_offer`, so no link was asked for. The two fixtures differ in exactly that, and
together they pin the "only when both carry `transport_offer`" clause.
