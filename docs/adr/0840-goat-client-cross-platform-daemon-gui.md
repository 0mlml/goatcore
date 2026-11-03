# ADR 0840 — goat-client: cross-platform daemon + GUI for wg-cp0 onboarding (fork netbird's client pattern)

| Field | Value |
|---|---|
| Status | Accepted (ratified by delivery 2026-05-09 / 2026-05-10 — 11 PRs merged across all 7 design tracks + 4 bonus tracks on `goat-maintainer/goat-client`; v0.1.0 release-prep landed) |
| Date | 2026-05-09 |
| Repo scope | New repo `goat-maintainer/goat-client`; references goat core for Block 22K bundle format + offline-CA trust anchor |
| Supersedes | — |
| Superseded by | — |
| Related | [`0313-silent-control-plane-outer-wg-wrap.md`](0313-silent-control-plane-outer-wg-wrap.md), [`0314-no-public-dns-for-control-plane.md`](0314-no-public-dns-for-control-plane.md), `0316-wg-cp0-mixed-cloud-and-local-relay-fleet.md`, `0406-offline-ca-yubikey-piv.md`, [`0110-license-posture-agplv3-forked-components.md`](0110-license-posture-agplv3-forked-components.md), [`0111-vendor-friendly-license-posture.md`](0111-vendor-friendly-license-posture.md), [`0112-tech-strategy-workload-shape-language-boundary.md`](0112-tech-strategy-workload-shape-language-boundary.md), [`0841-snitch-app-cross-platform-daemon-gui.md`](0841-snitch-app-cross-platform-daemon-gui.md) (sibling), [`0843-public-mgmt-signal-relay-mobile-crutch.md`](0843-public-mgmt-signal-relay-mobile-crutch.md) (parallel reachability surface; first activation closes the F-109 mobile gap before path A ships), F-109 (mobile tunnel-slot mutex; motivates the v0.2 path-A commitment recorded below) |

## TL;DR (for a layperson)

End users on phones and laptops cannot easily get onto our outer secure tunnel today. The current onboarding tool is a command-line program for engineers running our server hardware. We will build a friendly app — paste-the-bundle, click Connect — that runs on Windows, Mac, Linux, iPhone, and Android. We will model it on netbird's open-source client app (which already solves this exact problem class for a different but adjacent network) and adapt it to use our offline-signed enrollment bundle as the onboarding artifact. The app does only one thing — get and keep the outer tunnel up — so we ship a v1 in weeks rather than months.

## Context

The wg-cp0 silent control plane ([ADR 0313](0313-silent-control-plane-outer-wg-wrap.md)) has no public DNS endpoint, no public discovery, no setup-key model. Onboarding is via offline-CA-signed CBOR bundles ([Block 22K](../design/offline-enrollment.md)) that carry the full tunnel config + peer pubkey + endpoint set. Today, two CLIs handle the device-side bundle flow:

- [`bundle-extract`](../../ops/enrollment/cmd/bundle-extract/) — diagnostic / scripted extraction.
- [`wg-cp0-bundle-apply`](../../ops/enrollment/cmd/wg-cp0-bundle-apply/) — operator-fired tunnel-up on Linux Orin sites.

This works for operator-managed Orin sites where Ansible drives the bundle apply. It does not work for:

1. **Off-LAN end users** on phones and laptops who need to reach mesh services from outside any operator-controlled site (program staff, sponsors, partner liaisons, viewers).
2. **Mobile devices.** iOS and Android have no `wg-quick`, no `/etc/wireguard/`, no shell, and no offline-enrollment binary. The bundle CLI cannot run on a phone.
3. **Non-engineer roles.** Anyone who does not live in a terminal — the CLI is a wrong-shape artifact.

The general shape of the problem — "how do you get a phone or laptop user onto a WireGuard mesh with one click?" — is solved by netbird's client tier ([netbirdio/netbird](https://github.com/netbirdio/netbird)). netbird ships:

- A Go daemon that drives `wireguard-go` (or kernel WireGuard where available), running as a service.
- A Fyne-based desktop GUI on Linux + macOS + Windows.
- A gomobile-built daemon framework wrapped by native Swift (iOS, NetworkExtension) and Kotlin (Android, VpnService) UI shells.
- The whole client tier is **MIT-licensed** (F-010: only the server-side `management/`, `signal/`, `relay/`, `combined/` were relicensed to AGPLv3 in Aug 2025; the client tree at `client/` was not).

The MIT license + working cross-platform scaffolding + matching problem-shape make netbird's `client/` tree the obvious starting point for a goat-side client app.

## Decision

We will build **`goat-client`** as a separate public repo at `goat-maintainer/goat-client`, Apache 2.0, modeled on (and partially forking) netbird's `client/` tree, with the following decisions of record:

### D1 — Two-binary daemon-and-GUI shape on desktop; gomobile-wrapped daemon with native UI on mobile

Mirrors netbird's pattern exactly. On desktop:

- `goat-clientd` — Go daemon, runs as system service (systemd / launchd / Windows Service), has tunnel-management privilege.
- `goat-client` — Fyne GUI binary, runs as user, talks to daemon via Unix socket / named-pipe IPC.

On mobile:

- iOS: daemon compiled as gomobile `.xcframework`, linked into a Swift app that hosts a NetworkExtension Packet Tunnel Provider (Apple's only sanctioned WireGuard path).
- Android: daemon compiled as gomobile `.aar`, linked into a Kotlin app that hosts an Android `VpnService` (Google's only sanctioned WireGuard path).

**Rejected: single combined binary.** Conflating GUI and daemon defeats the privilege separation that systemd/launchd/Windows-Service give us "for free." Mobile platforms enforce the separation regardless (the NetworkExtension / VpnService runs in its own process).

### D2 — Fork netbird's client/ tree, do not clean-room

The bulk of the cross-platform work is *not* the WireGuard logic — it is the platform-specific glue: macOS launchd registration, Windows Service installer, Linux dbus + AppIndicator quirks, iOS NetworkExtension entitlement plumbing, Android VpnService lifecycle. netbird's tree has all of this working. We fork it under MIT, strip what is netbird-mesh-specific (signal, management-API, peer-discovery, setup-key flows), and replace it with bundle-import + wg-cp0-tunnel logic.

**Rejected: clean-room reimplementation.** The platform-glue has been written, tested, and shipped to thousands of users in netbird. Re-doing it from scratch is months of work for no architectural gain. The license is permissive; forking is the cheapest path.

**Rejected: vendor netbird's binary directly.** netbird's client knows how to drive the inner-mesh tunnel via netbird's signal/management API — not how to consume an offline-CA-signed bundle for the outer wg-cp0 tunnel. The tunnel-up logic stays similar; the onboarding logic is fundamentally different.

### D3 — Strictly wg-cp0 only at v1, NOT a netbird daemon replacement

goat-client v1 manages **only** the wg-cp0 outer tunnel. The inner-mesh netbird daemon is a separate concern and continues to be shipped as upstream `netbird` for now. A user who wants both installs both. Embedding netbird-client into goat-client (so a single app drives both tunnels) is a reasonable v2 ask but is **explicitly out of v1 scope** because:

- It triples the GUI surface (status of two tunnels, two sets of diagnostics, two configurations).
- It complicates code-signing and app-store packaging (one binary doing two privilege-distinct VPN paths).
- The inner-mesh netbird daemon is unchanged and works today; consolidating UX is a polish layer, not a structural unblock.

### D4 — Bundle is the onboarding artifact (no setup-key flow)

netbird onboards via a setup key — the user pastes a string into the app, which then calls the management API to retrieve the actual peer config. This model is structurally incompatible with wg-cp0:

- ADR 0313 says wg-cp0 has no public discovery endpoint. There is no management server to call.
- Setup-key models put the peer-identity decision in the management server. Bundle models put it in the offline CA. The latter is structurally stronger ([ADR 0315](0315-peer-key-attestation-via-offline-ca.md) Tailnet-Lock-style attestation lives on the same trust root).

goat-client consumes the existing [Block 22K bundle](../design/offline-enrollment.md) format unchanged at v1. The bundle carries everything needed to bring the tunnel up: peer privkey + peer pubkey + endpoint set + offline-CA trust anchor + TTL. An optional `--qr` extension to `bundle-create` for mobile-friendly delivery is a nice-to-have, not a v1 blocker.

### D5 — Pinned offline-CA trust anchor compiled into the binary, multi-anchor support during rotation

The trust anchor is **not user-configurable.** That would defeat the offline-CA chain — an attacker who can persuade the user to install a malicious trust anchor can mint bundles that pass verification. The anchor is compiled in at build time. Multiple anchors are supported during CA-root rotation windows (ADR 0406); the binary carries the active set. Stale binaries with retired anchors emit a refusal + "please update goat-client" prompt.

**Rejected: in-app trust-anchor refresh.** Refresh requires a separate signed-channel for anchor delivery, which is more crypto plumbing than this v1 needs. Re-build-and-ship-binary is the v1 mechanism; refresh is a v2 polish.

### D6 — Repo containment: separate public repo, Apache 2.0

`goat-client` lives in its own public repo `goat-maintainer/goat-client`, Apache 2.0 (matches polygloat and goat-moon-open-pod pattern; netbird's client tier is MIT but Apache 2.0 with patent grant is our default for first-party code per [ADR 0111](0111-vendor-friendly-license-posture.md)). Per [ADR 0217](0217-shim-repo-containment.md) and shim-pattern §11, severable per-component repos are the project's default for shippable user-facing artifacts. Composes with [ADR 0223](0223-goat-moon-pod-repo-containment.md) for the broader pattern.

The repo carries:

- The fork of netbird's client/ tree (heavily stripped down at first commit) under MIT (preserving netbird's notice).
- Our own additions (bundle parser, wg-cp0 onboarding, GUI re-skin) under Apache 2.0.
- Per-platform packaging (deb, rpm, dmg, msi, ipa, apk).
- CI matrix for five-platform builds.

**Rejected: in-tree.** The client app belongs with users, not with the substrate. Shipping cadence + GUI iteration cadence + app-store release cadence are all faster than core-substrate cadence. Severable repo lets us ship goat-client v1.0.1 without touching the core tree.

### D7 — Five-platform build matrix as v1 ship-target

CI builds in a matrix of: `linux/{amd64,arm64}`, `darwin/{amd64,arm64}`, `windows/{amd64,arm64}` for desktop; gomobile-bound iOS .xcframework + Android .aar for mobile. Six desktop binary targets + 2 mobile artifact targets at first release. Mirrors [Block 61H multi-platform snitch](../design/snitch.md) build pattern.

Code signing is the single highest-friction operational requirement; the lateral block to mobile-user reach. Apple Developer Program membership ($99/yr), Apple Developer ID + notarization, Windows Authenticode cert (~$200-700/yr), Google Play signing key, iOS distribution profile. These are operator-fired procurement items — captured as Block 76 H-row in implementation-plan.md.

## Trade-offs

**Cost:**

- Engineering: ~6-10 weeks single engineer for v1 desktop release across three platforms; +3-5 weeks each for iOS and Android (mobile gomobile + native shell + app-store submission). Mobile is parallelizable.
- Procurement: Apple Developer Program $99/yr, Windows Authenticode cert ~$200-700/yr, Google Play developer fee $25 one-time. Comparatively trivial.
- Maintenance: bounded — netbird's upstream client/ tree has been stable; security CVEs in `wireguard-go` are the main update channel and we inherit fixes by upstream tracking. Estimate: ~0.5 FTE-week / quarter ongoing.

**Benefit:**

- Off-LAN users and partners can join the mesh in one click — current onboarding is shell-CLI only, hostile to anyone not in engineering.
- Mobile becomes a first-class peer class (today: zero mobile users). Sponsors checking a map surface from a phone, operators triaging from a tablet, partner liaisons on the move.
- Procurement-resilience improvement: we have a sovereign client. If netbird's company changes its license or drops a feature we need, we already maintain a clients-tree fork.

**Risk:**

- App-store gatekeeping: Apple may reject the app if positioned as a "VPN" (App Store has historically been picky about VPN apps that don't go through one of their pre-approved pathways). Mitigation: present as an enterprise/on-premises management tool, follow netbird's submission pattern (which has cleared review).
- Code-signing key custody: a leaked Authenticode or Apple Developer ID cert would let an attacker ship trojaned releases. Mitigation: keys live in 1Password + per-CI-run pull, signed-by-cert audit trail, periodic rotation.
- Drift from upstream netbird client/: as netbird evolves, our fork diverges. Mitigation: bounded by D3 (we strip netbird-mesh logic, so our fork's surface area is small; upstream churn rarely touches the platform-glue we keep).

## Alternatives considered

### Continue CLI-only onboarding

**Rejected.** Acknowledges that off-LAN users + mobile + non-engineers stay locked out forever. Acceptable only if those user classes are not project goals — they are.

### Wrap an existing WireGuard GUI (`wg-quick-helper`, `WireGuard for Windows`, `tunsafe`)

**Rejected.** These all assume the user can paste a `.conf` file. They have no CBOR-bundle support, no offline-CA verification, no auto-reconnect-on-network-change. We would have to fork them anyway to add bundle import. netbird's client/ tree is closer to our shape than any generic WireGuard GUI.

### Use Tailscale's open-source client tier (`tailscale.com/cmd/tailscale`)

**Rejected.** Tailscale's mesh model is fundamentally different (node-key + key-server, not bundle + offline-CA). The client tier is BSD-3 (acceptable license-wise) but the architecture is more remote from our shape than netbird's. Forking would mean throwing away most of the model, not adapting it.

### Native-everywhere (no Go daemon, separate Swift/Kotlin/C# implementations)

**Rejected.** Three full-rewrite implementations of the WireGuard control logic, the bundle parser, the reachability prober, and the trust-anchor verifier. ~3-5× the engineering cost for no architectural gain. The Go-as-shared-daemon-with-native-UI pattern is specifically what works in netbird; it works because Go cross-compiles cleanly to every target while native UIs preserve the platform feel.

### Flutter everywhere

**Rejected.** Considered as Option B/C in the user-facing decision. Flutter on desktop is workable but adds another runtime + skin layer; Flutter on mobile means re-implementing the Go daemon either via FFI or platform channels. The "match netbird's ease of use" goal is best served by netbird's actual shape. (Block 76's predecessor Flutter sketch was minimal per user direction 2026-05-09; abandoning it for the Fyne-+-gomobile shape is the cheaper path.)

## Implementation tracking

Block 76 sub-tasks live at `docs/project/implementation-plan.md`. Backlog entry at `docs/project/backlog.md`. Acceptance: v1 ship is cosign-signed binaries on six desktop targets pulled by ≥3 distinct end users + the iOS / Android shells reaching at least TestFlight / Play Internal track.

## Amendment 2026-05-10b — v0.2 commits to path A (embed netbird as a library inside one PacketTunnelProvider)

F-109 surfaced a structural mobile-platform constraint that v1's per-platform design (D1) did not yet absorb: iOS `NEPacketTunnelProvider` and Android `VpnService` are single-claimant system-VPN slots. A user cannot run goat-client mobile (for the wg-cp0 outer tunnel) and the stock netbird mobile app (for the inner mesh) simultaneously on the same phone — the second install's first connect-attempt either fails (iOS) or revokes the first app's VPN grant (Android). The platform contracts do not allow both layers to be active independently in two separate PacketTunnelProvider extensions.

This amendment commits goat-client v0.2 to **path A** — embed the netbird inner-mesh implementation as a library link inside goat-client's single PacketTunnelProvider. Userspace WireGuard for the inner mesh tunnel runs inside the same Go runtime that drives the outer wg-cp0 kernel-WG tunnel via the OS-mandated PacketTunnelProvider. One app, one PacketTunnelProvider, one VPN slot, both layers active.

**What this changes vs. v1 (D3):**

D3 said v1 is strictly wg-cp0-only and embedding netbird-client is "explicitly out of v1 scope." That stays true for v0.1.x desktop and the v0.1 mobile shells. **v0.2 lifts the restriction for mobile only** — desktop coexistence stays unchanged (no platform-level mutex on desktops; goat-client desktop and stock netbird desktop coexist as today). The v0.2 mobile binary embeds netbird-client as a library; the v0.2 desktop binary does not (no need; no benefit).

**What stays the same:**

- D1 daemon-and-GUI shape on desktop, gomobile-+-native UI on mobile. v0.2 mobile is still gomobile-+-native; the change is what runs inside the gomobile-bound binary.
- D2 fork-netbird-client posture. The fork already has the netbird inner-mesh code; v0.2 stops stripping it on mobile builds.
- D4 bundle-as-onboarding-artifact. Bundle format extends to also carry the inner-mesh setup data (or a separate setup-key flow runs inside the same app); not yet finalized — Block 76 v0.2 sub-task.
- D5 pinned offline-CA trust anchor. Same.
- D6 Apache 2.0, separate public repo. Same.
- D7 five-platform CI matrix. Same; mobile builds stay parallel to desktop.

**Engineering work:** ~2-3 weeks of focused work after v0.1.x desktop hardening closes. Includes: pulling the netbird-mesh logic back into the mobile build (today stripped); designing the inner-mesh setup flow inside the goat-client mobile UI; reconciling the two-tunnel state machine inside one PacketTunnelProvider; testing co-active behavior on both iOS and Android.

**Bridging window before v0.2 ships:** [ADR 0843](0843-public-mgmt-signal-relay-mobile-crutch.md) public mTLS-gated crutch tier (Block 80) is the first activation of an operator-toggleable parallel reachability surface that lets mobile devices reach mgmt + signal + relay over a public mTLS path that consumes no system-VPN slot. Mobile-bridging is the crutch's **first** activation rationale — once goat-client v0.2 is generally available, mobile no longer requires the crutch day-to-day, but the substrate stays in the toolbox per ADR 0843 D5 for additional rationales (partner devices, emergency on-ramp, locked-down operator devices, external-perspective validation).

**Implication for Block 76 v1 mobile scope (76I + 76J):** the v1 mobile shells still ship as scoped (TestFlight / Play Internal track per the v1 verdict gate), but with a clear scope-limit notice in the operator-facing release notes — "v1 mobile is wg-cp0-only; for mesh participation use the crutch path; v0.2 will be the steady-state mobile experience." Operators can still hand a v1 mobile build to a user who only needs the wg-cp0 outer tunnel reach (e.g., for testing or for a degenerate use case).

## Amendment 2026-05-13 — three operating modes across all platform classes (subset of {wg-cp0 outer, netbird inner})

The 2026-05-10b amendment limited v0.2 path A to mobile, on the reasoning that desktop and headless have no platform-level slot mutex so the two-daemon shape (goat-client outer + stock netbird inner as separate daemons) "stays unchanged" and "no need; no benefit" to combine them. That framing also treated the Block 80 / ADR 0843 crutch tier as a transitional bridging window for mobile pre-v0.2 only.

Two operational realities are not captured by that framing:

1. **Behavior parity.** A user moving between phone and laptop should see the same shape — same setup flows, same status model, same failure surfaces. Mobile being force-combined and desktop being outer-only-only creates a parity gap that is real to non-engineer users and to operators triaging across both surfaces.
2. **The crutch tier is not transitional.** ADR 0843 D5 already enumerates additional rationales (partner devices, emergency on-ramp, locked-down operator devices, external-perspective validation) — the crutch substrate is a permanent toolbox item. A goat-client install on a device that reaches goat through the crutch is a *valid steady-state shape*, not a workaround. On mobile, that shape (netbird-only inside the single PacketTunnelProvider, mgmt reached over public mTLS) precisely replaces stock netbird mobile while burning no extra system-VPN slot.

This amendment generalizes v0.2 to **any subset of {wg-cp0 outer, netbird inner}**, selected at install or runtime, uniformly across mobile / desktop / headless:

| Mode | wg-cp0 outer | netbird inner | Mobile | Desktop | Headless |
|---|---|---|---|---|---|
| `wg-cp0-only` | ✅ | ❌ | Today's v0.1.x mobile | Today's v0.1.x desktop | Single-Orin sites composing stock netbird separately |
| `netbird-only` | ❌ | ✅ (mgmt reached via Block 80 crutch) | Replaces stock netbird mobile; rides Block 80 crutch | Locked-down hosts behind Block 80; goat-client-as-stock-netbird replacement | Server-class hosts behind Block 80 |
| `combined` | ✅ | ✅ | Path A (2026-05-10b) — both tunnels in one PacketTunnelProvider | Opt-in single-binary unified install | Opt-in single-binary `--include-inner-mesh` |

**Mobile constraint.** Single PacketTunnelProvider / VpnService — mobile picks one mode per session. The platform mutex constrains tunnel-count-per-app, not which tunnels.

**Desktop + headless.** No platform mutex; all three modes are runtime states. Same binary, same library set; mode picked at install time (`--mode <name>`) or at runtime (`setMode` IPC call).

**What this changes vs the 2026-05-10b amendment:**

- v0.2 mobile is no longer force-combined; it picks one of three modes per deployment posture. The 2026-05-10b "combined is platform-mandatory" framing was wrong — combined is mobile-mandatory *only when the deployment wants both layers on the device*. When the deployment uses Block 80 for reach (mobile-bridging activation), `netbird-only` is the natural mobile shape and combined would waste the slot on a redundant wg-cp0 outer.
- v0.2 desktop + headless gain both `netbird-only` and `combined` as opt-in modes alongside the existing `wg-cp0-only`. Same library substrate; runtime mode selection.
- One netbird-mesh-as-library implementation; reused across all three platform classes.

**What stays the same vs 2026-05-10b:**

- D3 (v1 wg-cp0-only) still describes v0.1.x correctly. v0.2 is the lift, and it now lifts uniformly across all platforms.
- Mobile commitment: when the deployment wants both layers, combined is the only shape that fits in one PacketTunnelProvider. Same architectural conclusion the 2026-05-10b amendment reached for that case.
- Existing CLI ceremony (`wg-cp0-bundle-apply`) stays alive as the operator-fired path for Ansible-driven Orin sites running modes that include wg-cp0. Headless modes are *additive*, not replacements.
- Block 80 crutch tier (ADR 0843) is no longer "transitional bridging" — it is the substrate that makes `netbird-only` reachable on every platform class. Permanent toolbox item.

**Engineering work** (sub-tasks scoped in implementation-plan.md Block 76 as 76N / 76O / 76P / 76Q):

1. **Foundation track (76N):** un-strip netbird-mesh from the goat-client fork, define the `Mesh` interface, wire mode selection (`--mode wg-cp0-only|netbird-only|combined`), add the bundle-format extension for `inner_mesh_setup` + `mobile_cert` fields, headless smokes for all three modes.
2. **Desktop + headless track (76O + 76P):** Fyne UI dual-status surface + per-mode display + mode-selector, install-time / runtime mode selection, single-package runtime-flag packaging, headless mode, documentation.
3. **Mobile track (76Q):** iOS NEPacketTunnelProvider + Android VpnService running any of the three modes inside one provider. Real-device co-active testing for `combined`; `netbird-only` end-to-end gated on Block 80 being live.

**Verdict gate (revised):** v0.2 ship = (a) `wg-cp0-only` mode unchanged from v0.1.x on every platform (regression bar); (b) `combined` mode active on ≥3 desktop installs with both tunnels passing inner-mesh peer reach; (c) `combined` mode active on ≥1 iOS device + ≥1 Android device; (d) `netbird-only` mode active on ≥1 mobile + ≥1 desktop with mgmt-API reach over Block 80 crutch (gated on Block 80 reaching live); (e) headless mode on ≥1 single-Orin site as one-binary install in any mode; (f) behavior-parity audit between mobile and desktop combined-mode passes per the design doc's §"Operating modes" parity bar; (g) TestFlight + Play Internal presence.

**Why three modes, not just combined.** Forcing combined-mode everywhere creates churn we don't have to take — today's stock netbird daemon is mature, has a healthy install / packaging / configuration ecosystem, and is composed across the existing fleet via Ansible roles and per-mesh-profile workflows that work cleanly. `wg-cp0-only` preserves that posture on hosts that want it. `netbird-only` lets goat-client serve as a stock-netbird replacement on devices that already reach goat through Block 80. `combined` is the unified shape for devices that benefit from single-binary install.

## Glossary

See `GLOSSARY.md`.
