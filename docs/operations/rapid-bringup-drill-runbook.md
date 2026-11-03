# Rapid bringup drill — operator runbook (first PASS S1)

Hand-on-keyboard runbook for the first PASS drill of Block 68 rapid
bringup. Targets the **S1 single-Orin total-loss restart** scenario (60
min budget). For S2 link-to-existing-Spock-peer, see §10 delta.

**Spec:** `docs/design/rapid-site-bringup.md`
**Implementation:** `docs/project/implementation-plan.md` Block 68
**Verdict register:** `docs/project/validation-state.md` §"Block 68 — rapid-bringup substrate"

## 0. Pre-flight gate (15 min) — confirm before starting

Operator must have ALL of:

- [ ] Operator workstation (Mac or Linux) on the LAN with reachable internet
- [ ] aws-vault profile `usareur-af` set up + reachable AWS prod creds
- [ ] Cloudflare API token in `.envrc.local` as `CLOUDFLARE_API_TOKEN_PROD` and `TF_VAR_cloudflare_account_id`
- [ ] Offline-CA YubiKey (or dev-CA age identity) for signing the bundle
- [ ] Operator's age private key at `~/.config/age/keys.txt` (or YubiKey-PIV X25519 identity via `age-plugin-yubikey`)
- [ ] Linux builder host (x86_64 Ubuntu 22.04+) with NVIDIA L4T BSP at `/opt/nvidia/jetpack-6.1/Linux_for_Tegra/`. NOT redistributable; pull from NVIDIA before drill.
- [ ] Target Orin with USB-A flash cable + serial console (USB-C → laptop)
- [ ] Fresh USB stick (≥1 GB)
- [ ] Approx 2.5 hours of contiguous time (60-min drill + ~90 min image build + ~30 min cloud prep)

Pre-flight verification commands (run on operator workstation):

```bash
aws-vault exec usareur-af --no-session -- aws sts get-caller-identity
echo "Account: $(aws-vault exec usareur-af --no-session -- aws sts get-caller-identity --query Account --output text)"
grep -c "^age1" ~/.config/age/keys.txt
shellcheck ops/bootstrap/*.sh ops/bootstrap/drill/*.sh ops/bootstrap/first-boot/*.sh
docker info >/dev/null 2>&1 && ./ops/bootstrap/first-boot/integration-test/run-test.sh
```

The integration test PASS reproduces the 7/7 PASS evidence; if it fails, halt drill and triage the substrate before going further.

---

## 1. Add operator pubkey to recipients.txt (5 min)

**Why:** age multi-recipient encryption requires the operator's pubkey in `recipients.txt` for `rebuild.sh` to encrypt the bundle to them.

```bash
grep "public key" ~/.config/age/keys.txt
```

Copy the resulting `age1...` line. Edit `ops/bootstrap/recipients.txt` and add it under "Active operators" with attribution:

```
age1abc...xyz  # <operator-name> <email>  added=2026-05-07
```

Optional: add the same identity's `ssh-keygen -Y verify` allowed_signers line at the bottom of the file (see comments in recipients.txt).

Verify + commit:

```bash
grep -c "^age1" ops/bootstrap/recipients.txt
git add ops/bootstrap/recipients.txt
git commit -m "ops(bootstrap): add <operator-name> as bundle recipient"
git push
```

## 2. terraform apply R2 mirror (5 min)

**Why:** stands up the T1 S3 + T2 R2 buckets that pull-image.sh will fetch from.

The Cloudflare provider in `terraform-prod/r2-mirror.tf` consumes `var.cloudflare_account_id` (i.e. env var `TF_VAR_cloudflare_account_id`). The repo convention in `.envrc.local` carries `CLOUDFLARE_ACCOUNT_ID_PROD`. **Add this aliasing line to `.envrc.local`** (one-time, before first apply):

```bash
echo 'export TF_VAR_cloudflare_account_id="$CLOUDFLARE_ACCOUNT_ID_PROD"' >> .envrc.local
direnv allow .
```

Confirm both variables are now exposed to terraform:

```bash
echo "TF_VAR_cloudflare_account_id=${TF_VAR_cloudflare_account_id:-MISSING}"
echo "CLOUDFLARE_API_TOKEN_PROD=${CLOUDFLARE_API_TOKEN_PROD:-MISSING}"
```

Plan + apply:

```bash
aws-vault exec usareur-af --no-session -- terraform -chdir=terraform-prod init
aws-vault exec usareur-af --no-session -- terraform -chdir=terraform-prod plan -target=aws_s3_bucket.bootstrap_bundle -target=aws_s3_bucket.image_mirror -target=cloudflare_r2_bucket.image_mirror
aws-vault exec usareur-af --no-session -- terraform -chdir=terraform-prod apply -target=aws_s3_bucket.bootstrap_bundle -target=aws_s3_bucket.image_mirror -target=cloudflare_r2_bucket.image_mirror
```

Verify:

```bash
aws-vault exec usareur-af --no-session -- aws s3 ls | grep -E "bootstrap-bundle|image-mirror"
```

## 3. Cloudflare DNS for `image-mirror.<zone>` (10 min)

**Why:** R2 bucket is reachable via custom domain so `pull-image.sh` can fetch by `https://image-mirror.<zone>/<image>.img.age` over Cloudflare's edge.

Two options:

**Option A — Cloudflare dashboard:** R2 → image-mirror bucket → Settings → Custom Domains → Connect `image-mirror.<zone>`. Cloudflare auto-provisions the TLS cert.

**Option B — terraform (preferred if `r2-mirror.tf` carries the resource):** rolls into step 2's `apply`.

Verify:

```bash
curl -I https://image-mirror.<zone>/
```

Expect HTTP 404 (bucket is empty) with `cf-ray` header confirming Cloudflare edge.

## 4. First bundle build (15 min)

**Why:** rebuild.sh produces the signed + age-encrypted bundle that the image build pipeline derives from. Bundle ciphertext goes to T1 S3.

Working tree must be clean — rebuild.sh enforces this:

```bash
git status -s | wc -l   # expect 0 after the commit in step 1
```

Run the build (with offline-CA YubiKey or operator's signing key):

```bash
./ops/bootstrap/rebuild.sh --upload --signing-key ~/.ssh/id_ed25519 --tag bundle-v$(date -u +%Y%m%d)
```

Verify the bundle and signature landed:

```bash
aws-vault exec usareur-af --no-session -- aws s3 ls s3://example-bootstrap-bundle/bootstrap/
```

Should show `bundle-vYYYYMMDD.tar.age` + `bundle-vYYYYMMDD.tar.sig`.

## 5. Build the Orin image (45–90 min)

**Why:** produces the bootable image that lays down OS + LUKS + Docker + repo + first-boot.service in one pass.

**On the Linux builder host** (NOT the operator workstation, unless workstation runs Linux):

```bash
ssh <builder-host>
cd /path/to/goat
git pull origin main

# Confirm L4T BSP layout
ls /opt/nvidia/jetpack-6.1/Linux_for_Tegra/{rootfs,flash.sh}

# Build (Step 4 of the script invokes flash.sh --no-flash --sign end-to-end)
sudo ./ops/bootstrap/build-image-orin.sh \
  --bundle-tag bundle-v$(date -u +%Y%m%d) \
  --output-dir /tmp/orin-mgmt-$(date -u +%Y%m%d) \
  --l4t-bsp-path /opt/nvidia/jetpack-6.1/Linux_for_Tegra
```

Output lands at `/tmp/orin-mgmt-YYYYMMDD/flash-images/`. Sign + push per [`docs/operations/image-builder-host.md`](image-builder-host.md):

```bash
ssh-keygen -Y sign -f ~/.ssh/id_ed25519 -n bootstrap-image \
  /tmp/orin-mgmt-YYYYMMDD.img > /tmp/orin-mgmt-YYYYMMDD.img.sig

age -r $(grep "^age1" ops/bootstrap/recipients.txt | head -1) \
  -o /tmp/orin-mgmt-YYYYMMDD.img.age \
  -e /tmp/orin-mgmt-YYYYMMDD.img

aws-vault exec usareur-af --no-session -- aws s3 cp /tmp/orin-mgmt-YYYYMMDD.img.age \
  s3://example-image-mirror/orin-mgmt-YYYYMMDD.img.age
aws-vault exec usareur-af --no-session -- aws s3 cp /tmp/orin-mgmt-YYYYMMDD.img.sig \
  s3://example-image-mirror/orin-mgmt-YYYYMMDD.img.sig
```

(Or use `wrangler r2 object put` for direct R2 upload — same content goes to both tiers.)

## 6. Flash to a target Orin in recovery mode (15 min)

**Why:** lays the image on the Orin's eMMC (or NVMe via `external` flash target) so the box boots into the rapid-bringup first-boot flow.

On the target Orin:

1. Power off
2. Hold the FORCE_RECOVERY pin while powering on (or short the pins per the AGX Orin devkit manual)
3. Confirm recovery mode: `lsusb` on the builder host shows `0955:7023 NVIDIA Corp.`

On the builder host:

```bash
cd /opt/nvidia/jetpack-6.1/Linux_for_Tegra
sudo ./flash.sh --flash-only jetson-agx-orin-devkit external
```

Wait for "The target t186ref has been flashed successfully" — typically 8–15 min for `external` flash to NVMe.

Power-cycle the Orin. It should now boot into the first-boot flow.

## 7. Prepare USB userdata.yml (5 min)

**Why:** non-secret config (site_id, profile, role, scenario) goes on a USB stick read at first boot per the Block 68A spike.

On operator workstation:

```bash
cp ops/bootstrap/first-boot/userdata.yml.example /Volumes/DRILL_USB/userdata.yml
```

Edit `/Volumes/DRILL_USB/userdata.yml` for the drill scenario:

```yaml
schema_version: 1
site_id: drill-test-1
mgmt_stack_profile: postgres-spock-only
hardware_class: orin-mgmt
scenario: total-loss
wg_cp0_endpoint: relay-test.example.com:51820
wg_cp0_relay_pubkey: AAAA...
```

Eject:

```bash
diskutil eject /Volumes/DRILL_USB
```

## 8. Boot the Orin + drive the drill (60-min budget — the actual measurement)

**Why:** this IS the drill. Phase budgets per `rapid-site-bringup.md` §6.

### 8a. On target Orin:

1. Plug USB into the Orin
2. Connect serial console (USB-C → operator laptop, or HDMI + keyboard)
3. Power on

### 8b. On operator laptop, terminal A — start the drill harness in auto mode BEFORE the Orin reaches the first-boot.serial-prompt phase:

```bash
./ops/bootstrap/drill/run-rapid-bringup-drill.sh \
  --scenario S1 \
  --hardware-class orin-mgmt \
  --target operator@<orin-ssh-target-ip-once-up>
```

(For the very first drill where the Orin doesn't have an SSH-reachable IP until partway through, use `--mode manual` and hit Enter at each phase boundary as you watch progress on the serial console.)

### 8c. On the serial console (operator types):

When prompted, type each secret + confirm:

- `DRILL_TEST_1_PG_PASS`
- `DRILL_TEST_1_REPLICATOR_PASS`
- `DRILL_TEST_1_LUKS_PASS`

(For `postgres-spock-only` profile only — see schema for the `full` profile's longer prompt list.)

### 8d. Watch the JSONL evidence file fill on the Orin:

```bash
ssh operator@<orin> "tail -f /var/log/goat/first-boot.jsonl"
```

Expect 7 phase markers ending in `first-boot.done`.

## 9. Summarize + record verdict (5 min)

**Why:** drill evidence is the audit trail; PASS verdict feeds Block 68F ADR promotion.

Pull the JSONL evidence to the operator workstation:

```bash
mkdir -p docs/reports
scp operator@<orin>:/var/log/goat/first-boot.jsonl \
    docs/reports/2026-MM-DD-rapid-bringup-drill-s1-orin-mgmt-firstboot.jsonl

# Drill-side JSONL is already on the operator laptop where step 8b ran:
ls docs/reports/2026-MM-DD-rapid-bringup-drill-s1-orin-mgmt.jsonl
```

Summarize:

```bash
./ops/bootstrap/drill/summarize.py docs/reports/2026-MM-DD-rapid-bringup-drill-s1-orin-mgmt.jsonl
```

Verdict: PASS (≤10% over budget) / WARN (≤25%) / FAIL (>25%).

Update `docs/project/validation-state.md` §"Block 68 — rapid-bringup substrate" with a new row:

```
| 2026-MM-DD | First S1 single-Orin PASS drill | ✅/🟡/❌ | <verdict>; total elapsed <X>s vs budget 3600s; evidence at docs/reports/2026-MM-DD-rapid-bringup-drill-s1-orin-mgmt.jsonl |
```

If PASS: update `Block 68F` status — one of the two PASS-drill prerequisites is now satisfied.

If FAIL: draft an F-NNN finding per [ADR 1011](../adr/1011-findings-register.md) capturing which phase blew the budget + root cause hypothesis.

---

## 10. S2 (link to existing Spock peer) — delta only

Same as S1 except:

- Pre-flight: confirm wg-cp0 reachability from bringup site to existing peer site; operator must have the existing site's `<SITE>_REPLICATOR_PASS` from sovereign 1Password vault.
- Step 7 userdata.yml:
  - `mgmt_stack_profile: full` (or whatever the existing cluster runs)
  - `scenario: spock-join`
  - Add `peer_site_mgmt_api: https://198.18.0.1:443` and `peer_site_pg_endpoint: 198.18.0.1:5432`
- Step 8c serial-console secret prompts include the cluster-wide `<SITE>_NB_ENCKEY` / `<SITE>_BKP_KEY` / `<SITE>_RELAY_SECRET` per the `full` profile (must MATCH existing cluster — generated values would silently break Spock peering)
- Step 9 verdict: budget is **120 min** (not 60); Spock initial-sync runs in background **after** the drill — separate observation
- Update validation-state.md row to `S2` and bump the budget reference

For full three-Orin S2: same pre-flight × 3 (Orins A/B/C, each with its own USB userdata.yml carrying its own `router_role` field).

---

## 11. Failure-mode triage

| Symptom | Likely cause | Fix |
|---|---|---|
| `first-boot.service` exits non-zero | USB userdata schema mismatch OR secrets prompt timed out | Drop to login shell on serial console; fix; re-run with `--force` |
| Schema validation fails on userdata.yml | Field mismatch vs userdata-schema.yml | Re-edit on operator workstation; re-flash USB |
| Bundle signature verification fails on pull | recipients.txt drift between build host and operator | Re-pull repo + recipients.txt on the verification host |
| Spock subscription cannot start (S2 only) | wg-cp0 unreachable OR replicator password wrong | Verify reachability with `nc -zu 198.18.0.1 51820`; re-confirm REPLICATOR_PASS from 1Password |
| Step 4 bundle upload fails | aws-vault profile expired | `aws-vault exec usareur-af --no-session -- aws sts get-caller-identity` to refresh |
| Step 5 flash.sh fails | L4T BSP version mismatch with target Orin firmware | Confirm both at JetPack 6.1 / L4T R36.4.7 |
| Drill exceeds budget by >25% on a single phase | Real bottleneck, not theater | Draft F-NNN per [ADR 1011](../adr/1011-findings-register.md); name the long-pole phase + suspected cause |

## 12. Post-drill cleanup

- Power down the target Orin (LUKS keys are zeroized at next cold boot per Block 65 multi-modal continuity)
- Wipe the USB userdata stick: `dd if=/dev/zero of=/dev/disk<N> bs=1m count=10`
- Push the validation-state.md update + the JSONL evidence to main
- If PASS: nothing further until the matching scenario's drill (S2 if you just did S1, or vice versa)
- If FAIL: hardening block backlog entry under Block 68 → next drill cycle

## 13. Two-PASS gate → Block 68F ADR promotion

Once both an S1 single-Orin and an S2 three-Orin drill pass within 10% of budget on target hardware, the design is ADR-promotion-eligible:

1. Author the ADR under the operational-discipline neighborhood (likely `0414` or similar — check [`docs/adr/INDEX.md`](../adr/INDEX.md) for the next free slot)
2. Reference both drill evidence files
3. Move `docs/design/rapid-site-bringup.md` status to Accepted
4. Add a quarterly drill cadence calendar entry per §9
