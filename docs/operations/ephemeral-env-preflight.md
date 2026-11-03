# Ephemeral cloud env pre-flight discipline

When standing up a new ephemeral validation env (cgnat-validation, pgbackrest-wave18, goat-relay-tier4b shape), AWS time + money is being used as the test cycle if you skip pre-flight. Tier 4b's first end-to-end fire took **6 attempts × ~$0.50 cumulative cost** because each AWS round-trip surfaced a different avoidable issue. This doc codifies the checks that would have caught 5 of those 6.

**Author each new ephemeral env's orchestrator against this list.** The cost of a 5-minute pre-flight check is two orders of magnitude less than a failed AWS round-trip.

## Pre-flight checks (in order)

### 1. Infrastructure quota

Before designing custom VPC / subnets / NAT GW, check the existing region capacity:

```bash
aws-vault exec <profile> --no-session -- aws ec2 describe-vpcs --region <region> \
  --query 'Vpcs[].[VpcId,CidrBlock,Tags[?Key==`Name`].Value|[0],IsDefault]' \
  --output table
```

If at the soft default (5 VPCs/region) — refactor to the **default VPC** (172.31.0.0/16). Force-relay-path / force-symmetric-NAT mechanisms can be expressed via SG-only without provisioning a new VPC.

CIDR-collision check before picking a custom CIDR:

```bash
aws-vault exec <profile> --no-session -- aws ec2 describe-vpcs --region <region> \
  --query 'Vpcs[].CidrBlock' --output text | tr '\t' '\n' | sort
```

Don't reuse a CIDR that overlaps a sibling project's allocation (a sibling project at `198.18.0.0/24` was the gotcha for tier 4b).

### 2. SSM bucket name + region

The SSM connection plugin needs an S3 bucket reachable to both the operator's IAM and the target's instance role. Verify both before wiring `ansible_aws_ssm_bucket_name`:

```bash
aws-vault exec <profile> --no-session -- aws s3 ls --region <region> | grep ssm
```

Bucket name **and region** matter. `aws s3 cp` and `aws s3 presign` default to the AWS_REGION env var or `us-east-1` if unset — pin `--region <region>` on every CLI invocation that touches the bucket. Bucket-region mismatch returns an HTML error page that downstream tools (e.g. `gunzip | docker load`) will fail on with cryptic errors like `gzip: stdin: not in gzip format`.

### 3. Diff against the closest precedent orchestrator

Before writing a new orchestrator, find the closest precedent (`cgnat-validation.yml`, `pgbackrest-wave18.yml`) and **diff the phase structure**. Differences should be deliberate, not accidental.

Concrete checklist:

- [ ] Does the precedent expose mgmt API on EIP via socat sidecar? (cgnat-validation Phase 2). Base mgmt-host compose binds management-a on `127.0.0.1:${MGMT_CONTAINER_HOST_PORT:-8080}` only — peers can't reach it via EIP without a sidecar.
- [ ] Does the precedent seed accounts via `mgmt-host-bootstrap` CLI (ADR 0410) or DB-direct? Today's canonical is the CLI; DB-direct is the broken pattern that burned 2026-04-25.
- [ ] Does the precedent use cloud-init self-enrolment for peers, or ansible-side SSM enrolment? Cloud-init is the speed-up pattern (ADR-pending) but requires the setup-key UUID to be terraform-input-pre-generated. SSM-side requires waiting for the key from mgmt-host-bootstrap output.

### 4. Read every imported playbook + role

`import_playbook: mgmt-host.yml` brings in roles you didn't write. Read each role's `tasks/main.yml` — particularly any `register / failed_when / changed_when` task. Race conditions like `systemctl is-active` returning `activating` (which I hit on tier 4b run #4) are visible at read time.

### 5. Localhost play discipline

For ansible plays with `hosts: localhost` (e.g. local image-archive upload + presign):

- [ ] `become: false` explicitly — Ansible's default may try to escalate, which fails on dev workstations without passwordless sudo.
- [ ] `connection: local` — don't use the inventory's connection plugin.
- [ ] `gather_facts: false` — usually unnecessary on localhost.

### 6. Trap discipline in orchestrator shell scripts

If `run.sh` does `cd ops/ansible` mid-run and trap calls `terraform -chdir=terraform destroy`, the trap fails because cwd has moved. Capture `REPO_ROOT` at script start and `cd "$REPO_ROOT"` inside the trap before any teardown commands.

```bash
REPO_ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"

cleanup() {
  cd "$REPO_ROOT"
  avx terraform -chdir=terraform destroy -auto-approve "${TF_TARGETS[@]}"
}
trap cleanup EXIT
```

### 7. Dry-run against an existing env

Before firing AWS for real, run `--syntax-check` and (where the orchestrator's prior phases are idempotent) `ansible-playbook --check` against an already-running env that resembles the target shape (integdev for mgmt-plane shape; cgnat-validation for symmetric-NAT shape). Surfaces `register / failed_when` mistakes without spinning fresh infra.

```bash
cd ops/ansible
OBJC_DISABLE_INITIALIZE_FORK_SAFETY=YES aws-vault exec <profile> --no-session -- \
  ansible-playbook --syntax-check -i inventory/<env>.ini <env>.yml
```

### 8. Static-audit the post-bring-up steps before firing

Before firing AWS, mentally walk every line of `run.sh` STEP 4+ and the
playbook's Phase 4+ for these specific patterns. Each pattern caught a real
bug in tier 4b's first-fire cycle.

- **SSM-as-ssm-user privilege**: `aws ssm send-command` runs as `ssm-user`
  by default. Check every write path — `/var/log/*`, `/etc/*`, `/usr/*` are
  root-owned and need explicit `sudo` or a write-path swap to `/tmp/*` or
  `~ssm-user/*`. Failure mode: silent permission-denied → downstream step
  hangs / returns empty.
  *Caught fire #9 audit: `nohup iperf3 -s -D > /var/log/iperf3-server.log`
  swapped to `/tmp/iperf3-server.log`.*

- **Poll-after-state-change requires retry-with-success-predicate**: A
  fixed `sleep 30` after `netbird up` doesn't guarantee enrolment is
  complete. Wrap the post-state-change probe in a 6-cycle loop with a
  validator (e.g. mesh-IP must match `^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$`).
  Each retry costs $0 if same env stays up.
  *Caught fire #9 audit: `resolve_mesh_ip()` lifted from one-shot to 6× retry.*

- **Multi-layer shell escape trace**: `bash → JSON --parameters → remote
  shell → python -c → Python source` is 4 layers of escaping. Trace each
  backslash level. Especially: JSON inside a single-quoted string, with
  `\"` for inner double quotes, with `\\\"` for python double-quotes
  inside a python -c argument.
  *Pattern: write the desired final command first, then add escapes per
  layer outward.*

- **Play-level var shadowing role-var names**: `vars: { goat_relay_image:
  ... }` at play level + `include_role.vars.goat_relay_image: "{{
  goat_relay_image }}"` triggers Jinja2 recursion. Rename one. Convention:
  bench-specific play vars use `bench_*` prefix; role vars stay as
  documented (`goat_relay_*`).
  *Caught fire #6.*

- **Roles run before tasks in a play**: `pre_tasks → roles → tasks →
  post_tasks`. If a role assumes a directory or file that's created in
  the play's `tasks:` block, it'll fail on fresh hosts (works on existing
  hosts because state persists). Either move the dependency into the
  role itself, or move the role include to `post_tasks`.
  *Caught fire #7: mgmt-host-compose-watch role assumed `/opt/mgmt-host`
  exists; created the dir as the role's first task.*

- **Race-condition systemd state checks**: `systemctl is-active <unit>`
  immediately after `state: started` may return `activating` (Type=simple
  with Restart=always loops if ExecStart fails). Use `failed_when not in
  ["active", "activating"]` + `retries: N + until: stdout == "active"`.
  *Caught fire #4.*

## Cataloging unavoidable transients

Some failures are real and inherent:

- SSM file-transfer flakes on certain file extensions / sizes. Re-fire usually clears.
- AWS API throttling on bulk SSM send-command. Pace requests.
- Mid-run cloud-init slow paths on boot-image install variations. Bias `sleep` higher rather than racing.

When you hit one of these, **catalog it** in a finding (F-NNN per ADR 1011) so the next person doesn't burn cycles reading "transient or bug?"

## Tier 4b first-fire post-mortem (2026-04-30)

Six fires to first verdict; 5 of 6 failures were avoidable per the checks above:

| Fire | Avoidable | Check that would have caught it |
| --- | --- | --- |
| 1 (VPC quota) | YES | §1 |
| 2 (sudo + trap cwd) | YES | §5 + §6 |
| 3 (SSM .service flake) | NO | (transient — would have caught on retry pattern) |
| 4 (is-active race) | PARTIALLY | §4 (read every imported role) |
| 5 (presign region) | YES | §2 |
| 6 (socat sidecar missing) | YES | §3 (diff against precedent) |

Cumulative AWS cost: ~$0.50. Time elapsed: ~2 hr. Net "useful test cycles" (time spent actually exercising the goat-relay code path under measurement): currently 0. This doc exists so the next ephemeral env doesn't repeat the cycle.
