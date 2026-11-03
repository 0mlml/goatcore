# YubiKey-PIV offline CA ceremony

**Block 22M.** One-time provisioning of the offline CA's signing root onto a YubiKey 5.7+ PIV slot 9c, replacing the dev-mode age-encrypted root for production bundle signing. Implements ADR 0406. Companion to `wg-cp0-bundle-ceremony.md` (which describes per-device bundle issuance).

> **Trust impact:** before this ceremony, the CA private key sits in `ops/enrollment/ca/dev/ca.key.age` — decryptable by anyone holding the operator's age identity. After, the private key lives ONLY on a YubiKey secured by PIN + touch-policy=always; it never exists in any operator-laptop file, RAM, or backup. Compromise of the laptop does NOT compromise the CA.
>
> **One-way:** the YubiKey-generated key cannot be exported. Lose the YubiKey + lose all backups → lose the ability to sign new bundles (existing bundles validate until their 90d TTL). Provision a backup YubiKey BEFORE retiring the dev-mode root.

## Table of contents

- [1. When to run this ceremony](#1-when-to-run-this-ceremony)
- [2. Pre-flight](#2-pre-flight)
- [3. The ceremony — primary YubiKey](#3-the-ceremony--primary-yubikey)
- [4. The ceremony — backup YubiKey](#4-the-ceremony--backup-yubikey)
- [5. Distribute the new pubkey](#5-distribute-the-new-pubkey)
- [6. Re-issue existing bundles under the new root](#6-re-issue-existing-bundles-under-the-new-root)
- [7. Retire the dev-mode root](#7-retire-the-dev-mode-root)
- [8. Test rehearsal](#8-test-rehearsal)
- [9. Rotation](#9-rotation)
- [10. Recovery from lost YubiKey](#10-recovery-from-lost-yubikey)

---

## 1. When to run this ceremony

- **Once at production go-live** — flips the CA root from dev-mode (age-encrypted file) to prod-mode (YubiKey PIV). Coordinate with Block 22K.4 retroactive re-enrollment: re-issue every active bundle under the new root in the same window.
- **CA rotation** — per ADR 0406 cadence (default 2 years; tighter if signals warrant). Rotation provisions a new YubiKey with a new CA-ID; bundles signed by the old YubiKey stay verifiable until their 90d TTL.
- **Compromise response** — if the primary YubiKey is suspected compromised. Burn the old CA-ID, provision a fresh one, re-issue every active bundle, distribute the new pubkey via Ansible.
- **Backup creation** — provision a SECOND YubiKey with the SAME ceremony but a different `--id` so each device's bundle can be re-issued by either custodian. (Note: each YubiKey holds an independent CA root with its own pubkey; "backup" here means redundant operators each capable of signing, not a copy of the same key.)

## 2. Pre-flight

On the air-gapped issuing workstation:

- [ ] **HARD GATE — YubiKey firmware ≥ 5.7.0** (F-061)
  - Verify FIRST, before any other ceremony step: `ykman info` → look for `Firmware version: 5.7.x` or higher
  - **Pre-5.7 YubiKeys cannot generate Ed25519 PIV keys; this ceremony will fail.** YubiKey firmware is non-upgradeable by design — only remedy is procurement of 5.7+ stock per `docs/project/yubikey-procurement.md`
  - YubiKey 5 FIPS series: confirm with Yubico support that current FIPS-shipping firmware is ≥ 5.7 before ordering — FIPS validation lags non-FIPS by months
  - If your YubiKey is < 5.7: STOP. Use it for FIDO2 LUKS unlock (Block 65D, no firmware-5.7 dependency) and order a 5.7+ YubiKey for the offline CA. The prod-CA migration waits until 5.7+ hardware is in hand.
- [ ] **PCSC daemon:** running and able to see the card
  - macOS: built-in; check via `system_profiler SPSmartCardsDataType`
  - Linux: `pcscd` running; `pcsc_tools` package recommended (`pcsc_scan` to verify)
- [ ] **Build the CLIs:**
  ```
  cd ops/enrollment
  go build -o cmd/ca-init/ca-init ./cmd/ca-init
  go build -o cmd/bundle-create/bundle-create ./cmd/bundle-create
  go test ./...
  ```
  All software tests must be green. Hardware tests cleanly skip without `GOAT_YUBIKEY_HARDWARE_TESTS=1`.
- [ ] **PIN known:** the operator's chosen YubiKey PIV PIN. Default factory PIN is `123456`; CHANGE IT before the ceremony via `ykman piv access change-pin`.
- [ ] **Management key set:** PIV management key is what authorizes slot operations. Default is `010203040506070801020304050607080102030405060708` (published) — CHANGE IT to a random 24-byte value before production use:
  ```
  ykman piv access change-management-key --generate --protect
  ```
  The `--protect` flag stores the new management key on the YubiKey itself, derived from the PIN. Operators authenticate once with PIN; the YubiKey unlocks the management key internally. Recommended.
- [ ] **PUK changed:** PUK unblocks a locked PIN; default PUK is `12345678`. Change it: `ykman piv access change-puk`.
- [ ] **Operator at the workstation** — this ceremony requires presence (touch + PIN entry). Don't try to script around it.

## 3. The ceremony — primary YubiKey

Insert the primary YubiKey. Pick a CA-ID — the `prod-` prefix is auto-applied if absent; the date stamp is for human readability:

```
cd ops/enrollment
./cmd/ca-init/ca-init \
  --mode prod \
  --id goat-ca-2026-05-06 \
  --piv-slot 9c \
  --gen-root-cert ca/prod/root-cert-prod-goat-ca-2026-05-06.pem \
  > public-keys/prod-goat-ca-2026-05-06.pem
```

> **2026-05-09 watchpoint per F-090:** the YubiKey-held key is Ed25519 because the CBOR bundle signing path (per [`offline-enrollment.md`](../design/offline-enrollment.md)) is Ed25519-end-to-end across `bundle/`, `ca.Signer`, and the validator-sidecar. Switching the YubiKey key to ECDSA-P256 would break bundle signing. As a side effect, the X.509 root cert wrapping that key is also Ed25519, and macOS Keychain Services cannot import Ed25519 X.509 certs (`SecCertificateAddToKeychain: Unknown format in import.`). Operators on macOS therefore use the per-leaf trust install (dashboard-bringup §"Step 3") rather than trusting the root. Closure options under design are queued as `pki-macos-trust-architectural-decision` in `active-work.yaml` — covers two-key YubiKey ceremony, separate ECDSA intermediate signed by the Ed25519 root, or accepting per-leaf trust as steady-state. Decision lands BEFORE the prod ceremony fires.

What happens:

1. The CLI prompts for the PIV PIN (or reads `GOAT_CA_PIN` if set). Type the PIN and press Enter.
2. The CLI calls `piv.GenerateKey(slot=9c, algorithm=Ed25519, touch=always, pin=once)`. The YubiKey LED blinks — **tap the YubiKey now**. The keypair is generated INSIDE the secure element; the private key is never exposed to the host process.
3. The CLI builds a self-signed X.509 cert wrapping the freshly-generated pubkey + signs it with the same just-generated key (LED tap again). The cert is stored in slot 9c so future `NewYubikeySigner` calls can read the pubkey back.
4. If `--gen-root-cert` is set, the CLI re-opens the YubiKey through `NewYubikeySigner` and runs `x509ca.GenerateRootCert` (LED tap again) to produce the long-lived TLS root cert per [ADR 0407](../adr/0407-internal-x509-ca-for-service-tls.md). This cert is what every peer's `tls_trust_anchor` will carry going forward.
5. The CLI writes the public-key PEM to stdout (redirected to `public-keys/<ca-id>.pem`) and a stderr summary (CA-ID, slot, cert validity, next steps).

Verify:

```
cat public-keys/prod-goat-ca-2026-05-06.pem | head -3
# Should show:
# -----BEGIN PUBLIC KEY-----
# CA-ID: prod-goat-ca-2026-05-06
# Created-At: 2026-05-06T...
```

Optional smoke test (does NOT mint any production artifact):

```
echo "ceremony smoke test" | ./cmd/ca-init/ca-init \
  --mode prod --id smoke-test --piv-slot 9c \
  > /dev/null  # this would re-provision; SKIP unless you mean to
```

(Don't actually run that command — it would overwrite slot 9c. Use the hardware test gate instead — see [§8 Test rehearsal](#8-test-rehearsal).)

## 4. The ceremony — backup YubiKey

Repeat §3 against a SECOND physical YubiKey with a different `--id`:

```
./cmd/ca-init/ca-init \
  --mode prod \
  --id goat-ca-backup-2026-05-06 \
  --piv-slot 9c \
  > public-keys/prod-goat-ca-backup-2026-05-06.pem
```

Both pubkey PEMs are committed; mgmt hosts trust both CA-IDs simultaneously. Bundles signed by either YubiKey validate. The two operators custody-ing the YubiKeys are independent failure domains — losing one does NOT block bundle issuance.

> Avoid the temptation to "back up" by copying. PIV-stored private keys cannot be exported (that's the point); each backup is a fresh independent root.

## 5. Distribute the new pubkey

```
git add ops/enrollment/public-keys/prod-goat-ca-2026-05-06.pem \
         ops/enrollment/public-keys/prod-goat-ca-backup-2026-05-06.pem \
         ops/enrollment/ca/prod/root-cert-*.pem
git commit -m "block-22M: prod CA root pubkey + backup + X.509 root cert"
git push origin main
```

Mgmt hosts pull these via the existing public-keys distribution path (Ansible role + Ansible-managed file syncs). The bundle validator sidecar trusts any pubkey present in `ops/enrollment/public-keys/`.

For the X.509 root cert (Block 22J / ADR 0407), Traefik's trust-store role at `ops/ansible/roles/traefik/` already references the cert path; an Ansible refresh pushes it to mgmt hosts.

## 6. Re-issue existing bundles under the new root

Bundles signed by the old (dev-mode) CA stay valid until their 90d TTL, but new peers MUST get bundles signed by the new (prod-mode) root, and existing peers should be re-issued at next rotation. The Block 22K.4 retroactive re-enrollment runbook is the natural moment — fold the cutover here:

For each peer in the cohort:

```
./cmd/bundle-create/bundle-create \
  --mode prod \
  --site vega \
  --device-id mgmt-vega-01-orin \
  --wg-cp0-address 198.18.0.1/24 \
  --wg-cp0-relays wg-cp0-relays-prod.json \
  --acl-groups VEGA,VEGA-MGMT \
  --ca-id prod-goat-ca-2026-05-06 \
  --piv-slot 9c \
  --tls-trust-anchor ca/prod/root-cert-prod-goat-ca-2026-05-06.pem \
  --update-allowlist wg-cp0-bundle-allowlist.json \
  --first-relay-route-subnet 198.18.0.0/24 \
  --out /tmp/mgmt-vega-01-orin.bundle.cbor
```

The CLI prompts for the YubiKey PIN once per `bundle-create` invocation (pin-policy=once caches it for the session). Each `bundle-create` requires ONE LED tap on the YubiKey. Issuing 4 bundles in a row = 4 taps.

Per-bundle issuance time: ~5 seconds + tap latency. The bottleneck is operator presence, not YubiKey throughput.

## 7. Retire the dev-mode root

After §6 completes successfully + verification of every cutover (per Block 22K.4 runbook §6 "Verify handshake on both ends"):

1. Wait one bundle TTL (90 days) so any in-flight or stuck-mid-rotation peer has time to enroll.
2. Remove the dev-mode pubkey from `ops/enrollment/public-keys/`:
   ```
   git rm ops/enrollment/public-keys/dev-goat-ca-ecdsa-2026-05-09.pem
   ```
3. Securely shred the age-encrypted dev key on every operator workstation:
   ```
   shred -u ~/.../ops/enrollment/ca/dev/ca.key.age
   ```
4. Commit + push. Mgmt hosts no longer trust the dev CA-ID; any leaked dev-key bundles are now rejected at validate time.

> Do NOT skip the wait. Retiring the dev root before the TTL window closes can strand peers mid-rotation. The 90 days is acceptable because the prod root is already accepted by every mgmt host as soon as §5 commits.

## 8. Test rehearsal

Before performing the live ceremony on a production-bound YubiKey, rehearse on a clearly test-flavored YubiKey to validate the operator workflow + catch any local PCSC / firmware quirks:

```
GOAT_YUBIKEY_HARDWARE_TESTS=1 \
GOAT_YUBIKEY_TEST_PIN=123456 \
GOAT_YUBIKEY_TEST_MGMT_KEY=010203040506070801020304050607080102030405060708 \
  go test -v -run "TestYubikey_Hardware" ./ca/...
```

This:
- Generates a fresh keypair in slot 9c (DESTRUCTIVE — overwrites whatever was there)
- Signs a known-plaintext message
- Verifies the signature against the freshly-generated pubkey
- Tests Close() idempotency

Use a test YubiKey, NOT the YubiKey you intend to use for the production ceremony. The rehearsal overwrites the slot.

If the rehearsal passes, the production ceremony is mechanically identical — the operator runs `ca-init --mode prod` instead of `go test`.

## 9. Rotation

Default cadence per ADR 0406: every 2 years, or sooner if signals warrant (firmware vulnerability disclosed, custodian changes, audit finding).

Rotation procedure:

1. Provision a new YubiKey via §3 with a new `--id` (new date stamp).
2. Distribute the new pubkey via §5 — mgmt hosts now trust BOTH the old and new CA-IDs.
3. Re-issue every active bundle under the new CA-ID via §6.
4. After the longest issued bundle's 90d TTL elapses, retire the old CA-ID via §7.

Total elapsed time: ~90 days from provision-new-key to retire-old-key. Old + new YubiKeys both valid throughout the overlap.

## 10. Recovery from lost YubiKey

Loss of the only signing YubiKey = loss of bundle-issuance capability until a fresh CA is provisioned. Existing bundles validate until their 90d TTL. Recovery:

1. **If a backup YubiKey exists** (per §4): no recovery needed. The backup signs new bundles independently; existing bundles signed by the lost YubiKey continue to validate via the lost YubiKey's published pubkey until they expire.
2. **If no backup**: provision a fresh CA via §3 with a NEW `--id`. Distribute the new pubkey via §5. Re-issue every active bundle via §6 BEFORE the existing bundles' 90d TTL expires. The lost YubiKey's CA-ID stays trusted in `public-keys/` until the longest issued bundle expires, then is retired via §7.
3. **If the YubiKey is suspected compromised** (vs simply lost): treat as compromise. Don't wait for natural expiry — proactively revoke every bundle issued by the compromised CA via the CRL pipeline ([offline-enrollment.md §6](../design/offline-enrollment.md#6-revocation)) AND the wg-cp0 allowlist sync ([§11.3](../design/offline-enrollment.md#113-relay-side-allowlist-sync)).

The compelling case for §4's backup discipline: it converts "lost YubiKey" from "scramble to re-issue 4+ bundles before TTL" into "no recovery needed."

---

## Cross-refs

- ADRs: `0406`, [`0407`](../adr/0407-internal-x509-ca-for-service-tls.md), [`0308`](../adr/0308-offline-enrollment-cbor-bundles.md)
- Companion runbooks: `wg-cp0-bundle-ceremony.md` (per-device bundle issuance under either CA mode), `wg-cp0-retroactive-re-enrollment.md` (Block 22K.4 cutover; folds prod-CA migration in)
- Design: [`offline-enrollment.md`](../design/offline-enrollment.md) §7 key ceremony
- Implementation: Block 22M in `implementation-plan.md`
- Findings: `F-053`, `F-054` (the FIDO2 override doesn't apply here — PIV operations carry their own touch/PIN policy semantics)
