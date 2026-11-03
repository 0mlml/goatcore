# ADR 1024 — goat-cli internationalization: English-arbiter catalogs, locale-invariant machine surface

**Status:** Proposed
**Date:** 2026-06-04
**Repo scope:** goat core (`crates/goat-cli`)
**Related:** [ADR 1019](1019-goat-cli-unified-operator-binary.md) (goat-cli as the operator surface) · [ADR 0112](0112-tech-strategy-workload-shape-language-boundary.md) (language-boundary discipline) · design `goat-cli.md` §Conventions item 10 (operator-facing help text) · `edge-attachment-growth-strategy.md` item D (i18n of partner-facing surfaces)

## TL;DR (for a layperson)

Our operator command-line tool speaks only English today. Partners we want to onboard don't all work in English, and a tool whose prompts and error messages they can't read is a tool they won't adopt. So we will let the tool show its human-readable text — help, error explanations, status labels — in the operator's own language, picked with a simple flag or their computer's language setting, defaulting to English. The catch in a tool that drives real infrastructure: anything a *script* reads (machine-readable output, error codes, command and flag names) must stay identical in every language, or people's automation breaks the moment they switch language. So we draw a hard line — translate the prose a human reads, never the structured data a script reads. English stays the master copy: if a phrase hasn't been translated yet, the tool falls back to English rather than showing a blank. For the riskiest moments — error messages, "are you sure?" confirmations — it shows the English right next to the translation, so the operator can search it, ask an English-speaking colleague, or catch a bad translation. The translations are built into the program, so it still works on a disconnected, air-gapped site with no internet, and we build it on the standard off-the-shelf tooling rather than inventing our own.

## Context

[ADR 1019](1019-goat-cli-unified-operator-binary.md) established `goat` as the everyday-operator surface — profile/doctor/topic/pub-sub/inspect/admin — and the tool we put in front of partners and vendors. The edge-attachment growth strategy names internationalization of partner-facing surfaces (item D) as a cheap, additive adoption lever: a partner operating in their own language is a partner who can self-serve. NATO works in English as its lingua franca (English + French are its two official languages), but member-nation and partner operators do not uniformly read English, and an unreadable diagnostic at the edge is an operational failure, not a cosmetic one.

`goat` today hardcodes every user-facing string in English at the call site. The recent operator-facing help-text pass (`goat-cli.md` §Conventions item 10) already disciplined those strings — tight summaries, no internal scaffolding — which is precisely the corpus that would become a translation catalog. Nothing else about the tool is localized.

Two things make CLI i18n different from web-UI i18n and easy to get dangerously wrong:

1. **The tool has a machine-readable contract.** `--json` payloads, the stable error-code namespace (`E_PROFILE_MISSING` etc.), completion candidates, Zenoh key expressions, and the command/flag *names* themselves are consumed by scripts, CI, `trycmd` goldens, and `--jq` pipelines. If any of those shift with locale, every downstream automation breaks when an operator changes language — a silent, locale-dependent failure.

2. **Mistranslation in an infrastructure tool has teeth.** A garbled remediation hint or a softened destructive-action confirmation is worse than English the operator must look up. The canonical, binding meaning has to live in exactly one language.

There is also a naming collision to head off: **polygloat** (`edge-attachment-growth-strategy.md` §polygloat) translates *data formats* between vendor schemas and the canonical pivot. It has nothing to do with human-language UI text. This ADR is about the latter; the two never share machinery.

## Decision

**Localize only the human-facing prose of `goat`, behind an English arbiter, with the machine-readable surface held locale-invariant. ADR-first; no code in this PR.**

1. **English is the default and the arbiter.** The English catalog is the canonical source of truth, extracted from today's hardcoded strings. Every other language is a layer over it. A missing or untranslated key falls back to English — never a blank, never a key name. A language may ship partial and still be safe to enable.

2. **A hard line between prose and contract.** Localize: command/flag *help* text, error *descriptions*, `doctor` remediation prose, `status`/`inspect` human labels, narrator cascade lines, confirmation prompts. Never localize: `--json` field names or values, error *codes* (the `E_*` identifiers), completion candidates, command/subcommand/flag *names*, key expressions, exit codes. The scriptable contract is locale-invariant by rule, enforced in CI. (`--json` therefore stays byte-stable across locales; `trycmd` goldens that assert structured output are unaffected.) **The boundary is by string role, not by surface:** the TUI ([`goat-cli-tui.md`](../design/goat-cli-tui.md) — Diagnose/Tail lens labels, picker columns, keystroke hints) localizes through the *same* catalog as the CLI, because the item-4 stack covers every owned string in the binary regardless of where it renders. There is no separate TUI i18n mechanism; TUI prose is prose, TUI-displayed key expressions / IDs are contract.

3. **Selection precedence mirrors the existing profile/context model:** `--lang <code>` flag → `GOAT_LANG` env → OS locale (`LC_ALL`/`LC_MESSAGES`/`LANG`) → English default. One global flag, resolved once, same shape operators already know from `--context` / `GOAT_CONTEXT`.

4. **Catalogs are compiled in, not fetched — using the standard Rust localization stack, not a bespoke one.** Translations are embedded in the binary as **[Fluent](https://projectfluent.org) (`.ftl`)** message catalogs (locale-aware plurals/interpolation) via the established `i18n-embed` + `fluent` + `unic-langid` + `rust-embed` toolchain, with `cargo-i18n` for catalog extraction and coverage reporting. This is the conventional choice for a self-contained localized Rust binary; we are not inventing machinery. It gives us, off the shelf: OS-locale detection (`i18n-embed`'s `DesktopLanguageRequester`, which the precedence in item 3 layers `--lang`/`GOAT_LANG` on top of), per-locale completeness reporting (provenance/staleness in item 6), and compile-time embedding. No runtime translation service, no network egress: the tool localizes identically on an air-gapped island site as on a connected laptop, deterministically.

5. **A tiered, demand-gated language catalog — not "all NATO languages" day one.** Tier 0: English, canonical, always complete. Tier 1: a small seed set proving the pipeline end-to-end (a member-nation language + a partner-nation language). Tier 2: remaining member-nation and partner languages added per real demand, each its own additive change. The architecture supports any locale; the *catalog* grows with adoption rather than front-loading two dozen speculative translations that rot untested.

6. **English stays binding for operationally-critical text.** Destructive-action confirmations and security-relevant instructions present the English canonical as authoritative (the localized text aids comprehension; the English is the contract). Translation provenance is tracked per catalog so a stale or unreviewed translation is visible, not silently trusted.

7. **Bilingual output (localized + English side-by-side) — default-on for the high-stakes subset, opt-in globally; never everywhere by default.** Showing the English canonical *alongside* the localized prose makes item 6's "English is authoritative" real on the screen (authoritative-but-hidden text isn't authoritative to the operator reading it), lets a stuck operator search/ticket/relay the English to English-speaking support, and lets them catch a mistranslation. This is nearly free with the item-4 stack: English is the always-present arbiter/fallback catalog, so the English rendering is a second lookup of the same key, no new machinery. Scoping:
   - **Default-on (bilingual):** errors (localized description + English description + the already-invariant `E_*` code), destructive/security confirmation prompts, `doctor` remediation. The "get help / don't get it wrong" surfaces.
   - **Opt-in global:** `--bilingual` flag / `GOAT_BILINGUAL` env renders English alongside *all* localized prose, for mixed teams or an operator learning the tool.
   - **Bounded to prose.** Side-by-side applies only to the localizable prose column; the machine-contract strings (item 2) are already English-only and invariant — there is nothing to double there.
   - **Not everywhere by default.** For the primary persona — a non-English operator — English stacked under every help line and status label is unreadable noise that doubles output volume. The render style (dimmed gloss vs. a parenthetical `(en: …)` line) is a design-doc detail, not pinned here.

This is the right shape because it (a) unlocks the partner-adoption lever the growth strategy names, (b) refuses the one move that would break every operator's automation — localizing the machine surface, (c) needs no network and works air-gapped, and (d) builds directly on the help-text corpus the recent §Conventions-item-10 pass already disciplined.

## Alternatives considered

- **Stay English-only.** Rejected — leaves the growth-strategy adoption lever on the table; an unreadable edge diagnostic is an operational failure for a non-English operator, not a cosmetic gap.
- **Localize everything, including `--json` / error codes / flag names.** Rejected — turns the scriptable contract locale-dependent; an operator switching language silently breaks their own automation, CI, and `--jq` pipelines. The machine surface must be invariant.
- **Runtime machine translation (call a translation API).** Rejected — island/air-gapped sites have no egress; introduces latency, a runtime dependency, and unreviewed mistranslation into a security tool. Catalogs are compiled in instead.
- **Reuse polygloat.** Rejected — category error. polygloat translates data formats between schemas; this is human UI language. No shared machinery.
- **Per-language binary builds.** Rejected — explodes the release matrix and forces operators to fetch the right artifact. One binary, language resolved at runtime.
- **Translate into every NATO-member language up front.** Deferred, not rejected — the architecture supports it, but front-loading ~two dozen untested catalogs rots them; languages are added demand-gated behind the English fallback.
- **Bilingual (English-alongside) output everywhere by default.** Rejected — for the non-English primary persona, English stacked under every help line and label is unreadable noise and doubles output volume. Bilingual is default-on only for the high-stakes subset and opt-in globally (decision 7).
- **A bespoke i18n layer (hand-rolled catalog loader / lookup).** Rejected — self-contained localized Rust binaries are a solved problem; `i18n-embed` + `fluent` is the conventional stack and gives OS-locale detection, compile-time embedding, and coverage reporting for free (decision 4).

## Consequences

- Every new user-facing string routes through the catalog with an English entry; a CI lint rejects new hardcoded human-facing strings and keys missing from English (the same discipline §Conventions item 10 began, now enforced).
- Partial translations are safe to ship — English fallback guarantees no blank or key-leaking output — so a language can land incrementally.
- The machine surface (`--json`, error codes, names, completions) is unchanged and locale-stable; existing scripts and `trycmd` goldens are unaffected by design.
- A small per-locale catalog adds to binary size and a string-lookup indirection on the human-output path only (negligible; the hot scriptable paths don't touch it).
- clap's own structural chrome (`Usage:`, `Options:`, `error:`) is English unless/where clap's own localization permits; our prose localizes regardless. Scope is bounded to strings we own.
- Establishes the **English-arbiter + invariant-contract** pattern as the reusable precedent for the other partner-facing surfaces the growth strategy names (moon-pod web UI, goat-client, portal) — this ADR sets it on the CLI first.
- Bilingual output costs nothing structurally: the English fallback catalog the design already requires *is* the side-by-side source, so the high-stakes-subset rendering is a second lookup, not a second catalog.
- Adopting the standard `i18n-embed`/`fluent` stack (vs. a hand-rolled layer) means OS-locale detection, embedding, and coverage reporting are library-provided — the work narrows to externalizing strings and writing the project-specific contract-boundary list + CI guard.

## Implementation status

- 🔲 **String externalization** — extract the (already-disciplined) hardcoded human-facing strings in `crates/goat-cli` into an English canonical catalog; leave the machine surface untouched.
- 🔲 **Selection + fallback** — `--lang` global flag + `GOAT_LANG` + OS-locale precedence; English fallback for any missing key; one resolved `Locale` on the output context.
- 🔲 **CI guard** — lint rejecting new hardcoded user-facing strings and non-English-complete keys; assert `--json`/error-code/`trycmd` stability across a non-English locale.
- 🔲 **Bilingual output** — localized + English side-by-side; default-on for errors / destructive-and-security confirmations / `doctor` remediation, opt-in globally via `--bilingual` / `GOAT_BILINGUAL`; prose-only.
- 🔲 **Tier-1 seed catalog** — one member-nation language + one partner-nation language, proving the pipeline end-to-end, with translation provenance recorded.
- ✅ **Design doc** — [`docs/design/goat-cli-i18n.md`](../design/goat-cli-i18n.md) (authored 2026-06-07) capturing the pinned crate stack (i18n-embed + fluent + unic-langid + rust-embed + cargo-i18n), catalog layout, the enumerated prose-vs-contract boundary list, language selection + fallback, the `(en: …)`-line bilingual render style, the provenance/trust model, and the CI guard. Names the error-render path (`errors.rs`, human-only) as the clean externalization proof-of-concept and `goat doctor` as the second, render-boundary-aware conversion (its `summary`/`hint` are entangled with `--json`).

## Addendum (2026-06-08) — machine-translation warning + full NATO-member catalog set

Two refinements landed alongside the first implementation:

- **Machine-translation warning + parameterized feedback (strengthens decision 6).** When a non-English, non-`native-reviewed` locale is active, the tool emits a stderr notice that the output is machine-translated, that the English is authoritative, and where to report errors. The per-language review status and the feedback channels (a shared partner email + a private partner repo) are parameterized in `crates/goat-cli/i18n-provenance.toml` and overridable at runtime via `GOAT_I18N_FEEDBACK_EMAIL` / `GOAT_I18N_FEEDBACK_REPO`, suppressible via `GOAT_I18N_NO_WARN`. Stderr-only; never touches `--json`.

- **Full NATO-member catalog set ships now (supersedes decision 5's strict demand-gating for the scaffolding tier).** All 28 distinct NATO-member languages ship as complete machine-translated catalogs, each marked `review: machine`. Decision 5 originally deferred the broad set to avoid front-loading untrusted catalogs that rot. That concern is now closed by the warning above rather than by withholding the catalogs: every unreviewed language announces itself and carries a route to fix it, and the English arbiter stays binding. Languages flip to `native-reviewed` per native-speaker review; partner-nation languages beyond NATO membership still arrive per demand.

---

*Decision is ADR-first per [INDEX.md](INDEX.md) authoring discipline; flips to Accepted in the PR that lands the externalization + selection + CI guard.*

## Addendum (2026-11-02): translations no longer gate a change or a release

Decision 2's CI enforcement stays for the line between prose and contract. Missing or out-of-date translations no longer fail anything: `ops/scripts/translate-ftl.py --check` reports them and exits 0, and a locale that lacks a message renders it in English. `--apply` fills the gaps when someone chooses to run it. The operator's reason: "it is takign up too much bandwidth and we have too much to get done". See [goat-cli-i18n.md](../design/goat-cli-i18n.md) §"The currency gate reports and does not fail".
