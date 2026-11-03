# nova-lan bootstrap helpers — Tier-T client install on a closed LAN

Substrate for Phase B.3 of `docs/design/nova-lan-instance.md`. Distinct from the broader Block 68 [`first-boot.sh`](../first-boot/first-boot.sh) flow: that script is shaped for WAN-connected mgmt-host bringup (wg-cp0 enrollment, T1 S3 image pulls, netbird mesh join). The closed-LAN posture has none of those — instead, Tier-T clients on the closed network need exactly two things at first boot:

1. The `nova-lan-ca` root certificate imported into the host trust store, so TLS to `*.nova.lan` services validates without warnings.
2. DNS for the `nova.lan` zone pointing at the on-LAN authoritative CoreDNS (nova-lan-A's LAN IP, port 53).

That's it. No mesh enrollment. No image pull from any external mirror. No keypair issuance ceremony on the client (the operator issues a client cert per `docs/operations/nova-lan-cert-issuance.md` and hand-delivers it separately).

## What lives here

| Path | Purpose |
|---|---|
| `tier-t-install.sh` | Idempotent installer. Reads a USB-supplied or local directory containing the closed-LAN bootstrap pack (CA root + resolver-IP marker file). Imports CA, configures resolver, emits a state marker + JSONL log mirroring the Block 68 first-boot.sh shape so the same drill harness can poll progress. |
| `pack-layout.md` | Specifies the USB-supplied directory layout the install script consumes. |

## The closed-LAN bootstrap pack

A tarball produced by the operator on their FDE-on laptop (the same laptop that ran the `nova-lan-ca` ceremony — see `ops/enrollment/ca/nova-lan/README.md`). Contents:

```
nova-lan-bootstrap.tar.gz
├── ca/
│   └── nova-lan-ca-root.pem    # Public root cert. Imported into the host trust store.
├── dns/
│   └── resolver-ip.txt       # Single line: <nova-lan-a-lan-ip>. Used to configure systemd-resolved
│                             # or fall back to /etc/resolv.conf for the nova.lan zone.
└── manifest.yml              # schema_version + ca SHA-256 + resolver-ip + minted_at + minted_by
```

The bundle has no secrets — the CA root is public material, the resolver IP is non-secret. The closed-LAN posture means there is no encrypted-blob layer (`age` recipients) on this pack; threat model is physical-media handling, not network interception. If physical-media handling becomes a concern, wrap the pack in an `age`-encrypted blob with the operator's pubkey as recipient.

## The install script — `tier-t-install.sh`

Idempotent. Re-running is a no-op if the state marker says "complete." Pass `--force` to bypass.

```bash
sudo /opt/goat/ops/bootstrap/nova-lan/tier-t-install.sh \
    --pack /mnt/usb/nova-lan-bootstrap
```

Phases:

1. Find pack — accepts `--pack <dir>` or scans common USB mount points if not specified.
2. Validate pack — schema check on `manifest.yml`, SHA-256 check on `ca/nova-lan-ca-root.pem` matches `manifest.ca_sha256`.
3. Install CA root — `cp` to `/usr/local/share/ca-certificates/nova-lan-ca-root.crt` + `update-ca-certificates`. Failure modes (non-Debian-family distro) emit a clear error pointing at per-OS docs.
4. Configure resolver — prefers systemd-resolved per-domain routing (`SetLinkDomains` for `nova.lan`); falls back to `/etc/resolv.conf` `search nova.lan` + `nameserver <resolver-ip>` when systemd-resolved isn't present.
5. Smoke test — `dig @<resolver-ip> portal.nova.lan +short` returns non-empty.
6. Emit state marker — `/var/lib/goat/nova-lan-tier-t-state.json` + JSONL log.

The script is Linux-Ubuntu-first (the floor target for the mgmt-host and modern producer/consumer boxes on the closed LAN). macOS / Windows operator workstations follow per-OS instructions — pointer in the script's `--help` output, and in `docs/operations/nova-lan-cert-issuance.md` § "Importing the CA root."

## Mounts to existing Block 68 substrate

The closed-LAN install script reuses the Block 68 conventions where it can without inheriting WAN assumptions:

| Block 68 convention | nova-lan adoption |
|---|---|
| State marker at `/var/lib/goat/<scope>-state.json` | ✅ — adopts the same path pattern, scope `nova-lan-tier-t` |
| JSONL log at `/var/log/goat/<scope>.jsonl` | ✅ — same pattern |
| `--force` re-run flag | ✅ — same semantics |
| `emit <phase> <status>` helper shape | ✅ — copy-paste the shape, distinct script |
| Drill harness poll-shape | ✅ — drill driver can ssh-poll the JSONL the same way |
| USB userdata schema | ❌ — schema is irrelevant to Tier-T install (no site_id, no profile, no wg_cp0_endpoint, no scenario, no netbird-mesh-join state). The pack's `manifest.yml` is its own minimal schema. |
| Configure secrets via expect on serial console | ❌ — no secrets to configure at install time. |
| Invoke site-bootstrap.yml against localhost | ❌ — closed-LAN clients don't run the mgmt-stack; only consume it. |

Reusing the conventions where they fit means the drill harness + operator muscle-memory transfer for free. Diverging where the postures genuinely differ keeps both paths honest.

## Composes with

- Phase B.1: `ops/enrollment/ca/nova-lan/` — produces `nova-lan-ca-root.pem` that this script imports.
- Phase B.2: [`ops/ansible/roles/lan-coredns/`](../../ansible/roles/lan-coredns/) — the resolver the script configures clients to talk to.
- Phase C (future): Ansible role tree — the mgmt-host (nova-lan-A) itself does NOT run this script. nova-lan-A runs the lan-coredns role + Traefik + Ory + etc. via the Phase C Ansible playbook. This script is exclusively for *clients* of nova-lan-A.
