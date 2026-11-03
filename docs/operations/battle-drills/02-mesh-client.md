# 02 — Mesh client absent, silent, or disconnected

## Symptom

Doctor step 0 fails `mesh.client` or `mesh.tunnel`.

## Conditions

You know whether this device uses NetBird or goat-clientd. Doctor reports
only the client's observed state; an address alone is not a live fabric proof.

## Steps

1. For NetBird, run `netbird status -d`. For goat-clientd, run
   `goat client status --detail` and `goat client diagnostics`.
2. If NetBird is installed but its daemon is stopped, start it with
   `sudo netbird service start` and repeat the status command. If goat-clientd
   has no bundle, import the issued client bundle with
   `goat client bundle import <bundle.cbor>`.
3. If an enrolled client responds but is disconnected, run `goat heal` or
   `goat client up` as appropriate, then rerun doctor. Doctor itself does not
   bounce a shared machine-wide client.

## Pass

The client reports a connected tunnel and doctor reaches the router path
probe. The router probe, not interface enumeration, establishes usable reach.

## Escalate

If the client repeatedly disconnects, provide its diagnostics to the
operator. Do not repeatedly re-enroll on the basis of a TCP timeout alone.

## Evidence

Record the client's reported profile, management URL, connection state, and
doctor check IDs. Redact setup keys and bundle contents.

Source: `crates/goat-cli/src/cmd/client.rs`, `crates/goat-cli/src/cmd/heal.rs`.
