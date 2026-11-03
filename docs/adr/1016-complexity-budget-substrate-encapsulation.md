# ADR 1016 — Complexity budget and substrate encapsulation as first-class engineering discipline

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-05-12 |
| Repo scope | goat core; applies to every consumer-repo where substrate has accumulated faster than adoption |
| Supersedes | — |
| Superseded by | — |
| Related | [`1011-findings-register.md`](1011-findings-register.md) (leak-register pattern), [`1015-code-review-discipline.md`](1015-code-review-discipline.md) (block-completion checklist gains a complexity-leak audit row). Companion design at [`docs/design/complexity-budget-and-encapsulation.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/complexity-budget-and-encapsulation.md). Composes with the engineering-health scorecard at [`docs/design/engineering-health-assessment.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/engineering-health-assessment.md), the OSS go-open execution plan in Block 29, and the engineering-health scorecard in Block 32. |

## TL;DR (for a layperson)

We've added a lot of useful pieces to the substrate — fifteen-ish, each one for a good reason. Each piece works. They compose correctly. The problem is that someone new picking up the project would have to learn all fifteen to get started, and that is too much. This ADR says: from now on, we measure how many of those pieces a new operator has to know about, and we engineer to keep that number small. The pieces themselves stay. What changes is how visible they are to a consumer. The bar is "no more expertise than would be required to adopt any other well-run open-source project."

## TL;DR (for a sponsor)

This codifies the discipline that keeps the goat substrate adoptable as it matures. The visible deliverable is a written bar, a quarterly review ritual, a layer inventory, and a leak register parallel to the findings register. Cost: roughly half a day per quarter to walk the four onboarding paths and update the inventory. Benefit: the substrate stays viable as a product that partners, vendors, and operating units can adopt without a multi-month learning curve. This is the spine of the OSS go-open strategy (Block 29); without it, every new substrate decision is unanchored.

## TL;DR (for SCA-R)

Complexity budget is a standing engineering metric anchored in this ADR; the framework design is at [`docs/design/complexity-budget-and-encapsulation.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/complexity-budget-and-encapsulation.md); the implementation tracking is Block 81. Four onboarding paths (5-min / 30-min / 1-day / 1-week), each completable with one primary document and one rollback. Substrate inventory classifies each layer as consumer-facing or maintainer-only. Leak register captures encapsulation failures. Quarterly review cadence runs parallel to the engineering-health scorecard at [`docs/design/engineering-health-assessment.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/engineering-health-assessment.md). New substrate landing consumer-facing requires a leak-register entry + fix plan in the same change-set. This ADR is the policy anchor; the discipline is the deliverable.

## §1 Vocabulary

- **Complexity budget** — the count of substrates a consumer is required to learn before they can run the system. A small, monitored number; the engineering target for keeping the substrate adoptable.
- **Substrate** — a discrete layer of the system that solves a specific real problem (e.g., wg-cp0 silent control plane, offline X.509 CA, Spock multi-master replication, Ory identity, mesh DNS, snitch fleet health). Each was a good decision at the time.
- **Consumer-facing substrate** — one that any of the four onboarding paths surface to an operator (asks them to configure it, name it, distinguish it, troubleshoot it).
- **Maintainer-only substrate** — one that the four onboarding paths can complete without the operator knowing it exists.
- **Onboarding path** — 5-min / 30-min / 1-day / 1-week paths from the design doc; the measurable cadence of consumer experience.
- **Encapsulation layer** — make-it-work / make-it-true / make-it-yours, per the design doc; the three independent disciplines for managing visibility.
- **Complexity leak** — an instance where an operator following an onboarding path was forced to drop down a layer (read a substrate-specific doc, make a substrate decision, learn a substrate they should not have needed). Captured in the leak register.
- **Leak register** — the durable append-only record of complexity leaks, parallel to the findings register ([ADR 1011](1011-findings-register.md)).

## §2 Context

By 2026-05-12 the goat substrate had accumulated roughly fifteen layered substrates (catalog at [`docs/design/complexity-budget-and-encapsulation.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/complexity-budget-and-encapsulation.md) § Goat as worked example). Each substrate was individually justified — each closed a specific gap, none was speculation. They compose correctly. The findings register (F-090 through F-111) shows roughly fifteen findings in a one-week window, none of which are bugs in any single substrate. Every one is an interaction bug between layers — TLS chain across offline-CA + Traefik + Go gRPC + macOS Keychain, trust-anchor distribution across bundle + first-boot + operator-laptop, image-distribution across upstream-vs-mirror-vs-pull-through.

This is the diagnostic signature of a substrate that is correctly built but un-encapsulated. The pieces are right. The user-facing surface is too large. New operators bounce off; vendor integrations stall; partner adoptions stretch.

Two other forces make this acute now:

1. **OSS go-open (Block 29)** is approaching publication. The 5-min `git clone && docker compose up` demo is the entry point a new adopter will use; without an encapsulation discipline, the demo becomes a curated facade rather than a true representation of the substrate.
2. **Partner standalone-goatnet productization (Block 72)** assumes a partner with ~2 engineers can stand up the substrate in ~6 weeks. That assumption is only viable if the partner is not required to internalize every layer.

Two failure modes were considered and rejected as the response:

- **Substrate removal.** Each layer is individually justified; removing them gives up real value. Some sub-layers may be repositioned in sequencing (see active-work.yaml queued track `netbird-fork-desktop-decouple` for one example), but the dominant pattern is encapsulation, not removal.
- **"Document harder."** More documentation grows the surface that operators must read, not shrink it. The failure mode is precisely that the substrate is well-documented and yet still too heavy to adopt.

The response is to make complexity itself a measured, governed engineering property — parallel to the engineering-health scorecard (Block 32, design at [`engineering-health-assessment.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/engineering-health-assessment.md)) — with a written bar, a leak register, and a quarterly cadence.

## §3 Decision

D1. Complexity budget is a first-class engineering metric for the goat substrate, owned by the maintainer team, measured quarterly, reported alongside the engineering-health scorecard.

D2. The bar is **the four onboarding paths** defined at [`complexity-budget-and-encapsulation.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/design/complexity-budget-and-encapsulation.md) § The bar. Each path is completable with one primary document and one rollback procedure. Any path that requires multiple primary documents to succeed contains a complexity leak.

D3. The strategy is **encapsulation, not removal** — three layers (make-it-work / make-it-true / make-it-yours) per the design doc. Substrate inventory does not shrink as a goal; user-facing surface does.

D4. **Substrate inventory.** Maintainers keep a living inventory document classifying every substrate as consumer-facing or maintainer-only. Inventory refresh is part of the quarterly review.

D5. **Leak register.** Complexity leaks are captured in an append-only register parallel to the findings register ([ADR 1011](1011-findings-register.md)). Leaks close when the implicated path runs end-to-end without dropping a layer. Closed leaks stay in the register for trend analysis.

D6. **Quarterly review cadence.** Once per quarter, maintainers (a) refresh the inventory, (b) run each of the four onboarding paths end-to-end and record every doc consulted, (c) triage open leaks, (d) record the count of consumer-facing substrates as the headline metric. This review is committed to the repo at `docs/project/complexity-budget-<YYYY>-Q<N>.md`.

D7. **No-new-substrate-without-budget rule.** New substrate that lands consumer-facing carries a leak-register entry + fix plan in the same change-set that introduces it. Substrate that lands maintainer-only requires no entry. This is the analog of "no new finding without ADR" — substrate that affects the user surface is treated as a measured cost.

D8. **Block-completion checklist amendment.** The block-completion checklist ([`docs/process/block-completion-checklist.md`](../process/block-completion-checklist.md), per [ADR 1015](1015-code-review-discipline.md)) gains a complexity-leak audit row. Authors of completed blocks attest whether their work added to or shrunk the user-facing surface, and link to the corresponding leak-register entries if any.

D9. **Severability.** The framework is severable from goat. The design doc is written so a different infrastructure project can adopt the discipline without depending on any goat-specific substrate. The goat-specific application is the worked example.

## §4 Alternatives considered

A1. **Cull substrates aggressively.** Rejected — many substrates that look optional today are doing real work (sovereignty, defense-in-depth, audit). Aggressive removal sacrifices the goat value proposition for adoption gain that is achievable through encapsulation.

A2. **Single-document onboarding (the "one big README")**. Rejected — the failure mode is precisely that operators are required to read too much; concentrating all the reading in one document does not change the budget.

A3. **Hire/staff a developer-experience team.** Rejected as the *response*. Eventually a healthy project does need adopter-focused engineering, but the discipline must be in place first or the team works against an undefined bar.

A4. **Defer until after OSS publication.** Rejected — publication is the moment the budget is tested at scale. Discipline must be in place before that, not after.

A5. **Treat complexity as a documentation problem.** Rejected — documentation supports encapsulation; it does not substitute for it. A documented substrate is still a substrate the consumer has to know about.

## §5 Consequences

- The substrate stays adoptable as it matures. Partners with their own engineering capacity can integrate without forking.
- New substrate decisions are made under an explicit budget rather than implicit growth. Each substrate landing carries a "consumer-facing or maintainer-only?" answer.
- Quarterly review surfaces drift early; trend in the count of consumer-facing substrates becomes a visible health metric.
- The OSS go-open publication (Block 29) has a load-bearing measurement (the 5-min path) instead of relying on curation.
- Some current-state work will reclassify: substrates that look maintainer-only on paper may surface in onboarding paths the first time they are walked end-to-end, generating leak entries that drive Block 81 sub-tasks.
- Existing block-completion reviews ([ADR 1015](1015-code-review-discipline.md)) gain one more checklist row. The cost is ~1 minute per block release.

## §6 Glossary pointer

See `docs/adr/GLOSSARY.md`.
