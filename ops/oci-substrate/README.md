# OCI image distribution substrate

Operational substrate for the three-tier OCI image distribution model
(T0 upstream → T1 parallel CSP mirrors → T2 per-site Orin-C cache →
T-local). See ADR 0838
for the decision-of-record and
design doc for the
operational reference.

## Layout

| Path | Purpose |
|---|---|
| [`canonical-set.yaml`](canonical-set.yaml) | Enumerated list of images we mirror through the substrate; consumed by CI fan-out + per-site cron |
| [`trust-anchors/`](trust-anchors/) | Cosign trust anchors for each T0 source (Chainguard sigstore, NVIDIA, Canonical, our offline-CA-rooted first-party key) |
| [`scripts/`](scripts/) | Operator-facing substrate scripts (T0→T1 manual fan-out, T1→T2 sync, push-from-host helper) |

## Today's status

Substrate launching in stages per Block 75:

- **75A substrate landing** — partial: canonical-set drafted with
  admit-audit only; CI workflow not yet wired (T0→T1 fan-out depends
  on terraform-prod ECR repos + GitHub Actions secrets, both
  operator-fired)
- **75B client wiring** — see [`ops/ansible/roles/oci-substrate-client/`](../ansible/roles/oci-substrate-client/)
- **75C T2 Forgejo `goat` org** — see `ops/ansible/roles/forgejo/` (org creation handled out-of-band)
- **75D T1→T2 cron** — see [`ops/ansible/roles/oci-substrate-t2-sync/`](../ansible/roles/oci-substrate-t2-sync/)
- **75E first-consumer (admit-audit)** — in flight; bridges existing
  vega-A local image into Orin-C Forgejo (no ghcr.io intermediate)

## Getting started

To add a new image to the canonical-set:

1. Pin the digest in `canonical-set.yaml` (sha256, immutable)
2. Identify the `sign_anchor` (must exist under `trust-anchors/`)
3. List `consumers` (which compose stacks pull this image)
4. PR to `main`. CI fan-out (when wired) auto-pushes to T1; per-site
   cron syncs to T2 within 48h; pull-through cache covers urgent
   misses between cron runs.

To bridge an image that has no real T0 source (e.g., locally-built
first-party that was never pushed to ghcr.io):

1. From the host that has the image: `sudo docker save <image-tag> | ssh <orin-c> sudo docker load`
2. Tag for Forgejo: `sudo docker tag <image-tag> orin-c.<site>.netbird-prod.goat.example:3030/goat/<service>:<tag>`
3. Push to Forgejo: `sudo docker push orin-c.<site>.netbird-prod.goat.example:3030/goat/<service>:<tag>`
4. Capture the digest: `sudo docker inspect ... --format '{{index .RepoDigests 0}}'`
5. Update canonical-set with the new digest

A push helper script that automates this lives at
[`scripts/push-from-host.sh`](scripts/push-from-host.sh).
