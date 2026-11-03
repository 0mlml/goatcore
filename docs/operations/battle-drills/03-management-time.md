# 03 — Management endpoint or device time

## Symptom

Doctor warns on `management.reachability` or fails `clock.skew`.

## Conditions

Use the management URL reported by the installed client. A connected peer
can remain usable during a management outage; do not infer data-plane failure
from management alone.

## Steps

1. Inspect `netbird status -d` or `goat client diagnostics` for the configured
   endpoint and any connection error. Check DNS and HTTPS to that exact URL.
2. If doctor measured skew over 60 seconds from the endpoint's HTTP Date,
   synchronize the device clock using its OS time service and rerun doctor.
3. If the Date header was unavailable, treat clock health as unknown and use
   another trusted time source before attributing a TLS error to skew.

## Pass

The configured endpoint answers and doctor measures clock difference within
60 seconds, or the data-plane probes pass despite a documented management
outage.

## Escalate

If the endpoint returns a server error or remains unreachable from multiple
clients, ask the goatnet operator to inspect the management service.

## Evidence

Provide the URL without credentials, HTTP status or connection error, and
local/remote time observations. Never provide a setup key.

Source: `ops/preflight-enrollment.sh`, `docs/findings/F-269-netbird-profile-drift-stale-relay-token-single-peer-isolation.md`.
