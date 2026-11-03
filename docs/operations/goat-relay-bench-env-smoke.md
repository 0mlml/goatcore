---
title: goat-relay — bench-env smoke runbook (Block 33B step 8)
audience: small-team operator
status: ready-to-execute (USER-FIRED — terraform + docker; not auto-run)
last-updated: 2026-04-30
---

# goat-relay bench-env smoke (Block 33B step 8)

First-light validation of the [`goat-relay`](../../goat-relay/) binary on
real Linux. Validates that:

1. The kTLS branch in [`listener/ws_ktls.rs`](../../goat-relay/src/listener/ws_ktls.rs)
   compiles on Linux (this is itself the first cross-platform validation
   surface — macOS dev only exercises the userspace branch).
2. The binary boots on a t3.micro and binds both WS (TCP 33073) and QUIC
   (UDP 33073) listeners.
3. A protocol-aware client can complete the auth handshake.
4. Two clients can exchange a `Transport` frame through the relay (the
   smallest end-to-end functional test).

**Out of scope here:** the M-006-tuned ≥110 Mbps single-flow acceptance
bar (that's step 9 / 33D). Step 8 is "any positive throughput proves
the binary works on Linux." See
`block-33-progress.md` for the
sequence.

## Prerequisites

- Workstation: Docker Desktop or a Linux machine with Docker.
- AWS access: `aws-vault exec <profile> --no-session -- ...`.
- Repo at the workstation: clean working tree, on `main`.
- ghcr.io login (if pushing image) — or build locally and ship via
  `docker save` / SSH.

## Tier 1 — binary boots + listeners bind (smoke)

The minimum viable proof of life. ~10 minutes; ~$0.05 spent.

### 1.1 Build the goat-relay container image

From the repo root:

```bash
docker build -f ops/goat-relay/Dockerfile -t goat-relay:dev .
```

This exercises the **Linux build** of the kTLS branch (the macOS dev
build only sees the userspace branch). On success: image tag
`goat-relay:dev` exists locally. On failure: the kTLS or `ktls-sys`
build is broken; capture the error and file as F-NNN before continuing.

### 1.2 Generate a self-signed cert (one-shot, local)

```bash
openssl req -x509 -newkey rsa:2048 -nodes \
  -keyout /tmp/goat-relay-cert/key.pem \
  -out /tmp/goat-relay-cert/cert.pem \
  -days 1 \
  -subj "/CN=relay-bench.localhost"
```

(Production deploy uses real certs from a CA; first-light uses
self-signed on a throwaway VPS.)

### 1.3 Provision the t3.micro relay VPS

Re-use `terraform/relay-bench.tf` which
already provisions a t3.nano peer pair behind separate NAT gateways.
Step 8a needs **one additional t3.micro** for the relay itself. Add a
sibling resource block in a sister file
`terraform/goat-relay-bench.tf` (NOT covered in the existing bench
file because that one is reserved for the netbird-upstream-comparison
shape).

Until the Terraform sibling lands, an ad-hoc `aws ec2 run-instances`
call on a t3.micro in `eu-central-1` is acceptable for tier-1 smoke.

### 1.4 Ship the binary to the relay VPS

Two paths:

- **Docker via SSH:**
  ```bash
  docker save goat-relay:dev | ssh ubuntu@$RELAY_IP docker load
  ```
- **Binary direct:** copy `target/release/goat-relay` (built **inside the
  Linux container**, not on macOS) to the VPS.

### 1.5 Run the relay

On the VPS:

```bash
docker run -d --name goat-relay \
  -v /tmp/cert.pem:/etc/goat-relay/cert.pem:ro \
  -v /tmp/key.pem:/etc/goat-relay/key.pem:ro \
  -p 33073:33073/tcp -p 33073:33073/udp \
  goat-relay:dev \
  --tls-cert /etc/goat-relay/cert.pem \
  --tls-key /etc/goat-relay/key.pem \
  --bind 0.0.0.0:33073
```

### 1.6 Verify

```bash
docker logs goat-relay
```

Expected (~2 lines after the boot banner):

```
ws listener bound on 0.0.0.0:33073
quic listener bound on 0.0.0.0:33073
```

If both lines appear: **tier 1 PASS**. The Linux kTLS-or-userspace path
worked through to "listener bound."

### 1.7 Tear down

```bash
docker rm -f goat-relay
# + AWS instance terminate
```

## Tier 1.5 — local-Linux validation via docker-compose (NO AWS)

Closes the gap between macOS dev (which can only build but cannot
exercise the `cfg(target_os = "linux")` kTLS branch at runtime) and
tier 4 (full AWS bench env). Runs the entire integration test suite
inside a real Linux container on Docker Desktop's Linux VM. ~5
minutes; **$0**.

This is the most appropriate environment for the kTLS hand-off
validation: the test client connects to the test listener over a
real `127.0.0.1:0` TCP socket, so on Linux the listener wraps in
[`ktls::CorkStream`] and calls
[`ktls::config_ktls_server`] — actually exercising the kTLS path
that ADR 0709 D8 specifies. macOS dev only sees the userspace
fallback.

### 1.5.1 Build the test image

```bash
docker compose -f ops/goat-relay/docker-compose.yml --profile test build tester
```

Reuses the `builder` stage from the production image; the `test`
stage adds an `ENTRYPOINT` of `cargo test --workspace --release
--locked`. ~3-4 minutes on a clean cache.

### 1.5.2 Run all non-ignored tests

```bash
docker compose -f ops/goat-relay/docker-compose.yml run --rm tester
```

Acceptance: 85 lib unit + 2 lib doctests + 13 binary tests (the same
counts the macOS host shows) all pass on Linux. If any fail: the
failure is platform-specific to the kTLS path; capture as F-NNN
before continuing.

### 1.5.3 Run the multi-peer-pair sweep (matches upstream's `t.SkipNow()`)

```bash
docker compose -f ops/goat-relay/docker-compose.yml run --rm tester -- --include-ignored
```

Runs the `#[ignore]`'d `ws_listener_multi_peer_pair_routing` and
`quic_listener_multi_peer_pair_routing` tests inside Linux. Sweeps
peer-pair counts [2, 10, 30] × 50 frames per pair, asserts
peer-id rewrite + payload preservation. Acceptance: all green.

### 1.5.4 Run the relay alone (smoke listener-bind)

```bash
mkdir -p ops/goat-relay/tls
openssl req -x509 -newkey rsa:2048 -nodes \
  -keyout ops/goat-relay/tls/key.pem \
  -out ops/goat-relay/tls/cert.pem \
  -days 1 -subj "/CN=relay-bench.localhost"
docker compose -f ops/goat-relay/docker-compose.yml up relay
```

Expected log lines (after the boot banner):

```
ws listener bound on 0.0.0.0:33073
quic listener bound on 0.0.0.0:33073
shard up; awaiting ctrl-c
```

`Ctrl-C` to stop. Per `.gitignore`, `ops/goat-relay/tls/` is
gitignored.

### 1.5.5 Tear down

```bash
docker compose -f ops/goat-relay/docker-compose.yml down -v
```

## Tier 2 — protocol-aware client completes the auth handshake

Confirms the rustls handshake + WS upgrade + auth FSM round-trip work
on Linux. ~15 minutes. ~$0.10.

A purpose-built test client lands at `ops/goat-relay/handshake-probe/`
(NOT yet written; queued as step 8a-prep). For now, the macOS-side
integration test
[`ws_listener_completes_auth_handshake`](../../goat-relay/src/listener/ws.rs)
demonstrates the wire shape; porting it to a standalone binary is the
queued work.

Acceptance: client receives an `AuthResponse` whose `address` field
matches the configured `--advertised-addr`.

## Tier 3 — two clients exchange a Transport frame

Two clients (A + B) connect, both auth, A sends Transport addressed to
B, B receives the rewritten frame.

This IS the macOS integration test
[`ws_listener_routes_transport_between_two_peers`](../../goat-relay/src/listener/ws.rs)
on real Linux. Same code path; standalone binary at
`ops/goat-relay/routing-probe/` (queued).

Acceptance: B's received frame's `peer_id` field equals A's id (proves
the in-place rewrite path works on Linux); payload bytes preserved.

## Tier 4a — production-shape kTLS validation on AWS t3.micro (USER-FIRED)

Closes the kTLS-runtime gap that tier 1.5 cannot cover (Docker
Desktop's LinuxKit VM lacks the `tls` ULP module per F-023).
Provisions a single t3.micro Ubuntu 24.04 VPS, runs the same tier 1.5
tester binary against it, asserts the kTLS hand-off path is exercised
(not the userspace fallback). ~15 minutes; **~$0.10** total.

This is the smallest tier-4 step that validates the F-017 → ADR 0709
D8 thesis: kTLS replaces userspace AEAD framing with kernel-level
sendmsg/recvmsg. tier-4b (full netbird daemon + iperf3 ramp against
the M-006 ≥110 Mbps acceptance bar) is bigger scope and lives at
step 9 / 33D.

### 4a.1 Build amd64 image locally (Apple Silicon dev hosts)

Docker Desktop on Apple Silicon defaults to `linux/arm64` builds. AWS
t3.micro is amd64; need `--platform linux/amd64` via buildx:

```bash
docker buildx build --platform linux/amd64 \
  -f ops/goat-relay/Dockerfile \
  -t goat-relay:amd64 \
  --target runtime \
  --load \
  .
```

`--load` loads the resulting image into the local docker daemon (you
can `docker save` it for SSH transport). `--target runtime` ships the
Chainguard-runtime stage (not the test stage).

### 4a.2 Provision the t3.micro relay VPS via Terraform

The `terraform/relay-bench.tf` shape provisions t3.nano peers behind
NAT gateways but does NOT include a t3.micro for the relay itself
(the existing bench tests against the production relays). For tier
4a, we need ONE additional t3.micro. Two paths:

- **Quick:** `aws ec2 run-instances` ad-hoc on a fresh t3.micro in
  `eu-central-1` (Ubuntu 24.04 AMI; security-group inbound TCP+UDP
  33073 from `0.0.0.0/0` for the duration). Cost: ~$0.012/hr; tear
  down within 1 hour.
- **Idempotent:** add a sibling Terraform file
  `terraform/goat-relay-bench.tf` that provisions the t3.micro +
  security group. Queued as a follow-up; not blocking tier 4a.

### 4a.3 Ship the image to the relay VPS

```bash
docker save goat-relay:amd64 | \
  ssh -i <bench-key> ubuntu@$RELAY_IP \
  "docker load && echo loaded"
```

Or push to ghcr.io / ECR if you want repeatable deploys.

### 4a.4 Generate a self-signed cert + run the relay

On the VPS:

```bash
openssl req -x509 -newkey rsa:2048 -nodes \
  -keyout /tmp/key.pem -out /tmp/cert.pem \
  -days 1 -subj "/CN=relay-bench"

docker run -d --name goat-relay \
  -v /tmp/cert.pem:/etc/goat-relay/cert.pem:ro \
  -v /tmp/key.pem:/etc/goat-relay/key.pem:ro \
  -p 33073:33073/tcp -p 33073:33073/udp \
  goat-relay:amd64 \
  --tls-cert /etc/goat-relay/cert.pem \
  --tls-key /etc/goat-relay/key.pem \
  --bind 0.0.0.0:33073

docker logs goat-relay 2>&1 | head -20
```

### 4a.5 Verify F-023 probe + kTLS path

`docker logs goat-relay` should show the F-023 probe verdict on the
FIRST inbound connection. Acceptance for tier 4a:

```text
ws_ktls: kernel TLS ULP available — using kTLS hand-off
    
```

If instead it logs `kernel TLS ULP NOT available — using userspace
TLS`, the AMI's kernel does not have the `tls` module loaded; either
`sudo modprobe tls` on the host (if module-loading is permitted) or
switch AMI to a kernel that has it built-in.

### 4a.6 Drive a connection (forces the probe to fire)

The probe fires on first `accept_tls` call, which only happens when a
client connects. Smallest test client: `openssl s_client` to verify
the TLS handshake succeeds end-to-end:

```bash
openssl s_client -connect $RELAY_IP:33073 -servername $RELAY_IP \
  -alpn '' -CAfile /tmp/cert.pem  # accept self-signed
```

After the handshake completes (or fails), check `docker logs
goat-relay` again — the probe verdict should now appear.

### 4a.7 Optional: run the tier-1.5 tester binary against this VPS

For full integration-test parity (matches what we run locally on
Docker Desktop), `docker save goat-relay:test` + load + run the
tester ON the t3.micro. The tester runs in-process clients against
its own listener, so it doesn't need network connectivity from
outside; the kTLS path is exercised purely by the in-process loopback
TLS handshake. ~5 min on t3.micro.

### 4a.8 Tear down

```bash
docker rm -f goat-relay
# + AWS instance terminate
aws ec2 terminate-instances --instance-ids $RELAY_INSTANCE_ID
```

Update `docs/project/validation-state.md` with the verdict + JSONL
evidence path.

## Tier 4b — full netbird daemon + iperf3 ramp (33D acceptance gate)

**Status: SCAFFOLDED 2026-04-30, USER-FIRED.** Terraform module is
`../../terraform/goat-relay-tier4b.tf`;
orchestrator and README are at
`../../ops/wave-33/tier-4b/`. Ansible playbook
is [`../../ops/ansible/goat-relay-tier4b.yml`](../../ops/ansible/goat-relay-tier4b.yml);
inventory generator is
`../../ops/ansible/inventory/gen-goat-relay-tier4b.sh`.
Design went with option (a) above (separate bench mgmt-plane) over (b)
(daemon relay-override) because option (a) needs no netbird-fork surface.

**Topology:** 1× t3.small mgmt+relay (public, EIP) + 2× t3.micro peers
(private subnet, NAT GW out). Peer-SG explicitly blocks intra-peer
WireGuard UDP 51820 → forces relay path. iperf3 between peer mesh IPs
through goat-relay measures the actual data-plane throughput.

**Acceptance bar:** ≥110 Mbps single-flow on t3.micro per
[ADR 0709](../adr/0709-block-33-relay-rewrite-architecture.md) +
M-006.

**Cost:** ~$0.20-0.40 per run.

**Pre-fire prerequisites** (see `../../ops/wave-33/tier-4b/README.md`):

1. goat-relay container image on GHCR (or buildable from source on the
   mgmt host).
2. Lean netbird-mgmt + signal compose (overlay or fresh `compose.tier4b.yml`
   — existing `ops/compose/mgmt-host/` includes Ory + Spock; overkill).
3. Bench account + setup-key seeding (DB-direct or API-once-up; pattern
   available in `ops/ansible/cgnat-validation.yml`).

**How to fire:**

```bash
AWS_VAULT_PROFILE=<profile> ./ops/wave-33/tier-4b/run.sh
```

The orchestrator's trap-on-EXIT runs `terraform destroy` immediately —
no env stays up after the run. Evidence lands at
`docs/reports/$(date +%F)-goat-relay-tier4b-M-014.jsonl`.

The current `run.sh` STEP 3 is intentionally a stub that exits 64 to
prevent partial-run cost spillover before the mgmt-bootstrap implementation
lands. First-fire prep work: implement STEP 3 against the lean compose,
plus the image-pull or build path chosen by the operator.

## Failure modes to expect

- **Docker build fails on `ktls-sys`** — the `cc-rs` build step
  needs `gcc` + glibc headers. The `rust:1.92-bookworm` builder image
  ships these by default; if absent (e.g., switched builder image),
  add `apt-get install -y build-essential` to the build stage.
- **kTLS hand-off `setsockopt` returns `ENOPROTOOPT`** — kernel
  TLS module not loaded. Run `modprobe tls` on the host (the Docker
  default kernel ships `tls` module on bookworm; AWS AMIs vary). If
  `modprobe tls` fails: the kernel was built without TLS support — use
  a different AMI (Ubuntu 24.04 / Debian 12 confirmed-good).
- **Cipher suite incompatibility** — `ktls::config_ktls_server` returns
  `CipherSuiteError`. Indicates rustls negotiated a cipher suite the
  kernel kTLS module doesn't support. Today only AES-GCM-128/256 +
  ChaCha20-Poly1305 work. Restrict rustls to those.
- **Listener binds but no peer can reach it** — AWS Security Group
  open inbound TCP+UDP 33073 from the bench peer subnets. Mirror the
  pattern in [`terraform/modules/relay/main.tf`](../../terraform/modules/relay/main.tf)
  (the existing relay's SG was the F-016 "QUIC-was-firewalled-at-our-
  own-SG" lesson — UDP 33073 inbound is critical).

## Evidence capture

Per [ADR 1012](../adr/1012-load-testing-discipline.md), step 8's
results land in `docs/measurements/` as M-NNN with
a JSONL evidence trail. Suggested IDs:

- M-009 — goat-relay tier 4a kTLS first light ✅ landed 2026-04-30
- `M-014` — goat-relay throughput baseline (tier 4b / 33D acceptance bar; reserved). M-010..M-013 already claimed (34J production-shape relay throughput; video-fabric MediaMTX viewer ceiling + LiveKit room scale + ingest segmentation rate; 34I CLI burst landed at M-012).

Each measurement updates `validation-state.md`
and cross-links to `block-33-progress.md`'s checkpoint table.

## Cost envelope

- Tier 1: ~$0.05 (one t3.micro, ~30 min).
- Tier 2: ~$0.10 (above + a t3.nano client, ~30 min).
- Tier 3: ~$0.15 (two clients in two regions for cross-region testing).
- Tier 4: per `relay-bench.tf` shape
  — ~$0.20-0.40 per run.

Tear down all instances + NAT gateways immediately after the verdict is
captured. See `docs/operations/cloud-env-lifecycle.md`
for the lifecycle tracker.

## Trigger to revive

Step 8 is **user-fired** — the small team runs Terraform / Docker /
SSH, not Claude. Sequencing:

1. Confirm Docker build of `ops/goat-relay/Dockerfile` succeeds locally
   on the workstation (this is the cheapest step-8 prep — runs in
   minutes, validates the Linux kTLS-branch compiles).
2. If build fails: file F-NNN, fix, retry, commit.
3. If build passes: provision t3.micro, run tier-1, capture verdict.
4. If tier-1 passes: schedule tier-2/3 via the protocol-probe binaries
   (queued; not yet written).
5. If any tier fails: file F-NNN with the specific failure mode + fix
   path; do not advance to the next tier.

## Related

- [ADR 0709 D8](../adr/0709-block-33-relay-rewrite-architecture.md) — kTLS
  hand-off design.
- `block-33-progress.md` §"Open
  work items (33B)" — sequencing.
- [ADR 1012](../adr/1012-load-testing-discipline.md) — measurement
  discipline.
- `relay-bench.tf` — existing peer-
  pair shape (for tier 4).
