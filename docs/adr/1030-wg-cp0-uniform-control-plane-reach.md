# ADR 1030 — Uniform control-plane reach: wg-cp0 hosted on the tactical edge node, relay forwards-or-drops

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-06-06 |
| Repo scope | goat core (wg-cp0 role + relay + bundle) + `goat-maintainer/goat-client` (endpoint selection) |
| Related | [0313](0313-silent-control-plane-outer-wg-wrap.md) (silent control plane), [0314](0314-no-public-dns-for-control-plane.md) (no public DNS), 0316 (mixed relay fleet), [0840](0840-goat-client-cross-platform-daemon-gui.md) (goat-client — the wg-cp0 client), F-121 (signal_uri reachability), `wg-cp0-prod.md`, `relay-paths.md` |

## TL;DR (for a layperson)

A device joining our private network reaches a "control desk" (enrollment + connection-brokering) behind a silent lock (`wg-cp0`). Today that lock is hosted only on **cloud** servers, so two devices in the same room sign in by going out to the cloud and back — and if the satellite link drops, they can't sign in new connections (a dent in island mode).

The fix is **one uniform mechanism**: host the silent lock on the **on-site control desk (the tactical edge node) itself** — the single place it lives. The cloud relay stops being a lock and becomes a **doorway that either passes a sealed message through to the on-site lock or throws it out**; it never opens the message. A device reaches the *same* on-site lock whether it's on the local network (direct) or remote (through the relay), so it works **identically everywhere** — inside the LAN or outside, with internet or without (on the LAN, the on-site lock is reachable with no internet at all). Our client picks the nearest doorway automatically. No new hardware, no new public exposure, no per-device fiddling.

## Context

`wg-cp0` is the silent outer WireGuard control plane ([ADR 0313](0313-silent-control-plane-outer-wg-wrap.md)/[0314](0314-no-public-dns-for-control-plane.md)); it exists because the netbird daemon dials `Signal.URI` over its vanilla stack **before the mesh is up** (F-121), so a pre-mesh transport is required.

Today the `wg-cp0` server runs on the **cloud relays**, and `Signal.URI = 198.18.0.x`, so every peer reaches signal/mgmt over `wg-cp0`. With cloud-only relays an on-LAN peer **hairpins to a cloud relay and back**; during an uplink outage new-connection setup is impaired (`ha-multi-site.md`: *"isolated sites … cannot enroll new peers … until connectivity returns"*). That is the island-mode gap.

The requirement: **one technology that works identically inside the LAN or outside, and inside the LAN with or without internet.** A patchwork (fixed peers use a local hub, operators hairpin, operators hand-edit bundles) fails that bar.

Two existing pieces make the uniform fix natural: the enrollment bundle already carries an **endpoint set** (`known_endpoints[]`, ADR 0316), and **[goat-client](0840-goat-client-cross-platform-daemon-gui.md)** is the client that brings the `wg-cp0` tunnel up from it.

## Decision

The control plane has **one** `wg-cp0` server, on the **tactical edge node (the mgmt-host / Orin)** — the single silent door. The relay's role changes from terminator to forwarder.

1. **wg-cp0 is hosted on the edge node — and only there.** The Orin runs the `wg-cp0-server` role and owns `198.18.0.1`. It is the **only** place a `wg-cp0` session is decrypted/terminated. It is silent (drops unauthenticated handshakes) and exposes no public mgmt listener. On-LAN peers reach it directly over the LAN, with **no internet required**.

2. **One identity, many endpoints.** A peer holds **one** `wg-cp0` `[Peer]` — the edge node. Its endpoint is the edge node's LAN address when on-LAN, or a cloud relay's public address when off-LAN. Because the identity never changes (only the path does), the endpoint **roams with no `AllowedIPs` overlap, ever**.

3. **The cloud relay forwards or drops — it does not terminate.** It passes **still-encrypted** `wg-cp0` packets through to the edge node (never decrypting them), and **drops anything that isn't a valid attempt to that edge node at the cloud edge**, so junk never travels down the tactical uplink. The edge node stays the single silent door; the relay can read nothing.

4. **goat-client selects the endpoint** ([ADR 0840](0840-goat-client-cross-platform-daemon-gui.md), implemented in `goat-maintainer/goat-client`): probe the bundle's endpoint set, prefer the on-site edge node when reachable, fall back to a cloud relay when off-LAN, re-select on network change. Automatic, uniform, **no per-operator action**.

Uniform across all conditions:

| Device location | Internet | Path to the (one) edge-node lock |
|---|---|---|
| Inside LAN | yes | direct over the LAN |
| Inside LAN | **no** | direct over the LAN — **island works** |
| Outside LAN | yes | through a cloud relay (forward-or-drop) |

**Fixed on-LAN infrastructure** (sensors, effectors — they don't move) needs no selection logic: a static bundle with the edge node's LAN address as endpoint. goat-client's active selection is for **devices that move** (laptops, mobile) — exactly goat-client's user class. Same substrate for both; only static-vs-managed config differs.

*Implementation note (not part of the model):* to forward to an edge node that has no public IP, the relay bridges the peer's outbound connection to the edge node's outbound connection — **the same way our data relay (`rels://`) already bridges two peers without public IPs**. This is existing tech, not a new design; specifics live in the plan.

### Cross-repo split

Decision here. Implementation spans two repos ([ADR 0219](0219-multi-repo-iac-state-separation-coordinated-compliance.md) pattern): the edge-node `wg-cp0-server` + the relay forward-or-drop change + bundle endpoint-set land in goat core; the **active endpoint selection + network-change re-selection** lands in `goat-maintainer/goat-client` (governed by [ADR 0840](0840-goat-client-cross-platform-daemon-gui.md); add a short 0840 amendment when scheduled). No ADR is created in the goat-client repo — ADRs are centralized here.

## Alternatives considered

- **Host wg-cp0 in two places (terminating relays + edge hub, client swaps between them).** Rejected — two identities, the relay decrypts the outer tunnel, and "uniform" only at the surface. The single-identity-at-the-edge model is cleaner and keeps the relay unable to read anything.
- **Asymmetric on-LAN bypass of `wg-cp0`** (on-LAN peers hit signal/mgmt on the LAN IP directly). Rejected — needs a per-peer `Signal.URI` (a third netbird fork) and re-exposes a LAN control-plane listener (erodes [ADR 0313](0313-silent-control-plane-outer-wg-wrap.md)).
- **Per-device manual endpoint config / per-peer bolt-on agent.** Rejected — roaming is too critical for per-operator diligence; selection belongs in goat-client, uniform by construction.

## Consequences

- **Positive.** One uniform mechanism across inside/outside LAN and with/without internet, automatic and per-peer-action-free. Single silent door at the edge; the relay can read nothing (a privacy gain over today's terminating relay). Island-mode control plane with zero new hardware on the edge side. Reuses the bundle endpoint set and the existing data-relay bridge pattern.
- **Cost.** The relay changes from terminating wg-cp0 to forward-or-drop (the one real new piece — it must drop junk cheaply at the cloud edge so the tactical uplink is never burdened, and bridge to the no-public-IP edge node). goat-client gains active endpoint selection (aligns with its v0.2 work, [ADR 0840](0840-goat-client-cross-platform-daemon-gui.md)). The edge node runs the `wg-cp0-server` role.
- **Boundary.** No change to the data plane, Zenoh, or [ADR 0313](0313-silent-control-plane-outer-wg-wrap.md). Inter-Orin transport (Spock, signal-federation, F-060) is out of scope here — this ADR is about **peer → control-plane** reach. Non-goat-client roaming peers are out of scope (fixed infra uses static config; movers use goat-client).

## Implementation status / plan

Not started. Phased plan in `docs/project/wg-cp0-local-hub-plan.md`; backlog row `block-22i-tier2-local-physical-relay` updated to point here.

See GLOSSARY.md for acronyms.
