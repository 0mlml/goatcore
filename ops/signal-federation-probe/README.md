# signal-federation-probe

Synthetic-traffic probe for the goat custom federated signal-server.
Dual-use: validation (CGNAT env qualification + future env tests) and
production (per-site canary that catches silent federation breakage before
real user traffic does).

Sends a `signalexchange.SignalExchange/Send` RPC with a synthetic source key
and a `remoteKey` belonging to a peer at the OTHER site. The local signal
server sees `notfound` locally, forwards to its configured federation peer,
and returns `delivered` on success. Result is reflected in
`signal_server_forward_sent_total{outcome="delivered"}` on the target signal.

## Modes

```
--mode=validation   # one-shot; exit 0 on delivered, 1 on any other outcome
--mode=canary       # loop on --interval; expose Prometheus metrics on --metrics-addr
--mode=diagnostic   # one-shot, verbose JSON log of the round-trip
```

## Usage

### Validation (CGNAT env qualification, ad-hoc env smoke)

```
signal-federation-probe \
  --mode=validation \
  --target=localhost:10000 \
  --remote-key=<wg-pubkey-from-OTHER-site> \
  --source-key=<dummy-or-real-wg-pubkey>
```

Exit code = pass/fail. Use in CI / soak driver / Ansible smoke checks.

### Canary (production per-site cron)

```
signal-federation-probe \
  --mode=canary \
  --target=localhost:10000 \
  --remote-key=<wg-pubkey-from-PEER-SITE> \
  --interval=60s \
  --metrics-addr=:9091
```

Loops forever. Exposes:

- `federation_probe_attempts_total{outcome=...}` — outcomes per probe
- `federation_probe_latency_seconds` — histogram of round-trip latency
- `federation_probe_last_success_unix_timestamp` — gauge for "is federation alive"

Pair with a Prometheus alert on `time() - federation_probe_last_success_unix_timestamp > 300`
to page when federation has been silently broken for >5 min.

### Diagnostic (operator-on-demand)

```
signal-federation-probe \
  --mode=diagnostic \
  --target=localhost:10000 \
  --remote-key=<wg-pubkey-from-OTHER-site>
```

Prints structured JSON of the probe call, response body, and signal-a metric
delta before/after. Use when investigating "did federation forward fire for
this specific request?" or "why is cross-site auth failing?"

## Production deployment

Land via `ops/ansible/roles/signal-federation-canary/`. One canary per site.
Targets the federation peer endpoint (the OTHER site's signal). `--remote-key`
should be a stable, well-known peer at the other site (e.g., the mgmt-host's
own WireGuard key, or a dedicated synthetic-canary peer).

## Validation env usage

Already qualified the CGNAT validation env — see
`docs/project/validation-state.md` "CGNAT signal federation forward (Phase 3 gate)"
section for evidence.
