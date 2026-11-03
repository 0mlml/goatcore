# Onboarding-email formula — one shape that subsumes welcome, upgrade, and retroactive notice

**Status:** Reference pipeline live; Go onboarding-engine adoption is the named follow-on. Related: operator onboarding, the console welcome renderer (`ops/goat-id-console/internal/welcome/`), relay-health-dashboard.md.

## The idea

Every email the system sends to a *person about their standing* is the **same email**. Initial onboarding, a permission upgrade, and a retroactive notice (a new feature, a migration, an update) differ only in two inputs — not in structure:

- **prior-state** — brand-new recipient (welcome; everything is "to do") vs already-active (acknowledge what they already have, then what changed).
- **reason block** — the pluggable "what's new": *all-steps* (first onboard) · *permission-diff* (upgrade) · *feature / migration / update* (notice).

So we do not add a third email type beside "welcome" and "upgrade." The formula **subsumes** all three. The console's `welcome.render(cat, r, a, up *Upgrade)` already carries the bones (a welcome that optionally folds in an `Upgrade` diff); the durable form promotes `reason` to a first-class value so welcome / upgrade / notice collapse into one code path.

## Fixed 4-part shape

1. **Acknowledgement** — their prior state, customized to their role. (For a brand-new onboardee this is the welcome greeting; for an active member it is "you're already active as `<role>`.")
2. **Recap** — what they already have, role-driven. (Empty for a first-time onboardee.)
3. **What's new** — the reason block. The *only* campaign-specific input.
4. **Why it matters** — short, with a role-aware line where relevant.

## Environment identity — every email says which goatnet, and what it's for

A person can hold access to more than one goatnet (ZAGROS now, zagros-prod later; a low-risk sandbox goatnet is likely), so every email about their standing states **which environment it is about and what that environment is for** — not just a name. Two per-goatnet console config values (`NOTIFY_GOATNET_TAGLINE` + `NOTIFY_GOATNET_PURPOSE`, from `id_admin_console_notify_goatnet_*` group_vars):

- **Tagline** — short descriptor in parentheses after the goatnet name in subjects and titles, via `welcome.DisplayLabel`: "Welcome to ZAGROS (integration gateway) — …". This is the subject-line disambiguator: "ZAGROS" vs "ZAGROS-PROD" alone is exactly the collision it prevents.
- **Purpose** — a one-to-three-sentence paragraph rendered as an "About this environment" block at the top of the welcome/upgrade/reference package (before any instruction), in the invite email, and in the stage-notification "onboarded" email.

Both blank → name-only rendering (the pre-existing behavior); the block is never fabricated. ZAGROS's values live in `group_vars/zagros_hosts/vars.yml`; every new goatnet sets its own pair at standup (goatnet-turnkey-standup.md cross-cutting requirements).

## Determinism

Identical `(roster row, reason)` → identical bytes. No randomness, no time-of-render values in the body. This is what makes a batch re-runnable and auditable: you can render the whole roster, diff it, review it, and send, and a re-render produces the same emails. Customization is a pure function of the person's **role** (`traits.groups`) and deterministic attributes (affiliation/domain, onboarding tier) — never a heuristic.

## Role-driven customization (v1 vocabulary)

Acknowledgement + recap resolve from `traits.groups`:

| Group | Reads as | Recap surfaces |
|---|---|---|
| PLATFORM-ADMIN | a platform administrator | admin console, NetBird dashboard, portal, observability |
| IAM-ADMIN | an identity & access administrator | admin console, identity administration, access surfaces |
| COMPLIANCE | a compliance reviewer | compliance dashboard |
| OBS-ADMIN / GRAFANA-ADMIN | an observability administrator | Grafana + observability |
| GRAFANA-READONLY / OBS-READONLY | an observability viewer | read access to observability |
| DEVELOPER | a developer | mesh, data fabric, developer surfaces |
| data.view.* | a data-access grantee | granted data views |
| (none) | an onboarded participant | base (mesh + sign-on) |

Multiple groups resolve to the highest-privilege label deterministically. An `admin` flag (PLATFORM/IAM/OBS-ADMIN, GRAFANA-ADMIN, COMPLIANCE) unlocks role-specific "why" lines.

## Presentation — one document, a text part and an HTML part (built 2026-09-08)

Every welcome, reissue and access-change email is sent as `multipart/alternative`: a plain-text part and an HTML part, from one document. The renderer (`ops/goat-id-console/internal/welcome/doc.go`) builds typed blocks — title, heading, paragraph, step, command, credential, link, bullet, note — and emits both presentations from them. The text part is the record and what a text-only client shows; it is byte-for-byte what the renderer produced before the HTML part existed, so every phrase test still holds. The HTML part is what Gmail and Outlook show: headings, numbered steps with each command in a monospace block, credentials in labelled boxes, the deadline section as a callout, the troubleshooting notes as a lettered list after the guide pointer, prose that wraps to the reader's window. Styles are inline because mail clients strip stylesheets. `Mailer.SendMultipart` sends both; `reissue-preview -html` prints the HTML part for review. A test asserts the HTML carries every word of the text, in order.

The steps stay a short list. Every caveat a support case earned (the netbird.io sign-up trap, WSL, the silent "Already connected", which Linux families the installer covers, the spent one-time login link, the portal key's second copy) sits once, lettered, under "IF SOMETHING DOES NOT WORK — notes", and the steps refer to them by letter.

## Deliverability

Reserved-TLD synthetic identities (`.test`, `.example`) are excluded from a real send — they cannot receive mail. The batch reports them explicitly rather than silently dropping (no silent truncation).

## Reference pipeline

`ops/goat-id-console/tools/retro-notice/` — the deterministic renderer + batch driver used for retroactive campaigns. Inputs: the active roster (name / email / `traits.groups`, from Kratos `/admin/identities` filtered to `state=active`) + a `notice.json` (the reason block). Output: per-person `{subject, body}` + a send manifest. Sending is via SES v2. PII (roster, rendered bodies, results) stays out of git.

## The follow-on: future onboardees get this in their initial welcome

Because the formula subsumes onboarding, the durable end-state is the **Go onboarding engine** rendering the same shape at first-onboard, with the relay-dashboard (and any then-current) notice folded in as a reason block on the welcome package — so a new person is told about the dashboard the same way, in the same email, without a separate retroactive campaign. That is the promotion of `reason` to a first-class input in `welcome.render`, tracked as the implementation follow-on.
