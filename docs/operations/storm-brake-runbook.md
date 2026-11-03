# Storm brake runbook — `goat admin storm brake`

The manual operator brake for a control-plane signaling storm. Layer 4 of
[ADR 1081](../adr/1081-control-plane-storm-resilience.md)
([design](../design/control-plane-storm-resilience.md)). **Manual phase only** —
there is no automation, no timer, no auto-engage. You fire it; you revert it.

It is a **safety net, not a cure.** M-085
proves a signal flush alone does not restore the mesh: the storm re-forms.
Layers 1-2 (client retry backoff + signal-server shedding) are the fix; the
brake buys recovery time while those decay the load.

## When to fire

Read the trigger off the relay-health **host-net band** (per site,
`relay-health.<site>.<zone>`; relay-health-dashboard.md
§"Site network hosts"). Fire when:

| signal | source field | engage |
|---|---|---|
| signaling futility | `control.signal_notfound_60s / signal_msgs_60s` | ≥ 0.5 with ≥ 100 msgs/min |
| uplink loss | `wan.loss_pct` | ≥ 5% sustained 10 min |
| tunnel flap | `tunnels.errs_15m` | ≥ 30 (corroborating, not required) |

The futility ratio is the discriminator: "many messages, delivered" is a
healthy reconnect surge and needs **no** brake; "many, undeliverable" is the
storm. Do not fire on message volume alone.

Preview any time without touching the host:

```
goat admin storm brake --dry-run
```

That prints the plan and the trigger guidance and exits.

## What it does

`goat admin storm brake [--dry-run] [--yes]` runs four steps in order against
the mgmt host, over SSH (host + user resolved from the active goat context —
`--mgmt-host` / `$GOAT_MGMT_SSH_HOST` / `$<CONTEXT>_MGMT_SSH_HOST` /
`mgmt_ssh_host` in the context `profile.toml`; user defaults to `goat`).
The goatnet is the active `--context` / `$GOAT_CONTEXT`.

1. **FLUSH** — `docker restart <signal-container>` (default `signal-a`). Drops
   the zombie signaling streams so registration state re-converges. The
   load-bearing step: if it fails, the command exits nonzero.
2. **TIGHTEN** — clamps the Layer-2 signal limiter for a bounded,
   operator-reverted window, **if L2 is deployed**. The two knobs tighten in
   opposite directions: `SIGNAL_SENDER_RATE_LIMIT` is **halved** (lower =
   stricter) and `SIGNAL_NEGATIVE_CACHE_TTL` is **doubled** (higher = more
   shedding — longer offline memory, fewer expensive federation re-probes); a
   knob that is off (0) is enabled at the L2 baseline (rate 30 / TTL 20). It
   detects an L2-capable build by probing the signal server's own
   `/metrics` (`http://127.0.0.1:9090/metrics`) for the
   `signal_server_send_rate_limited_total` counter, which only exists when an
   L2 binary is running — the true capability signal (env-var presence does not
   discriminate, since the L2 compose sets the vars to 0 even on a pre-L2
   image). When the metric is absent (pre-L2 image, or signal down — the common
   case today) it is a clean **skip** with a message, never an error. When
   applied, it writes the new values to `<compose-dir>/storm-brake.env` and
   recreates the service (container env is fixed at create time — no hot
   reload); the step prints the exact revert line.
3. **SHED** — `sudo systemctl stop <unit>` for each declared non-essential
   publisher. The list comes from `storm_brake_shed_units` in the context
   `profile.toml` (a comma-separated string) and/or repeated `--shed-unit`
   flags. If none are declared, SHED is a no-op with a note.
4. **ANNOUNCE** — emits one `OperatorActionV1` through the audited `goat admin`
   chokepoint ([operator-action-audit.md](../design/operator-action-audit.md))
   carrying the per-step outcomes (`ok` / `partial` / `failed`). Best-effort:
   an audit failure never changes the brake's own exit.

Each step reports its outcome honestly — `ok`, `failed <reason>`, or
`skipped <reason>`. A failed TIGHTEN or SHED is reported but not fatal; only a
failed FLUSH makes the command exit nonzero (mesh recovery is not expected if
the signal container did not restart).

## What it does NOT fix

- **It does not stop the storm from re-forming.** M-085: flush alone is
  insufficient. Without Layers 1-2, the blind-retry load rebuilds within
  minutes. The brake is not a substitute for deploying the client backoff
  patches (L1) and the signal-shedding env flags (L2).
- **It does not shape the uplink.** Bufferbloat and control-vs-bulk priority
  are Layer 3 (Thor egress `cake`/BBR) — a host baseline change, not this
  command.
- **It does not touch the fabric.** No Zenoh QoS, no DSCP — ADR 1033 governs
  fabric classes and is untouched.

## Rollback

The brake **is** a command — there is no persistent "brake engaged" state to
disengage, and **no auto-mode to turn off** (auto-engage with hysteresis + a
once-per-hour cap is a separately-gated follow-on that does not exist yet).
Reverse its effects manually once the storm clears:

- **TIGHTEN** — remove the override and recreate at full limits:
  `rm <compose-dir>/storm-brake.env && cd <compose-dir> && docker compose up -d <signal-container>`
  (the TIGHTEN step prints this exact line when it applies).
- **SHED** — restart each paused unit: `sudo systemctl start <unit>`.
- **FLUSH** — nothing to revert; the container is already running post-restart.

Confirm recovery on the same host-net band: futility should fall below ~0.1
within ~5 min. If it does not, the cure is Layers 1-2 — escalate to deploying
those, not to re-firing the brake.
