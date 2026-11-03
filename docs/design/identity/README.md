# Universal identity — the grammar source-of-truth

> **The ambition:** one coherent model of *who and what every actor is* — people,
> systems, and processes; commercial, government, allied, and civil — that many
> different systems can share. Almost nobody attempts this; identity is fragmented
> per-system, and that fragmentation is exactly what makes connecting many systems
> lossy, brittle, and capped in scale.
>
> **The foundation for it:** write the identity vocabulary **once** in a
> machine-readable source and **generate** every schema, taxonomy, and doc from it
> — so it never fragments, even internally.

*(This page renders on GitHub. For the fully-designed visual version, open
[`grammar-sot-synthesis.html`](grammar-sot-synthesis.html) in a browser — GitHub
shows HTML as raw source, so read the narrative here and use the HTML locally.)*

---

## The problem (why this exists)

**The big problem: there is no universal identity, and almost no one attempts one.**
Every platform, org, nation, and vendor defines identity its own way — a person, a
unit, a machine is a different, incompatible record in each. So when you connect
many systems (a coalition, a data fabric, a multi-vendor ecosystem) there is no
shared notion of *who or what an actor is*: every integration becomes a bespoke
N×M mapping, meaning is lost in translation, trust doesn't carry across the seam,
and the whole thing stays brittle and capped in how far it can scale. A single,
coherent identity that spans commercial / government / allied / civil actors **and**
people / systems / processes is rare-to-nonexistent — and that absence is the
ceiling on interconnecting systems at scale. goatnet — a NATO-scale unclassified
backbone meant to connect thousands of orgs and millions of systems across many
nations and vendors — lives or dies on solving it.

**The same disease, in miniature, inside our own stack.** Even here, one identity
vocabulary is already hand-copied across ~13 places — the onboarding manifest
schema (twice), three Ory Kratos schemas, the NetBird group taxonomy, the
certificate grammar, and a dozen doc tables. It has already bitten us: the `civil`
affiliation kind landed in the manifest schema but **not** the Kratos schema, so
onboarding a real civil org would have been silently rejected. If identity
fragments inside a single well-run codebase, imagine it across a coalition.
Refusing to let it fragment *even at the smallest scale* — one coherent source,
generated everywhere — is the precondition for the universal model we ship outward
(the portable descriptor, the ontology export). CI guards catch drift today, but a
guard is a band-aid over fragmentation; this removes the fragmentation itself.

## The idea (one source, generated outward)

```
        ops/identity/grammar.yaml            <-- the ONE source you edit
                   |
             generate.py  (deterministic)
                   |
   +---------------+----------------+----------------+
   v               v                v                v
 manifest      Ory Kratos      NetBird          cert value-sets
 schema        schemas         taxonomy         + descriptor schema
   |
   +--> human doc tables + glossary + an external "ontology" export
```

Derivation only ever flows **one way** (source → outputs). You never edit a
generated schema; you edit `grammar.yaml` and regenerate. Adding a value becomes
**one edit**, and drift becomes *impossible* instead of *caught-after-the-fact*.

## The one conceptual thing to get: three sources of identity

Identity here isn't one clean taxonomy — it's the **reconciliation of three worlds
that don't line up**, and every value in the vocabulary is tagged with which world
it comes from (`authority: doctrinal | native | industry`):

| Source | What it is | Who's the authority |
|---|---|---|
| **doctrinal** | Identity that already exists in military canon — UIC, NATO LOGFAS codes, echelon doctrine, nation codes. | **Not us.** We conform + map to it. |
| **native** | New ground doctrine doesn't name — `PROCESSOR`/`PLATFORM` node-classes, `autonomy`, the `civil` kind, the relationship edges. | **Us.** Canonical-to-goat. |
| **industry** | Patterns adopted from the field — SCIM, SPIFFE, Zanzibar tokens, SKOS. | The standard; we align to it. |

The load-bearing rule that ties them together:

> **goat's own UUID anchor is the identity key — *because* doctrinal codes are
> often latent, wrong, non-unique, too coarse, or valid only in one nation.**
> A UIC or LOGFAS code is attached to the anchor as a *graded, mapped attribute*
> (with a provenance grade and a scope), **never** as the primary key. When
> doctrine is wrong or missing, we record that — identity doesn't break, because
> it never depended on the external code being right.

## See it concretely

- **[`EXAMPLES.md`](../../../ops/identity/EXAMPLES.md)** — five worked identities
  (a US developer, a partner-nation operator with a logistics-system code, a civil
  academic, a vendor autonomous processor, an allied autonomous sensor) showing
  the dimensions *composed*. **Start here to see the model in action.**
- `python3 ops/identity/generate.py --catalog` — the whole vocabulary (every
  dimension, value, authority, definition) in one legible dump.

## The files, and what each is *for*

| File | What it is | Read it when |
|---|---|---|
| **this page** | the plain-language overview | you want to *get the idea* |
| [`EXAMPLES.md`](../../../ops/identity/EXAMPLES.md) | five worked identities | you want it *concrete* |
| [`grammar.yaml`](../../../ops/identity/grammar.yaml) | the source-of-truth (data) | you want the *actual vocabulary + definitions* |
| [ADR 1067](../../adr/1067-identity-grammar-generated-sot.md) | the decision + rationale | you want *why we chose this* |
| [`grammar-sot-synthesis.html`](grammar-sot-synthesis.html) | the visual synthesis | you want the *designed deep-dive* (open locally) |

## Status

Built + proven, nothing flipped yet ([ADR 1067](../../adr/1067-identity-grammar-generated-sot.md)):
`grammar.yaml` + a generator that reproduces the current schemas **byte-for-byte**
(CI-gated, so the source can't silently drift). Flipping the real schemas to be
*generated from* the source (and retiring the drift-guards) is the next increment,
timed to a real trigger — building the descriptor schema, or the next
grammar-change wave.

## Where this sits in the arc

The foundational **vocabulary** layer. The
[identity-descriptor standard](../identity-descriptor-standard.md) *packages and
ships* this grammar, the actor-relationship graph
*projects* it, and the RFC *narrates* it — all
deriving from this one machine source.
