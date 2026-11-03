# snitch-app — Cross-platform daemon + GUI shell for the snitch probe

**Status:** Proposed (Block 77 — backlog).
**Authoritative ADR:** [`0841-snitch-app-cross-platform-daemon-gui.md`](../adr/0841-snitch-app-cross-platform-daemon-gui.md).
**Repo (planned):** `snitch-app` — public, Apache 2.0.
**Companion:** [`goat-client.md`](goat-client.md) (Block 76, separate app — different problem, same client-tooling pattern).
**Wraps:** the existing [snitch probe binary](snitch.md) (Block 61) — unchanged.

---

## 1. Problem

The snitch probe (per [Block 61](snitch.md)) runs on every mesh peer as a background daemon — Linux systemd service, macOS launchd LaunchDaemon, Windows Service per [Block 61H](snitch.md#multi-platform-probe-builds). It produces fleet-health visibility for operators by POSTing measurements to the snitch collector every cycle.

Current operator + user experience:

- **Operator:** queries the collector dashboard, watches the Grafana panels, reads the alert stream. Good experience.
- **End user with the probe installed on their device:** has no idea the probe is there, no way to see its status, no way to pause it, no way to know whether their device is healthy in the fleet's eyes. The probe is a CLI service running silently in the background.
- **Mobile users:** the probe is not available on iOS or Android at all. Snitch is a root daemon by design ([Block 61H](snitch.md)) — mobile sandboxes do not host root daemons.

Two consequences:

1. **No self-service troubleshooting.** A user whose tunnel is misbehaving cannot tell whether snitch is reporting them as healthy. They escalate to the operator, who consults the dashboard. The user is a passive subject of fleet observability rather than a participant.
2. **No mobile fleet visibility.** Mesh peers running on phones — operators triaging from a phone, partner liaisons on the move — produce no probe data and have no health surface.

`snitch-app` solves both with a thin GUI shell over the existing probe daemon, plus a strictly-cut-down mobile probe that fits inside iOS / Android sandbox restrictions.

## 2. Scope

### In scope (v1)

- **Desktop GUI shell** (Go + Fyne) on Linux + macOS + Windows that wraps the existing snitch probe binary.
- Display: probe status (running / paused / failing), policy mode (off / minimal / default / focused-research per [Block 61](snitch.md)), kill-switch toggle, time-since-last-successful-collector-POST, recent reports (last N from local cache), self-metrics (cycles_completed, cycles_skipped, bytes_in/out estimate).
- Operator-facing diagnostics: "run probe now" button (one-shot `snitch run`), "view last report JSON" surface, "test collector reachability" button.
- System-tray / menu-bar integration showing a single-glance health indicator (green / amber / red).
- Bundles + uses the existing snitch probe binary unchanged (as a co-installed sibling) — does not fork or modify it.

### In scope (v2)

- **Mobile clients** (iOS Swift + Android Kotlin) that wrap a strictly-cut-down probe surface — see §5.
- Push notifications when policy mode changes or kill-switch is engaged.

### Explicitly out of scope (v1)

- **Modifying the snitch probe daemon itself.** The probe binary stays canonical; snitch-app is a shell over it. Any feature requested for the GUI is added by extending the probe's IPC surface, not by forking the probe.
- **Operator-side collector / dashboard / alerting.** That is server-side ([Block 61](snitch.md)) and unchanged.
- **End-user policy-set authority.** The GUI surfaces the *current* policy mode and lets the user engage the per-peer kill switch (which is already an existing escape valve per [Block 61](snitch.md)). It does not let the user set fleet-wide policy.
- **Bundle / wg-cp0 onboarding.** That is [`goat-client`](goat-client.md) (Block 76).

## 3. Architecture

```
┌──────────────────────────── User device ──────────────────────────────┐
│                                                                       │
│  ┌────────────────────┐         ┌──────────────────────────────────┐  │
│  │   snitch-app GUI   │  IPC    │       snitch probe daemon        │  │
│  │  (Fyne / Swift /   │◄───────►│       (existing, unchanged)      │  │
│  │   Kotlin)          │         │                                  │  │
│  │                    │         │  - measure cycle                 │  │
│  │  - Status display  │         │  - POST to collector             │  │
│  │  - Policy mode     │         │  - policy-mode poll              │  │
│  │  - Kill switch     │         │  - self-metrics                  │  │
│  │  - Recent reports  │         │  - local report cache            │  │
│  │  - Run-now button  │         │                                  │  │
│  │  - Tray indicator  │         └──────────────┬───────────────────┘  │
│  └────────────────────┘                        │                      │
│           │                                    │                      │
│           │                                    │                      │
│           ▼                                    ▼                      │
│  ┌────────────────────┐                 ┌────────────────────┐        │
│  │ Local report cache │                 │   netbird daemon   │        │
│  │ ~/.snitch/cache/   │                 │ (probed by snitch) │        │
│  └────────────────────┘                 └────────────────────┘        │
│                                                                       │
└────────────────────────────────────────────────┬──────────────────────┘
                                                 │
                                                 ▼ HTTPS POST every ~60s
                                       ┌─────────────────────┐
                                       │   snitch collector  │
                                       │   (per Block 61)    │
                                       └─────────────────────┘
```

### Components

| Component | Role | Platform |
|---|---|---|
| `snitch` (probe daemon) | Existing — measures + POSTs to collector. Unchanged. | Linux / macOS / Windows desktop ([Block 61H](snitch.md) — unchanged) |
| `snitch-app` (GUI) | Fyne desktop GUI shell | Linux / macOS / Windows |
| `SnitchApp.app` | Swift UI wrapping cut-down mobile probe | iOS / iPadOS (v2) |
| `snitch-app.apk` | Kotlin UI wrapping cut-down mobile probe | Android (v2) |

### IPC contract: GUI ↔ existing probe

The probe daemon today exposes its state through:

- The on-disk report cache (local copies of recent reports).
- The systemd / launchd / Windows service log surface.
- The collector-side dashboard (round-trip via the collector).

For the GUI shell, we add a **local read-only IPC** to the probe (small extension of the existing binary, not a fork). Choices:

- **Option A — Unix socket (desktop) / named pipe (Windows):** probe daemon exposes `~/.snitch/sock` returning current state on a single-shot read. Stateless, well-bounded, matches goat-client's pattern. **Lean: this.**
- **Option B — local HTTP on loopback:** simpler, but introduces a port binding (firewall / SELinux annoyance) and gives any local user read access (not ideal).
- **Option C — read-only state file:** probe writes `~/.snitch/state.json` every cycle; GUI reads it. Simplest but stale data between cycles.

The kill switch and "run-now" buttons require *write* IPC (not just read). Option A handles both via a JSON-RPC method set (`getState`, `setKillSwitch`, `runOnce`, `getLastReport`). The probe gates write methods on a local-uid check.

This is the *only* probe-side change. The probe binary remains the canonical artifact per Block 61H; snitch-app is a separate repo that ships a sibling GUI that talks to it.

### Existing self-metrics surface

Per [Block 61](snitch.md), the probe already emits a `self_metrics` dict in every report (policy mode + source, kill-switch status, bytes_out/in estimate, cycles_completed/skipped, relay_probes_run, tls_handshakes, tcp_connects). The GUI displays these directly — no new collection logic needed on the GUI side.

### System tray / menu bar

- **macOS:** native menu bar item via Fyne's `desktop.App.SetSystemTrayMenu` (works since Fyne v2.2).
- **Linux:** AppIndicator / KStatusNotifierItem via Fyne — works on GNOME, KDE, XFCE, Cinnamon. Some minimal WMs (i3, sway) require the GUI window itself; documented limitation.
- **Windows:** native notification area icon via Fyne's tray API.

Tray indicator states:

- **Green** — probe running, last successful POST < 90s ago, policy mode is `default` or `focused-research`.
- **Amber** — probe running but last POST > 90s old, OR policy mode is `minimal`, OR kill switch engaged.
- **Red** — probe not running, OR last POST > 5 min old, OR collector reports user-side error.

## 4. Repo layout (planned)

```
snitch-app/
├── cmd/
│   └── snitch-app/           # Fyne GUI binary
├── internal/
│   ├── ipc/                  # JSON-RPC client (matches probe-side surface)
│   ├── ui/                   # Fyne GUI components
│   ├── tray/                 # platform-abstracted tray indicator
│   └── policy/               # local cache of last-known policy mode
├── mobile/
│   ├── ios/                  # Xcode project, Swift shell (v2)
│   └── android/              # Gradle project, Kotlin shell (v2)
├── docs/
│   └── README.md
├── packaging/
│   ├── deb/
│   ├── rpm/
│   ├── dmg/
│   ├── msi/
│   └── github-actions/
└── go.mod
```

The IPC contract lives in this repo as a Go interface; the probe binary in this repo's main goat tree depends on it as an import (small surface-area dependency, single-direction). Versioned with semver.

## 5. Mobile (v2)

Mobile snitch is **not the same probe** as desktop snitch. The desktop probe's measurement surface relies on shell tools (`ip route`, `iwgetid`, `ss`, `nc`, `journalctl`, `ping -M do`) that are inaccessible from inside an iOS / Android sandbox. A naive mobile port would either fail every probe or require root / nova.

Mobile probe — **strictly cut-down** to what the platform actually allows:

| Probe | iOS | Android | Notes |
|---|---|---|---|
| Tunnel up? | ✅ via `NEVPNManager` | ✅ via `ConnectivityManager.NetworkCallback` | Reports whether the goat-client wg-cp0 tunnel is active. |
| Collector reachable? | ✅ Go `net.DialTimeout` | ✅ Go `net.DialTimeout` | TCP-connect probe, no shell. |
| Mesh peer reachable (selected)? | ✅ `URLSession` HTTP | ✅ `OkHttp` | One operator-selected target. ICMP not available on iOS without entitlements. |
| Path RTT to collector | ✅ Go `net.DialTimeout` round-trip | ✅ same | Coarse but useful. |
| Local network metadata | ✅ `CTTelephonyNetworkInfo` (cellular) + `NEHotspotNetwork` (Wi-Fi SSID, with location permission) | ✅ `TelephonyManager` + `WifiManager` | Carrier name, technology (LTE/5G/Wi-Fi), Wi-Fi SSID. |
| Battery / power state | ✅ `UIDevice.batteryLevel` | ✅ `BatteryManager` | Useful for fleet-health context. |
| Race-dialer log read | ❌ inaccessible | ❌ inaccessible | netbird daemon's logrus output is per-OS sink-shaped per F-081 and unreachable from sandbox. |

The mobile probe shares the **report schema** with the desktop probe (same wire format to the collector) but populates a strict subset of fields. The collector accepts both shapes; reports without the desktop-only fields just have nulls. No collector-side changes are required.

Mobile probe runs:

- **iOS:** as part of the goat-client NetworkExtension process (per-app entitlement, runs as long as VPN is active) OR as a `BGAppRefreshTask` when the app is backgrounded (~once per few hours, OS-controlled). Not a continuous 60s loop — that doesn't fit the iOS background model.
- **Android:** as a foreground service (with persistent notification) for the active session, plus optional `WorkManager`-scheduled probes when backgrounded.

Cadence on mobile is therefore **best-effort**, not the desktop probe's strict 60s cycle. The collector knows this from the report's `client_kind: mobile` field.

## 6. Build + release

Same five-platform CI matrix as goat-client (Block 76):

- Desktop: `linux/{amd64,arm64}`, `darwin/{amd64,arm64}`, `windows/{amd64,arm64}` via Go cross-compile.
- Mobile: gomobile-bound iOS .xcframework + Android .aar (v2).
- Reproducible builds, cosign-signed releases, code signing per platform store.
- Tagged releases at `snitch-app-v<semver>`.

## 7. Naming

Why a separate name (`snitch-app`) and not just "snitch"? The CLI binary on disk is already `snitch` per Block 61 — the GUI shell is functionally the *user-facing* surface. Two-word naming pattern matches project convention (`goat-client`, `goat-shim-framework`). The mobile App-Store / Play-Store name will likely be friendlier-cased ("Goat Snitch" or "Snitch by goat") — that's a v2 marketing detail.

## 8. Relationship to existing components

- **[snitch probe daemon](snitch.md) (Block 61):** unchanged. snitch-app shells over it via an additive IPC surface; no probe-side behavioral change.
- **snitch collector + dashboard:** unchanged. The collector continues to receive reports from probes (desktop or mobile) on the same wire format.
- **[`goat-client`](goat-client.md) (Block 76):** sibling, separate app, separate repo. snitch-app does *not* require goat-client (the snitch probe runs on devices regardless of how they reached the mesh) and goat-client does not require snitch-app (the wg-cp0 tunnel is independent of probe presence). Users may install one, the other, or both.
- **[Block 22K](offline-enrollment.md) bundle:** snitch-app does not consume the bundle for v1 (the existing snitch-bundle Ansible role and `bundle-extract` flow handle probe-key delivery to operator-managed peers). End-user devices that want to participate in fleet-health visibility will need a separate per-device probe-key delivery — either via the bundle on first install, or via a future operator-issued enrollment QR. Captured as open question Q3.
- **[`netbird-client`](https://github.com/netbirdio/netbird):** unrelated at the daemon level (snitch-app does not drive a tunnel). On the GUI side, snitch-app borrows the same tray-indicator + status-card patterns netbird's GUI uses; no code shared.

## 9. Open design questions

| # | Question | Decision deadline | Notes |
|---|---|---|---|
| Q1 | Single binary (probe + app) vs separate (today's split)? | v1 | Lean: keep separate. The probe is a privileged service; the GUI is a per-user app. Conflating them complicates packaging + privileges. |
| Q2 | Tray indicator default-on vs default-off? | v1 | Lean: default-on for operators, default-off for end users — operator-controllable via install profile. |
| Q3 | How does an end-user device get its probe API key? | v1 | Today: via the offline-enrollment bundle (operator-managed sites). For end-user devices on goat-client, lean: extend the bundle to optionally carry a `snitch_probe_key` that goat-client surfaces to snitch-app on first run. Decide before mobile v2. |
| Q4 | Does snitch-app run if the probe is not installed? | v1 | Lean: yes, with a "snitch probe not detected — install?" prompt that links to the install instructions. Unhelpful to require both. |
| Q5 | Mobile cadence policy | v2 | Best-effort by platform. Documented limitation; collector tags reports with `client_kind`. |
| Q6 | Does the mobile app embed the goat-client tunnel, or expect goat-client installed separately? | v2 | Lean: separate apps; user installs both. Bundling complicates iOS NetworkExtension (one app per VPN configuration is the cleanest model). |
| Q7 | Release-channel naming: stable / beta / nightly? | post-v1 | Lean: stable + beta only at first; nightly adds CI complexity. |

## 10. Implementation tracking

Block 77 sub-tasks live at `docs/project/implementation-plan.md`. Backlog entry at `docs/project/backlog.md`.

## 11. Glossary

See `docs/adr/GLOSSARY.md` — snitch, probe, collector, gomobile, NetworkExtension, VpnService, Fyne all defined there (snitch / probe / collector defined in [Block 61 design doc](snitch.md)).
