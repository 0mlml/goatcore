# 05 — Local TLS material is missing, invalid, or out of time

## Symptom

Doctor step 2 fails `zenoh-tls-material`.

## Conditions

Step 2 checks local certificate and CA PEM files and the leaf validity
interval. It does not assert that a remote router trusts them; that requires
the step 3 session.

## Steps

1. Run `goat profile show` and read the named file and certificate expiry in
   doctor's result. Check step 0's measured clock before treating a future
   or expired validity result as a bad certificate.
2. If an issued profile file is missing or malformed, recover the original
   bundle and use `goat profile rotate <bundle.cbor>`.
3. If the certificate genuinely expired, ask the operator for a new bundle,
   then rotate. `goat ca add` affects bundle-signer trust at import time; it
   does not repair the profile's router CA file.

## Pass

Step 2 passes and doctor attempts a real mTLS session in step 3.

## Escalate

If a fresh, verified bundle still installs unusable material, report the
bundle issuer and error code to the operator.

## Evidence

Provide file names, certificate validity times, and the doctor cause. Do not
share the private key or bundle.

Source: `crates/goat-cli/src/cmd/doctor.rs`, `crates/goat-cli/src/cmd/profile.rs`.
