# goat net deployment profiles — identity/access is a config decision, not a rebuild

**BLUF.** Deploying goat net's identity, affiliation, and access model is a **simple configuration choice**, not a code change. The default is the smallest possible: **organic-only** — one org, everyone `organic`, no coalitions, no partners, no sectors, community access. That is the "just try it out for yourself" path most first-time adopters take, and it is what an open-source pickup gets with zero identity configuration. Coalition, partner, sector, and release-lane machinery are **additive** — turned on only when a deployment actually has coalition partners. Nothing about the richer cases is in the way of the simple one.

This is the [complexity-budget](https://github.com/usareur-af/goatdocs/blob/main/docs/design/complexity-budget-and-encapsulation.md) principle (one primary doc, one rollback per path) and instance-variability-model.md applied to identity: the simple path is the **on-ramp, not a filter**.

## Why this matters (open-source + self-serve)

goat net is being open-sourced. Most people who pick it up first stand up a **single-org instance to try it** — they should not have to understand NATO, coalition frameworks, or releasability to do that. And it must be **entity-neutral**: no org-, nation-, or alliance-specific assumption is baked into the code. Every such specific is data/config (the coalition-frameworks registry, the active framework, the display lexicon, site names) — the code ships neutral.

## The profiles

A profile is a small bundle of per-goatnet config defaults. It sets no policy the deployer can't override; it just makes the common case one decision.

| Profile | Who it's for | What it sets |
|---|---|---|
| **`organic-only`** *(default — try-it-out)* | One org trying goat net; a single-service/agency deployment | active coalition framework = **none**; onboarding does not prompt for nation/coalition/sector/partner; `kind` defaults to `organic`; default data-access lane = community. Coalition standing is simply unused. |
| **`coalition`** | A multi-national / mission-partner deployment | active framework set (e.g. `nato`, or any entry in [`coalition-frameworks.yaml`](../../ops/identity/coalition-frameworks.yaml)); onboarding prompts for nation + coalition standing + partner kind; release-lane eligibility keyed on `affiliation.coalition` (ADR 1077). |
| **`interagency`** *(reserved)* | A gov-mil + law-enforcement / interagency deployment | as `coalition`, plus `affiliation.sector` prompted and (future) sector-scoped authorities. Deliberately not fully built — the primitive (`sector`) exists; the dynamics land when needed. |

Profiles are additive: a deployment can start `organic-only` and turn on `coalition` later by editing config — no rebuild, no re-mint (the identity attributes were optional all along; existing organic identities keep validating).

## What makes the simple path real (not just prose)

Every affiliation sub-attribute beyond `{id, kind}` is **optional** in the manifest schema — verified: `{id: my-unit, kind: organic}` validates with no nation/coalition/sector/partner. So an organic-only deployment:
- mints identities with `affiliation: {id, kind: organic}` and nothing else;
- never consults `coalition-frameworks.yaml` (no active framework);
- gets community data access by default (the recommended posture, [data-access-roles.md](data-access-roles.md) §12).

The coalition/sector machinery is inert until a deployment opts in. There is no organic-only-vs-coalition fork in the *code* — only in *config*.

## Config surface (where a profile is chosen)

- **Per-goatnet active framework** — instance config (group_vars / console config), default `none`. `coalition`/`interagency` profiles set it to a framework id. This is the single lever that turns coalition standing on. *(Console honoring the profile in the onboarding form — prompt-gating, defaults — is the sequenced build-next; the model + defaults are in place.)*
- **The frameworks registry** ([`coalition-frameworks.yaml`](../../ops/identity/coalition-frameworks.yaml)) ships with NATO populated as an example; an open-source adopter edits/replaces it for their region (or ignores it for organic-only). Membership is effective-dated, so relationship changes are data edits (ADR 1077 "living relationships — no rebuild").
- **Data-access default lane** — per-goatnet (`data_label_access_default`, default community).

## Composition

This profile model is the deployer-facing front of the identity grammar (ADR 1043), the affiliation coalition-standing + sector attributes (ADR 1077), and the data-access roles (ADR 1074). It adds no new enforcement — it selects defaults and gates onboarding prompts, so the same neutral code serves a solo hobbyist and a multi-national mission partner network.
