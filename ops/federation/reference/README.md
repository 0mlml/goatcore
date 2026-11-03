# Reference implementation — federation package v1

Implements [`docs/design/federation-package-spec.md`](../../../docs/design/federation-package-spec.md).

**The specification is the contract; this is one implementation of it.** A partner must be
able to implement the specification from scratch, in any language, without running this code.
If any behaviour here is required for interoperability but is not written in the
specification, the specification is wrong and must be corrected — not this code.

The conformance vectors in [`../vectors/`](../vectors/) are normative. They were derived by
hand from the specification without running this code, which is what makes them a test of it.
**If this code and a vector disagree, this code is wrong.**

## Layout

| File | What |
|---|---|
| `internal/fedpkg/canonical.go` | RFC 8785 canonical form. Numbers are carried as `json.Number` and never through a float, and every integer is checked against ±(2^53 − 1) before it is written. `LessUTF16` is the one comparison the format uses (§4) |
| `internal/fedpkg/package.go` | Package parsing, structural validation, derived names, signature verification, digest |
| `internal/fedpkg/compose.go` | Two halves to one agreement. Sorted by `sovereign_id` into positions, so both sides compute identical bytes |
| `cmd/fdverify/` | The operator surface: `canonical`, `digest`, `lint`, `verify`, `compose`, `diff` |

## Running the vectors

```bash
cd ops/federation/reference
go test ./...          # 33 fixtures, byte-exact, plus the property test
go vet ./... && gofmt -l .
```

`internal/fedpkg/vectors_test.go` handles both fixture kinds:

| Kind | Count | What the test asserts |
|---|---|---|
| `compose-*` pairs | 13 | The agreement's canonical bytes byte-for-byte, and the digest string |
| `refuse-*` pairs | 10 | Refused, **under the rule it is named for**, and under no other |
| `malformed/*.json` | 10 | A single package, read as **bytes**, refused at parse or by structural validation — never as a composition refusal |

The malformed kind is not a directory of pairs: one file, no counterparty, no agreement, no
digest. Four of the ten carry tokens no conforming serialiser emits and two are not valid JSON
at all, so the test reads the committed bytes and never round-trips them through this
implementation's encoder — anything this code produced would not be the fixture.

Every file there is unsigned by design, which keeps each refusal attributable to the rule the
file is named for rather than to a failed verification. So the assertion is that the findings,
setting aside that shared unsigned-ness, are **exactly** the one named rule. Asserting only
"it was refused" would pass an implementation that noticed nothing but the missing signature.
`no-signature.json` is the exception: there the absent member is itself the finding.

A fixture that refuses for the wrong reason is a passing test hiding a broken implementation.
For the malformed kind the verdict alone is not enough: an implementation that reports one as a
composition refusal has the right verdict from the wrong place, and names a rule the package
does not break.

## The command

```bash
go build -o fdverify ./cmd/fdverify

V=../vectors
./fdverify compose -keydir $V/testkeys -now $(cat $V/now.txt) -digest \
    $V/compose-full/package-xaa.canonical.json \
    $V/compose-full/package-xab.canonical.json
```

Exit status is `0` for success, `1` for a refusal, a lint finding or a difference, and `2` for
a usage error. A refusal is the mechanism working, so it is distinguished from a failure.

`diff` is the verb for the moment two digests disagree: it names the differing field and shows
both values, which is what section 9 promises an operator staring at two unequal hashes.

**The verifying key is a parameter.** It is the key bound to a party's `sovereign_id` over a
channel authenticated independently of the relationship being established (§5.1). This program
never fetches one, and a directory is a cache of that binding, never its source.

## Where each rule is enforced, and why it matters

**Malformed before composed.** A malformed package is refused by structural validation before
§6 step 0, not by it. Some malformed packages have no canonical form at all —
`"measured_mtu": 1380.0` is the fixture — so their signing input cannot be produced and a
signature check would report an unverifiable signature. That is the right verdict from the wrong
place. The `spec` value is the deliberate exception: it is a literal pin at step 1, so that a
mismatched pair and a both-on-another-version pair produce the same named composition refusal.

**A null canonicalises; a null is refused.** Those are not in tension. §4 forbids nulls as a
package rule and an agreement rule, not as a serialisation rule. A package carrying a null still
has a canonical form and a signature over it, and a receiver must reproduce those bytes to check
that signature before it can say anything about the package — `malformed/null-member.json` ships
exactly that: byte-exact canonical form, valid signature, refused by structural validation. So
`Canonical` writes `null` faithfully, `Package.structure` refuses it at any depth, and `Compose`
checks the agreement it built carries none.

**Some rules can only be enforced on the raw bytes.** Go's JSON decoder silently replaces an
unpaired surrogate escape with `U+FFFD`, and both the raw input and the decoded string are then
valid UTF-8 — so no check after decoding can see what happened. `DecodeJSON` therefore scans the
input text before parsing it. Substitution is the worst available behaviour here: silent, and it
changes the bytes and so the digest, with nothing reported on either side and no field that
differs. A decoder that replaces invalid input has composed a document that was never signed.

**One comparison, everywhere.** Member names, set-array elements and the party sort all use
`LessUTF16` (§4). Go's `sort.Strings` compares bytes, and byte-wise UTF-8 order is code-point
order, which differs from UTF-16 on exactly one thing: a character above the basic multilingual
plane is a surrogate pair beginning `D800`–`DBFF`, so UTF-16 places `U+20000` before `U+FF21`
where a code-point comparison places it after. `compose-collation-utf16` is the fixture. The diff
report in `cmd/fdverify` uses the same comparator, so the codebase holds no second one that could
drift from the first.

## Two properties this code is arranged around

**The canonicaliser cannot return a clean digest over bytes that parse nowhere.** Output with a
nil error that satisfies no parser is the worst failure this format can have, because both
sides can agree on it. `Canonical` refuses an empty number, a non-integer, an integer outside
the exactly-representable range, a member with no value, and a string that is not valid UTF-8.
`TestCanonicalNeverLies` asserts over fifty thousand generated inputs that every call either
errors or produces parseable JSON, and never both a nil error and unparseable bytes.

**A member is never emitted without a value.** `transport` appears only when both packages
carry a `transport_offer`; a flow appears only when one side releases and the other accepts.
Where a value cannot be derived the containing member is omitted, or the composition refuses.

## What is derived and never read

`peering_cn` and `publish_under` follow entirely from the two `sovereign_id` values, so this
code computes them and ignores any package that declares them. A value that can only be
derived cannot be disagreed about, and a name that is never written cannot be mistyped into a
refusal that is silent at both ends.

## Coverage the vectors do not reach

Measured by mutating one rule at a time and re-running only the tests that measure this
implementation against fixture content. Of the eighteen gaps the first sweep found against 88
files, **seventeen are now closed**. What is left:

| Rule | Wrong implementation that still passes all 33 | Pinnable? |
|---|---|---|
| §4 text is Unicode | No check that the **raw input bytes** are valid UTF-8. `unpaired-surrogate.json` pins the escape form; no fixture carries a raw invalid byte | **Yes.** A `malformed/` file with a raw `0xFF` inside a string — the last pinnable gap |
| §4 no null in an agreement | An agreement carrying a null emitted rather than caught. The package side is pinned by `null-member.json` | No. A fixture is an input; this is a rule about output the implementation itself produces |
| §4 one ordering | Member names sorted by bytes rather than UTF-16 | No. Every member name the format defines is ASCII, so the collations cannot disagree. `compose-collation-utf16` pins the comparator through set-arrays, which is the reachable half |
| §4 UTF-8 output | No validity check on decoded strings | Unreachable rather than unpinned: once the raw bytes are checked, a decoded string cannot be invalid unless a caller builds one in Go. Defence in depth |

`compose-transport-outside-preference-ignored` closed the last of the §7 selection rules, and
closed two at once: both "first of the sorted intersection" and "first of either party's declared
list" select `quic` there, which §8.1 does not permit as a `transport.kind`.

`internal/fedpkg/compose_test.go` and `canonical_test.go` cover all of these against the
specification text. That is this implementation protecting itself — a second implementation
still has no vector to check itself against on any of them.
