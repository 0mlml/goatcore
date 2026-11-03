# Refusal fixture — bad signature

| | |
|---|---|
| Rule | **Refuse** unless each package's signature verifies under the key bound to that package's `sovereign_id`. |
| Spec | §6 step 0 |
| Evaluated at | `now` = 2026-09-05, from `../now.txt` |


## The fixture

`package-xaa.canonical.json` is the `compose-full` xaa package with one value changed **after
it was signed**: `accept_hz` went from `100` to `1000`. The `signature` member is byte-identical
to the one in `compose-full`, and it covers the earlier bytes.

`package-xaa.signing-input.canonical.json` here is the **untampered** signing input — the bytes
the signature actually covers. It is included so the failure is inspectable: signing input and
package disagree by exactly one token.

`package-xab.canonical.json` is unmodified and verifies.

## Expected outcome

Composition refuses at step 0 and produces no agreement and no digest. It must refuse before
evaluating anything else: the tampered value is a rate limit, and a composer that read it
before checking the signature would have accepted a limit the counterparty never signed.

An implementation that reports the raised `accept_hz` as an envelope difference, or that
reports any finding other than the signature, has run step 0 too late or not at all.
