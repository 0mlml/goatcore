# goat CLI — onboarding, sandbox, and delight

Status: proposed (design). Builds on `goat-cli.md` ([ADR 1019](../adr/1019-goat-cli-unified-operator-binary.md)) and the i18n surface ([`goat-cli-i18n.md`](goat-cli-i18n.md), ADR 1024).

## Why

Two gaps in `goat` today:

1. **No on-ramp.** A new operator gets a binary and a wall of subcommands. There is `goat doctor` (health) and `goat ui` (inspection), but nothing that *teaches* the fabric or walks a first-timer from "I have the binary" to "I published my first sample and watched it land." Partner/ZAGROS onboarding is exactly the audience that needs this — often non-native speakers (hence the i18n work) on their first contact with the fabric.
2. **Nothing to practice against.** Every learning command needs a real profile + a reachable mesh. You cannot learn `goat` on a plane, in a classroom, or before your bundle is issued.

This design adds a **guided onboarding** that runs entirely through the CLI and works against either a **notional local sandbox** (an in-process simulated goatnet, zero real env) or a **real env** (a normal context), plus a small set of **delight** touches that make the tool a pleasure without ever getting in the way of serious work.

## Design principles — the Three Rules of goat delight

Every fun/onboarding affordance obeys all three (this is the "doesn't get in the way of serious work" contract):

1. **Never on the machine surface.** Nothing fun or pedagogical ever touches `--json`, exit codes, completion candidates, or piped/non-TTY output. Scripts and CI see byte-identical output forever.
2. **Opt-in, or success-only.** Easter eggs and the tutor require an explicit subcommand/flag/locale. The *only* thing allowed to appear unprompted is a flourish on **full success** in pretty mode — which doubles as a useful at-a-glance "all clear."
3. **One kill switch.** `GOAT_NO_FUN=1` (and `--quiet`) silences every flourish at once. Honored everywhere.

A fourth, specific to the sandbox: **the sandbox can never be mistaken for a real mesh.** Every sandbox-mode invocation carries a persistent `sandbox` marker (banner + glyph), the same honesty discipline as the i18n machine-translation notice.

## Inspiration (the classics we borrow from)

| Source | What it nails | goat borrow |
|---|---|---|
| vimtutor | hands-on, self-paced lessons in a scratch space you can't break | `goat tutor` lessons completed by running real commands |
| rustlings | progression of small "make it work" checks; resumable | tutor verifies each step before advancing; saved progress |
| docker `hello-world` | one command proves the whole stack and explains every hop | `goat hello` round-trips a sample and narrates each substrate hop |
| `gcloud init` / `aws configure` / `gh auth login` | guided first-run wizard; detect state; defaults-in-brackets | `goat init` wizard |
| `git hint:` advice | contextual next-step nudges, opt-out | post-command next-step hints |
| `brew doctor` + 🍺 | friendly health check + success flourish | `goat doctor`/`goat status` all-green flourish |
| `sl`, `apt moo`, git easter eggs | joy on purpose, never in the way | hidden ASCII goat; Klingon locale (shipped) |

## Architecture: the `Substrate` seam

The keystone that makes "sandbox or real" work without forking every command.

Today each command builds its own clients (portal `PortalClient`, zenoh session, stethoscope HTTP). We introduce a backend seam — a small set of capability traits the onboarding/learning commands depend on instead of concrete clients:

```
trait Substrate {
    fn portal(&self) -> &dyn PortalLike;      // list/create topics, vendors, health
    fn fabric(&self) -> &dyn FabricLike;      // pub / sub / query
    fn diagnostics(&self) -> &dyn DiagnoseLike; // stethoscope-style verdicts
    fn describe(&self) -> SubstrateKind;       // Real { context } | Sandbox
}
```

Two implementations:

- **`RealSubstrate`** — thin adapters over the existing clients, selected by the active context/profile. No behavioural change to existing commands; they can adopt the seam incrementally.
- **`SandboxSubstrate`** — a self-contained in-process simulator. Holds tiny local state under `$GOAT_SANDBOX_DIR` (default a temp dir): topics you create persist; a value you `pub` is replayed to your `sub`; `doctor`/`status` probes "pass" against simulated endpoints with realistic latencies; `inspect` returns canned-but-plausible verdicts. No network, no profile required.

Mode selection: `--sandbox` (or `GOAT_SANDBOX=1`) forces the sandbox; `--env <context>` / default uses real. The tutor defaults to sandbox; `goat hello`/`goat init` detect and offer.

Scope guard: only the **onboarding/learning** commands (`init`, `hello`, `tutor`) and optionally `doctor`/`status`/`pub`/`sub`/`inspect` when run with `--sandbox` route through the seam. Mutating *production* admin paths are out of scope — you cannot "sandbox" a real onboarding of a real partner.

## The onboarding flow

### `goat init` — first-run wizard
gcloud-init / aws-configure shaped. Detect existing profile/context. Offer: **sandbox** (learn now, no bundle) or **real** (walk `goat profile init <bundle.cbor>`). Run `doctor` against the chosen substrate. Print a friendly "you're on the fabric" summary. Offer to launch `goat tutor`. Idempotent and re-runnable; never clobbers an existing profile without confirmation.

### `goat hello` — the hello-world smoke
docker-hello-world analog and the most demoable first slice. One command: create (or reuse) a `hello/<you>` topic, publish a sample, subscribe and watch it round-trip, then **narrate every substrate hop it just exercised** (portal accept → fabric route → delivery), ending with next-step hints (`try 'goat tutor'`, `try 'goat sub'`). Works in sandbox (default if no profile) or real. Pretty, friendly, suppressible; a `--json` form emits a plain structured proof for CI smoke use (machine-surface clean).

### `goat tutor` — vimtutor for the fabric
Interactive, self-paced, hands-on. Defaults to `--sandbox` so anyone can run it cold. Lesson arc (each: explain → prompt the learner to run a real `goat` command → verify they did → advance; progress saved to `$GOAT_SANDBOX_DIR/tutor-progress.json`, resumable):

1. Orientation — what the fabric is (mesh + identity + Zenoh), the sandbox marker.
2. Identity & profile — `goat profile show` / `goat context`.
3. Reachability — `goat doctor`, reading the step verdicts.
4. Topics — `goat topic create` / `goat topic ls`.
5. Publish & subscribe — `goat pub` / `goat sub`, the round-trip.
6. Diagnose — `goat inspect`, reading findings/remedies.
7. Cascade — `--narrate`, watching consequences settle.
8. Access — the data-access COP (`goat access`), who-can-read.
9. Sealed data — encryption that carries its own read cut (`goat pub --sealed` / `goat sub --open`); goat-cli as a convenience consumer of the grant-crypto SDK (ADR 1106).
10. Rotate — member-leaf key rotation for forward secrecy (`goat access key rotate`), distinct from `goat admin rotate` (org cert reissue).
11. Wrap — recap + how to switch from sandbox to a real env.

Lessons are data (a manifest), not hard-coded prose, so they localize through the i18n catalog and extend without code changes.

## Delight touches (independent of onboarding)

- **All-green flourish** — when `goat doctor`/`goat status` come back fully green, append one understated line (a small 🐐 + localized "all green"). Pretty + success only; never on failure; never in `--json`; suppressed by `GOAT_NO_FUN`/`--quiet`. Doubles as a useful all-clear signal.
- **Hidden ASCII goat** — a hidden `goat goat` subcommand prints a charming ASCII goat + version + tagline. Only when explicitly invoked.
- **Next-step hints** — after a successful mutating command, an opt-out `hint:` line suggesting the natural next step (git-advice shaped). **Shipped** as `fun::hint()` (stderr-only, TTY-only, `GOAT_NO_FUN`-suppressed), wired into `goat topic create` (→ `goat sub`/`goat pub`); extend to other mutators as needed.
- **Klingon (`tlh`) locale** — shipped (ADR 1024 / goat-cli-i18n.md): an opt-in easter-egg locale that proves the i18n surface supports playful locales at zero machine-surface cost.
- **Early-internet easter eggs (hidden subcommands).** Tasteful deep cuts, all hidden from `--help` and off the machine surface:
  - `goat goat` — a charming ASCII goat.
  - `goat truths` — RFC 1925, "The Twelve Networking Truths" (1996). Apt for a brokerless fabric: truth 11 (*"every old idea will be proposed again with a different name"* — the broker we keep rejecting) and truth 12 (*"perfection is when there's nothing left to take away"* — goat's "minimal" principle).
  - `goat xyzzy` — the Colossal Cave Adventure (1976) magic word → `Nothing happens.`
  - `goat brew` — HTTP 418 "I'm a teapot" (HTCPCP, RFC 2324, 1998), with the goat twist.
  - `goat bound` and `goatbuilder bound` — the same game in both binaries (`crates/goat-fun`): a mountain goat bounds along a cliff ledge in the terminal, in the spirit of the browser's offline dinosaur, jumping rocks and gaps and ducking an eagle once the pace is up, with an argan tree on the horizon and the sky turning every 700 points. Space/↑ jumps, ↓ ducks, q or Esc quits, any key runs again. It refuses in one line without an interactive terminal, writes no files, opens no connections, always restores the terminal, and is never launched by an error.
  - Shipped elsewhere: the first ARPANET message **"lo"** (1969) is `goat hello`'s payload + footnote.
  - Reserved for later: an RFC 1149 avian-carrier nod in `--narrate`; `finger`/`.plan` peer status.

## Phasing

1. **P1 — delight + hello (most demoable):** the delight touches + the `Substrate` seam (minimum to back `hello`) + `goat hello` (sandbox + real). **Shipped:** the flourish + the `goat goat`/`truths`/`xyzzy` eggs; the `Substrate` enum seam (`src/substrate.rs`); `goat hello` round-tripping `lo` through the sandbox (always-on, simulated, SANDBOX-banner-marked) or a real env (opens the profile's mTLS session, publishes, awaits the echo), with a machine-clean `--json` proof and next-step hints.
2. **P2 — init wizard:** **Shipped.** `goat init` orchestrates the pieces — detect state, pick sandbox or real (one TTY-gated prompt; deterministic sandbox default on a non-TTY), deliver the instant win (`goat hello --sandbox`), and point at the tutor. Thin glue over `hello`/`profile init`/`doctor`, not a reimplementation.
3. **P3 — tutor:** **Shipped.** `goat tutor` — a 10-lesson, self-paced, resumable hands-on tour (orientation → identity → doctor → pub/sub → inspect → narrate → access → sealed data → key rotate → wrap). Each lesson explains, shows the real command, and runs a live sandbox demo where it's network-free; progress saved per context. `--list` / `--reset`; interactive on a TTY, prints the outline on a non-TTY (never blocks). The sealed-data + key-rotate lessons teach the grant-crypto SDK-backed path (ADR 1106) — real seals need a SPIRE bundle + env, so they are explain-and-`--help`, not live-demo. Lessons are a data array today; lifting them to a localizable manifest is the next refinement.

Each phase is independently shippable and obeys the Three Rules. This doc is the spec; ADR to follow for the `Substrate` seam (an architectural seam worth recording).

## What this is NOT

- **Not a deployed sandbox env.** The sandbox here is an *in-CLI simulation* for learning, distinct from ZAGROS / nova-a (real deployed sandbox goatnets). It never touches a network.
- **Not a replacement for `goat admin onboard`.** That provisions real partner credentials. `goat init`/`tutor` teach and practice; they do not issue identity.
- **Not in the machine surface.** See the Three Rules.
