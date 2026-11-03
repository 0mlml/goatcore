# Partner conformance bundle

Makes the federation specification and its conformance vectors deliverable to a
partner as one archive, with a manifest binding them together and a verifier the
partner runs before trusting any of it.

```sh
ops/federation/bundle/make-bundle.sh          # writes ops/federation/bundle/dist/
```

| File | |
|---|---|
| `make-bundle.sh` | Builds the archive. Deterministic |
| `write-tar.py` | Deterministic ustar + gzip writer. Its own file so byte-identity does not depend on which `tar` is installed |
| `fixture-expiry.py` | Per-fixture earliest `expires` and the §6 step it pins, for the `now.txt` warning |
| `bundle-README.md` | Ships as `README.md` inside the bundle. Partner-facing |
| `verify-bundle.sh` | Ships inside the bundle. The partner runs it |
| `content.sha256` | The staleness ratchet, checked by `ops/ci/check-federation-bundle.sh` |

## The gap this closes

[`docs/design/federation-package-spec.md`](../../../docs/design/federation-package-spec.md) §10
makes the vectors **normative**, and §8.3 concedes the problem in the same
breath — *"an external partner may not have them"*, because they are 210 files
in a repository nobody outside can reach. The
[partner runbook](../../../docs/operations/partner-federation-runbook.md) lists
the vectors as one of the things that must be sent, and until now sending them
meant a person remembering to attach a directory: no manifest, no integrity
check, and nothing binding the files to the specification revision they were
derived from.

A partner given a stale or short bundle finds out as a digest mismatch against
us. That is the one failure mode this whole format exists to make loud and
diagnosable, arriving instead as a mystery.

## Why this bundle is not signed

The alternative was a detached signature over the manifest, under a key we
publish. It was not built, and the reasoning belongs here rather than in a
commit message.

**A signature needs a key the partner already has.** §5.1 establishes exactly
one authenticated channel per relationship, and it carries exactly two values —
our Ed25519 signing public key and our CA fingerprint — read aloud in groups of
four with a readback. That step is the irreducible human one, and the
specification is explicit that it cannot be solved cryptographically from a
standing start. Signing the bundle adds a third value to that call, or a second
channel. Either way it is **a second key-distribution problem introduced to
solve the first one**, and the first one is not solved.

**The manifest digest is the same value on the same call.** `content-sha256`
is one more 64-hex readback on a call already reading one. No key, no rotation
story, no expiry, no code. §9 makes the same argument for the agreement digest:
it may travel by any means at all, including one nobody trusts, because
altering it causes a visible mismatch and causing a false match would require
forging a signature.

**And the bundle is not on the trust path.** This is the part that settles it.
Nothing a partner does in production depends on this archive being ours. It is
a test suite over public content — a specification, fixtures, and test keys we
publish precisely because they are worthless. A tampered bundle can make our
format look wrong to somebody who is not yet a partner. It cannot make a wrong
agreement look right, because that needs an Ed25519 forgery under a key bound
over §5.1. Signing an artifact off the trust path buys the *appearance* of
assurance at the cost of a key nobody needed.

**What we give up, stated so it is a decision and not an oversight.** A bundle
cannot be redistributed with its origin intact: a partner cannot hand it to a
third party who can then establish it came from us. That is acceptable, and
arguably right — the third party should get their own bundle and their own §5.1
call, which is the same reason the specification says a directory is a cache of
a binding and never its source.

**What would change this.** If the bundle ever carries something a partner acts
on without independently verifying — an endpoint, a key, a policy input — the
argument above stops holding and it must be signed. Nothing here is that today,
and `MANIFEST` says so explicitly under *WHAT IS NOT HERE*.

## Determinism

Same inputs, byte-identical archive, so two people building from one commit can
compare. Everything that could carry the build environment is pinned: entry
order is byte-wise on the path, mtime 0, uid/gid 0, empty owner names, modes
normalised to 644 (755 for the verifier), ustar headers with no pax extensions,
no directory entries, gzip with mtime 0 and no embedded filename.

That is `write-tar.py` rather than `tar` flags because GNU tar and bsdtar
disagree on those defaults and on the flags that pin them, so an archive built
on a CI runner and one built on a laptop would differ in bytes while carrying
identical files. `make-bundle.sh` prints the digest of the uncompressed tar as
well: if two builds' `.tar.gz` differ while their `.tar` agree, the difference
is the gzip version, not the content.

The archive is named for its content — `goat-federation-v1-<first 12 of
content-sha256>.tar.gz` — so two bundles with the same name hold the same
files, and a stale one is a visibly different name rather than an identical
name over different bytes.

## Staleness is detected, not documented

`content-sha256` covers the manifest's **file list** and not its provenance
header, so it does not move when an unrelated commit changes the source commit;
it moves when, and only when, what a partner would receive changes. It is
committed at `content.sha256` and recomputed by
[`ops/ci/check-federation-bundle.sh`](../../ci/check-federation-bundle.sh),
which goes red on a specification or vector change that did not rebuild the
bundle, and tells you to run `--update`.

`content-sha256` and not `MANIFEST.sha256` is the value read aloud on the §5.1
call, and the exclusion is safe rather than merely convenient: every header
field a partner acts on is already determined by the file list. `evaluation-date`
comes from `vectors/now.txt`, `spec-sha256` is a line in the list, `file-count`
is the length of it. What sits outside is `source-commit` and prose — and
keeping `source-commit` outside is the point, because otherwise two bundles with
identical files but different commits would compare unequal and the question
"do we hold the same vectors" would have no answer. `MANIFEST.sha256` still
covers the whole manifest, and the verifier checks it first.

That check also runs a self-test proving the verifier goes red on a missing,
altered, extra or tampered file, and that two builds are byte-identical. A
verifier that only ever prints "intact" is indistinguishable from one that does
not look.

On the partner's side the two questions have different answers, and
`bundle-README.md` says so: *are these the right vectors for this
specification* is answerable from the manifest alone, because it carries
`spec-sha256` next to every vector digest; *has the specification moved since*
needs one exchange with us, and `content-sha256` makes that exchange a single
comparison.

## Sending a bundle

1. `make-bundle.sh`. Refuse to send one built from a dirty tree — it warns, and
   the manifest's `source-commit` would name a commit that does not describe
   the bytes.
2. Send the archive by any channel at all. It carries nothing secret.
3. Read `content-sha256` to the partner on the §5.1 call, alongside the CA
   fingerprint, in groups of four with a readback.
4. `--with-doc <path>` adds a document to the bundle — the partner runbook, an
   interconnection annex. Default content is fixed so that the default bundle
   is comparable between builders; anything added this way appears in the
   manifest's file list like everything else.
