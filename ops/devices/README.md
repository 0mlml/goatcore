# ops/devices/ — per-device wg-cp0 bundle manifests

Substrate for `goat admin client onboard <handle>` (Phase F.4 — see
`docs/design/goat-cli.md`).

## What lives here

| Path | Purpose | Committed? |
|---|---|---|
| `schema/device-manifest.schema.json` | JSON Schema for per-device manifests. | ✅ |
| `validate.py` | Validator (used by `goat admin client onboard` pre-flight + CI). | ✅ |
| `example.yml.example` | Canonical template — copy + edit per device. | ✅ |
| `<handle>.yml` | Per-device manifest, gitignored (carries device identity). | ❌ |
| `out/` | Minted bundle output dir, gitignored. | ❌ |

## Workflow

```bash
# 1. Author a manifest for a new device:
cp ops/devices/example.yml.example ops/devices/op-laptop.yml
$EDITOR ops/devices/op-laptop.yml

# 2. Validate:
ops/devices/validate.py ops/devices/op-laptop.yml

# 3. Mint the bundle (requires admin scope + offline-CA access):
goat admin client onboard op-laptop

# 4. Operator drops the bundle onto the device:
goat client bundle import out/op-laptop-device.cbor  # on the device
goat client up                                              # device-side
```

## Per-site fleet config is NOT here

CA-id, age-identity / piv-slot, trust-anchor path, snitch-collector —
fleet config that doesn't belong in per-device manifests. The
`goat admin client onboard` wrapper resolves those from env vars +
CLI flags (`--ca-id`, `--age-identity`, `--mode`, etc.). Keep this
directory device-scoped only.

## Mirror of ops/operators/ convention

This directory parallels [`ops/operators/`](../operators/) (Block 83
operator manifests). Same gitignore shape, same schema-validator-
example structure. Devices and operators share none of their
substrate (devices wrap the wg-cp0 outer tunnel; operators carry
identity across NetBird + Ory + Zenoh), but the manifest authoring
ergonomics are identical.
