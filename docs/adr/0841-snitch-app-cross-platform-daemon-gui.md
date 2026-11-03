# ADR 0841 — snitch-app: cross-platform GUI shell over the existing snitch probe (additive IPC, mobile cut-down)

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-05-09 |
| Repo scope | New repo `goat-maintainer/snitch-app`; depends on the in-tree snitch probe via small additive IPC contract |
| Supersedes | — |
| Superseded by | — |
| Related | [`0840-goat-client-cross-platform-daemon-gui.md`](0840-goat-client-cross-platform-daemon-gui.md) (sibling), [`0112-tech-strategy-workload-shape-language-boundary.md`](0112-tech-strategy-workload-shape-language-boundary.md), [`0111-vendor-friendly-license-posture.md`](0111-vendor-friendly-license-posture.md), [`0217-shim-repo-containment.md`](0217-shim-repo-containment.md) |

## TL;DR (for a layperson)

Today our fleet-health monitor (snitch) runs invisibly on every device. Operators see a dashboard; users see nothing. We will build a small companion app that lets a user see "is my device healthy in the fleet's eyes? am I being measured? can I pause it?" The app is a thin GUI shell over the existing snitch program — not a rewrite. It runs on Windows, Mac, Linux, and (in v2) iPhone and Android. Mobile gets a strictly cut-down probe because phones cannot run the full set of measurements due to platform sandbox rules.

## Context

The snitch probe ([Block 61](../design/snitch.md)) runs as a privileged background daemon on every mesh peer — Linux systemd, macOS launchd LaunchDaemon, Windows Service per [Block 61H](../design/snitch.md#multi-platform-probe-builds). It measures + POSTs every ~60s to the per-mesh collector. Operators get a dashboard; users get nothing.

Two real problems result:

1. **No self-service troubleshooting.** A user with a misbehaving connection cannot tell whether snitch is reporting them as healthy. They escalate to an operator who consults the dashboard.
2. **No mobile fleet visibility.** Snitch is a root daemon; mobile sandboxes do not host root daemons; therefore phones produce no probe data and have no health surface in the fleet.

Mobile peers are increasingly common (program staff, sponsors, partner liaisons, operators triaging from a phone). The fleet is opaque in exactly the user-class that grows fastest.

The desired user-facing surface for snitch is structurally similar to goat-client (Block 76): a tray indicator + status card + diagnostic buttons. The Go-+-Fyne / gomobile-+-native pattern from ADR 0840 fits — same scaffolding, different content.

## Decision

We will build **`snitch-app`** as a separate public repo at `goat-maintainer/snitch-app`, Apache 2.0, structured as a thin GUI shell over the existing snitch probe binary, with the following decisions of record:

### D1 — GUI shell, NOT a probe rewrite

snitch-app is a *consumer* of the existing snitch probe daemon. The probe stays canonical at [Block 61H](../design/snitch.md). snitch-app does not:

- Fork the probe.
- Replace the probe.
- Re-implement the probe's measurement logic.
- Bypass the probe's policy-mode controls.

Any feature requested for the GUI is added by extending the **probe's IPC surface**, not by forking the probe. The probe binary remains the single canonical artifact.

**Rejected: monolithic single binary that bundles GUI + probe.** Conflates the privileged service with the per-user app, defeats privilege separation, complicates platform-store packaging (privileged-service installers + GUI installers are different shapes on every platform), couples release cadence between substrate-grade probe and polish-grade GUI.

### D2 — Additive IPC: probe gains a single Unix-socket / named-pipe surface

The probe gains exactly one new feature in its trunk codebase: a local IPC surface (Unix socket on Linux/macOS at `~/.snitch/sock`; named pipe on Windows at `\\.\pipe\snitch`) that exposes a small JSON-RPC method set:

| Method | Direction | Auth | Purpose |
|---|---|---|---|
| `getState` | read | none | Returns last-cycle status: policy mode, last-POST timestamp, kill-switch state, self-metrics. |
| `getLastReport` | read | none | Returns the last report JSON (already on disk; convenience accessor). |
| `setKillSwitch` | write | local-uid match | Engages or disengages the per-peer kill switch (existing escape valve, exposed to GUI). |
| `runOnce` | write | local-uid match | Forces a one-shot probe cycle outside the timer (operator-fired diagnostic). |

Local-uid auth is the same pattern systemd / launchd use for user-scoped sockets. No network exposure; no auth-secret distribution.

**Rejected: state-file polling.** Stale data between cycles (60s default), no write surface for kill-switch / run-once. State files are the right answer for batch consumers; a GUI is interactive.

**Rejected: loopback HTTP.** Adds a port binding, complicates firewall / SELinux posture, gives any local user read access (named-socket uid-check is structurally narrower).

### D3 — Five-platform GUI build matrix matches goat-client (ADR 0840 D7)

Same shape. `linux/{amd64,arm64}`, `darwin/{amd64,arm64}`, `windows/{amd64,arm64}` for desktop; gomobile-bound iOS .xcframework + Android .aar for mobile (v2). Cosign-signed releases, code signing per platform store. Mirrors [Block 61H](../design/snitch.md) snitch probe build pattern (so the probe binary + GUI binary are both five-platform shipped from independent CI matrices in two repos).

### D4 — Mobile probe is a strictly cut-down companion, NOT the desktop probe

The desktop probe's measurement surface relies on shell tools (`ip route`, `iwgetid`, `ss`, `nc`, `journalctl`, `ping -M do`) that are inaccessible from inside an iOS / Android sandbox. A mobile port of the desktop probe would either fail every probe or require root access.

Mobile probe scope is reduced to what the platform allows (per design doc §5):

- Tunnel up? (via `NEVPNManager` / `ConnectivityManager`)
- Collector reachable? (via `URLSession` / `OkHttp` TCP-connect)
- One operator-selected mesh peer reachable? (HTTP probe; ICMP not available without entitlements)
- Path RTT to collector
- Local network metadata (carrier, technology, Wi-Fi SSID — with permission)
- Battery / power state

Wire format matches the desktop probe; reports tagged `client_kind: mobile` so the collector knows to expect nulls in desktop-only fields. **No collector-side changes.**

Cadence on mobile is **best-effort** — iOS BGAppRefreshTask grants ~one wake-up per few hours when backgrounded; Android WorkManager is similar. The desktop probe's strict 60s cycle does not fit the mobile background model. Documented limitation, not a defect.

### D5 — Repo containment: separate public repo, Apache 2.0

`snitch-app` lives in its own public repo `goat-maintainer/snitch-app`, Apache 2.0 (matches goat-client + polygloat + goat-moon-open-pod pattern; per [ADR 0217](0217-shim-repo-containment.md) and shim-pattern §11). The probe stays where it is in the goat trunk at [`ops/snitch/`](../../ops/snitch/) — only the GUI shell carves out.

Why separate from snitch-trunk? GUI iteration cadence + per-platform store-release cycle is not the substrate cadence; severable repo lets snitch-app v1.0.1 ship without touching the goat trunk, and future per-platform release branches (`snitch-app-ios-1.x`) live alongside Mac-only fixes without coupling. The probe-side IPC contract lives in this snitch-app repo as a shared Go interface that the snitch trunk imports — narrow, single-direction dependency, semver-versioned.

### D6 — End-user probe-key delivery: deferred to v1 implementation, leaning on Block 22K bundle

Operator-managed Orin sites get probe keys via the existing snitch-bundle Ansible role + offline-enrollment bundle's `snitch_probe_key` field ([Block 61G](../design/snitch.md)). End-user devices on goat-client (Block 76) need a separate per-device probe-key delivery — either by extending the bundle to optionally carry an end-user probe key that goat-client surfaces to snitch-app on first run, or by an operator-issued enrollment QR shipped alongside the bundle.

Lean: **bundle extension.** goat-client is already the bundle consumer; same trust path; no new operator step. Decided at v1 design review. Captured as Q3 in design doc §9 to remain visible.

### D7 — v1 = desktop only; v2 = mobile

Desktop ship reuses 100% of the existing probe install surface from Block 61H (systemd, launchd, Windows Service). Mobile requires gomobile + Apple/Google submission + the cut-down probe per D4. Sequencing v1-desktop → v2-mobile lets us validate the IPC contract + GUI surface against the existing probe + collector before adding the mobile-cadence + reduced-measurement surface.

## Trade-offs

**Cost:**

- Engineering: ~3-5 weeks single engineer for v1 desktop GUI across three platforms (smaller than goat-client because no tunnel-management privilege chain, no per-platform VPN entitlement plumbing). +3-5 weeks each for iOS and Android v2 (cut-down probe + native shell + app-store submission). Total v1+v2 ≈ 9-15 engineer-weeks, parallelizable.
- Probe-side change: ~1-2 days. Adding the named-socket IPC surface to the existing Go daemon is a small additive change; no behavioral impact on the existing measurement or POST loops.
- Procurement: same Apple/Google/Authenticode footprint as goat-client. If goat-client has already procured the certs, snitch-app reuses them.
- Maintenance: ~0.25 FTE-week / quarter ongoing — the GUI surface is small and the probe-side changes are bounded.

**Benefit:**

- Self-service troubleshooting collapses one operator-escalation class entirely (user can see "yes, snitch is reporting me as green").
- Mobile fleet visibility lights up — today, *zero* mobile peers report. Even the cut-down probe surface is strictly more than nothing, and the fleet-health view becomes representative of the actual peer population.
- Operator-side dogfood: the operator's own laptop running snitch-app is the fastest first-look-fault-localizer.

**Risk:**

- IPC contract evolution: as we add features (Q-row items in design doc §9), the IPC interface grows. Mitigation: semver the interface module, run interface-version negotiation on probe-side connect.
- Tray-indicator surface bloat: tray UIs accumulate complexity ("show me also X", "color-code Y"). Mitigation: design doc §3 fixes the green/amber/red taxonomy upfront; additions require explicit decision-of-record.
- Mobile cadence misunderstood as "broken": users may report "the mobile app is not measuring as often as the desktop one." Mitigation: in-app explainer text on first run; operator runbook; collector-side tagging so the dashboard reflects the cadence difference correctly.

## Alternatives considered

### Add a GUI to the existing snitch trunk repo (no separate snitch-app)

**Rejected.** Couples GUI release cadence to the trunk's commit pace. Per-platform store-release cycles are a different shape than substrate-grade trunk releases; severable repo lets the GUI iterate at its own pace.

### Build a web dashboard the user opens in a browser

**Rejected.** Requires a local HTTP listener (D2 alternative — already rejected for read-access posture) or routes through the collector (which means the collector dashboard, which is already operator-only). The tray-indicator + native-feel-status-card pattern is structurally cheaper for the use case.

### Mobile via React Native / Flutter

**Rejected.** Same reasoning as ADR 0840 D2 — the Go daemon + native-UI pattern is what works for snitch's measurement-tool shape, where the daemon is the load-bearing piece and the UI is a thin shell. Three GUI codebases (Fyne / Swift / Kotlin) is the easier-to-maintain shape than two (Fyne + Flutter) where Flutter still has to bridge to the Go daemon via FFI / platform channels for the cut-down measurement code.

### Skip mobile entirely

**Rejected.** Mobile peers are a project goal (sponsors, partner liaisons, off-LAN operators) and the fleet-health view is materially incomplete without them. The cut-down probe (D4) makes mobile useful within platform constraints; v2 sequencing keeps the v1 desktop release light.

## Implementation tracking

Block 77 sub-tasks live at `docs/project/implementation-plan.md`. Backlog entry at `docs/project/backlog.md`. Acceptance: v1 ship is cosign-signed desktop binaries pulled by ≥3 distinct users + tray-indicator working on Linux + macOS + Windows + at least one operator-side "snitch-app caught a misconfig before the operator dashboard would have" anecdote.

## Glossary

See `GLOSSARY.md`.
