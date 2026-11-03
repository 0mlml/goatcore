# Closed-LAN bootstrap pack — layout spec

Specifies the USB-supplied directory the [`tier-t-install.sh`](tier-t-install.sh) helper consumes. The pack is produced by the operator on their FDE-on laptop and carried into the closed LAN on physical media.

## Directory tree

```
<pack-root>/
├── ca/
│   └── nova-lan-ca-root.pem     # required. ECDSA P-256 self-signed root cert
│                              # from ops/enrollment/ca/nova-lan/root-cert.pem.
├── dns/
│   └── resolver-ip.txt        # required. Single line, no trailing newline,
│                              # the LAN IP of nova-lan-A (where lan-coredns is
│                              # serving on UDP port 53). e.g. "10.10.0.42".
└── manifest.yml               # required. See § manifest.yml below.
```

## `manifest.yml`

```yaml
schema_version: 1

# Identifier of the closed-LAN instance this pack targets. Must match
# the trust-domain identifier the CA was minted under.
trust_domain: nova-lan-instance

# CA-ID of the root cert in ca/. Tracks the ceremony date so multiple
# packs can be distinguished by which root they install.
ca_id: dev-nova-lan-ca-ecdsa-<YYYY-MM-DD>

# SHA-256 of ca/nova-lan-ca-root.pem. tier-t-install.sh refuses to
# proceed if this doesn't match what's on disk — guards against silent
# pack tampering on the USB.
ca_sha256: <64-hex-chars>

# Mirror of dns/resolver-ip.txt for one-stop validation.
resolver_ip: <nova-lan-a-lan-ip>

# Provenance metadata. Non-load-bearing, audit-only.
minted_at: 2026-MM-DDThh:mm:ssZ
minted_by: <operator-handle>
```

## How the operator produces a pack

One-shot script (lives at the operator's discretion; not committed to the repo because it touches the post-ceremony root-cert which lives in operator state):

```bash
#!/usr/bin/env bash
set -euo pipefail
CA_PEM=ops/enrollment/ca/nova-lan/root-cert.pem
CA_ID=dev-nova-lan-ca-ecdsa-2026-MM-DD
RESOLVER_IP=10.10.0.42
OPERATOR=maintainer

STAGE=$(mktemp -d)
mkdir -p "$STAGE/ca" "$STAGE/dns"

cp "$CA_PEM" "$STAGE/ca/nova-lan-ca-root.pem"
printf '%s' "$RESOLVER_IP" > "$STAGE/dns/resolver-ip.txt"

CA_SHA=$(sha256sum "$STAGE/ca/nova-lan-ca-root.pem" | awk '{print $1}')

cat > "$STAGE/manifest.yml" <<EOM
schema_version: 1
trust_domain: nova-lan-instance
ca_id: ${CA_ID}
ca_sha256: ${CA_SHA}
resolver_ip: ${RESOLVER_IP}
minted_at: $(date -u +%Y-%m-%dT%H:%M:%SZ)
minted_by: ${OPERATOR}
EOM

OUT=~/.goat/state/nova-lan-bootstrap-packs/nova-lan-bootstrap-$(date +%Y%m%d).tar.gz
mkdir -p "$(dirname "$OUT")"
tar -czf "$OUT" -C "$STAGE" .
rm -rf "$STAGE"

ls -la "$OUT"
shasum -a 256 "$OUT"
```

The output tarball lives under the operator's `~/.goat/state/` (same parent as the CA private key, leaves, and onboarding packets — sibling to the nova convention). Carry it onto the LAN on the USB stick that already carries the install image; one stick covers the whole closed-LAN onboarding flow.

## What's deliberately NOT in the pack

- **No private keys** — neither CA private key nor any leaf key. Leaves are minted per `docs/operations/nova-lan-cert-issuance.md` and hand-delivered as separate bundles (one per consumer).
- **No service images** — closed-LAN image distribution is via the on-box T2 mirror (Forgejo + registry-mirror), not this pack. The pack only addresses Tier-T client *participation*, not the mgmt-host substrate itself.
- **No `age` encryption layer** — closed-LAN posture treats physical-media handling as the threat model. If a deployment surfaces a stronger requirement (multi-operator handling, untrusted transit), wrap the tarball in `age` with operator pubkey(s) before carrying.
- **No userdata schema with site/profile/scenario** — none of those apply to Tier-T clients. The pack's `manifest.yml` is its own minimal schema.
