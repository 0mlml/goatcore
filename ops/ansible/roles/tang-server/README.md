# tang-server

Per-Orin Tang server for Block 65C peer-auto-unlock per ADR 0612. Companion to the `tang_client` mode in [`../luks-unlock/`](../luks-unlock/).

## What it does

- Installs `tang` (Linux distribution package).
- Generates ECDH key material under `/var/db/tang/` on first run (idempotent — Tang itself only generates keys when none exist).
- Renders a systemd socket unit listening on a wg-cp0-internal address (default: the host's address inside `198.18.0.0/24`, port 7500).
- Renders a corresponding systemd service unit.
- Enables both units; starts the socket.
- Pre-flight asserts the configured listen address is in `198.18.0.0/24` — public Tang is rejected to preserve the silent-control-plane invariant ([ADR 0313](../../../../docs/adr/0313-silent-control-plane-outer-wg-wrap.md)).

## What it deliberately does NOT do

- **No public listener.** Tang on Orin binds wg-cp0 only.
- **No TLS reverse proxy in front of it.** The wg-cp0 tunnel is the encryption layer; Tang's own cleartext HTTP is fine inside that envelope (this matches the standard Tang+Clevis deployment pattern, where mTLS or VPN is the trust anchor).
- **No automatic key rotation.** Operator-fired via `tang-show-keys` / `/var/db/tang/<thp>.jwk` rotation. A key rotation requires re-binding every client; not appropriate for autonomous play execution.

## Variables

| Variable | Default | Description |
|---|---|---|
| `tang_listen_address` | `{{ wg_cp0_address }}` | Address Tang listens on. Pre-flight assertion: must be in `198.18.0.0/24`. |
| `tang_listen_port` | `7500` | Port Tang listens on. |
| `tang_db_dir` | `/var/db/tang` | Directory holding the JWK key files. Tang itself manages contents. |

## Example

```yaml
# group_vars/vega_hosts/vars.yml
tang_listen_address: "{{ wg_cp0_address }}"   # e.g. 198.18.0.11 (vega-A) or 198.18.0.12 (vega-B)
tang_listen_port: 7500
```

Then in the inventory's mgmt-host playbook:

```yaml
- hosts: vega_hosts
  roles:
    - role: tang-server
```

The corresponding `tang_client` config on the *peer* Orin's luks-unlock role:

```yaml
luks_volumes:
  - name: docker-data
    device: /dev/nvme0n1
    mount_point: /opt/docker-data
    filesystem: ext4
    unlock_modes: [passphrase, fido2, tang_client]   # parallel keyslots per ADR 0612
luks_tang_servers:
  - name: peer-orin-b
    url: "http://198.18.0.12:7500"   # peer's wg-cp0 address — A binds to B, B binds to A
```

## Operational notes

- **Key rotation operator workflow** (manual): rotate the on-disk JWK files, restart `tangd.socket`, then re-bind every client (`clevis luks regen ...`). Documented at `docs/operations/tang-key-rotation.md` (TODO: page to author when first rotation is needed).
- **Simultaneous-cold-boot failure mode** (Block 65C.3): if both Orins in a site reboot together, neither has a peer Tang to unlock against. Handled by mode A (fTPM auto-unseal) or mode B (dropbear remote unlock); Tang alone does not close this case.

## References

- ADR 0612 — multi-modal LUKS unlock continuity-priority posture
- Block 65C — Tang/NBDE peer auto-unlock implementation tracking
- [Tang upstream](https://github.com/latchset/tang)
- [Clevis upstream](https://github.com/latchset/clevis)
