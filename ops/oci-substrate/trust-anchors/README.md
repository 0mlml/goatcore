# OCI substrate trust anchors

Cosign trust anchors for verifying images at every tier. See
design doc §7 Cosign provenance.

| Anchor file | Verifies | Source |
|---|---|---|
| `goat-first-party.pem` | `oci.goat/<service>` images we build ourselves | Offline-CA root per ADR 0406. |
| `chainguard-sigstore.json` | Chainguard images via sigstore-keyless verification | Chainguard's public sigstore identity policy — verified via Rekor transparency log + GitHub Actions OIDC issuer |
| `nvidia.pem` | NVIDIA NGC images (`nvcr.io/...`) | NVIDIA's public container signing key, distributed via NVIDIA NGC docs |
| `canonical.pem` | Canonical Ubuntu Pro FIPS images | Canonical's public signing key |

## When each anchor lands

- `goat-first-party.pem` — lands when the first-party signing
  ceremony fires.
- `chainguard-sigstore.json` — lands with 75F third-party sweep when
  the first Chainguard image enters the canonical-set
- `nvidia.pem` — lands with Block 35-L6 DeepStream or Block 74 prebake
  use of nvcr.io
- `canonical.pem` — lands when first Ubuntu Pro FIPS image enters
  the canonical-set

## Verification

Each anchor file is consumed by `cosign verify` at the consumer.
Example for first-party:

```bash
cosign verify --key goat-first-party.pem oci.goat/admit-audit@sha256:...
```

For sigstore-keyless (Chainguard):

```bash
cosign verify --certificate-oidc-issuer=$(jq -r .oidc_issuer chainguard-sigstore.json) \
  --certificate-identity-regexp=$(jq -r .identity_regex chainguard-sigstore.json) \
  cgr.dev/chainguard/postgres@sha256:...
```

## Rotation

Per design doc §9.5. Anchor rotation is operator-fired; new key
overwrites this file in a PR; CI nightly drift sweep validates
consumer-side verifications still succeed.
