# compose-collation-utf16 — the fixture that separates the collations

§4 orders set-array elements **by UTF-16 code unit**. This is the only fixture where that rule
has an observable consequence: every other `release` and `transports` array in this directory is
ASCII, and on ASCII every candidate collation agrees.

## The three entries

`package-xaa`'s `release` carries three key expressions, chosen so the orderings diverge:

| Entry | Distinguishing character | Code point | UTF-16 code units |
|---|---|---|---|
| `release/weather/**` | `w` | `U+0077` | `0077` |
| `release/𠀀/**` | `𠀀` CJK ideograph | `U+20000` | `D840 DC00` |
| `release/Ａ/**` | `Ａ` fullwidth capital A | `U+FF21` | `FF21` |

A character above the basic multilingual plane is a surrogate pair beginning `D800`–`DBFF`, so
UTF-16 places `U+20000` **before** `U+FF21`. A code-point comparison places it after.

| Ordering | Result |
|---|---|
| **UTF-16 code unit — this specification** | `weather`, `𠀀`, `Ａ` |
| Unicode code point | `weather`, `Ａ`, `𠀀` |
| Byte-wise over UTF-8 | `weather`, `Ａ`, `𠀀` — identical to code point |

**Two orderings, not three.** UTF-8 byte order and code-point order are provably the same
ordering: UTF-8 was designed so that comparing encoded bytes gives code-point order. So a fixture
cannot separate them, and no fixture here claims to. What divides implementations is UTF-16
against the other two, and that is what this file pins.

## What the expected bytes encode, and why

`agreement.canonical.json` carries `["release/weather/**","release/𠀀/**","release/Ａ/**"]` —
**the UTF-16 order**, because §4 specifies the same comparison RFC 8785 already applies to member
names, and this document uses no other. An implementation that sorted with Go's `sort.Strings` or
Python's `sorted()` produces the other order and a different digest.

The characters appear as literal UTF-8 in the canonical form (`F0 A0 80 80` and `EF BC A1`), not
as `\u` escapes: RFC 8785 escapes only control characters, `"` and `\`. This fixture is therefore
also the only one that tests UTF-8 output.

`package-xaa`'s `release` is already in UTF-16 order, so the signer's §4 obligation is met and
this fixture is about collation alone. The unsorted case is `../compose-unsorted-release/`.

## Digest

`sha256:497a17ecae106ec320c38f75a2247c92a0ff107bdc7cafddc063b17ba54209b7`
