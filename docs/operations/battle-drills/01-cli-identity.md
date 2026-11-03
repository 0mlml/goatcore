# 01 — CLI identity setup

## Symptom

Doctor step 0 reports `profile.context`, `profile.permission`, or
`profile.material` as failed.

## Conditions

You have an issued **goat-cli** bundle and its public signing CA. A mesh-only
device enrollment bundle without goat-cli material cannot initialize this
profile.

## Steps

1. Run `goat context ls` and `goat profile path`. If the intended context
   already exists, select it with `goat context use <name>` and rerun doctor.
2. For first setup, run `goat ca add <bundle-ca.pem>`. Compare its printed
   fingerprint with the one supplied separately by your operator. Then run
   `goat profile init <bundle.cbor>` and rerun doctor.
3. If doctor names a permission failure, repair ownership or use the owning
   account. If it names a missing file in an initialized profile, recover the
   issued bundle and use `goat profile rotate <bundle.cbor>`; `init` refuses
   to overwrite an existing context.

## Pass

`profile.context` passes and doctor advances to local client or network checks.

## Escalate

Ask the issuer for a fresh bundle if verification says it expired, was altered,
or lacks goat-cli material. Do not add an unverified CA to make it pass.

## Evidence

Record the selected context, the error code, and the CA fingerprint. Never
attach the bundle, portal token, private key, or full profile directory.

Source: `crates/goat-cli/src/bundle.rs`, `crates/goat-cli/src/profile.rs`.
