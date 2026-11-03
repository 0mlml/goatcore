# Federation conformance bundle

Everything you need to implement the federation package format without running
any of our software, and to check that you received it complete and unmodified.

Two files, and then the work:

| | |
|---|---|
| `federation-package-spec.md` | The format. This is the authority. Our tool is one implementation of it |
| `vectors/` | The conformance vectors. **Normative** — if your implementation and a vector disagree, your implementation is wrong |
| `MANIFEST` | A SHA-256 for every file, the specification version, and the commit this was built from |
| `MANIFEST.sha256` | The manifest's own digest. The manifest cannot cover itself |
| `verify-bundle.sh` | Checks the two above against every file here. POSIX shell, no dependency of ours |

Start:

```sh
./verify-bundle.sh
```

It prints the evaluation date on every run, and you need it.

---

## The evaluation date is in `vectors/now.txt`, and it is not today

Composition compares each package's `expires` against a date the specification
calls `now` (§6). In production that is the current UTC date. **In this bundle
it is an input**, supplied in `vectors/now.txt`, and your composer must read it
from there.

A composer that reads the clock instead does not fail loudly. Expiry is §6
step 3, so a stale `now` pre-empts every rule tested after it, and two
different things happen:

- **Every composing pair refuses**, because its `expires` has passed. Noisy,
  and you will find it.
- **The step-4 refusal fixtures still refuse — for the wrong rule.**
  `refuse-neither-listener` exists to prove your composer refuses a pair where
  neither party will accept a connection. Run at a later date it refuses
  because a package expired, never reaches step 4, and reports a pass. The
  specification (§10) calls that a passing test hiding a broken implementation,
  and it is the specific way a suite like this goes quietly wrong.

The fixtures for §6 steps 0, 1 and 2 are unaffected — their rule fires before
expiry is reached — so a stale `now` gives you a *partial* pass, which is worse
than a total failure because it looks like progress.

`./verify-bundle.sh` compares today against the evaluation date and names every
affected fixture, marking which ones refuse for the wrong rule, on the day you
run it. If it names none, that is luck rather than correctness, and it expires.

`compose-expiry-boundary` is the fixture whose verdict turns on the rule itself:
its second package's `expires` **equals** `now`, and the boundary is inclusive,
so it composes. Every other expiry in the set is months away, which means an
off-by-one at the boundary passes the whole suite silently.

---

## Why this bundle is not signed

Nothing here carries a signature over the bundle as a whole. That is a decision,
not an omission, and the reasoning is short enough to check.

**A bundle signature would need its own key, distributed to you.** The
specification already has exactly one authenticated channel per relationship
(§5.1), and it carries exactly two values: our Ed25519 signing public key and
our CA fingerprint, read aloud in groups of four with a readback. Signing the
bundle adds a third value to that call, or a second channel — a second
key-distribution problem, introduced to solve the first one.

**The manifest digest is the same kind of value on the same call.** Confirm
`content-sha256` — the line in `MANIFEST`, which `./verify-bundle.sh` recomputes
and prints — the way you confirm the CA fingerprint. One more 64-character
readback, no key, no rotation, nothing to expire.

**And the bundle is not on the trust path.** Nothing you do in production
depends on this archive being ours. It is a test suite over public content. A
tampered bundle can make our format look wrong to someone who is not yet a
partner; it cannot make a wrong agreement look right, because that would mean
forging an Ed25519 signature under a key bound over §5.1 — and every signature
inside these vectors verifies against `vectors/testkeys/`, which is published
here so you can check it yourself without asking us anything.

What we give up is offline redistribution: you cannot hand this bundle to a
third party and have them establish that it came from us. They should get their
own, and their own call.

---

## What is not here

Stated so that absence is not read as omission.

| Not included | Why |
|---|---|
| **Any implementation** — no library, no container image, no composer | The specification and these vectors are the whole of what you need. If they are not, that is the defect we want reported. We are not building a reference you would have to trust |
| **Our reference implementation** | It predates this revision of the specification and disagrees with it in about twenty places. `vectors/README.md` lists them. Shipping it would teach the wrong format |
| **Any private key except the published test seeds** | `vectors/testkeys/*.seed.hex` are committed on purpose so you can reproduce every signature here. They are known to everyone who has this bundle and they are **worthless**. Never bind one to a `sovereign_id` |
| **Our signing key, our CA fingerprint** | Those reach you over the independently authenticated channel of §5.1 — a call, a meeting, a courier. A key in a file that arrived by the same route as the file is a key that authenticates nothing |
| **Endpoint addresses, ports, partner declarations** | Facts about a deployment, not about the format. They change, and they are not ours to publish |
| **A verifier for *your* implementation** | `verify-bundle.sh` checks that you received this bundle intact. Whether your composer is correct is what the vectors answer, and running them is your work |

---

## Checking this bundle without the script

`verify-bundle.sh` reports missing, altered and extra files. If you would rather
not run someone else's shell script — a reasonable position — the manifest is
plain text and the content check is one pipe:

```sh
grep -E '^[0-9a-f]{64}  ' MANIFEST | sha256sum -c        # GNU coreutils
grep -E '^[0-9a-f]{64}  ' MANIFEST | shasum -a 256 -c    # macOS, BSD
```

That checks every listed file's contents. It does **not** notice a file that is
missing from the bundle but present in the manifest as an unreadable path, and
it does not notice an extra file nobody accounted for. The script does both.

And check the manifest itself first, since everything above trusts it:

```sh
sha256sum -c MANIFEST.sha256      # or: shasum -a 256 -c MANIFEST.sha256
```

---

## Is this bundle current?

Two questions, and they have different answers.

**"Are these the right vectors for this specification?"** — Yes, and you can see
it without asking us. `MANIFEST` carries `spec-sha256`, the digest of
`federation-package-spec.md` as shipped, alongside the digest of every vector.
The bundle binds them. There is no arrangement in which you hold vectors from
one revision and a specification from another and cannot tell.

**"Has the specification moved since this was built?"** — That one needs us.
`content-sha256` is the bundle's identity: it covers the file list and not the
provenance header, so it is the same value for the same files however and
whenever they were packed, and it changes the moment the specification or any
vector changes. Quote it back to us and the answer is immediate. On our side,
continuous integration recomputes it on every change and fails if it moved
without the bundle being rebuilt, so a stale bundle is a red build for us before
it is a surprise for you.

`source-commit` in the manifest names the commit these bytes came from, for when
the answer needs to be more specific than yes or no.

---

## Then what

Implement from `federation-package-spec.md`. §8.3 lists every rule that changes
the bytes and where each one is stated, and the claim there is that an
implementer working only from that document can produce byte-identical output.
The vectors confirm that; they are not where you learn the rules.

For each composing pair in `vectors/`: canonicalise each package, check it
equals the committed canonical bytes, verify the signature against the committed
signing input, compose, check the agreement equals the committed canonical
bytes, check the digest.

For each `refuse-*` fixture: compose, assert it refuses, **and assert which rule
fired.**

For each file in `vectors/malformed/`: assert the single package is refused
before composition begins, and which structural rule refused it.

`vectors/README.md` is the map, including which fixture pins which rule — and
which twelve specification defects were found by an engineer implementing it
from the document alone, in another language, with no access to our repository.
You are the second such engineer, and the defects you find are the ones we want.
