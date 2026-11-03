# 06 — Router reachable, mTLS session did not open

## Symptom

Doctor step 1 reaches the router and step 2 validates local material, but
step 3 fails.

## Conditions

The session probe performs the TLS and Zenoh open. A timeout, transport
failure, handshake rejection, and unknown failure are different evidence.

## Steps

1. Read step 3's cause and `goat profile show`. A timeout calls for a router
   health and path comparison; it does not prove certificate rejection.
2. If doctor reports a handshake or admission-shaped rejection while the
   local certificate is valid, give the operator the result to check the
   router's trust and authorization for this identity.
3. Rerun doctor after the operator changes the admission rule or issues a
   replacement bundle. Do not bounce the mesh for a proven admission refusal.

## Pass

Step 3 opens and closes a real Zenoh session, then doctor reaches the portal.

## Escalate

The operator checks router logs and the admitted identity. If the reason is
unknown, preserve that uncertainty instead of labeling it an ACL denial.

## Evidence

Provide the step 3 cause, bounded error text, router endpoint, cert expiry,
and timestamp. Never provide the client private key.

Source: `crates/goat-cli/src/probe.rs`, `crates/goat-cli/src/classify.rs`.
