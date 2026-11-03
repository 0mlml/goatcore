# Malformed inputs — single packages, no pair, no agreement

**A second kind of vector, and it exists because §4's rules fire on inputs a well-formed pair
cannot contain.**

Every package in this directory's siblings is well-formed by construction: it was built by a
conforming serialiser, so it cannot carry an empty number token, a duplicate member name, or an
unpaired surrogate. That is a property of how the fixtures are made, not a choice — and it means
no directory of valid pairs exercises these rules however many pairs anyone adds. §6 is covered
by nine `refuse-*` fixtures; §4 had no fixture form at all.

Each file here is **one package**, on its own. There is no counterparty, no agreement and no
digest, because none of these inputs survives long enough to be composed with anything.

## Why none of them is signed

A malformed input is refused by §3 and §4 **before** §6 step 0 is reached, so signature
verification never runs — and for several of these a canonical form does not exist, so there is
nothing to sign. Omitting signatures throughout keeps the kind uniform and keeps the refusal
attributable to the named rule rather than to a failed verification.

**`non-integer-number.json` is why that order is forced rather than chosen.** Verifying a
signature means computing the signing input, and the signing input is a canonical form; §4 permits
no `1380.0` token, so this file has no canonical form and there is no byte string to verify
against. An implementation that put verification first would have to fail on a package it cannot
serialise, and would report an unverifiable signature for a package whose signature is not the
problem. §6 states the order and this file is the proof of it.

The rule that follows: **no file here may produce a `compose.*` finding.** Each must be refused
naming its own rule from the table below.

`no-signature.json` is the exception that proves the arrangement: it is a **complete, well-formed
package** whose only defect is the absent member, so it is the one file here where the missing
`signature` is itself the finding.

## The files

| File | Carries | Refused by | Wrong implementation it catches |
|---|---|---|---|
| `empty-number-token.json` | `"measured_mtu":` | §4, no member without a value | A canonicaliser that emits a nil member as `null`, or emits nothing and returns a clean digest with a nil error |
| `null-member.json` | `"measured_mtu":null` | §4, no null at any depth | One that treats a null as an absent member and normalises it away |
| `empty-release.json` | `"release":[]` | §3, `data_offer`'s halves | One that reads it as "carries `release`" and emits a flow, or as "send-only" |
| `non-integer-number.json` | `"measured_mtu":1380.0` | §4, integers only | One that coerces an exactly-integral float |
| `leading-zero-number.json` | `"measured_mtu":01` | §4, number tokens | A lenient parser that accepts it and hands on a value whose canonical form is a different token |
| `negative-zero.json` | `"accept_hz":-0` | §4, number tokens | One that passes the token through. JCS emits this value as `0`, so the bytes differ for the same number |
| `integer-above-range.json` | `"accept_distinct_keys":9007199254740992` | §4, ±(2^53 − 1) | **No range check at all.** 2^53 is the first integer JavaScript cannot represent exactly |
| `unpaired-surrogate.json` | `"contact":"fed-ops@xaa.example\ud800"` — the **escape** form | §4, text is Unicode | One that substitutes `U+FFFD` — silent, changes the digest, reports nothing |
| `invalid-utf8-byte.json` | A raw `0xFF` inside `cut.contact` — the **raw-byte** form | §4, text is Unicode | One with no UTF-8 validity check on its raw input at all |
| `duplicate-member.json` | `"expires"` twice, `2026-12-05` then `2027-12-05` | §4, member names unique | One that resolves rather than refuses. Go's decoder keeps the last, others keep the first |
| `no-signature.json` | No `signature` member | §3, always required | One that treats an unsigned package as verified |

Five of these — `empty-number-token`, `leading-zero-number`, `unpaired-surrogate`,
`invalid-utf8-byte` and `duplicate-member` — carry tokens **no conforming serialiser emits**, so
they were written as raw bytes rather than produced from a data structure. `empty-number-token`
and `leading-zero-number` are not valid JSON at all; `invalid-utf8-byte` is not valid UTF-8, so it
is not text; `unpaired-surrogate` and `duplicate-member` are valid JSON syntax denoting either no
character or no single value.

### The UTF-8 rule has two halves, and they fail differently

`unpaired-surrogate.json` and `invalid-utf8-byte.json` are the same §4 rule and are **not
redundant**:

| | Form | Why it needs its own file |
|---|---|---|
| `invalid-utf8-byte.json` | A raw `0xFF` in the byte stream | The **blunt** half. Caught by validating raw input before decoding — and an implementation with no such check passes everything else here |
| `unpaired-surrogate.json` | The escape `\ud800`, which is valid JSON syntax | The **subtle** half, and the one that found a real bug. A raw-bytes check cannot see it: the file *is* valid UTF-8. A decoded-string check cannot see it either, because Go's decoder has already replaced it with `U+FFFD`, and the substitution is itself well-formed |

The second is why §4 says never substitute `U+FFFD`. The replacement is silent, it changes the
bytes and therefore the digest, and it leaves nothing on either side to point at.

## The two rules §4 argues hardest for were the two with no fixture

Worth naming, because it is the shape of the gap rather than a list of files:

- **The empty-token paragraph**, which §4 itself calls *"the worst failure this format can have,
  because both sides can agree on it"* — output that parses nowhere, carrying a clean digest and
  a nil error. `empty-number-token.json`.
- **The ±(2^53 − 1) range**, argued at length in §4 because a larger integer silently loses
  precision in JavaScript and re-serialises to different bytes. `integer-above-range.json`.

Both were entirely untested while the suite passed. A rule that is argued for at length and
tested nowhere is the one most likely to be read as advice.

## Expected outcome, for every file here

Refused, with a finding naming the rule in the table above. No agreement, no digest, no partial
output. An implementation that accepts any of these and produces a canonical form has failed,
whatever bytes it produced.
