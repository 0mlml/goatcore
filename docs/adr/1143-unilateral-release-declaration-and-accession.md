# ADR 1143 — Release is a declaration the owner publishes and the recipient accedes to, not terms the parties negotiate

| Field | Value |
|---|---|
| Status | **Accepted** (2026-08-19, operator-ratified). The decision is settled; the build behind it is not — see Consequences for what is not yet built, and expect the open question on version changes to be closed by a later amendment rather than by judgement in the moment. |
| Date | 2026-08-19 |
| Repo scope | Docs + governance: the framework policy, the instrument template, the design of record. No code changes in this decision. |
| Supersedes | — (amends the *default* set by [ADR 0821](0821-mission-partner-sharing-pattern.md); the charter itself survives) |
| Superseded by | — |
| Related | [ADR 0821](0821-mission-partner-sharing-pattern.md) (mission partner sharing — the bilateral charter this displaces as the default) · [ADR 1027](1027-partner-release-set-governance.md) (release sets — the lane a declared asset lands on) · [ADR 1075](1075-producer-data-labels-and-access-request-loop.md) (labels + the request loop) · [ADR 1094](1094-grant-keyed-payload-encryption.md) (what makes withdrawal bite) · [ADR 1102](1102-organization-owned-data-sharing.md) (the org owns the sharing authority a declaration exercises) · design: `release-declaration-and-accession.md` · policy: `docs/rfc/nato-data-policy-rewrite.md` paragraph 21 and Annex D D3 · template: [`release-declaration-and-accession.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/partner-templates/release-declaration-and-accession.md) |

## TL;DR (for a layperson)

Today, opening a data feed to another organization means negotiating an agreement with them: drafts, lawyers on both sides, disclosure officers, caveats argued clause by clause, a stack of signatures, months. Do it again for the next organization, in full. This decision replaces that, for unclassified data, with one document the owner writes **once** and publishes: here is what I share, here is how you must handle it, here is how I take it back. Anyone who wants the data signs a one-page acceptance of those terms and the data flows. The tenth recipient costs what the first cost, and partners come through the same door as allies.

**What this does not do is remove the recipient's obligations.** The recipient still commits, in writing and under signature, to handle the data as the owner requires — what it may be used for, whether it may be passed on, keeping the origin attached, protecting it in proportion to its sensitivity, keeping an access record, reporting an incident, and what happens when the owner withdraws. Every one of those survives; what is removed is the *negotiation* over them. The owner writes the terms because it is the owner's data and the owner carries the risk; the recipient accepts them as written or does not receive the data.

## Context

**The cost is quadratic and nobody pays it.** For *n* owners and *m* recipients, bilateral agreements mean *n·m* instruments, each a full staffing cycle. This is the same shape as the pair-by-pair network bridging the framework policy rejects in paragraph 7 — the pairs are just lawyers instead of firewalls. In practice the cost is not paid: the sharing does not happen, and the absence is invisible because nothing records a flow that was never requested.

**The charter we already have is bilateral.** The Partner Data Sharing Charter ([`mission-partner-sharing.md`](../design/mission-partner-sharing.md), ADR 0821) is a good instrument and correctly framed as "necessary but not compelling." But it is signed and countersigned per partnership, and its onboarding workflow puts charter exchange and signature at step 2 of nine — before OIDC federation, before namespace allocation, before any data moves. For a partnership that genuinely has bilateral terms, that is right. For an owner that simply wants to open its own unclassified data, it is the whole cost of the thing.

**The authority already exists; only the shape is wrong.** For non-classified information, C-M(2002)60 paragraph 12 delegates release to the body or nation **holding** the information, which "shall determine and elaborate any additional procedures necessary for its release," and paragraph 14(b) contemplates release subject to "whatever arrangements are in place for the exchange of information with the entity concerned." Nothing requires the procedure to be negotiated per recipient. It is negotiated per recipient by habit.

**NATO already has the recipient-side form.** A Security Assurance (C-M(2002)49-REV1 Enclosure H paragraphs 10–12) is a recipient's own commitment, signed by "a representative duly mandated," to give "an appropriate degree of protection" to what it receives, "limited to the specific activity, for a specific period of time." That is an accession in all but name — used for the harder, classified case.

**And the categorical problem.** Enclosure H paragraph 3 defines a closed roster of seven privileged non-NATO nations. A partner outside it has no category to occupy. Any instrument that inherits that shape excludes the participants this substrate exists to reach — and the roster as printed is itself stale, naming two states that have since acceded to NATO.

## Decision

1. **The default instrument for releasing unclassified data is a published, unilateral release declaration.** The owner states what it releases, who may accede, the handling terms, the duration, and the revocation terms. It is complete on adoption. No countersignature, no negotiation, no second party required for it to exist.

2. **A recipient activates a release by accession — one recorded act, accepting the declaration as published.** Accession is a **substantive undertaking, signed by a representative mandated to give it**, not an acknowledgement of receipt: the recipient binds itself to the declaration's handling terms, including protecting the data in proportion to its sensitivity. What accession removes is the negotiation, not the obligation. Editing the declaration is not acceding; it is asking for a different declaration, which the owner may write or refuse. **This is load-bearing**: the moment a counter-draft is accepted the mechanism decays into the bilateral drafting it replaced.

3. **Eligibility to accede is a demonstrable property, never a category.** Allies and partners accede on the same terms. A declaration may condition accession on something a recipient can show; it may not condition it on alliance membership, nationality, or partner status. This repudiates the closed-roster shape explicitly.

4. **An accession is enacted as exactly one access grant** — to the acceding organization's identity, over the declared assets, carrying the declaration identifier as its recorded reason and the declaration's expiry. **There is no second store of rules.** The declaration and accession are documents; the grant is the only artifact the enforcement plane reads.

5. **Withdrawal is unilateral, uncaused, and effective at the key.** The owner may withdraw the whole declaration or any single accession at any time. Withdrawal revokes the grant and changes the key **first**, then marks the register — so the register is never the thing that has to be believed.

6. **Expiry is lapse, not termination.** An accession that is not renewed stops on its own. An unattended declaration decays toward closed.

7. **No reciprocity.** A recipient owes nothing back. A recipient that wants to share publishes its own declaration. Bundling reciprocity reintroduces the negotiation.

8. **The declaration binds only the declarant's own data.** It commits no other participant and releases no one else's assets.

9. **The charter survives as the exception.** Where a relationship genuinely needs bilateral terms — reciprocal obligations, jointly-owned data, negotiated liability — the Partner Data Sharing Charter is the right instrument. **Never both over the same data.**

10. **Classified release is untouched.** Nothing here substitutes for a Security Agreement, a Security Assurance for classified information, or any national classified-release process.

## Consequences

**Good.** Owner-side staffing cost is paid once instead of per recipient. Recipient-side cost is a signature. Partners reach the same data as allies without a category being invented for them. Withdrawal becomes real — a key change, not a termination clause. And "who is reading my data, under what terms?" is answerable from the grant registry, where every other access question is already answered.

**Costly.** A **register of declarations and accessions is not built.** Today an accession would land as a grant with a free-text reason, losing which declaration and which version it was made under. That is the honest gap and the natural next build.

**Contentious.** Legal reviewers accustomed to bilateral instruments will read a unilateral declaration as one-sided, because it is. The answer is that it is one-sided in the direction of the party taking the risk: the owner sets terms over its own data and can withdraw at will, while the recipient's only exposure is the eight handling terms it read before acceding.

**Unresolved.** Whether a material change to the handling terms lapses existing accessions or grandfathers them. Leaning lapse-on-material-change, but the rule is not sharp enough yet to apply without judgement.

## Alternatives considered

**Keep the bilateral charter as the default.** Rejected: the arithmetic is the objection, and it does not improve with practice.

**A multilateral framework agreement all participants sign.** Rejected: it needs every participant to agree before any two can share, which is the *n·m* problem converted into an *n*-party negotiation — strictly worse to start.

**Terms of service accepted by clicking at first subscribe.** Rejected: it removes the recorded, attributable acceptance that makes the audit trail meaningful, and an organization's obligation should be undertaken by someone mandated to undertake it.

**Extend the seven-nation roster to cover more partners.** Rejected: a fixed roster is the wrong instrument regardless of who is on it, and this decision rejects the shape rather than the membership.
