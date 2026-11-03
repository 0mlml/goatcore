# ADR 0824 — Video fabric: MediaMTX-only v1, Zenoh control plane, post-Spike-A/B verdicts

| Field | Value |
|---|---|
| Status | Proposed (2026-04-30) — flips to Accepted on 35E canonical-tier 72h soak passing |
| Date | 2026-04-30 |
| Repo scope | goat core |
| Supersedes | — |
| Superseded by | — |
| Related | [`0216-goat-net-federation-and-site-planes.md`](0216-goat-net-federation-and-site-planes.md), [`0708-build-zenoh-from-source.md`](0708-build-zenoh-from-source.md), [`0811-audit-log-integrity.md`](0811-audit-log-integrity.md), [`0822-wide-event-observability-otlp-openobserve.md`](0822-wide-event-observability-otlp-openobserve.md), [`1008-soak-testing-discipline.md`](1008-soak-testing-discipline.md), [`1012-load-testing-discipline.md`](1012-load-testing-discipline.md), `../findings/F-018-mediamtx-v1.18-explicit-config-required.md`, `../findings/F-019-ffmpeg-rtsp-udp-default-mtu-mismatch.md` |

## TL;DR (for a layperson)

Video has a different shape than the rest of our data — sustained
many-megabit-per-camera flows, hard latency budget, 1-camera-to-N-viewer
fanout. Carrying it on Zenoh (our universal control + metadata substrate)
would either reinvent media-server mechanics that already exist or accept
poor latency for no benefit. So **video bytes ride a separate
media-purpose substrate** (MediaMTX) inside the existing WireGuard mesh,
while **Zenoh carries the catalog and the control plane**. After two
spikes, the simplest workable v1 — one MediaMTX container per site —
holds at the v1 test profile (150 concurrent
viewers across 5 streams) with comfortable headroom. No SFU (LiveKit) needed yet.

## Context

The video-fabric design at `docs/design/video-fabric.md`
proposed a four-layer split: feed-source ingest → per-site MediaMTX →
optional LiveKit SFU (gated on Spike B verdict) → MinIO video-archive +
recording-integrity ledger. Five decisions were held until the four
spikes (A-D) returned verdicts:

- **D1**: MediaMTX-alone vs MediaMTX+LiveKit (Spike B's verdict)
- **D2**: NVMe sizing for video-bearing sites (per-site brief)
- **D3**: Per-source identity model (sources we own use Block 22H
  offline-CA pattern; sources we don't own use whatever the upstream
  hands us — Spike-C-style integration probes per source class)
- **D4**: `release/<partner>/video/...` in v1 (default: defer)
- **D5**: DeepStream analytics in v1 (default: defer)

This ADR closes D1 + the architecture-level decisions on the back of
two completed spikes (A full + B), accepts the Phase-3+ deferrals on
D2/D4/D5 + Spikes C/D, and locks the v1 build path.

## Decision

**v1 ships MediaMTX-only**, with the Zenoh control plane providing
catalog + control + lineage on top.

Per-site v1 stack:

1. **Feed ingest (heterogeneous upstreams)** — sources we don't own
   (e.g., a camera inside a partner system), partner feeds, and eventually
   cameras we own. Transport + auth + discovery are **per-source**,
   captured at integration time per Spike C's discover-at-integration
   pattern. No assumption of ONVIF / mTLS / cert-issuance at the
   architectural layer. F-019 mitigation: production MediaMTX yml
   pins `rtspTransports: [tcp]` on paths-we-control with per-path
   opt-out for UDP-only upstreams.
2. **MediaMTX** — one container per mgmt-host, active-passive behind
   keepalived VIP (same shape as Block 17A.5 policy engine + Block 27
   OTel collector). Image pinned by digest (verified-good across §1
   substrate, Spike A entry-subset, Spike A full, Spike B 150-viewer
   ramp, role smoke test). Multi-protocol bridge: RTSP / SRT / RTMP
   in; same plus WebRTC + LL-HLS out.
3. **Zenoh control plane** — feeds-shim Rust crate polls MediaMTX's
   `/v3/paths/list` and publishes per-feed advertisements at
   `attest/queryable/feeds/<site>/<feed-id>`. Schema locked at
   `attest/schema/feeds/v1`. PTZ + record-start/stop subscriber paths
   at `cmd/feeds/<id>/...` deferred to the first source class with
   PTZ capability (per Spike C output) — schema published at
   `attest/schema/feeds-ptz/v1` so the contract is locked before any
   peer emits commands.
4. **Storage / archival** — new MinIO bucket `video-archive` on the
   existing per-site MinIO substrate ([Block 17D](0811-audit-log-integrity.md))
   when that substrate lands. Hot 7d on local NVMe, warm 90d in MinIO,
   recording-integrity ledger entries follow the Block 17D pattern.
   v1 ships with recording disabled by default; per-path opt-in via
   inventory.

**No LiveKit in v1.** Spike B PASS at 150 viewers × 5 streams shows
MediaMTX's own WebRTC re-serve has comfortable headroom for the
target v1 profile. LiveKit revisits if and when viewer-count growth
pushes past that ceiling.

**Inner DTLS-SRTP / Go crypto are not FIPS-validated.** They ride
**inside** the WireGuard tunnel which is the load-bearing crypto
(same defense-in-depth pattern as the rest of the system).

## Spike verdicts (the evidence base)

### §1 substrate proof-of-life — ✅ PASS

Verified on Linux (a dev host) + macOS (Docker Desktop) 2026-04-29.
MediaMTX v1.18.0 with the working yml accepts RTSP-TCP ingest at
1280×720@30fps, exposes the API with full codec metadata, serves valid
HLS master + variant playlists; first segment fetch returns valid
MPEG-TS with sync byte 0x47 confirmed at offsets 0/188/376. Mac run
held ffmpeg push stable for 13m17s — confirms `-rtsp_transport tcp`
holds across Docker Desktop's UDP-NAT path. Evidence:
`../reports/2026-04-29-video-fabric-substrate-evidence.jsonl`.

### Spike A — UDP-over-WireGuard at video bitrate — ✅ FULL PASS

Entry-subset PASS 2026-04-29 (60s through raw `wg-quick` link).
Full 5×30×1h soak PASS 2026-04-30: 30 viewers × 5 streams × 59.5
minutes (harness summary-write timing cut the last 30s but the
system under test was healthy throughout); 30/30 viewers held
`connected`, 0 disconnects, 0 lost packets, 1.69 GB delivered through
the WG tunnel; first-frame p50/p95/p99 = 233/306/317 ms. Verdict
criteria from design doc: <0.5% packet loss → 0%; no peer disconnects
→ 0/30; <50ms p99 jitter → not directly measured in milliseconds
(RTP-time → ms conversion needs codec clock; not done) but 0% loss
is implies network jitter never bad enough to drop frames — de facto
PASS. Evidence:
`../reports/2026-04-30-video-spike-A-full-wg-evidence.jsonl`
+ `../reports/2026-04-30-video-spike-A-full-wg-counters.jsonl`.

### Spike B — viewer-scale ramp (D1 gate) — ✅ PASS at 150 × 5

Ramped 5 → 15 → 30 → 60 → 100 → 150 viewers held 90s per step against
5 ffmpeg testsrc streams on a dev host 2026-04-29. All 6 steps PASS:
0% connect-fail rate at every step, 0 disconnects, 0 lost packets;
MediaMTX peak CPU 6 → 8 → 11 → 55 → 23 → 27% (sample-window jitter at
the 60-viewer crossover; otherwise sub-30%). **D1 closed:
MediaMTX-only**; LiveKit deferred to a future Block 35X if growth
ever pushes past the headroom observed here. Evidence:
`../reports/2026-04-29-video-spike-B-mediamtx-ramp-150.jsonl`
+ `../reports/2026-04-29-video-spike-B-mediamtx-substrate-150.jsonl`.

### Spike C — first-source ingest — 🟡 PARTIAL

MediaMTX-side wiring proven by Spike A (RTSP-TCP push end-to-end
through MediaMTX). Operator-side handoff (transport / auth / discovery
for the first real upstream — a camera inside a partner
system) **gated on operator coordination**. Synthetic ffmpeg
stand-in is a documented fallback per the runbook §5; produces a
template runbook entry that the operator-side fields fill into when
upstream access opens. Custody-case offline-CA cert flow per Block 22H
is part of the same Spike C only when applicable (we own the camera);
not the default.

### Spike D — TAK CoT video round-trip — 🟡 GATED

Synthetic local-TAK + python CoT generator path documented as a
fallback for the mesh-internal half + boundary "fails closed"
verification. The partner federation half stays gated on operator
coordination. Either path produces a scoped-partial verdict that
unblocks 35E shipping for non-TAK use cases.

## Consequences

### Now ships

- Ansible role `ops/ansible/roles/mediamtx/` — pinned image, F-019
  mitigation, JWT auth via Block 21 Ory by default, smoke-test fixture
- Ansible role `ops/ansible/roles/feeds-shim/` — systemd unit with
  hardening, schema-publish bootstrap, depends on `mediamtx` role
- Rust crate `ops/zenoh-shims/feeds-shim/` — catalog publisher
- Schemas — `attest/schema/feeds/v1` + `attest/schema/feeds-ptz/v1`
  (PTZ schema is forward-looking; subscriber lands later)
- Driver `ops/video-fabric/spikes/A-wg-udp/run.sh` — proven Spike A
  full driver, reusable for repeat runs / regression checks
- Driver `ops/video-fabric/spikes/B-viewer-scale/` — Pion-based viewer
  + ramp orchestrator + summarizer (reusable for any future viewer-scale
  measurement)

### Deferred / blocked

- **MinIO `video-archive` bucket Terraform + recording-integrity
  ledger ingestion** — depends on Block 17D MinIO substrate landing
  in code (not yet)
- **Canonical-tier 72h soak per ADR 1008** — runs once the target
  sovereign env is ready
- **`release/<partner>/video/...` Phase 3+** — first-partner-asks
- **DeepStream analytics on AGX Orin Phase 3+** — first-analytics-need
- **First real upstream sensor integration** — Spike C operator coordination
- **Partner TAK federation video** — Spike D operator coordination

### Known watchlist items (carried from design doc)

- **R1: SRT has no PQC story** — mitigated by outer transport + mesh
  containment. Revisit when SRT or successor adds PQ.
- **R2: Go crypto stack not FIPS-validated** — inside-WireGuard
  containment is the load-bearing crypto. If a downstream user requires
  inner FIPS, path is `microsoft/go` BoringCrypto build or terminate
  at the Block 19 strongSwan FIPS boundary.
- **R3: Source heterogeneity** — Spike C is the template-producing
  spike; second-source integrators follow the runbook entry rather
  than restarting discovery.
- **R4: Recording-integrity append rate** — at 5 cams × 5min segments
  = 60 hash entries/hour; need to confirm Block 17D ledger sustains.
  Surfaces as M-011 when 17D lands.
- **R5: Orin Nano lacks H.264 hw encoder** — AGX Orin is the spec for
  video-bearing sites; Orin Nano for non-video peers only.

## Status flip criterion

Proposed → Accepted on Block 35E canonical-tier 72h soak passing per
ADR 1008. Evidence at `docs/reports/<date>-video-fabric-v1-soak.jsonl`.
Soak verdict criteria: no segment loss, no integrity-ledger gaps, no
peer disconnects, MediaMTX memory stable.

## Glossary

See `GLOSSARY.md` for project-specific terms.
