# Image builder host — Block 68 pre-baked image build runbook

Operator runbook for building Block 68 rapid-bringup images
(`docs/design/rapid-site-bringup.md`)
on a trusted x86 builder host.

**Block:** 68B.1.
**Hardware:** operator workstation OR a dedicated lab x86 host. The
build process is heavy enough (40–80 GB disk, ~15 min wall) that a
dedicated host is cleaner long-term, but a beefy laptop works for v1.

## When to use

- Initial image build for a new bundle tag (after `ops/bootstrap/rebuild.sh`
  produces a new `bundle-vYYYYMMDD`).
- Operator-rotation rebuild (recipient roster change).
- After a hardware-class is added.
- After a drill failure that traces to image staleness.

Cadence in steady state: ≤2 weeks behind the latest bundle tag, per
bootstrap-bundle drift counter.

## Prerequisites

### Builder host

- **Orin image build:** x86_64 Ubuntu 22.04 (NVIDIA L4T flash tools
  target). Native ARM64 build is *not* supported by NVIDIA's BSP today.
- **Karbon image build:** any x86_64 Linux with systemd ≥ 254 + mkosi
  ≥ 22 + ≥ 40 GB free disk.
- Docker installed (used by both image-build paths to pull arm64
  service images for the Orin cache).
- `git` + `yq` + `shellcheck` installed.

### Operator-side

- Offline-CA YubiKey present in a USB slot (or YubiKey ssh-agent
  forwarded).
- AWS prod-account credentials (T1 S3 push).
- Cloudflare API token with R2 push rights for the
  `image-mirror` bucket (T2 push).
- Bundle ciphertext + signature for the target tag, decrypted in-memory.

### NVIDIA L4T BSP (Orin path only)

Download to the builder host **once per JetPack revision**:

- L4T_RELEASE_*.tbz2 → expand to `/opt/nvidia/jetpack-6.1/Linux_for_Tegra/`
- sample_root_filesystem → expand into the BSP's `rootfs/`
- Run NVIDIA's `apply_binaries.sh` once after every BSP update.

Source: NVIDIA Developer downloads (manual — not redistributable). This
is the only piece of the rapid-bringup chain that lives outside the
sovereignty envelope; the operator pulls it directly from NVIDIA at
build time.

## Procedure

### Build an Orin image

```bash
# On the x86 builder host
cd /path/to/goat

# Step 1: confirm the bundle tag exists
git tag -l 'bundle-v*' | tail -5
TAG=bundle-v20260506

# Step 2: build (skeleton-mode — see Step 4 of build-image-orin.sh
# for the manual L4T flash steps)
./ops/bootstrap/build-image-orin.sh \
  --bundle-tag "$TAG" \
  --output-dir "/tmp/orin-mgmt-${TAG}" \
  --l4t-bsp-path /opt/nvidia/jetpack-6.1/Linux_for_Tegra

# Step 3: manually drive the L4T flash-image assembly per the inline
# TODO block printed by the script. Output: a Tegra-flashable image
# under <L4T_BSP>/tools/kernel_flash/images/.

# Step 4: sign the image with the offline-CA YubiKey
ssh-keygen -Y sign \
  -f /dev/<yubikey-piv-identity> \
  -n bootstrap-image \
  /tmp/orin-mgmt-${TAG}/orin-mgmt.img \
  > /tmp/orin-mgmt-${TAG}/orin-mgmt.img.sig

# Step 5: verify signature locally before pushing
ssh-keygen -Y verify \
  -f ops/bootstrap/recipients.txt \
  -n bootstrap-image \
  -s /tmp/orin-mgmt-${TAG}/orin-mgmt.img.sig \
  < /tmp/orin-mgmt-${TAG}/orin-mgmt.img

# Step 6: push to T1 S3 (prod AWS account, sovereign)
aws-vault exec usareur-af --no-session -- aws s3 cp \
  /tmp/orin-mgmt-${TAG}/orin-mgmt.img \
  s3://example-image-mirror/orin-mgmt-${TAG}.img
aws-vault exec usareur-af --no-session -- aws s3 cp \
  /tmp/orin-mgmt-${TAG}/orin-mgmt.img.sig \
  s3://example-image-mirror/orin-mgmt-${TAG}.img.sig

# Step 7: push to T2 Cloudflare R2 (edge cache)
rclone copy /tmp/orin-mgmt-${TAG}/orin-mgmt.img cloudflare:image-mirror/
rclone copy /tmp/orin-mgmt-${TAG}/orin-mgmt.img.sig cloudflare:image-mirror/

# Step 8: cross-tier verify with pull-image.sh
./ops/bootstrap/pull-image.sh \
  --hardware-class orin-mgmt \
  --tag "$TAG" \
  --tier T2 \
  --output /tmp/verify.img
ssh-keygen -Y verify \
  -f ops/bootstrap/recipients.txt \
  -n bootstrap-image \
  -s /tmp/verify.img.sig \
  < /tmp/verify.img
```

### Build a Karbon image

```bash
cd /path/to/goat
./ops/bootstrap/build-image-karbon.sh \
  --bundle-tag "$TAG" \
  --output-dir "/tmp/karbon-mgmt-${TAG}"

# Steps 4–8 as above with karbon-mgmt-* paths
```

## Sign-as-you-build vs detached sign

The build scripts deliberately do **not** sign the image inline. The
operator runs the `ssh-keygen -Y sign` step manually with the YubiKey
present. Rationale: it is both intentional (signing is a deliberate
ceremonial act, per [`bootstrap-bundle.md`](../design/bootstrap-bundle.md)
§6) and necessary (build hosts may not have the YubiKey present; the
sign step can move to the operator's laptop with the YubiKey there).

## Reproducibility

v1 builds are not bit-reproducible. Two builds of the same `bundle-vT`
on different hosts (or the same host two days apart) produce different
bytes due to mtime + Docker layer ordering + apt cache state. v1
accepts this; signature is the chain of trust. Block 40 reproducible-
build work fixes this when it lands.

## Failure modes

| Symptom | Likely cause | Fix |
|---|---|---|
| `docker pull` fails for an arm64 image | builder is on a network without arm64 mirror | use a host with full arm64 multi-arch pull, or pre-cache via `docker buildx imagetools` |
| mkosi exits "package not found" | apt mirror cache stale | `sudo apt-get update` on the host |
| L4T flash.sh produces a 0-byte image | rootfs not properly populated | verify `apply_binaries.sh` ran without error |
| signature verify fails | wrong YubiKey identity OR `recipients.txt` outdated | confirm YubiKey slot + cross-check recipients.txt against committed-state |

## Validation-state register

Every successful build run appends a row to
`docs/project/validation-state.md`
with `bundle-v<tag>`, `<hardware-class>`, signature verifier, T1/T2
push timestamps. Failed builds become F-NNN findings per
[ADR 1011](../adr/1011-findings-register.md).
