# ADR 1168 — Self-hosted paging on the relay tier, and paging enrolment as part of the PLATFORM-ADMIN grant

**Status:** Accepted (operator-directed 2026-09-06). **Instance 1 built and measured on a sandbox goatnet the same day** — `paging.md` is the design of record for what runs. **Amends ADR 1059**, which kept PagerDuty as the critical-tier pager and deferred the sovereign alternative. The commercial pager was never activated, so the deferred alternative became the only one. ADR 1059's *reasoning* survives intact and is what this decision is built on; only its choice of vendor changes.

**Repo scope:** Decision, then the build. Written as design only in the morning; by the afternoon instance 1 was running on a relay-health host, the site host's Alertmanager was publishing to it, and the enrolment hook was in `operator-tier-onboard.yml` — see `paging.md` for what is measured and what is not built (second instance, dead-man, revocation twin).

**Related:** ADR 1059 (the decision this amends) · `failure-isolation.md` §9 · [ADR 1031](1031-credential-revocation-mesh-first-offboarding.md) (the reverse of the enrolment added here) · `identity-grammar.md` (why affiliation must not become a paging group) · [`external-icam-federation.md`](../design/external-icam-federation.md) (federated PLATFORM-ADMINs) · `site-resilience-and-continuity.md` (the partition behaviour this has to survive) · [`onboard-tier-role-model.md`](../design/onboard-tier-role-model.md) + `ops/ansible/operator-tier-onboard.yml` (where enrolment hooks)

## Plain-language summary

Alerts about our own infrastructure have to reach a person's phone. We were going to use PagerDuty for that; it was never activated, so we need our own. The catch is that a pager which runs on the machine it is watching cannot tell you that machine died — so ours runs on the relay servers instead, which are somewhere else entirely. And because being an administrator is what makes someone responsible for answering, being *made* an administrator is what should put them on the pager, automatically, rather than a step someone remembers separately.

## Context

- **The commercial option is gone.** ADR 1059 accepted PagerDuty as a bounded sovereignty delta and made activation per-site "operational only — set `PAGERDUTY_INTEGRATION_KEY`". That activation never completed.
- **The warning tier reaches no one.** The route tree sends `severity="warning"` and `severity="info"` to `webhook-noop`, and only `critical` to the PagerDuty fan-out. Warnings can fire for weeks without anyone ignoring them — nobody is ever told.
- **ADR 1059 already did the hard reasoning.** It rejected ntfy-on-the-site-host because it "dies *with* the site host: it cannot alert on its own host's death", and named **ntfy on a relay VPS (external failure domain)** as "the ideal sovereign answer" and "the recommended next sovereign increment", deferred only for want of the server and bridge. That reasoning does not change because the vendor did.
- **Slack and email are not pagers.** ADR 1059's own words: no escalation, no on-call rotation, no phone/SMS/voice fallback. Both remain useful for the warning tier and neither substitutes for the critical one. SES is already provisioned per goatnet (per-goatnet Terraform and SMTP credentials) and Ory Courier uses it, so email is available cheaply — as a complement.
- **Two constraints the operator added, which the shape has to carry.** A PLATFORM-ADMIN may be **federated in from a partner org**, so enrolment cannot assume a locally-minted identity. And a goatnet has **many host nodes that must tolerate partition** — a paging design that is correct only while every site can see every other site is not a paging design.

## Decision

### 1. The pager runs on the relay tier, never on a host node

ntfy, self-hosted, on relay VPSes. Relays are the fleet's "non-building failure domain": they are not mesh peers, they sit in cloud regions independent of any host site, and they already have deploy tooling (`ops/relay/deploy-relay.sh`, `relay-server.yml`).

**No paging component runs on any host-site node.** That is the whole property ADR 1059 identified and it is the one that is easy to lose later for convenience.

### 2. At least two ntfy instances, in different regions, and every alert goes to both

A single pager instance reintroduces the problem one layer out. Alertmanager posts each notification to every configured instance.

**Duplicate pages are accepted; a missed page is not.** This is a deliberate trade and the reason it is recorded here rather than left to whoever configures it: the obvious alternative — electing one node or one instance to send — goes silent exactly when the elected one is the partitioned or dead one. A pager optimised for tidiness fails closed in the only situation it exists for.

### 3. Each site's Alertmanager posts independently

No cross-site election, no shared queue, no gossip dependency for the send path. When a site is partitioned from the rest of the goatnet it still reaches the relay tier and still pages.

The cost is duplicate notifications when several sites observe the same fleet-wide condition. That cost is bounded by Alertmanager's existing `group_by` and by ntfy's own delivery behaviour, and it is the correct direction to err.

### 4. Paging topics are derived from the goatnet and the tier — never from a person

Topics are named for the goatnet and severity, e.g. `goat/<goatnet>/critical` and `goat/<goatnet>/warning`. A person subscribes their own device to a topic through the ntfy app.

**No phone number, address or personal identifier enters the repository at any point.** This is not incidental — it is what makes automatic enrolment possible at all. `ops/ci/check-no-identity-spillage.sh` refuses operator-identity material in committed files, and the vendor-manifest discipline already forbids person names in `ops/operators/`. A topic name derived from a handle carries none of that.

### 5. Being granted PLATFORM-ADMIN is what enrols you

Paging enrolment hooks into the tier grant in `ops/ansible/operator-tier-onboard.yml`, not into a separate remembered step. Granting the tier provisions the operator's ntfy credential and grants it read on that goatnet's paging topics; the onboarding bundle carries the topic name and how to subscribe.

**Enrolment keys off the tier the registry asserts, not off where the identity came from.** A PLATFORM-ADMIN federated in through external ICAM is enrolled by the same path as a locally-minted one, because the hook sits downstream of identity origin. Affiliation stays identity-only and never becomes a paging group — the same rule `identity-grammar.md` sets for NetBird groups and the Zenoh write anchor, for the same reason.

### 6. Offboarding reverses it in the same path

Revoking the tier revokes the ntfy grant. ADR 1031 makes revocation mesh-first and reversible; paging enrolment inherits that rather than becoming a second thing to remember. An ex-administrator who still receives pages is both a leak and a false signal of coverage.

## What this does NOT decide

Stated so a reader does not infer more than was settled:

- **Whether the sandbox pages humans at all.** A sandbox goatnet's warning tier can carry a long backlog and its restarts include deliberate experiments. Whether a sandbox pages the roster, or records to a low-urgency destination while production pages, is an operator policy call and is not made here.
- **The escalation model.** ntfy delivers to subscribed devices; it has no on-call rotation or automatic escalation. Whether the fleet needs those, and from what, is unsettled. This ADR chooses the transport and the enrolment path, not a rota.
- **Whether ntfy is the final answer.** The template's peer fan-out means the vendor is a `group_vars` edit. If a paid pager returns, it composes rather than replaces.

## Alternatives considered

- **Wait for a paid PagerDuty account.** Rejected: the gap is measured, and a fleet without a pager silently discards its warning tier. The decision to have a pager should not be blocked on procurement.
- **Email via SES as the pager.** Available today and genuinely cheap — the identity is provisioned and Ory Courier already sends through it. Rejected as the *critical-tier* answer for ADR 1059's reason: it is not a pager. Retained as a warning-tier complement.
- **ntfy on a site host.** Rejected, again, and recorded because it is the tempting shortcut: it is the one deployment that cannot report the failure that motivated having a pager.
- **A single elected pager node per goatnet.** Rejected under decision 2 — clean while everything is healthy, silent under exactly the partition it needs to survive.
- **Enrolment as a documented manual step.** Rejected: a remembered step is the one that gets skipped, and an unenrolled administrator is never told.

## Consequences

- **A pager exists that survives the failure domain it watches, with no third-party account and no egress of alert metadata off our own infrastructure** — which also retires ADR 1059's recorded sovereignty delta rather than merely re-scoping it.
- **Administrator and on-call become the same act.** The population that can break the goatnet is the population that hears about it, by construction.
- **Duplicate pages are a normal condition, not a defect.** Anyone tuning this later needs to know the duplication is bought deliberately and what it was bought with.
- **New operational surface.** Two ntfy instances and a bridge are things that can themselves fail, and the monitor-the-monitor question ADR 1059 raised applies to them: something must notice when the pager stops. A dead-man path is required and is not designed here.
