# Block 80 crutch tier — activate / deactivate / re-activate runbook

| Field | Value |
|---|---|
| Status | Initial substrate landing (Block 80A + 80B + 80G). Substrate-as-code in trunk; no live VPSes until an operator runs through §3 below. |
| Audience | Operators activating, deactivating, or re-activating the public mTLS-gated crutch tier (Block 80 / ADR 0843). |
| Related | `docs/design/public-mtls-crutch-tier.md`, [ADR 0843](../adr/0843-public-mgmt-signal-relay-mobile-crutch.md), [ADR 0407](../adr/0407-internal-x509-ca-for-service-tls.md), ADR 0406, Block 80, F-109. |

## What this is

The crutch tier is **operator-toggleable** per ADR 0843 §D5. This runbook is the single durable guide an operator follows to move the substrate among its three lifecycle states:

```
   ┌──────────────────────┐  deactivate    ┌──────────────────────┐  destroy   ┌──────────────────────┐
   │     Activated        │ ─────────────▶│    Deactivated       │ ─────────▶│  Substrate-as-code   │
   │ (VPSes up, DNS       │               │ (VPSes up OR down,   │            │  only                │
   │  published, certs    │ ◀────────────│  no public DNS,      │ ◀─────────│  (terraform + role   │
   │  minting)            │  re-activate │  no fresh certs)     │  reapply   │   in trunk; no live  │
   └──────────────────────┘               └──────────────────────┘            │   AWS resources)     │
                                                                              └──────────────────────┘
```

State of trunk **today** is *Substrate-as-code only*. `terraform-prod` has `crutch_tier_enabled = false` by default, the Ansible role is a no-op when its gate is false, and the bundle field for `mobile_cert` is dormant.

Per ADR 0843 §D5, activation is an operator-fired action with a **stated rationale** + **expected deactivation trigger** + **review cadence**. The activation rationale log in §7 below is the system of record.

---

## 1. Pre-flight gate (do before any transition)

| Check | Command | Pass |
|---|---|---|
| In an isolated worktree | `git rev-parse --git-dir`, compare to common-dir | not master |
| `aws-vault` profile loaded | `aws-vault list \| grep usareur-af` | row present |
| Cloudflare prod token in env | `printenv CLOUDFLARE_API_TOKEN_PROD \| head -c 6` | not empty |
| terraform-prod tree clean | `cd terraform-prod && terraform fmt -check` | exit 0 |
| Offline-CA root + cert-sign reachable | `ls ops/enrollment/ca/dev/root-cert.pem && which ops/enrollment/cmd/cert-sign` | both exist |
| wg-cp0 fleet healthy | `ops/snitch/cli probe-cycle --include wg-cp0` | all green |

If any check fails, stop — fix the underlying dependency first. The crutch tier cannot run without a healthy wg-cp0 fleet (the inner-leg path depends on it; ADR 0843 §D8).

---

## 2. Authorize the activation rationale

A new activation requires a recorded rationale before the transition begins. Open §7 below and add a new row with:

- **Rationale** (one of: `mobile-bridging`, `partner-device`, `emergency-on-ramp`, `locked-down-operator`, `external-validation`, or a free-form short string if a sixth rationale emerges).
- **Activation date** (today).
- **Expected deactivation trigger** (concrete; e.g. "goat-client v0.2 generally available to mobile user pool" / "wg-cp0 outage resolved" / "60-day partner pilot ends").
- **Population** (concrete device count or "≤N devices" envelope; the cert minting in §3.3 will be the canonical census).
- **Owner** (single operator handle; the per-60-day review-or-deactivate decision lands on this owner).

Commit the row + open a PR (does not block §3 — the rationale-row commit lands on a worktree branch in parallel with the activation work).

---

## 3. Activate (Substrate-as-code → Activated)

### 3.1 Provision the three crutch VPSes

```bash
cd terraform-prod
aws-vault exec usareur-af --no-session -- terraform plan \
  -var crutch_tier_enabled=true \
  -var crutch_dns_records_published=false
```

Plan should add 3 × `module.crutch_prod_*` modules (VPC + subnet + EIP + EC2 + SG + SSH key + Secrets Manager entry per region). **No Cloudflare records yet** — `crutch_dns_records_published=false` keeps the hosts undiscoverable until certs + Traefik are in place (§3.4).

Apply:

```bash
aws-vault exec usareur-af --no-session -- terraform apply \
  -var crutch_tier_enabled=true \
  -var crutch_dns_records_published=false
```

Capture `goat_crutch_tier` output (the three public IPs + instance IDs); paste into the §7 row.

### 3.2 Inventory + Ansible reach

Today's inventory generator (`ops/ansible/inventory/generate.sh`) only reads the legacy Terraform tree; the crutch tier is hand-authored at `ops/ansible/inventory/crutch-tier.ini` with three SSM-connected hosts (`crutch-r1` / `crutch-r2` / `crutch-r3`) and a static SSM bucket name pinned in the file header. Per-host vars (FQDN + wg-cp0 client address) live under `ops/ansible/host_vars/crutch-{r1,r2,r3}.yml`; shared role config lives at `ops/ansible/group_vars/crutch_tier/vars.yml`.

Re-confirm the three instance IDs against terraform-prod state before applying:

```bash
aws-vault exec usareur-af --no-session -- \
  terraform -chdir=terraform-prod output -json goat_crutch_tier
```

If the IDs disagree with `crutch-tier.ini`, the inventory file is wrong — fix it before continuing. **Durable follow-on:** teach `generate.sh` to read terraform-prod outputs so this file goes away. Queued as substrate work; out-of-scope for activation.

### 3.3 Mint per-host server certs (Traefik terminator side)

Each crutch host needs a server cert covering its three per-route hostnames (`mgmt.<fqdn>`, `signal.<fqdn>`, `relay.<fqdn>`). Mint via `cert-sign` against the offline CA:

```bash
for h in r1 r2 r3; do
  mkdir -p ~/.goat/state/crutch-server-certs/${h}
  ops/enrollment/cmd/cert-sign/cert-sign \
    -root-cert         ops/enrollment/ca/dev/root-cert.pem \
    -root-key          ops/enrollment/ca/dev/ca.key.age \
    -intermediate-cert ops/enrollment/ca/dev/intermediate-cert.pem \
    -intermediate-key  ops/enrollment/ca/dev/intermediate-key.pem.age \
    -age-identity      ~/.config/age/keys.txt \
    -site-id           crutch-prod \
    -cn                "goat-public-${h}.netbird-prod.goat.example" \
    -san               "goat-public-${h}.netbird-prod.goat.example" \
    -san               "mgmt.goat-public-${h}.netbird-prod.goat.example" \
    -san               "signal.goat-public-${h}.netbird-prod.goat.example" \
    -san               "relay.goat-public-${h}.netbird-prod.goat.example" \
    -san               "goat-public.netbird-prod.goat.example" \
    -out-cert          ~/.goat/state/crutch-server-certs/${h}/cert.pem \
    -out-key           ~/.goat/state/crutch-server-certs/${h}/key.pem
done
```

Notes:
- The cert-sign binary uses single-dash flags (Go's stdlib `flag` package); each `-san` adds one SAN — IPs and DNS names are auto-classified, no `-san-dns` flag exists.
- Pass both `-intermediate-cert` + `-intermediate-key` so leaves are signed by the intermediate, keeping the offline root cold.
- The fifth `-san` adds the round-robin pool name `goat-public.netbird-prod.goat.example` to the cert; without it, mobile clients hitting the pool URL would see a SAN mismatch.

Stage onto each host at `/etc/goat/tls/goat-public-mtls-crutch/{cert.pem,key.pem}` via SSM (the role does not auto-mint server certs; ADR 0407 keeps cert custody operator-side).

### 3.3a Mint per-host wg-cp0 client bundles (silent-control-plane side)

The role chain in [`crutch-tier-deploy.yml`](../../ops/ansible/crutch-tier-deploy.yml) runs `wg-cp0-client` before `goat-public-mtls-crutch` so the crutch host can reach the inner mgmt / signal / relay backends over the silent control plane. The `wg-cp0-client` role's bundle-rooted flow (F-053 closure) needs one CBOR bundle per crutch host. Mint via the existing wg-cp0 bundle ceremony, with the per-crutch-host parameters below:

| Host | `--device-id` | `--wg-cp0-address` |
|---|---|---|
| crutch-r1 | `crutch-r1-01-vps` | `198.18.0.25/24` |
| crutch-r2 | `crutch-r2-01-vps` | `198.18.0.26/24` |
| crutch-r3 | `crutch-r3-01-vps` | `198.18.0.27/24` |

Shared parameters: `--site crutch-prod --acl-groups VEGA-MGMT --first-relay-route-subnet 198.18.0.0/24 --update-allowlist ops/enrollment/wg-cp0-bundle-allowlist.json`. Bundle paths land at `/tmp/<device-id>.bundle.cbor`; record each `cp_device_pubkey` from the stderr summary for §4 of the ceremony runbook.

After all three bundles are issued and the allowlist JSON has the three new entries, re-run the wg-cp0 orchestrator's Pass 2A to push the updated allowlist onto every relay (see wg-cp0-bundle-ceremony §6). Without this push, the crutch host's handshake against any relay's wg-cp0 listener is rejected.

### 3.4 Apply the role

Shared group_vars sit at `ops/ansible/group_vars/crutch_tier/vars.yml`; per-host FQDN + wg-cp0 client address at `ops/ansible/host_vars/crutch-{r1,r2,r3}.yml`. Both are committed substrate — nothing to edit at apply time unless the active mgmt-host shifts off Orin-A (e.g. during a B-active window), in which case override the three `crutch_inner_*_backend` vars.

Apply per host (one ansible-playbook invocation each, so the wg-cp0 bundle path can vary):

```bash
cd ops/ansible
for h in r1 r2 r3; do
  OBJC_DISABLE_INITIALIZE_FORK_SAFETY=YES aws-vault exec usareur-af --no-session -- \
    ansible-playbook \
      -i inventory/hosts.ini -i inventory/crutch-tier.ini \
      -l "crutch-${h}" \
      -e wg_cp0_bundle_path=/tmp/crutch-${h}-01-vps.bundle.cbor \
      -e wg_cp0_ca_pubkey_local_path=$(pwd)/../enrollment/public-keys/dev-goat-ca-ecdsa-2026-05-09.pem \
      crutch-tier-deploy.yml
done
```

The role's post-deploy self-test verifies the strict-required gate via a no-cert handshake against TCP 443; the play fails-fast if the gate drifts to `VerifyClientCertIfGiven`.

### 3.5 Publish DNS

```bash
cd terraform-prod
aws-vault exec usareur-af --no-session -- terraform apply \
  -var crutch_tier_enabled=true \
  -var crutch_dns_records_published=true
```

Verify the records resolve:

```bash
for h in r1 r2 r3; do
  dig +short "goat-public-${h}.netbird-prod.goat.example" @1.1.1.1
done
dig +short "goat-public.netbird-prod.goat.example" @1.1.1.1   # round-robin pool, three A records
```

### 3.6 Per-device cert issuance

For each device that will reach the mesh via the crutch path, mint a per-device X.509 client cert (90-day TTL per ADR 0843 §D4) + bundle it via `bundle-create --mobile-cert <path>`:

```bash
# Mint the client cert
ops/enrollment/cmd/cert-sign \
  --root-cert    ops/enrollment/ca/dev/root-cert.pem \
  --root-key     ops/enrollment/ca/dev/ca.key.age \
  --age-identity ~/.config/age/keys.txt \
  --site-id      crutch-prod \
  --cn           "<device-handle>" \
  --client-auth  \
  --ttl-days     90 \
  --output-dir   /tmp/mobile-certs/<device-handle>

# Concatenate cert + intermediates + key into the bundle-format pem
cat /tmp/mobile-certs/<device-handle>/{cert.pem,intermediates.pem,key.pem} \
  > /tmp/mobile-certs/<device-handle>/bundle.pem

# Mint the enrollment bundle with the mobile-cert embedded
ops/enrollment/cmd/bundle-create \
  --device-id   "<device-handle>" \
  --mobile-cert "/tmp/mobile-certs/<device-handle>/bundle.pem" \
  --output      "/tmp/bundles/<device-handle>.cbor" \
  ... <other bundle-create args per Block 22K runbook>
```

Record the cert serial in the per-fleet manifest (TODO Block 80D — for now, keep an ad-hoc list under §7 per-rationale row's `Population` column).

### 3.7 First-device smoke

Pick one device, deliver its bundle, and confirm end-to-end reachability via the crutch path:

```bash
# On the device (with goat-client v0.1.x cert-extraction surface):
goat-client bundle import /path/to/<device-handle>.cbor
goat-client status   # expects: has_mobile_cert = true
curl --cert <extracted-cert> --key <extracted-key> \
     "https://mgmt.goat-public.netbird-prod.goat.example/api/version"
# expects: 200 + JSON version response
```

If the curl fails, walk through §6 troubleshooting before proceeding to wider rollout.

---

## 4. Per-rationale checklist

Different activation rationales gate on different evidence. After §3, check the boxes for the active rationale.

### 4.1 Mobile bridging

- [ ] goat-client v0.1.x `has_mobile_cert` capability surfaced in the mobile shells.
- [ ] One iOS + one Android device through end-to-end via crutch path.
- [ ] Expected deactivation trigger documented (typically "goat-client v0.2 path-A GA + 60-day user-pool soak").

### 4.2 Partner device on-ramp

- [ ] Partner Data-Sharing Charter signed.
- [ ] Per-partner offline-CA leaf custody confirmed (operator-side).
- [ ] `release/<partner>/` namespace ACL pre-staged per ADR 0821.

### 4.3 Emergency on-ramp

- [ ] Concrete wg-cp0 outage signal (Snitch dashboard / Forward RPC failure / regional ISP outage incident-channel link).
- [ ] Expected duration in row (often "until wg-cp0 fleet healthy + 24h").
- [ ] Deactivation trigger = the matching incident-resolved signal.

### 4.4 Locked-down operator device

- [ ] Affected operator(s) identified with sponsor-MDM constraint description.
- [ ] Per-device cert minted under operator's Ory identity (not a shared cert).
- [ ] Expected deactivation trigger = device upgrade or operator role change.

### 4.5 External-perspective validation

- [ ] Validation question stated (e.g., "what does mgmt-API look like to an outside-mesh observer?").
- [ ] Activation bounded to ≤24h.
- [ ] Population = the validation operator's device only.

---

## 5. Deactivate (Activated → Deactivated / Substrate-as-code)

Two sub-states under Deactivated:

### 5.1 Pull DNS, keep VPSes (fast re-activation)

```bash
cd terraform-prod
aws-vault exec usareur-af --no-session -- terraform apply \
  -var crutch_tier_enabled=true \
  -var crutch_dns_records_published=false
```

Traefik keeps serving on the public IPs, but the hostnames stop resolving in public DNS. Existing connections that already resolved keep working until they drop; new clients can't find the surface. Useful for "pause this rationale" without paying the re-provision cost.

### 5.2 Tear down VPSes (cheap monthly cost)

```bash
cd terraform-prod
aws-vault exec usareur-af --no-session -- terraform apply \
  -var crutch_tier_enabled=false
```

terraform destroys the three modules cleanly (EC2 + EIP + SG + Secrets Manager). No effect on the wg-cp0 silent-control-plane tier; ADR 0843 §D2 isolation invariant preserved by design.

### 5.3 Revoke per-device certs

Append every active mobile cert serial to the offline-CA CRL with reason `cessationOfOperation`. Once Block 80D lands, the `mobile-revoked` selector + 5-minute pull picks this up automatically; until then, the crutch's `/etc/traefik/crl.pem` needs a manual refresh (or the VPS-teardown path in §5.2 makes the question moot).

### 5.4 Close out the §7 row

Mark `Status = deactivated` + `Deactivation date = <today>` + `Deactivation rationale = <trigger that fired>` on the active row.

---

## 6. Troubleshooting

| Symptom | Likely cause | Action |
|---|---|---|
| `terraform apply` complains "module not found" | Worktree out of date | `git fetch origin main && git rebase` |
| Self-test fails with `Connection refused` | Traefik container didn't start | `ssh <host>; docker logs goat-public-mtls-crutch-traefik` — usually a cert path mismatch |
| Self-test fails with "200 OK from backend" | Strict-required gate degraded | Inspect `/etc/traefik/dynamic.yml` for `clientAuthType` value; expect `RequireAndVerifyClientCert` |
| Client gets `certificate required` alert with a valid bundle | Bundle's mobile-cert chain incomplete | Re-mint with `cat cert.pem intermediates.pem key.pem` ordering |
| Client connects but mgmt-API returns 502 | Inner-leg wg-cp0 down | `ssh <host>; wg show wg-cp0` — confirm peer endpoint up |
| CRL stale alarm firing | CRL endpoint unreachable | Inspect `crutch-crl-refresh.service` last run + journal; check `crutch_crl_endpoint` is set correctly |

If the gate failure mode (self-test reports 200 from backend on a no-cert handshake) is observed in production, **deactivate immediately** (§5.2) — the strict-required gate is the entire authentication boundary and a degradation is a security event, not a performance event.

---

## 7. Activation rationale log

Append-only table. Add a new row at every activation; close the row at deactivation. **60-day review** column tracks the canonical check-in cadence — each open row needs a `Last reviewed` value no older than 60 days, or the row's owner explicitly re-justifies or deactivates per ADR 0843 risk row "substrate becoming silently load-bearing past its activation rationale."

| # | Rationale | Activation date | Expected deactivation trigger | Population | Owner | Last reviewed | Status |
|---|---|---|---|---|---|---|---|

---

## 8. Cross-references

- Authoritative design: `docs/design/public-mtls-crutch-tier.md`
- Authoritative ADR: [`docs/adr/0843-public-mgmt-signal-relay-mobile-crutch.md`](../adr/0843-public-mgmt-signal-relay-mobile-crutch.md)
- Implementation tracking: Block 80
- Bundle format (mobile-cert field extension): [`docs/design/offline-enrollment.md`](../design/offline-enrollment.md), [`ops/enrollment/cmd/bundle-create/`](../../ops/enrollment/cmd/bundle-create/)
- Terraform module: [`terraform/modules/goat-public-mtls-crutch/`](../../terraform/modules/goat-public-mtls-crutch/)
- Ansible role: [`ops/ansible/roles/goat-public-mtls-crutch/`](../../ops/ansible/roles/goat-public-mtls-crutch/)
- Apply playbook: [`ops/ansible/crutch-tier-deploy.yml`](../../ops/ansible/crutch-tier-deploy.yml)
