# Offline Enrollment

Go module implementing the offline-enrollment CA tooling and bundle format for Block 2C (Phase 2A).

Design: `docs/design/offline-enrollment.md`.
Runbook: `docs/operations/enrollment-bundle-runbook.md`.
Plan: `docs/project/implementation-plan.md` Block 2C.

## Current state (Week 1)

- `bundle/` — `EnrollmentBundle` + `CRLEntry` with canonical CBOR + Ed25519 signing / verification / activation-deadline + expiry checks.
- `ca/` — `Signer` interface with two backends:
  - `AgeSigner` — age-encrypted file (dev mode), usable immediately.
  - `YubikeySigner` — production backend.
- `cmd/ca-init/` — CLI that bootstraps a dev CA: generates Ed25519 key, age-encrypts to the configured recipients, emits PEM public key for distribution.

What is explicitly **not** in Week 1: the `issue-bundle` CLI, the validator sidecar, the CRL HTTP endpoint, device-side cloud-init. Those land in Weeks 2-5 per the implementation plan.

## Developing

```
cd ops/enrollment
go test ./...
go vet ./...
```

No mesh dependencies. Tests use in-memory keypairs and `t.TempDir()` scratch.

## Dev CA quickstart

```
# Pick (or create) an age X25519 identity to encrypt the CA private key,
# plus a second recipient (the break-glass key). ca-init refuses fewer than
# two recipients: a CA key encrypted to one identity is lost with it.
age-keygen -o ~/.config/age/dev-ca.key
RECIPIENT=$(grep '# public key:' ~/.config/age/dev-ca.key | cut -d: -f2 | tr -d ' ')
BREAK_GLASS=<break-glass-age-public-key>

mkdir -p ca/dev public-keys
go run ./cmd/ca-init \
    --mode dev \
    --out ca/dev/ca.key.age \
    --recipients "$RECIPIENT,$BREAK_GLASS" \
    --public-key-dir public-keys

# Check an existing CA without minting (recipient count, root CN, and that
# the public key is the root's):
go run ./cmd/ca-init --verify \
    --out ca/dev/ca.key.age \
    --root-cert ca/dev/root-cert.pem \
    --public-key public-keys/<ca_id>.pem

# ca-init writes public-keys/<ca_id>.pem and refuses to overwrite one.
# Commit public-keys/*.pem. Do NOT commit ca/dev/*.key.age.
```

Gate to switch off dev mode — all must be true before any bundle signed by a YubiKey production CA ships:

1. YubiKey 5.7.0+ hardware in hand
2. Firmware verified (`ykman info` shows `Firmware version: 5.7.x`)
3. Production ceremony dry-run completed
4. Production CA public key distributed to management hosts

See `docs/operations/enrollment-bundle-runbook.md` §"Dev-mode CA" for the full procedure.

## Threat model pointer

Dev mode is **not safe to ship to production**. The age-encrypted file is weaker than a hardware token — anyone with the age recipient key plus repo access can mint bundles. Dev bundles carry `ca_id` with a `dev-` prefix so provenance is visible in audit logs. Dev public keys are removed from `public-keys/` after the 7-day rollover period at promotion.
