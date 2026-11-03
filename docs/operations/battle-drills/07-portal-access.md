# 07 — Portal reachability or authorization

## Symptom

Doctor's router and session checks pass, but step 4 fails.

## Conditions

Step 4 calls the portal API with the selected profile's Bearer token. HTTP
401, HTTP 403, network failure, and an unreadable success response imply
different remedies.

## Steps

1. Run `goat profile show` and read step 4's cause. Confirm the portal
   endpoint belongs to the intended context.
2. For HTTP 401, ask for a fresh goat-cli bundle and run
   `goat profile rotate <new-bundle.cbor>`. For HTTP 403, ask the operator to
   check this identity's grant to the probed API; rotating the same grant is
   not a fix.
3. For a network error, compare the portal endpoint from another mesh peer.
   For a response decode error, update to a compatible `goat` release and
   rerun doctor.

## Pass

Step 4 receives and decodes an authorized portal response.

## Escalate

Give the operator the status or cause and endpoint. Distinguish a permission
refusal from an unavailable service in the report.

## Evidence

Provide step 4's status, cause, selected context, goat version, and time.
Never provide the Bearer token or the full bundle.

Source: `crates/goat-cli/src/cmd/doctor.rs`, `docs/design/goat-cli-error-codes.md`.
