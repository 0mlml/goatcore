# compose-unsorted-release — the composer normalises, the package is left alone

Closes the contradiction between §4's "the signer sorts" and the old §8.1 "verbatim from the
originating party". For a package that arrived unsorted those were different instructions and
gave different digests.

## The fixture

`package-xaa` declares, in this order and with the repeat:

```
["release/weather/**", "release/alerts/**", "release/weather/**"]
```

That is **not** conforming as a signer's emission — §4 obliges the signer to sort and
de-duplicate. It is signed over exactly those bytes, so the signature verifies and step 0
passes: the package is what it is, and a composer cannot repair it.

## Expected outcome

Composes. `flows[0].release` is `["release/alerts/**", "release/weather/**"]` — de-duplicated,
sorted, and otherwise unaltered.

| Wrong reading | What it produces here |
|---|---|
| "Verbatim from the originating party" | Three entries in arrival order, with the duplicate |
| "The signer sorted, so trust the order" | The same |
| Refuse an unsorted package | No agreement at all |

The rule: the obligation is on the signer, and **the composer normalises regardless**. The
agreement never inherits an ordering from a package. That is what keeps two composers
byte-identical on an input that ignored the obligation.

## Digest

`sha256:0d345e4a230a4cb29b62152f369a04f3b60e56c7f7eac18e1a29e8b3539235a9`
