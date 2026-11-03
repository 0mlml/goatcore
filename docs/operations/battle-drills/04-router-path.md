# 04 — Tunnel reported up, router path failed

## Symptom

Doctor step 0 sees a tunnel but step 1 cannot open TCP to the configured
router.

## Conditions

Have the router endpoint from `goat profile show`. A TCP failure may arise at
name resolution, route, firewall, or router service.

## Steps

1. Rerun `goat doctor --all` and note the endpoint and any signed-name
   fallback annotation. Confirm `netbird status -d` or
   `goat client status --detail` still reports the intended connection.
2. Compare the same router endpoint from another healthy mesh peer. If only
   this device fails, inspect its selected client profile, DNS, and route.
3. If both devices fail, have the operator check the router service and
   host firewall. Do not issue a new setup key solely because TCP timed out.

## Pass

Step 1 opens TCP and doctor advances to TLS material and the real session.

## Escalate

An operator needs the endpoint, error, and comparison peer result to separate
a device fault from a goatnet fault.

## Evidence

Provide doctor output, client connection state, endpoint, and comparison
result. Redact token-bearing profile files.

Source: `crates/goat-cli/src/cmd/doctor.rs`, `docs/design/name-fallback-resolver.md`.
