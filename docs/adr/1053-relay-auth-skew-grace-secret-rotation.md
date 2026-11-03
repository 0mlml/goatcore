# ADR 1053 — goat-relay auth: clock-skew grace on expiry + secret-rotation acceptance window

**Status:** Accepted — built in code (track `relay-flap-fix`); behavioral validation via integration test P2 + measurement M-072 before fleet rotation.
**Repo scope:** goat core — `goat-relay/src/auth.rs`, `goat-relay/src/main.rs`.
**Related:** ADR 1021 (the timed-HMAC auth this hardens) · F-221 · [ADR 1037](1037-fabric-internal-time-synchronization-first.md) (the fleet time-hygiene context that makes skew real) · [ADR 1044](1044-credential-rotation-in-place-reissue.md) (the rotation discipline this mirrors at the relay layer).

## Plain-language summary

The relay accepts a client only if its short-lived signed token checks out. Two routine operational events used to break that check: (1) a relay whose clock runs slightly ahead would call a still-valid token "expired"; (2) rotating the per-site relay secret would instantly reject every token signed with the old one. This ADR adds a small clock-skew grace on the expiry comparison and a rotation window where the relay accepts tokens signed by either the current or the previous secret.

## Context

- Token expiry was a hard, unslewed `now_unix() > expiry`. The fleet has live clock-hygiene concerns (ADR 1037, chrony-presync, ntp-health); a relay host ahead of the minter rejects valid near-expiry tokens.
- The HMAC key was derived once from a single secret — no overlap on rotation.
- Established sessions are not re-validated, so the blast radius is reconnects; but reconnect-failure is exactly what turns a transient flap into a persistent outage.

## Decision

1. **Skew grace:** reject only when `now > expiry + CLOCK_SKEW_GRACE_SECS` (60s, saturating). Tolerates a relay clock up to the grace ahead of the minter. No "not-before" check is added — the token carries no issued-at/nbf field.
2. **Rotation window:** the validator holds one-or-more HMAC keys; a token is valid if its MAC verifies against any held key. `with_rotation(current, previous, policy)` + an optional `GOAT_RELAY_AUTH_SECRET_PREVIOUS` env build a relay that accepts both during a rotation; drop the previous once the fleet has re-minted on the current.

## Alternatives considered

- **No grace / hard cutover (status quo).** Rejected — makes clock drift and key rotation into outages.
- **Larger grace (e.g. 5 min).** Rejected — every second of grace is a second a truly-expired token still works; 60s covers realistic relay-vs-minter skew without materially extending token lifetime.
- **N-key ring / unbounded previous secrets.** Deferred — two keys (current+previous) covers a standard rotation; more is unneeded complexity.

## Consequences

- Routine secret rotation and ordinary clock skew no longer lock peers out.
- A token remains usable up to 60s past its nominal expiry — an accepted, bounded extension of effective lifetime, far smaller than the token TTL.
- The rotation window is opt-in per relay via env; absent the previous-secret env, behavior is unchanged (single key).
- Per-reason auth-failure classification (expired vs bad-sig vs skew vs unbound-rejected) for the metrics/alerting surface is tracked in the relay observability workstream (ADR 1055 / F-228), not here.
