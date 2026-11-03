# Partner onboarding — the track, and where it is documented

| Field | Value |
|---|---|
| Status | Built (guide partner view + partner-data topic + tool surfacing, 2026-08-11); live when the console pin carrying it is deployed to both goatnets |
| Audience | Internal — author / reviewer / admins who onboard partner-org people. The partner-facing surface is the mesh-hosted guide itself: `guide.<site>.<zone>/public/onboarding-guide?role=partner` |
| Anchors | [three-stage-candidate-onboarding.md](three-stage-candidate-onboarding.md) (ADR 1062); [partner-data-exchange.md](partner-data-exchange.md) (ADR 1027); partner-read-partition.md; machine-enrollment.md (ADR 1096); identity-grammar.md (ADR 1043); partner-standalone-goatnet.md (the three on-ramps) |
| Companion | `ops/goat-id-console/internal/welcome/` — `guide_model.go` (partner role view), `guide_topics.go` (`partner-data` topic) |

## Why a separate track

A partner-organisation member is onboarded by the same machinery as everyone
else — that is the point, and this track adds **no new machinery**. What
partners need is separate **documentation**, because their concerns differ in
one dimension the tier paths never cover: what happens where data crosses
organisation lines. The release-lane model, the write-grant model, the
partition honesty rule, and the machine-custody path are all invisible in the
browse/developer/admin views, and a partner reading only their tier's path
would first meet them as unexplained refusals.

The identity grammar makes the shape of the fix non-negotiable: **partner is
affiliation, not a tier** (ADR 1043 — affiliation is identity-only, never a
NetBird group, never in the write anchor). So the partner documentation is an
*overlay view*, not a sixth tier. The guide's `partner` role view follows the
network-admin precedent — a real situation deserves one page about itself —
and its own first steps say "your access level still decides what you may do;
read that path too."

## What the partner view documents (and its ground truth)

| Concern | What the guide says | Enforced today? |
|---|---|---|
| Person onboarding | Sponsor invites → candidate submits own record at intake → admin approves + mints | Live on ZAGROS (ADR 1062, proven 2026-07-01); partners are the motivating persona |
| Own namespace | Single-writer `<uuid>/**` root, no second-writer grant exists | Live — wire ACL, both goatnets |
| Release lanes | Data crosses orgs via `release/<set>/**`; set membership IS the audience (one member = bilateral, broad set = release-to-all) | Read + write-grant halves live-fired on ZAGROS; registry population is per-partner operator work |
| Lane writes | Granted per-producer by admins, fail-closed, silent refusal | Live where granted (`zenoh_release_writers`) |
| Partition honesty | "One room until it is carved" — read separation is applied **per-anchor by operators**, never switched on by being a partner; raise sensitivity concerns BEFORE publishing | Deliberate. The ADR 1073 §3.4 tripwire requires external descriptions to say the goatnet boundary is the machine-plane trust boundary until M1/M2 are live |
| Org sharing authority | Org's nominated admins hold it; in-org co-publish is opt-in; cross-org is always lanes | Built (ADR 1102/1105); co-publish default-off |
| Machines | Custodian self-service, quota default 0, external dual-path gear gets `EXTERNAL-*` classes with mgmt-plane-only reach | Live (ADR 1096; ADR 1116 phases 1/3/5) |
| Encrypted audiences | Grant-keyed streams decrypt member-side; browser surfaces mediate for cert-less staff | SDK frozen (ADR 1106); mediated read live in panoscope |

The guide text was written against these rows and must not drift ahead of
them; `TestGuide_partnerViewSeparatesAffiliationFromTier` pins the honesty
statements (lanes are granted; separation is not automatic).

## How a partner finds the view — the welcome email routes them

The welcome email links the guide at the reader's tier (`?tier=…`), and for a
recipient whose manifest carries `affiliation.kind: partner` it adds a second
pointer to `?role=partner` — additive, never a replacement: the tier link
stays primary because the partner view is an overlay, and the extra paragraph
says why the second read matters (data across organisation lines). Threaded
manifest → `Recipient.AffiliationKind` → render
(`ops/goat-id-console/internal/welcome/welcome.go`); display-only routing —
affiliation still gates nothing, and a blank or non-partner kind renders the
ordinary email. Pinned by `TestRender_partnerOrgGetsThePartnerViewPointer`
(no other kind, blank included, ever gets the pointer).

## Partners behind a corporate firewall

A partner joining from a corporate network or cloud VPC usually has to request
egress rules first. The guide's `?topic=firewall` page (linked from every role,
`partner` included) prints that goatnet's management and relay hostnames, their
resolved public addresses and the ports; the canonical write-up, including why a
client X.509 certificate does not substitute for an IP allowlist, is
[corporate-firewall-egress](../operations/corporate-firewall-egress.md).

## Tool surfacing shipped with this track

The same change closed the guide's tool gaps for **all** roles — surfaces that
existed on both goatnets but appeared in no onboarding surface:

- **Microscope** (per-entity lens) and **Grafana** — new `surfaces` sections,
  admin-audience-tagged, derived from the portal base like their siblings.
- **Identity admin console** — now named and linked as its own surface rather
  than implied by two deep links.
- **Geographic map** (goat-map) — configured, never derived (`NOTIFY_MAP_URL`,
  the relay-health pattern), because only goatnets carrying a geographic feed
  have one; blank renders "not configured on this
  goatnet". `TestGuide_mapSurfaceIsConfiguredNotDerived` pins this.
- **CLI lenses** — `goat top` / `goat who` / `goat flow` in the `cli` topic,
  with the update-from-get-page recourse for older binaries.
- **Sandbox** — its section now carries the link (the resolver existed unused).

Not surfaced, deliberately: netvis (no code), goatdrop (release pending),
Matrix (proposed), trashgoat (live-fire pending). The guide points only at
what a reader can open today.

## Credential-delivery copy follows the goatnet, not the author

How a person gets their fabric credential is per-goatnet config
(`onboard_runner_defer_credential`), and the guide now branches on it:
mint-on-collect goatnets get "you mint the portal key + bundle yourself, once,
on /fabric-credential"; provisioning-mint goatnets get "your key is printed in
the welcome email, the bundle is the one-time link in it". Both the developer
path step and the `fabric` deep dive resolve from the same flag at render time,
so they cannot disagree.

This exists because the unconditional copy was wrong on whichever goatnet it
was not written for — twice, in opposite directions. Before mint-on-collect the
step said the bundle "never travels by email" beside a deadline card naming the
emailed link; after it, zagros-prod (which mints at provisioning and prints the
key in the email) told readers their credentials "do not travel by email",
verified live 2026-08-12. The console reads the flag from
`NOTIFY_MINT_ON_COLLECT`, templated from the same group_var that drives the
runner — one fact, one home. `TestGuide_credentialFlowFollowsTheGoatnet` pins
both directions and pins that the step and the deep dive always agree.

### Deploying a console bump that adds config

A console-only `docker compose up -d` swaps the image and nothing else — it
does **not** re-template `/opt/mgmt-host/.env`. A bump that introduces new
`NOTIFY_*` vars therefore comes up on the right image reading them as unset,
which for this change meant zagros briefly served the *emailed* flow (its own
flag never arrived) with the map still unconfigured, on the very build that
fixed both. Use `id-admin-console-deploy.yml -e target=<group>` for anything
that adds or changes config; it re-templates the env and does not run the
admit-audit role, so it does not carry the F-312 traefik vector. Verified live
2026-08-12: traefik untouched on both goatnets, LE certs unchanged.

## Gaps this track does not close

- **Partner-peer now has a pre-enrollment packet** —
  [`docs/partner-templates/partner-peer-onboarding-packet.md`](https://github.com/usareur-af/goatdocs/blob/main/docs/partner-templates/partner-peer-onboarding-packet.md)
  (2026-08-11), written sanitization-safe: no internal hostnames, operator
  names, or goatnet-specific values (the host contact fills those in at
  handoff), so it does not wait on the Block 30 sweep.

  SCOPE NOTE, because this list drifted once and cost a reader an
  explanation they did not ask for: this track is about how a PERSON is
  onboarded onto an existing goatnet — credentials, the guide, the surfaces,
  the welcome email. How an ORGANISATION attaches infrastructure (partner
  site, standalone goatnet) is a different subject with its own doc and its
  own on-ramp table in
  partner-standalone-goatnet.md. Gaps there
  are recorded there; importing them here made a finished track read as
  unfinished.
- **Upstream-OIDC federation** (partner enterprise IdP) awaits RP
  registration; CAC ingress modes are built and live-fired but deployed to no
  goatnet ([external-icam-federation.md](external-icam-federation.md)).
