# ADR 1148 — Define alliance and coalition, name the broad lane `REL-ALL`, and delete the alliance lane

- **Status:** Proposed
- **Date:** 2026-08-28
- **Deciders:** operator
- **Amends:** [ADR 1077](1077-affiliation-coalition-standing.md) — removes the member release-lane it introduced and fixes the vocabulary it hedged. Everything else in 1077 stands, including §5, which this decision relies on.
- **Related:** `docs/rfc/data-access-explainer.html` (the circulated model, correct) · [ADR 1143](1143-unilateral-release-declaration-and-accession.md) · [ADR 1094](1094-grant-keyed-payload-encryption.md) · [ADR 1027](1027-partner-release-set-governance.md) · F-408

## TL;DR

Two words were never defined and got used as synonyms. Defining them removes a lane: an **alliance** lane grants nothing, because alliance members already read everything. Delete it, keep the broad lane and call it **`REL-ALL`**, and write both words down.

## Context

### The model is already correct where it is written down

`docs/rfc/data-access-explainer.html`, the document external readers hold, states the mechanism plainly — **namespace-is-audience**:

| Desired audience | Mechanism |
|---|---|
| All allies | publish to your own namespace — the zero-configuration default |
| Alliance + one partner | publish to `REL-<partner>` — a single publish |
| Alliance + all partners | publish to the broad lane — a single publish |
| Fewer than all allies | grant-keyed encryption to a named release-set |

A partner's read set is cut once at onboarding — own + `REL-<self>` + the broad lane — and is then **static**. Sharing is a publish decision by the producer, not a change to anybody's grants.

### The two words were never defined

There is no GLOSSARY entry for either, and the three places the distinction decides something all hedge: `identity-grammar.md:43` ("coalition / alliance standing"), ADR 1077 §1 ("the operative alliance/coalition"), `coalition-frameworks.yaml:1` ("the coalition / alliance framework registry").

They are not synonyms. An **alliance** is treaty-based, standing and **exclusive**. A **coalition** is ad hoc and **inclusive** — allies *and* non-allied partners.

## Decision

### 1. Define both words, once, in the GLOSSARY

| Concept | Word |
|---|---|
| The treaty body and membership of it | **alliance** / **member** |
| The wider operating grouping — allies *and* partners | **coalition** |
| Which body is operative for a goatnet | **framework** |

The `affiliation.coalition` field is renamed to **`standing`**; its values `member`/`partner` already name it, and `coalition` as a field name implies a partner sits outside the coalition, which is false. The three hedges above are corrected.

### 2. The broad lane is `REL-ALL`

`REL-ALL` says what it does. **`REL-COALITION` is rejected as a lane name**: "coalition" names no *particular* coalition, so it stands in generically for a specific thing. A lane named for a coalition should name *which* coalition, and this one is not that — it is simply everyone on the backbone.

The circulated explainer currently uses `REL-COALITION` throughout and **must be updated to match**. That document is externally held, CI-gated by `check-circulated-docs-no-internal-refs.sh`, and carries one shared revision date across the bundle, so the change is coordinated with the bundle owner and rides the bundle's next revision — it is not made here.

### 3. There is no alliance release lane, and the code that derives one is deleted

Alliance members hold read-all. A lane restricted to alliance members therefore grants nothing that its audience does not already have.

`DeriveCoalitionReleaseSet` and its projection are removed, along with the framework registry's `release_lane` field. This is a **display** removal, not an access change: the derivation feeds only the Data-Access COP reveal (`internal/api/access.go`), never the ACL, so deleting it changes what an operator *sees* and nothing about who reads what.

**Checked:** `internal/access/query.go` reads those graph nodes to answer *"who may receive this topic"* — the `access why` / topic view. Removing the derivation drops the alliance lane from those answers, which is correct: it stops advertising an entitlement that grants nothing. `releasesets_test.go` is the test to re-run.

### 4. A lane controls reach; a key controls readability

Insiders read `**`, lanes included, so a bilateral lane is reachable by every ally — consistent with allied read-all. Where confidentiality between specific parties is required the mechanism is [ADR 1094](1094-grant-keyed-payload-encryption.md): the payload is encrypted and the key distributed to named certificates, so routing bytes past other readers discloses nothing. The explainer already documents this pattern.

Stated as a property. Making a routing construct carry a confidentiality guarantee is how lanes would acquire per-pair complexity.

### 5. Standing bounds eligibility and never grants — unchanged

[ADR 1077](1077-affiliation-coalition-standing.md) §5 is explicit:

> Standing bounds *who is eligible*; **it never issues a grant** (recognition ≠ authorization; the grant stays a deliberate recorded originator act).

The implementation matches: the derivation reaches the reveal, never the ACL, and ACL entries stay hand-declared. **This composes with [ADR 1143](1143-unilateral-release-declaration-and-accession.md)**, whose signed acceptance of a published release declaration *is* such a recorded act. Eligibility bounds who may be granted; a recorded act grants. **The two ADRs do not conflict**, and an earlier draft of this decision wrongly asserted they did. (That instrument is unused today — only the blank template exists, no declaration has been written — so nothing in this decision waits on it.)

### 6. Being a partner must exclude you from the insider set by construction

The partition sorts the population two ways: alliance members are enumerated in `trial-peer-read` and read the fabric; partners match a `partner-<anchor>` subject and read own + `REL-<self>` + `REL-ALL`. **Nothing in that path checks standing.** The enumeration is every ordinary slot in the pool, so which side you land on is decided by which slot number you were allocated.

Positioning that is correct today is correct **by accident**: a partner that was hand-carved after the fact sits on the right side, at the cost of the router restart the pre-cut blocks exist to avoid.

**A non-member onboarded through the ordinary allocator lands in the insider set, reading everything, and nothing reports it.**

Two changes close it, and the first is much the smaller:

1. **Teach the allocator to place a partner in a partner block.** [ADR 1138](1138-pre-cut-partner-slot-blocks.md) already pre-cuts slot ranges whose subjects are rendered in the ACL before anyone holds the cert, and those ranges are already excluded from the insider enumeration. The only missing piece is that `Registry.AllocateSlot` takes the lowest free slot with no block awareness, so a partner cannot be minted into one. Give the mint a block-targeted allocation and a partner is outside the insider set **from birth**, with no restart and no hand edit — which is what the pre-cut blocks were built for.
2. **Assert it.** A check that no identity whose standing resolves to `partner` appears in the `trial-peer-read` enumeration. It runs offline against the rendered ACL plus the roster, it is independent of how the slot was allocated, and it fails loudly rather than leaving the absence of a carve to be noticed by a person.

(2) is worth doing whichever way (1) goes, because it catches the case that produced this ADR: an identity that is on the wrong side of the partition and no surface saying so.

## The rename in the circulated bundle

`docs/rfc/data-access-explainer.html` (17 occurrences) and `docs/rfc/data-access-model.html` (4) use `REL-COALITION` for the broad lane. Both are in the CI-gated circulated set, which requires **one shared revision date across all fifteen documents**.

The change is decided here and **handed to the bundle owner** rather than made in this branch: altering 2 of 15 circulated files without restamping the set is the exact inconsistency the shared-revision rule prevents, and restamping in a parallel branch would conflict on all fifteen files and their rebuilt PDFs. It rides the bundle's next revision.

The explainer already anticipates it — it calls `REL-COALITION` *"a placeholder … concise, but not yet frozen."*

## Consequences

- Two words defined once; three hedges removed; one field renamed to what it means.
- One lane deleted rather than renamed, and the deletion follows from the definitions rather than being a separate opinion.
- The broad lane keeps the name that describes it, and the circulated document is brought to match rather than the repo bending to it.
- No access changes. Positioning on existing goatnets was audited; this decision does not move a single anchor.
- §6 is the only part with real build cost, and it is the only part that changes behaviour: today the partition is correct because of who happens to be onboarded, and after §6 it is correct because of what the system checks.
