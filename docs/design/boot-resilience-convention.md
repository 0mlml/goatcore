# Boot-resilience convention

**One job:** keep a long-lived service from starting into a not-yet-ready
network at boot and then **staying wedged** with no self-recovery. This is the
"connect once at boot, then die" class — the most-repeated operational failure
shape on the fleet.

## Status: the class is fixed per-service; this codifies the convention

Every *known* instance is already fixed where it lives — there is **no shared
role to build and no scatter to consolidate** (verified 2026-07-29):

| Instance | Service | Fix (in place) |
|---|---|---|
| F-261 | netbird | `network-online` drop-in + probe-driven self-heal watchdog (`netbird-boot-selfheal`) |
| F-203 | audit-subscriber | retry-forever reconnect |
| F-200 | SPIRE agent | `agent_ttl=360h` (join token survives downtime) |
| F-202 | SPIRE trust bundle | `spire-trust-sync.timer` |
| F-251 | RTC-less Jetson | `fake-hwclock` (cold boots don't land in 1970 → mTLS survives) |

The ordering directives and restart policies these use are **one-line systemd
config, correctly local to each unit** — not harmful duplication. A shared
"boot-resilience" role that centralized them would be over-abstraction (a role
dependency for what systemd expresses in a line), and would ship with no
consumer since the failures are already fixed. So the durable artifact is this
**convention**, applied by every new long-lived service, not a role.

## The convention — three tiers, reach only as far as the failure mode needs

**Tier 1 — order after the network is actually up (default for any networked unit).**
`network.target` fires before routes/DNS are usable; that race is the root cause.

```ini
[Unit]
After=network-online.target
Wants=network-online.target
```

**Tier 2 — let it recover from a crash (default).**
```ini
[Service]
Restart=always
RestartSec=5        # tune per service
```
Containers: `restart: unless-stopped` in compose. This covers the *crash* case.

**Tier 3 — self-heal a functional wedge (only when a plain restart can't catch it).**
Add a probe-driven watchdog **only** for the "**process is `active`/`Up` but its
actual function is dead**" mode — where Tier 2 never fires because nothing
crashed. Model it on the reference
implementation — do not invent a new shape:

- **Reference:** [`netbird-boot-selfheal`](../../ops/ansible/roles/netbird-boot-selfheal/) + [netbird-boot-selfheal.md](netbird-boot-selfheal.md).
- **Shape:** a timer runs a **functional probe**; if the control plane is up but
  the service stays functionally-wedged for N consecutive samples, restart it
  **once** — rate-limited (cooldown) + **capped per boot** + emitting an
  off-host textfile metric so the wedge is visible externally (the incident's
  whole blind spot). Never restart when the real dependency is down (a restart
  can't fix an upstream outage).

Do **not** build a Tier-3 watchdog speculatively. Add one when a real wedge mode
appears. If a *second* service needs the same watchdog, that is the moment to
generalize the netbird script into a reusable role (rule of three) — not before.

## Time is a boot dependency too

RTC-less nodes (Jetson) cold-boot to 1970, which fails every cert/TLS check
before NTP corrects it. `fake-hwclock` (in the `ntp-hygiene` role)
persists a monotonic-safe wall clock across reboots so a denied-edge node comes
up trusting itself. Any offline-capable node inherits this.

## Adoption checklist (new long-lived service)

1. Networked unit → Tier 1 ordering. Always.
2. Restart policy → Tier 2. Always.
3. A "healthy-but-wedged" failure mode a crash-restart won't catch → Tier 3
   watchdog, modeled on `netbird-boot-selfheal`. Only if the mode is real.
4. Offline-capable node → confirm `fake-hwclock` (via `ntp-hygiene`).

## Open

(omitted)
