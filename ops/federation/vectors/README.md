# Conformance vectors — federation package v1

**These vectors are normative. If an implementation and a vector disagree, the implementation is
wrong.** That includes ours: `ops/federation/reference/` is one implementation of
[`docs/design/federation-package-spec.md`](../../../docs/design/federation-package-spec.md), and
it predates the revision these vectors were written against.

The one exception, and it is narrow: if a vector disagrees with the **specification**, the vector
is wrong and is fixed here — never by changing what the specification says to match a file.

Every value was derived by hand from the specification. No file here was produced by running the
reference implementation, which is the property that makes these vectors a test of it.

## Evaluation date

`now.txt` holds `2026-09-05`. Composition step 3 compares `expires` against it (§6). Run the
vectors at that date, not at the wall clock, or `refuse-expired`, `compose-expiry-boundary` and
the composing pairs will change verdict as time passes.

In production `now` is the current date **in UTC**, and the boundary is **inclusive** —
`expires == now` composes. `compose-expiry-boundary` is the fixture for that, and it is the only
one whose verdict turns on the rule: every other expiry here is months away, so an off-by-one at
the boundary passes the rest of the suite silently.

## What is here

| Directory | Kind | Outcome |
|---|---|---|
| `compose-full/` | Both offer transport, both offer data | Agreement with `transport` and two flows |
| `compose-no-flows/` | xab offers no data | Agreement with `transport` and `flows: []` |
| `compose-no-transport/` | Neither offers transport | Agreement with **no `transport` member**, two flows |
| `compose-one-way/` | xaa send-only (`release` only), xab receive-only (`accept_*` only) | Agreement with `transport` and **one** flow |
| `compose-expiry-boundary/` | xab's `expires` is exactly `now` | Agreement — the boundary is inclusive |
| `compose-unsorted-release/` | `release` arrives unsorted and with a duplicate | Agreement carrying it **normalised** |
| `compose-collation-utf16/` | Non-ASCII `release` where UTF-16 and code-point order disagree | Agreement in **UTF-16** order |
| `compose-preference-and-tiebreak/` | Both transports declared in opposite orders, both listening, one validated build each | `kind` `ipsec-fips`, `listener` `xaa`, `fabric_fips_image` `true`/`false` |
| `compose-crossed-extremes/` | Smaller MTU on xab, earlier `expires` on xaa | `mtu` 1380, `expires` 2026-10-01 |
| `compose-listener-second-party/` | The sole listener is xab | `listener` `xab`, `link_subnet` still xaa's |
| `compose-transport-one-sided/` | xaa offers transport, xab does not | Agreement with **no `transport`** — byte-identical to `compose-no-transport` |
| `compose-party-order-not-trust-domain/` | `sovereign_id` and `trust_domain` order differently | `parties[0]` is xaa, by `sovereign_id` |
| `compose-transport-outside-preference-ignored/` | Both offer `quic` **and** `wireguard` | `kind` `wireguard` — `quic` is shared and ignored |
| `refuse-bad-signature/` | §6 step 0 | Refuse |
| `refuse-version-mismatch/` | §6 step 1 — the two `spec` values differ | Refuse |
| `refuse-both-v2/` | §6 step 1 — both are `v2`, so they **agree with each other** | Refuse |
| `refuse-same-sovereign-id/` | §6 step 2 | Refuse |
| `refuse-same-trust-domain/` | §6 step 2 | Refuse |
| `refuse-expired/` | §6 step 3 — the expired package is xab, `parties[1]` | Refuse |
| `refuse-expired-first-party/` | §6 step 3 — the expired package is xaa, `parties[0]` | Refuse |
| `refuse-no-transport-overlap/` | §6 step 4(a) — disjoint sets | Refuse |
| `refuse-transport-outside-preference/` | §6 step 4(a) — both offer `quic`, which the preference order does not name | Refuse |
| `refuse-neither-listener/` | §6 step 4(b) | Refuse |
| `malformed/` | **A second kind** — eleven single packages, no pair | Refuse — **before composition**. See [`malformed/README.md`](malformed/README.md) |

Nine `refuse-*` fixtures covering the seven rules of §6, which is every way *composition* can
refuse. Two rules carry two fixtures each, because in both cases an implementation can pass one
and fail the other:

| Rule | Two fixtures, because |
|---|---|
| Step 1, the `spec` pin | `refuse-version-mismatch` catches an implementation with no version check. `refuse-both-v2` catches one that compares the two packages with **each other** and finds them consistent — the pin is literal, not an equality test |
| Step 4(a), transport | `refuse-no-transport-overlap` has disjoint sets, which a raw intersection test catches. `refuse-transport-outside-preference` has a **non-empty** intersection outside §7's preference order, which it does not |

Every one of those is built so that **only** its own rule fires: every other package in it is
valid, correctly signed, and unremarkable.

### The second kind: `malformed/`

`malformed/` holds **single packages, not pairs** — eleven of them, each refused by §3 or §4 before
composition begins.

It is a separate kind rather than more directories of the first kind because **§4's rules fire on
inputs a well-formed pair cannot contain.** Every package in a pair fixture was produced by a
conforming serialiser, so it cannot carry an empty number token, a duplicate member name, an
unpaired surrogate or a leading-zero integer. No number of additional pairs reaches those rules.

The consequence, stated because it went unnoticed for a whole revision: **§6 was well covered and
§4 had no fixture form at all** — including the empty-token paragraph §4 itself calls the worst
failure this format can have, and the full ±(2^53 − 1) range argument. Both were untested while
the suite passed.

## Which fixture covers which specification defect

Twelve defects were found by an engineer implementing the specification from the document alone,
in another language, with no access to this repository. Each one changes the bytes, or changes
whether an agreement exists at all. The prose was fixed; **this table is what keeps it fixed**,
because a rule with no fixture goes silent again the moment someone reads the sentence
differently.

| Defect | Fixture |
|---|---|
| The `spec` test is a literal pin, not an equality check between the packages | `refuse-both-v2` |
| "Verbatim" and "sorted" contradicted; the composer normalises regardless | `compose-unsorted-release` |
| "Sorted lexicographically" named no collation | `compose-collation-utf16` |
| `release` and the three `accept_*` are independently optional | `compose-one-way` |
| `"release": []` is malformed, not an empty declaration | `malformed/empty-release.json` |
| The digest input excludes a trailing newline and a byte-order mark | *see below* |
| A shared transport outside the preference order must refuse | `refuse-transport-outside-preference` |
| A null is an error at any depth | `malformed/null-member.json` |
| `1380.0` is refused, not coerced | `malformed/non-integer-number.json` |
| `now` is the current UTC date; the expiry boundary is inclusive | `compose-expiry-boundary` |
| The base64 alphabet, and the encoding of the §5.1 verifying key | *see below* |
| A duplicate member name is refused, never resolved — found after the other eleven | `malformed/duplicate-member.json` |

**Two of the eleven have no fixture of their own, and need none.** Stating that here so it is not
re-derived later as a gap:

- **The digest input.** Every `agreement.canonical.json` here ends `}` with no trailing newline,
  and `agreement.digest.txt` is the hash of exactly those bytes. Any implementation that appends
  a newline, or emits a byte-order mark, fails every composing fixture at once. A dedicated
  fixture would only restate what the whole set already enforces — and it could not be built
  anyway, since its expected value is a digest that must *not* match.
- **The signature encoding — partly.** Every `signature` here is 88 base64 characters ending
  `==`, in the standard alphabet, decoding to 64 bytes, over the committed signing input; all 44
  contain `+` or `/`. So an implementation that decodes with the **URL-safe alphabet only** fails
  every fixture, and the alphabet is pinned.

  **Strictness is not pinned, and claiming it was would be wrong.** The realistic wrong
  implementation is not a URL-safe-only decoder — it is a *permissive* one that tries the
  standard alphabet and falls back, or accepts embedded whitespace, or skips the length check.
  Every such decoder passes all 22 pair fixtures, because every signature here is well-formed.
  Catching those needs a malformed input carrying a badly-encoded signature, which this suite
  does not have. §5's strict-decoding paragraph is normative and untested.

  The §5.1 verifying-key encoding *is* pinned, by `testkeys/*.public.b64` being 44 characters
  ending in one `=` over the 32 raw bytes — the padding that differs from a signature's, which is
  the part of §5 easiest to get wrong.

## Fixture design rules

Learned from mutation-testing the reference implementation against this suite. Two of them were
found only after six §7 rules turned out to be unpinned while all files passed.

**Cross the extremes.** For any rule selecting a minimum, maximum, earliest or latest, put the
selected value on `parties[1]` in at least one fixture. Position and the stated rule coincide
whenever the extreme value always sits on the same party, so an author who habitually puts the
winning value on the same side reproduces this gap for every such rule in the format — and the
suite then passes an implementation that never computes anything. `compose-crossed-extremes` is
the fixture; `compose-listener-second-party` is the same discipline applied to a positional
tie-break.

**Vary what is uniformly clean.** Every original fixture had sorted, de-duplicated,
single-element, ASCII `release` arrays; complete `data_offer` blocks; `v1` on both sides; only
the two preference-order transports; and expiries months away. Each of those uniformities hid a
rule. A suite of clean inputs tests the algorithm and nothing about any disputed reading.

**Make the wrong answer different.** A fixture where a wrong rule happens to give the right value
tests nothing, and recording it as coverage is worse than having no fixture — the
`compose-preference-and-tiebreak` notes name one rule that survives that file by coincidence,
rather than claiming it as killed.

**One rule per fixture, and assert which one fired.** A fixture that refuses for the wrong reason
is a passing test hiding a broken implementation.

**Some rules are unpinnable by construction, and that is a fact to record rather than a gap.**

**The fixture inventory is complete for v1.** Every rule that a fixture can reach has one. Four
survive that no fixture can close, and each is unreachable for a different reason:

| Rule | Why no fixture reaches it |
|---|---|
| **An agreement carrying a null** (§4) | A fixture is an **input**. This is a rule about output an implementation produces about itself, and nothing anyone can hand it makes it produce one |
| **Member names ordered by bytes rather than UTF-16** (§4) | Every member name this format defines is **ASCII**, where all candidate collations agree. Making them disagree would need a member the schema does not have. `compose-collation-utf16` pins the comparator through **set-array elements**, which is the reachable half — and §4 specifies one comparison for both, so pinning it there pins it |
| **The UTF-8 check on decoded strings** (§4) | **Unreachable, not merely unpinned.** Once raw input is validated (`malformed/invalid-utf8-byte.json`), a decoded string cannot be invalid unless a caller constructs one directly in Go. Defence in depth, not coverage |
| **Strict base64 decoding** (§5) | Every signature here is well-formed, so a permissive decoder passes. See the signature note above |

Record these beside the coverage map. Left unstated, each gets re-derived later as missing
coverage, and someone spends a day building a fixture that cannot exist.

**But check the claim before recording it.** "No fixture can distinguish these" is a claim about
the fixtures that exist, not a property of the format, and it is settled by attempting the
construction. The preference-order rule was recorded here as needing a third transport v1 does
not have; it needed the **open list** instead, and
`compose-transport-outside-preference-ignored` is the fixture that was reachable all along.

**A committed fixture is not a running fixture.** `compose-transport-outside-preference-ignored`
was committed, documented and absent from the test table — the suite passed green without ever
loading it. A fixture that runs nowhere looks exactly like coverage and provides none, which is
the failure the repository's own test-coverage guard exists for, one level up. Whatever enumerates
these directories should fail on a directory it does not know about, rather than skipping it.

**A fixture that depends on a property should assert that property, not just its expected
output.** The fix to the one above is the part worth generalising: it now reads the committed
fixture and asserts the **separation still holds** — that the shared out-of-preference entry sorts
*before* the preference-order winner, so the correct rule and the wrong one genuinely select
differently. Edit that fixture later to a shared name sorting after `wireguard` and the two rules
agree again: the expected output would still match, the fixture would still pass, and it would be
testing nothing. The expected value cannot tell you the fixture is still a discriminator.

**Ask what shape of input a rule needs, before asking for another fixture.** Six rules here were
unreachable by any pair, however many pairs were added, because every package in a pair is
well-formed by construction. That is not thin coverage of an existing kind; it is a missing kind,
and adding fixtures of the kind you already have never finds it. When a rule stays untested after
a deliberate attempt, check whether the fixture *form* can express its input at all.

## Files in each directory

| File | Bytes |
|---|---|
| `package-<sid>.signing-input.canonical.json` | **Byte-exact, no trailing newline.** The canonical form with `signature` removed — the exact input to Ed25519 (§5) |
| `package-<sid>.canonical.json` | **Byte-exact, no trailing newline.** The complete signed package in canonical form |
| `package-<sid>.pretty.json` | Indented rendering of the same data. Not normative; a reading aid |
| `agreement.canonical.json` | **Byte-exact, no trailing newline.** The composed agreement (composing pairs only) |
| `agreement.pretty.json` | Indented rendering |
| `agreement.digest.txt` | `sha256:` + lowercase hex over `agreement.canonical.json` |
| `NOTES.md` / `REFUSAL.md` | What the vector pins, and why. `malformed/` documents its ten files in one `README.md` instead |

The byte-exact files have **no trailing newline**, because a canonical form is bytes, not a text
file, and §9 states the digest input as the canonical bytes with no trailing newline and no
byte-order mark. A tool that appends one produces a different digest over an identical agreement,
which is exactly the class of failure these vectors exist to catch. Read them as bytes.

Measured, so it is a number rather than a warning: `compose-full/agreement.canonical.json` digests
to `sha256:b29bb230…`, and the same bytes with one `\n` appended digest to `sha256:27f0dcf0…`.
Diffing the two agreements shows nothing, because they are the same agreement.

The pretty forms carry their members in canonical order rather than the reading order used in the
specification's examples, so that the two forms can be compared line for line.

## Digests

| Vector | Digest |
|---|---|
| `compose-full` | `sha256:b29bb23071a5c668d0d0f2a3aec0a1c85337ed8a9d4cf94feae1f052d34410df` |
| `compose-no-flows` | `sha256:4b62680ea3795682ec45675f4257d1f697c990cb4041c632f98e06bebb175fd3` |
| `compose-no-transport` | `sha256:974778f7eece5bbb65dc33755c12cb99a35e4dbe180fbe16eaeba87ff06c4ce8` |
| `compose-one-way` | `sha256:16d898d9496b4d043b487a922a386aaee7a611eefcfd6e19e80229423c16257e` |
| `compose-expiry-boundary` | `sha256:32580792386d28ca02f50a6833aaf631b870333c7c7e8f8ca8d7d4a3baf598ed` |
| `compose-unsorted-release` | `sha256:0d345e4a230a4cb29b62152f369a04f3b60e56c7f7eac18e1a29e8b3539235a9` |
| `compose-collation-utf16` | `sha256:497a17ecae106ec320c38f75a2247c92a0ff107bdc7cafddc063b17ba54209b7` |
| `compose-preference-and-tiebreak` | `sha256:49ee55a8f1b0e8d4cd2cd3bde4f699315a8c976b30c2c0ad82e42964a2dd7bca` |
| `compose-crossed-extremes` | `sha256:a3926bc840cea86157f7833ea1b6fe17833d397f29722ab09b3296fad1b55f50` |
| `compose-listener-second-party` | `sha256:fb6996e1dbecea7ef7ac9419afc8488e53c650cb195a15d9eed3901ccea26f10` |
| `compose-transport-one-sided` | `sha256:974778f7eece5bbb65dc33755c12cb99a35e4dbe180fbe16eaeba87ff06c4ce8` — **the same agreement as `compose-no-transport`, from a different input.** One party offering a transport must produce exactly what neither offering produces |
| `compose-party-order-not-trust-domain` | `sha256:a8d25d0295ad7dfc28c4ea31f1c0dc84dfb99a8f0ab7592cb5697d7e6ec513ab` |
| `compose-transport-outside-preference-ignored` | `sha256:09325573ce801219f61ac0f8d5d8378fc6dab8601e6465b4f600e78932d2cb61` |

A refusal produces no agreement and no digest. An implementation that emits a partial agreement
alongside a refusal has failed the vector.

## Parties

`xaa` and `xab`, from ISO 3166-1's user-assigned range (§3). A fixture copied into production
cannot collide with a real party, and an example can never be mistaken for a real relationship.

## Test keys

`testkeys/` holds the Ed25519 keys used to sign every package here, **private material included**.

**These keys are public, they are in a public repository, and they are worthless.** They exist so
that anyone can reproduce every signature in this directory and confirm the vectors are internally
consistent. Never bind one to a `sovereign_id` anywhere, never accept a package signed by one, and
never use one for anything but these files.

Each key is derived from a 32-byte ASCII seed, recorded so the whole set can be rebuilt from
nothing:

| Party | Seed | Public key (base64) |
|---|---|---|
| `xaa` | `goat-federation-v1-test-key-xaa\x00` | `VTFghAMPXAcjUb9cFBKCmt7XtJvaCCqw/58I3gpRbS0=` |
| `xab` | `goat-federation-v1-test-key-xab\x00` | `+CYZzyeVKC/5hrt3RxI1RdnF+iRXJtDdeaJ3MtLpgL4=` |

`<sid>.seed.hex` is the raw Ed25519 seed (RFC 8032 private key). `<sid>.public.b64` and
`<sid>.public.hex` are the 32-byte public key.

Two other values in the packages are derived rather than invented, so they can be recomputed too:

- `identity.ca_fingerprint` is `sha256:` + `SHA-256("federation-vectors/test-ca/<sid>")` in hex.
- `transport_offer.public_key` is base64 of `SHA-256("federation-vectors/test-transport-key/<sid>")`.
  It is a 32-byte stand-in, not a real transport key.

## Verifying a vector by hand

```
# the signature covers the signing input, not the package
python3 - <<'EOF'
import base64, json
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey
d = "compose-full"
si  = open(d + "/package-xaa.signing-input.canonical.json", "rb").read()
pkg = json.load(open(d + "/package-xaa.canonical.json"))
pub = Ed25519PublicKey.from_public_bytes(bytes.fromhex(open("testkeys/xaa.public.hex").read().strip()))
pub.verify(base64.b64decode(pkg["signature"]), si)      # raises if wrong
EOF

# the digest covers the agreement's canonical bytes
shasum -a 256 compose-full/agreement.canonical.json
```

## The reference implementation agrees

`ops/federation/reference/` passes every fixture here: byte-exact canonical forms, byte-exact
agreements, matching digests, and each refusal naming its own rule.

**This section once listed eighteen behaviours where the code and these vectors disagreed.** It
is deleted rather than kept as history, because a list of resolved gaps reads to a later
reader as a list of current ones — and the git history holds it if anyone needs it.

**If the code and a fixture ever disagree again, the code is wrong.** These vectors were
derived by hand from the specification without running the implementation, which is what makes
them able to catch it. Two of the defects they found were real bugs rather than drift: an
implementation normalising `-0` to `0` instead of refusing it, and one blind to an unpaired
surrogate because the substitution Go performs is itself well-formed UTF-8.

## Using these as a test suite

For each composing pair: canonicalise each package, check it equals the committed canonical bytes,
verify the signature against the committed signing input, compose, check the agreement equals the
committed canonical bytes, check the digest.

For each refusal fixture: compose and assert it refuses, **and assert which rule fired**. A
fixture that refuses for the wrong reason is a passing test hiding a broken implementation, and it
is the specific way this kind of suite goes quietly wrong.
