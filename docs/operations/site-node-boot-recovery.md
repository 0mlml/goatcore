# Site-node boot and LUKS recovery

Use this procedure when a site infrastructure node stops before the normal OS,
waits at a LUKS prompt, or cannot be reached through its normal shell door.
This is the portable recovery sequence. Hardware-specific installation remains
in [`bare-metal-install.md`](bare-metal-install.md); the declared unlock classes
and their certification status remain in
`host-site-node-disk-encryption.md`.

> **Authority and data-safety boundary.** Reading console state and attempting
> one already-enrolled unlock method are recovery operations. Adding, deleting,
> or rebinding a LUKS slot; rebuilding an initramfs; changing firmware, boot
> order, networking, or an operator roster; reimaging; and promoting another
> node are separate changes and require explicit authorization. Never record a
> passphrase, private key, or recovery secret in command output or an issue.

## 1. Establish the declared recovery shape

Before touching the node, identify these facts in inventory and group vars:

- node role and hardware class;
- encrypted device and expected mapper/mount;
- declared unlock class (`tpm2`, `tang_initramfs`, or another documented class);
- whether physical/serial console access exists;
- whether `dropbear_remote_unlock` is enabled and, if so, its declared
  initramfs address, port, and authorized-key roster;
- the independent network-jump or break-glass path for the site.

Do not infer an address, interface, device, or unlock method from another site.
If any required fact is absent or contradictory, stop and escalate with the
evidence packet from
[`site-node-troubleshooting.md`](site-node-troubleshooting.md#escalation-packet).

## 2. Observe before attempting unlock

Use the physical, serial, or out-of-band console to record the last visible
stage and exact error. Classify it before proceeding:

| Observation | Next step |
|---|---|
| A normal LUKS passphrase prompt is visible | [Physical-console fallback](#3-physical-console-fallback) |
| No console is locally available and declared dropbear initramfs is reachable | [Remote initramfs fallback](#4-remote-initramfs-fallback) |
| Firmware cannot find the intended boot device, or failure occurs before LUKS | [Boot-source recovery](#5-boot-source-or-pre-luks-failure) |
| The prompt rejects the approved passphrase, the LUKS header/device differs from declaration, or the disk reports I/O errors | [Stop and escalate](#7-stop-and-escalate) |

Do not repeatedly try an uncertain passphrase. Do not reboot-loop a node whose
disk is reporting errors.

## 3. Physical-console fallback

This is the recovery path for an enrolled passphrase slot when the automatic
TPM or Tang path did not unlock the volume.

1. Confirm that the prompt names the expected encrypted device or mapper.
2. Enter the approved passphrase at the console. Do not echo, photograph, or
   transcribe it.
3. If it is accepted, allow the current boot to complete. Do not reboot again
   merely to test the automatic path during incident recovery.
4. Continue with [post-boot verification](#6-post-boot-verification).
5. If the approved passphrase is rejected once, stop. Confirm custody and the
   declared device out of band before any further attempt.

This procedure uses an existing keyslot only. It does not authorize a keyslot
change.

## 4. Remote initramfs fallback

Use this only when the node declares `dropbear_remote_unlock`, the operator's
public key is in the declared roster, and an authorized path reaches the
declared initramfs address. The mechanism and restrictions are canonical in
[`dropbear-initramfs`](../../ops/ansible/roles/dropbear-initramfs/README.md).

1. Reach a declared same-site jump host through the independent recovery
   transport. Do not expose the initramfs listener publicly.
2. From trusted inventory and the enrolled-key record, establish which profile
   is declared. A forced-command profile explicitly sets either Dropbear
   `-c /bin/cryptroot-unlock` or the operator key's
   `command="/bin/cryptroot-unlock"` restriction. If neither is declared, the
   role-default profile opens an initramfs shell; do not assume that a bare SSH
   login invokes the unlock helper.
3. From that jump host, use the exact address and port from the node's
   declaration. For a declared forced-command profile, connect without a
   remote command:

   ```text
   ssh -o StrictHostKeyChecking=ask \
     -p "<initramfs-port>" "root@<initramfs-address>"
   ```

   For the role-default shell profile, explicitly request the installed helper
   and allocate a terminal:

   ```text
   ssh -t -o StrictHostKeyChecking=ask \
     -p "<initramfs-port>" "root@<initramfs-address>" cryptroot-unlock
   ```

   An operator who intentionally opens the initramfs shell instead must run
   `cryptroot-unlock` there before entering the passphrase.
4. Before accepting the connection or entering any secret, verify the
   presented host key by the site's approved out-of-band method. Do not accept
   an unexplained changed key.
5. Enter the approved passphrase once at the prompt. A successful unlock ends
   the SSH session as boot pivots to the normal OS.
6. Continue with [post-boot verification](#6-post-boot-verification).

If key authentication, routing, or host-key verification fails, preserve that
failure as evidence and stop. Do not enable password SSH, copy a new key into
the initramfs, or rebuild it during this recovery attempt.

## 5. Boot-source or pre-LUKS failure

If the expected LUKS prompt never appears, the failure is below the unlock
mechanism. Record the firmware-selected boot device and the disk identifiers
visible to firmware or rescue media. Then use the matching hardware section in
[`bare-metal-install.md`](bare-metal-install.md): its firmware/boot-source rules
are canonical, including the Raspberry Pi NVMe-versus-SD boot-order trap.

Changing boot order, repairing partitions, or reimaging is not part of this
incident procedure. Stop for authorization after the read-only comparison. A
reimage is destructive and must not begin until the node's data-recovery and
cluster-role consequences have been decided.

## 6. Post-boot verification

After either fallback reaches the normal OS, capture the current boot before
changing anything:

```text
lsblk --fs
findmnt --target "<encrypted-mount>"
systemctl --no-pager --full status 'systemd-cryptsetup@*'
journalctl -b -u 'systemd-cryptsetup@*' --no-pager
systemctl --failed --no-pager
```

Verify all of the following:

- the expected encrypted device is open and backs the declared mount;
- the normal roster-backed shell door works;
- the independent recovery path still works;
- services that require the encrypted mount did not start against an empty
  mountpoint; and
- the node's applicable acceptance rows pass.

Recovery of the current boot does not prove automatic unlock is healthy. File
the automatic-path failure with the evidence below. Repair or re-enrol the
declared TPM/Tang/dropbear mechanism later through
`mgmt-host-luks.yml` and its owning role, in a separately authorized maintenance
window. The certification requirement remains a real cold boot as defined by
`host-site-node-disk-encryption.md` §7.

## 7. Stop and escalate

Stop without further unlock attempts when:

- the approved passphrase is rejected;
- the observed disk, UUID, mapper, or unlock class differs from declaration;
- no declared console or independent recovery path is available;
- the host key changes without an approved explanation;
- the disk reports I/O, metadata, or LUKS-header errors;
- successful boot would risk a conflicting active core/standby role; or
- the next action would add/remove a keyslot, rebuild initramfs, alter boot
  order, repair a filesystem, reimage, or promote a node.

Collect the site/node role, hardware class, declared unlock and access paths,
last console output, boot identifier, sanitized `lsblk`/unit/journal output,
each path attempted and its failure class, and last-known-good boot. Exclude all
secret material.

## Pass condition

The recovery attempt passes when the node reaches the normal OS through an
already-enrolled method, the expected encrypted mount is present, dependent
services did not start on an empty mountpoint, the normal shell door works, and
the declared independent recovery path remains available. Automatic-unlock
repair is a separate task until a subsequently authorized cold boot certifies
it.
