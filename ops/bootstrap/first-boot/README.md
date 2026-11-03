# first-boot — rapid-bringup config absorption

On-host first-boot logic for Block 68 rapid-bringup images. Runs as a
oneshot systemd unit at first boot of a freshly-flashed image.

**Spec:** `docs/project/block-68a-config-input-spike.md`
**Design:** `docs/design/rapid-site-bringup.md` §4

## Files

- [`userdata-schema.yml`](userdata-schema.yml) — JSON Schema for the
  USB-stick userdata.yml file. Operator-side editors should validate
  against this before flashing the USB.
- [`userdata.yml.example`](userdata.yml.example) — annotated example
  with every field documented for every profile + scenario combination.
- [`first-boot.sh`](first-boot.sh) — the on-host script. Reads USB
  userdata, prompts for secrets via serial console, assembles
  `.envrc.local` + group_vars overlay, invokes
  `site-bootstrap.yml -e site_id=<site>` against localhost, emits
  per-phase JSONL evidence.
- [`first-boot.service`](first-boot.service) — systemd unit. Activated
  by the build-image-* scripts at image build time.

## Operator workflow (laptop side, before bringup)

1. Copy `userdata.yml.example` to a fresh USB stick as `userdata.yml`.
2. Fill in non-secret fields per the schema. Validate locally with
   `yq` + the JSON Schema.
3. Eject USB. Carry to bringup site.

## On-host workflow (Orin / Karbon, at first boot)

1. Image boots; LUKS already enrolled at build time with the Block 65
   multi-modal stack.
2. `first-boot.service` fires once after multi-user.target is reached.
3. Script finds the first USB with `userdata.yml` at the root.
4. Validates against the schema; honest-fail-and-pause-for-fix on
   schema mismatch.
5. Prompts on serial console for the per-profile secret set.
6. Renders `/etc/goat/.envrc.local` + `group_vars/<site>_hosts/local.yml`.
7. Execs `cd /opt/goat/repo/ops/ansible && \
   ansible-playbook -i inventory/<env>.ini site-bootstrap.yml \
   -e site_id=<site>` against localhost (using `connection: local`).
8. Emits per-phase JSONL records to
   `/var/log/goat/first-boot.jsonl`.

## Re-run safety

State marker at `/var/lib/goat/first-boot-state.json`. If
present and `complete=true`, the script exits 0 immediately (an
operator-fired re-run requires `--force`).

## Failure modes

- USB not found → wait 60s, retry; if still not found, fail with clear
  serial-console message + drop to a login shell.
- Schema validation fails → print field-level errors; drop to login
  shell so operator can fix the USB and re-run.
- Serial-console prompt times out → no default; require operator
  intervention. (We do NOT auto-generate secrets in S2 mode because the
  cluster invariant per CLAUDE.md is that `<SITE>_NB_ENCKEY` MUST equal
  the existing cluster's value — a generated value would silently break
  Spock peering.)
- `site-bootstrap.yml` fails → JSONL evidence captures the phase that
  failed; first-boot.service exits non-zero; operator gets a login shell
  to triage.

## What this script deliberately does NOT do

- Pull bundle / image from cloud at first boot. The image already
  contains everything; cloud is only the build-time + image-pull-time
  source.
- Generate cluster-wide secrets in S2 mode. Cluster invariants
  (`<SITE>_NB_ENCKEY`, etc.) require operator-supplied values that match
  the existing cluster.
- Touch any non-localhost network until LUKS is up + secrets are
  absorbed + `site-bootstrap.yml` is invoked.
