# goat-relay-protocol

Wire-protocol library for the Netbird relay (`rels://` over WebSocket and QUIC).
Clean-room reimplementation; Apache-2.0.

This crate is the parse / serialize / state-machine layer. It owns no I/O and
no async runtime — the async transport binary lives elsewhere (Block 33B work).

## Status

**Skeleton (Block 33A in progress).** Module layout matches the spec doc; each
module is a placeholder pending upstream-source reading.

## Where to start reading

- Spec: [`../docs/design/netbird-relay-wire-protocol.md`](../docs/design/netbird-relay-wire-protocol.md)
- Working state: `../docs/project/block-33-progress.md`
- Plan + sub-block status: `../docs/project/implementation-plan.md` §"Block 33"

## Build

```
cargo check
cargo test
```

The skeleton compiles and passes a single trivial test (`VERSION` constant
matches the Cargo metadata). Real tests land as the modules fill in.

## License

Apache-2.0. See [`LICENSE-APACHE`](LICENSE-APACHE).

The protocol semantics this crate implements derive from reading upstream
`netbirdio/netbird/` source — `relay/` (AGPLv3) for routing and lifecycle,
`shared/relay/messages/` (BSD-3-Clause) for wire-format. Per Oracle v Google
and Sega v Accolade, protocols are not copyrightable subject matter; this
crate transliterates no upstream code. See
[ADR 0110](../docs/adr/0110-license-posture-agplv3-forked-components.md) for
the project-wide license posture this crate sits within.
