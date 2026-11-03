# goat CLI — end-to-end testing strategy

Status: in progress (T0/T1 built; T1s built 2026-10-29; T2 built 2026-10-29; T3 built 2026-10-29 with the fabric cases waiting on an e2e slot certificate). Covers `goat-cli.md` ([ADR 1019](../adr/1019-goat-cli-unified-operator-binary.md)), the onboarding surface ([`goat-cli-onboarding-and-delight.md`](goat-cli-onboarding-and-delight.md)), and the i18n surface ([`goat-cli-i18n.md`](goat-cli-i18n.md), ADR 1024). T1s, T2 and T3 run inside the shared deployment e2e harness as its goat suite: [`deployment-e2e.md`](deployment-e2e.md) §Suites (ADR 1200).

## Why

goat is the operator's first and most-used contact with the fabric, distributed as a binary to on-mesh vendors. A regression in `goat hello`/`doctor`/`status` or a broken `--json` contract is a fleet-wide papercut. The harness must catch (a) functional regressions, (b) the machine-surface contract (`--json`/exit-code stability), (c) the "Three Rules of goat delight" guardrails, (d) i18n contract-token stability, and (e) that the *served* binary actually works — not just the one CI built.

## The tiers

### T0 — Unit (`#[test]` in-crate) — **built**
Pure functions, parsers, the i18n render boundary, the sandbox simulator, tutor progress. Fast, no I/O. Gate: `cargo test -p goat-cli` in `goat-cli-pr.yml`.

### T1 — Golden CLI (`trycmd`, `tests/cmd/*.toml`) — **built, now extended**
Drives the *built binary* end-to-end against expected stdout/stderr/exit. trycmd runs **non-TTY**, which is a feature: it naturally exercises the machine-safe path, so a golden that shows *no* flourish but *does* show the SANDBOX banner is itself a guardrail assertion.

The **in-process sandbox simulator is the keystone**: because `goat hello`/`init`/`tutor` round-trip with no network and no profile, the entire onboarding flow is deterministically golden-testable in CI with zero infrastructure. Coverage: `hello` (sandbox pretty + `--json` purity), `init --sandbox`, `tutor --list` + the non-TTY outline (never-blocks guardrail), and the hidden eggs (`goat goat`/`xyzzy`/`brew`). Variable fields (`elapsed_ms`, version) are `[..]`-masked. Existing cases cover the error paths + the i18n error-render + machine-translation notice (`doctor_no_profile_uk`, `doctor_machine_translation_warning`).

Regenerate after an intentional output change: `TRYCMD=overwrite cargo test --test cli_golden`, then re-apply `[..]` masks to any varying field.

**T1 limits:** the localized *runtime* summaries of `status`/`inspect` only render with a live profile + reachable mesh, so their i18n is a T3 concern; trycmd can't supply a TTY, so the flourish-present (🐐) path and `tutor`'s interactive walkthrough are unit-asserted, not golden-asserted.

### T1s — Local stack — **built** (2026-10-29)
The real binary against real services on one machine, every case read back from the service's own record. `ops/e2e/suites/goat/stack.sh` starts the real portal and onboarding console (SQLite), the console's identity stand-in, and — with Docker — the Zenoh 1.9.0 router with an mTLS listener; `run.sh` runs the cases (profile and context state, the portal and console contracts, invite and operator-key lifecycles, `--json` shapes, a stopped service, the sandbox round trip, and with the router the mTLS session and a publish received). CI job `goat-suite` in `deploy-e2e.yml`, on every pull request touching goat, panoscope-core, the portal or the console. First run on a laptop without Docker: 26 pass, 3 expected failures (the two router cases, and the open contract item below), and it found two defects, both fixed in the same change: the portal answered a topic under an unknown org with a 500 (now a 404 naming the org), and the client dropped the portal's problem-JSON `detail` line.

### T2 — Deployed-binary smoke — **built** (2026-10-29)
`ops/e2e/suites/goat/live.sh`, from a machine on the goatnet's mesh: downloads `goat` and `goatbuilder` **from `get.<site>`**, checks each against the `SHA256SUMS` served beside it, requires `--version` to equal the site's `version.json`, and runs `goat hello --sandbox --json` on the served binary. First run on the dev goatnet: all five rows pass (goat 0.5.13, goatbuilder 0.1.0). Not yet wired into `goat-get-deploy.yml` as a post-deploy step; that is the next increment.

### T3 — Real-fabric live-fire — **built** (2026-10-29)
The same script, with the served linux binary run on the goatnet's management host against the context `ops/validation/goatnet-runners.yaml` names (`goat_via`): `doctor --json --all` (router TCP, mTLS session, portal), `org ls`, one org's `topic ls --json`, `admin queue ls --json`, a real `hello` round trip through the fabric, and the synthetic intake round trip (`onboard-intake-smoke.sh`: invite, browser submit, reject). First run on the dev goatnet: 11 pass; the mTLS session and the real round trip are recorded as expected failures because that context is portal-only — issuing the e2e runner a slot bundle turns them into real cases.

## Portal answers are not "unreachable"

A status the portal answered has its own code and exit code (#2087): 404 → `E_PORTAL_NOT_FOUND` (7), 429 → `E_PORTAL_RATE_LIMITED` (8, with `Retry-After`), 5xx → `E_PORTAL_SERVER_ERROR` (9), any other uncurated status → `E_PORTAL_REFUSED` (4); `E_PORTAL_UNREACHABLE` (4) is a network fault only. Locked by the T1s cases `topic-under-unknown-org-names-it` and `portal-answer-is-not-called-unreachable`, by `crates/goat-cli/tests/portal_answers.rs` (each status against a stub portal through the built binary), and by the classification unit tests in `crates/goat-cli/src/errors.rs`.

## Conventions
- One `.toml` per behavior; comment the *invariant* it locks, not just the command.
- Mask only genuinely-variable fields (`elapsed_ms`, timestamps, version, temp paths) with `[..]`; never mask contract tokens.
- A new operator-facing surface ships with its T1 goldens in the same PR, and a T1s case when it reads or writes a service.
- The `--json`/exit-code/contract surface is sacred: any case that touches it asserts byte-stability.
- A T1s or T3 case asserts on the object the service stored, not on goat's output alone.

## Status map
- T0 built · T1 built + extended · T1s built (local stack, CI `goat-suite`) · T2 built (`live.sh`; post-deploy wiring next) · T3 built (fabric cases wait on an e2e slot certificate).
