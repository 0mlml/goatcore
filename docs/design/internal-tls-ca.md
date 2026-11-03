# Internal TLS CA — design

> **Anchor:** [ADR 0407 — Internal X.509 CA for service TLS](../adr/0407-internal-x509-ca-for-service-tls.md).
> Implementation tracker: `../project/implementation-plan.md` Block 22J.
> Trust root: same Ed25519 key as ADR 0406.
> First consumer: Block 22 wg-cp0 silent control plane at vega.

This doc concretizes the X.509 issuance machinery and the
operator workflow for getting a TLS cert onto a service host.
ADR 0407 is the *why*; this is the *how*.

## 1. Trust hierarchy at a glance

```
Offline-CA Ed25519 root  (age-encrypted file in dev mode; YubiKey-PIV per [ADR 0406])
  │
  ├──► CBOR-bundle signing  (offline-enrollment.md)        — existing
  │
  └──► X.509 leaf certificates  (this doc)                 — new under ADR 0407
        ├── leaf: node-A mgmt-API     CN=mgmt.vega.netbird.legacy.example
        ├── leaf: node-B mgmt-API     CN=mgmt.vega.netbird.legacy.example (same SAN set, different key)
        ├── leaf: node-A signal       CN=signal.vega.netbird.legacy.example
        └── leaf: ...future per-service certs as services land...
```

Single tier — root signs leaves directly. No intermediate. ADR
0407 §"Decision" + §"Trigger conditions for revisit" cover the
when-to-add-an-intermediate pivot.

## 2. Key + cert encoding

| Artifact | Algorithm | Encoding | Format |
|---|---|---|---|
| Root private key | Ed25519 | PKCS8 inside age-encrypted file (dev) or YubiKey-PIV slot 9c (prod future) | unchanged from ADR 0406 |
| Root public key | Ed25519 | PEM SubjectPublicKeyInfo | available today via `Signer.PublicKey()` |
| Root self-signed cert | Ed25519 | X.509 v3, PEM | **new — this doc** |
| Leaf private key | Ed25519 | PKCS8 PEM | new — generated per cert |
| Leaf cert | Ed25519 | X.509 v3, PEM, signed by root | new |
| Trust anchor for distribution | DER-encoded root cert | binary | embedded in CBOR bundle as `tls_trust_anchor` |

The root cert is technically self-signed (root is its own issuer)
but we generate it once at CA-init time and ship it forever. Root
cert validity: 10 years. Re-issued only if the root key rotates.

## 3. Cert template (every leaf cert)

```
Version:               3
Serial Number:         random 128-bit (RFC 5280)
Issuer:                O=goat, OU=offline-ca, CN=goat Root CA
Subject:               O=goat, OU=<site_id>, CN=<service-fqdn>
Validity:              now → now+1 year
Subject Alt Names:     <FQDN>, <wg-cp0 internal IP>, 127.0.0.1, <mesh-IP if known>, <VIP if applicable>
Public Key:            Ed25519 (leaf)
Signature Algorithm:   Ed25519 (root signs this)
Key Usage:             digitalSignature, keyEncipherment
Extended Key Usage:    serverAuth (also clientAuth if mTLS planned)
Basic Constraints:     CA:FALSE
SubjectKeyID:          SHA-1 of leaf SPKI
AuthorityKeyID:        SHA-1 of root SPKI
```

Notes:

- **SAN list is exhaustive** — every address a peer might use to
  reach this service. WireGuard tunnels and keepalived VIPs both
  count. Operator runs `cert-sign` with explicit SAN list; no
  guessing.
- **CN matches the canonical FQDN** even when peers reach by IP.
  TLS clients verify SAN; CN is for human inspection. Don't
  duplicate FQDN as both CN and SAN; just use SAN.
- **No `serverAuth`-only restriction** when a service needs mTLS —
  add `clientAuth` and re-run cert-sign for the same service.

## 4. Issuance interface

### 4.1 Go package — `ops/enrollment/x509/`

```go
package x509ca

// IssueCert produces a leaf X.509 certificate signed by the offline CA root.
//
// Inputs:
//   leaf:    template carrying SANs, CN, validity period
//   signer:  ca.Signer (existing — age-file in dev mode, YubiKey-PIV per ADR 0406)
//   rootCert: the root self-signed cert (loaded from ca-init output)
//
// Output:
//   pem-encoded leaf cert + leaf private key + leaf SPKI hash
//
// Audit:
//   each call writes a JSON line to the operator-laptop's audit log
//   (path: ~/.goat/ca-audit.jsonl). Bundled into the next
//   online sync per ADR 0406 audit trail.
func IssueCert(leaf *LeafTemplate, signer ca.Signer, rootCert *x509.Certificate) (*Issued, error)

type LeafTemplate struct {
    CommonName    string
    Organization  string   // default: "goat"
    OrgUnit       string   // site_id or service-class
    SANs          []string // hostnames + IPs (mixed types resolved internally)
    Validity      time.Duration
    EKU           []x509.ExtKeyUsage
}

type Issued struct {
    CertPEM  []byte
    KeyPEM   []byte
    SPKI     []byte // for AuthorityKeyID linkage
    Serial   *big.Int
    NotAfter time.Time
}
```

Implementation notes:

- The Ed25519 leaf keypair is generated **inside `IssueCert`** so
  the leaf private key never crosses a process boundary. Caller
  receives PEM bytes and writes them under their own custody.
- SAN parsing: `net.ParseIP()` first → if nil, treat as DNS name.
- Serial: `crypto/rand` → 128-bit big.Int.
- Signature: `signer.Sign(tbsCertificateBytes)` — uses existing
  ca.Signer interface unchanged. The TBS bytes are produced via
  `x509.CreateCertificate` with a custom signer adapter.

### 4.2 Operator CLI — `ops/enrollment/cmd/cert-sign/`

```
cert-sign \
  --root-cert ops/enrollment/ca/dev/root-cert.pem \
  --root-key ops/enrollment/ca/dev/ca.key.age \
  --age-identity ~/.config/age/keys.txt \
  --site-id vega \
  --cn mgmt.vega.netbird.legacy.example \
  --san 198.18.0.1 \
  --san 127.0.0.1 \
  --san mgmt.vega.netbird.legacy.example \
  --validity 8760h \
  --eku serverAuth \
  --out-cert ./out/mgmt-vega-a-cert.pem \
  --out-key  ./out/mgmt-vega-a-key.pem
```

Flags follow the established pattern of `ca-init` (which already
takes `--mode dev|prod`, `--out`, etc.). The `--root-key` +
`--age-identity` pair drives the dev-mode signer; production-mode
signing reads `--yubikey-slot 9c` against ADR 0406's YubiKey
backend when implemented.

Output written to two files; mode `0400 root:root` enforced by the
CLI before exit. Audit-log line written before exit.

### 4.3 First-run — root cert generation

```
ca-init --mode dev --out ops/enrollment/ca/dev/ca.key.age ...
```

today produces an age-encrypted Ed25519 *key* but no X.509 cert.
We extend `ca-init` with a `--gen-root-cert <path>` flag that
generates a self-signed X.509 v3 root cert from the just-created
key, validity 10 years, written to the given path. Existing
ca-init invocations without the flag continue to work.

## 5. Trust distribution

### 5.1 Bundle format extension

Existing CBOR bundle ([offline-enrollment.md](offline-enrollment.md) §3)
gains one field:

```cbor
tls_trust_anchor: bstr  ; DER-encoded root X.509 cert
```

Optional via `omitempty`. Bundles issued before this ADR don't
carry it; peer software treats absent → "no internal TLS CA in
trust set" (graceful for the legacy dev/test envs that don't
need internal TLS).

`ops/enrollment/bundle/bundle.go` adds the field with a unit test
asserting:
- DER round-trip (encode → decode → byte-equal)
- Bundle signature still valid after the field addition (CBOR
  canonical encoding stays stable)
- Bundle without the field still validates (back-compat)

### 5.2 Peer-side trust install

Peer software (snitch / netbird daemon / future agents) reads
`tls_trust_anchor` from its bundle on boot and adds it to the TLS
verification CA pool used for outbound HTTPS calls into the
mgmt-plane. Standard `crypto/tls.Config.RootCAs` pattern.

For node-A self-enrollment (no bundle round-trip per
wg-cp0-prod.md §"Day-1 simplification"),
Ansible drops the same root cert PEM into
`/etc/ssl/certs/goat-root.pem` and netbird daemon points
at it via `--root-ca` (or equivalent flag).

## 6. Service-host integration — Traefik

### 6.1 Compose service definition

Added to [`ops/compose/mgmt-host/docker-compose.yml`](../../ops/compose/mgmt-host/docker-compose.yml)
as a new top-level service:

```yaml
traefik:
  image: traefik:v3.2.3@sha256:<pin>
  container_name: traefik
  restart: unless-stopped
  command:
    - --providers.file.directory=/etc/traefik/dynamic
    - --providers.file.watch=true
    - --providers.docker=true
    - --providers.docker.exposedbydefault=false
    - --entrypoints.websecure.address=:443
    - --entrypoints.metrics.address=:8082
    - --metrics.prometheus=true
    - --log.level=INFO
    - --accesslog=true
  ports:
    - "443:443"
  volumes:
    - /var/run/docker.sock:/var/run/docker.sock:ro
    - /etc/goat/tls/:/etc/goat/tls/:ro
    - /etc/traefik/dynamic/:/etc/traefik/dynamic/:ro
  networks:
    - mgmt
  deploy:
    resources:
      limits:  { cpus: "0.5", memory: 256M }
      reservations: { memory: 64M }
```

Cert and key live on the host filesystem (mounted read-only into
Traefik) so the cert-rotation operator step is "Ansible push +
docker compose restart traefik" rather than "rebuild image."

### 6.2 Dynamic config (Ansible-rendered)

`ops/ansible/templates/traefik-mgmt-host-dynamic.yml.j2`:

```yaml
tls:
  options:
    default:
      minVersion: VersionTLS13
      cipherSuites:
        # FIPS 140-3-aligned, per ADR 0708 inheritance:
        - TLS_AES_256_GCM_SHA384
        - TLS_CHACHA20_POLY1305_SHA256
  certificates:
    - certFile: /etc/goat/tls/mgmt-a/cert.pem
      keyFile: /etc/goat/tls/mgmt-a/key.pem

http:
  routers:
    mgmt-api:
      rule: "Host(`mgmt.{{ site_id }}.netbird.legacy.example`) || Host(`{{ wg_cp0_internal_ip }}`)"
      entryPoints: [websecure]
      service: management-a
      tls: {}
  services:
    management-a:
      loadBalancer:
        servers:
          - url: "http://management-a:33073"
```

Future routers (dashboard, Ory, etc.) drop in as additional
entries under `http.routers` without touching this design.

### 6.3 Cert delivery — Ansible role `traefik`

```
ops/ansible/roles/traefik/
├── defaults/main.yml      # var defaults: traefik_image_pin, mount paths
├── tasks/main.yml         # render dynamic config, push cert+key, ensure compose up
├── templates/
│   └── traefik-dynamic.yml.j2
└── handlers/main.yml      # restart traefik on cert/config change
```

Tasks order:
1. Ensure `/etc/goat/tls/<service>/` dir exists, mode 0700 root
2. Push `cert.pem` (mode 0644 root:root) — sourced from operator
   laptop var `traefik_cert_files[<service>]`
3. Push `key.pem` (mode 0400 root:root) — same source pattern
4. Render dynamic config, mode 0644
5. Ensure traefik service is up via compose (notifies handler if
   any of the above changed)

Cert files come **from the operator laptop**, not generated on
the target host. Each `cert-sign` ceremony produces files the
operator commits to a local-only path under their `~/.goat/`
state dir; Ansible reads them via `lookup('file', ...)`.

## 7. Operator workflow

### 7.1 First-time CA setup (one-time per environment)

```
# 1. Generate Ed25519 root key (existing, dev mode)
ca-init --mode dev \
  --out ops/enrollment/ca/dev/ca.key.age \
  --recipients "$AGE_PUB,$AGE_BREAK_GLASS_PUB" \
  --gen-root-cert ops/enrollment/ca/dev/root-cert.pem
```

`--gen-root-cert` is the new flag (§4.3). The root cert PEM goes
into the bundle generator's input set so future bundles carry
the trust anchor.

### 7.2 Issue a leaf cert

```
cert-sign \
  --site-id vega \
  --cn mgmt.vega.netbird.legacy.example \
  --san 198.18.0.1 --san 127.0.0.1 --san mgmt.vega.netbird.legacy.example \
  --validity 8760h \
  --out-cert ~/.goat/state/vega/mgmt-a-cert.pem \
  --out-key  ~/.goat/state/vega/mgmt-a-key.pem
```

Audit line written. Operator verifies cert is correct:

```
openssl x509 -in ~/.goat/state/vega/mgmt-a-cert.pem \
  -noout -text | grep -E "Subject:|DNS:|IP:|Not After"
```

### 7.3 Push to host via Ansible

Use the dedicated `traefik-refresh.yml` helper (lands 2026-05-08) — re-renders dynamic config + re-pushes certs without re-running the rest of `site-bootstrap.yml`. Same shape as a `kratos-refresh` or `signal-refresh` would have if needed:

```
cd ops/ansible
OBJC_DISABLE_INITIALIZE_FORK_SAFETY=YES ansible-playbook \
  -i inventory/hosts.ini -i inventory/mgmt-hosts.ini -i inventory/mgmt-hosts-mesh.ini \
  traefik-refresh.yml -e site_id=vega -l mgmt-vega-01
```

Useful for: cert rotation pushes, Traefik dynamic-config edits (added router, new TLS option, attached middleware), per-service cert-key swaps. Traefik's `--providers.file.watch=true` picks up the rendered dynamic.yml without a container restart, so this is safe to run under traffic.

Idempotent — Ansible only marks tasks `changed` if the underlying content changed; the file-watcher only reloads if `dynamic.yml` actually changed.

### 7.4 Rotation (annual)

Same as 7.2 + 7.3. Old cert files overwritten on the host;
Traefik picks up the new files (file-watch enabled).

### 7.5 Compromise response (ad-hoc)

1. Re-issue every cert under the current root (loop §7.2 over the
   cert manifest tracked in `~/.goat/state/<site>/manifest.yaml`).
2. Push all new certs via Ansible (`-t traefik` across the whole
   inventory).
3. Note: this does **not** revoke the compromised cert at peers
   that have it cached. CRL/OCSP day-1-out-of-scope per ADR 0407
   §"Decision."
4. If root-key compromise: full re-issue including the bundle CRL
   field flipping every issued bundle. Larger ceremony per ADR
   0406.

## 8. Monitoring

- Prometheus blackbox_exporter probe to `https://198.18.0.1:443/`
  reports `probe_ssl_earliest_cert_expiry`. Alert at 30 days from
  expiry (`certmanager` Grafana dashboard already has the panel
  shape from NETHOST; reuse.)
- Audit-log probe verifies one entry exists per `cert-sign`
  invocation; alert if Ansible deploys a cert without a matching
  audit-log entry within 24 hours.

## 9. What this doc is not

- **Not workload identity.** SPIFFE/SPIRE per
  ADR 0412 is a
  separate layer for short-lived workload SVIDs (Phase 3+).
  This doc covers platform-service TLS (long-lived per-service
  certs).
- **Not threshold signing.** Block 43 FROST K-of-N over
  distributed YubiKey custodians lands later. The cert-sign CLI
  signs through the existing `ca.Signer` interface unchanged
  when threshold lands.
- **Not OCSP/CRL.** Out of scope for ADR 0407 day-1.
- **Not external CA federation.** If a partner's IdP needs to
  trust our internal CA, that's a per-partnership decision under
  [`mission-partner-sharing.md`](mission-partner-sharing.md);
  share the root cert PEM via the partnership artifact, not via
  this CA's automated channel.

## 10. Cross-references

- [ADR 0407](../adr/0407-internal-x509-ca-for-service-tls.md) — decision
- ADR 0406 — root custody
- [ADR 0314](../adr/0314-no-public-dns-for-control-plane.md) — why no LE
- [ADR 0313](../adr/0313-silent-control-plane-outer-wg-wrap.md) — wg-cp0
- `wg-cp0-prod.md` — first consumer (mgmt-API at 198.18.0.1:443)
- [`offline-enrollment.md`](offline-enrollment.md) — bundle format (gains `tls_trust_anchor`)
- [`../adr/INDEX.md`](../adr/INDEX.md) — full ADR series
- `../project/implementation-plan.md` Block 22J — implementation tracking
