package main

// Block 22R.2 — keypair reuse + rotation flags for bundle-create.
//
// ADR 0322 Property 4: today every bundle-create mints a fresh
// Curve25519 keypair, so endpoint additions, TTL extensions, ACL group
// changes, and forwarder-tier flips all force gratuitous rotation.
// Every gratuitous rotation is a chance for F-114-class drift.
//
// Two new flags give the operator explicit control:
//
//   --reuse-keypair-from <peer-name>
//     Pull the peer's CurrentPubkey from the peerset and reuse it in
//     the new bundle's CPDevicePubkey. Privkey is intentionally
//     omitted (the device-side handler preserves its existing
//     privkey for metadata-refresh bundles). The killer feature for
//     "add an endpoint to every peer" (Block 22Q SJC redistribution).
//
//   --rotate-keypair --reason "..."
//     Mint a fresh keypair, then call peerset.RotatePubkey to record
//     the rotation in pubkey_history[] with the operator-supplied
//     reason. Explicit, audited.
//
// The two are mutually exclusive. Ambiguous bare invocations
// (no flag, but the peer already exists in the peerset) fail-loud —
// the previous behavior of silently minting a new keypair is the
// F-114 trigger class and is no longer reachable.
//
// New peers (device-id not in peerset) keep the default fresh-mint
// behavior; no flag is required.

import (
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/usareur-af/goatcore/ops/enrollment/peerset"
)

// ErrBareCreateForExistingPeer is returned when an operator runs
// bundle-create against a device-id that already exists in the peerset
// without passing --reuse-keypair-from or --rotate-keypair. The previous
// silent re-mint is the F-114 trigger class; the operator must now
// declare intent explicitly.
var ErrBareCreateForExistingPeer = errors.New(
	"bundle-create: peer already exists in peerset; pass --reuse-keypair-from " +
		"(additive metadata-refresh) or --rotate-keypair --reason \"...\" " +
		"(explicit rotation) to declare intent — the previous silent re-mint " +
		"was the F-114 trigger class and is no longer reachable")

// ErrMutuallyExclusiveKeypairFlags is returned when both
// --reuse-keypair-from and --rotate-keypair are passed.
var ErrMutuallyExclusiveKeypairFlags = errors.New(
	"bundle-create: --reuse-keypair-from and --rotate-keypair are mutually " +
		"exclusive (one means 'preserve the peer's pubkey', the other 'mint a fresh one')")

// ErrRotateKeypairMissingReason is returned when --rotate-keypair is
// passed without a non-empty --reason. Rotation must carry a documented
// reason for the audit log + pubkey_history entry.
var ErrRotateKeypairMissingReason = errors.New(
	"bundle-create: --rotate-keypair requires --reason \"<text>\" " +
		"(rotation reason is recorded in peerset pubkey_history[])")

// ErrReuseKeypairFromUnknownPeer is returned when --reuse-keypair-from
// names a peer that doesn't exist in the active set of the peerset
// (including missing peerset path).
var ErrReuseKeypairFromUnknownPeer = errors.New(
	"bundle-create: --reuse-keypair-from names a peer not in the peerset; " +
		"check the peer-name (case-sensitive) or omit the flag for a new peer")

// ErrReuseKeypairWithoutPeerset is returned when --reuse-keypair-from
// or --rotate-keypair is passed but --update-allowlist isn't, so we
// have no peerset path to consult.
var ErrReuseKeypairWithoutPeerset = errors.New(
	"bundle-create: --reuse-keypair-from / --rotate-keypair require " +
		"--update-allowlist <path> so the peerset can be read + written")

// keypairIntent captures the operator's declared lifecycle intent. The
// run() main path consumes it after validateKeypairIntent decides which
// branch fires; the result drives:
//   - whether generateWGKeypair is called (rotate: yes; reuse: no);
//   - what goes into bundle.CPDevicePubkey + CPDevicePrivkey;
//   - whether peerset.RotatePubkey fires after the bundle is built.
type keypairIntent struct {
	// Kind is one of "new" (default, new peer / no peerset / no
	// existing entry), "reuse" (--reuse-keypair-from), or "rotate"
	// (--rotate-keypair). The bare-create case for an existing peer
	// is rejected before this is built.
	Kind string

	// ReusedPubkey is the Curve25519 pubkey looked up from the peerset
	// when Kind == "reuse". Empty otherwise.
	ReusedPubkey []byte

	// RotateReason is the operator-supplied audit string when Kind ==
	// "rotate". Empty otherwise. Recorded in peerset pubkey_history[].
	RotateReason string

	// PeerExists is true when the device-id already has an entry in
	// the peerset's active set (regardless of Kind). Used by the run()
	// flow to know whether peerset.RotatePubkey vs peerset.Upsert is
	// the right write path post-bundle.
	PeerExists bool
}

// resolveKeypairIntent inspects the operator's flag combination + the
// peerset state and returns the declared intent. Validation errors
// (mutual exclusion, missing reason, unknown peer) surface as typed
// sentinels. Returns (nil, nil) when no peerset path is configured AND
// neither new flag is set — the historical behavior is preserved for
// today's callers that don't pass --update-allowlist.
func resolveKeypairIntent(args runArgs) (*keypairIntent, error) {
	// Step 1 — mutual exclusion + reason check, independent of peerset
	// state. These catch operator-typos at the earliest point.
	if args.ReuseKeypairFrom != "" && args.RotateKeypair {
		return nil, ErrMutuallyExclusiveKeypairFlags
	}
	if args.RotateKeypair && args.RotateReason == "" {
		return nil, ErrRotateKeypairMissingReason
	}

	// Step 2 — flags require a peerset to operate against.
	if (args.ReuseKeypairFrom != "" || args.RotateKeypair) && args.UpdateAllowlistPath == "" {
		return nil, ErrReuseKeypairWithoutPeerset
	}

	// Step 3 — if no peerset path is configured, return the legacy
	// path's intent (a fresh-mint new peer) regardless of whether the
	// peer-name "looks new". Today's bundle-create runs without
	// --update-allowlist for ad-hoc dev bundles; that path stays.
	if args.UpdateAllowlistPath == "" {
		return &keypairIntent{Kind: "new"}, nil
	}

	// Step 4 — load the peerset and learn whether the device-id is
	// already in the active set. ErrNotExist from peerset.Load is
	// silently treated as "empty peerset" upstream (see peerset/io.go);
	// no separate handling needed.
	ps, err := peerset.Load(args.UpdateAllowlistPath)
	if err != nil {
		return nil, fmt.Errorf("load peerset for keypair-intent resolution: %w", err)
	}

	intent := &keypairIntent{}
	if _, exists := ps.Lookup(args.DeviceID); exists {
		intent.PeerExists = true
	}

	// Step 5 — branch on the flag combination.
	switch {
	case args.ReuseKeypairFrom != "":
		intent.Kind = "reuse"
		peer, ok := ps.Lookup(args.ReuseKeypairFrom)
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrReuseKeypairFromUnknownPeer, args.ReuseKeypairFrom)
		}
		pub, err := base64.StdEncoding.DecodeString(peer.CurrentPubkey)
		if err != nil {
			return nil, fmt.Errorf("decode stored pubkey for peer %q: %w", args.ReuseKeypairFrom, err)
		}
		if len(pub) != wgPubkeySize {
			return nil, fmt.Errorf(
				"stored pubkey for peer %q has wrong size: got %d want %d",
				args.ReuseKeypairFrom, len(pub), wgPubkeySize)
		}
		intent.ReusedPubkey = pub
	case args.RotateKeypair:
		intent.Kind = "rotate"
		intent.RotateReason = args.RotateReason
		if !intent.PeerExists {
			// Rotating a peer that doesn't exist is a footgun — the
			// operator likely meant a fresh onboard. Refuse rather
			// than silently treat it as a first-mint.
			return nil, fmt.Errorf(
				"--rotate-keypair: peer %q not in peerset; this flag is for "+
					"rotating an existing peer's key — omit the flag for a new peer",
				args.DeviceID)
		}
	case intent.PeerExists:
		// Bare invocation against an existing peer — the F-114 trigger
		// class. Fail-loud.
		return nil, fmt.Errorf("%w: %q", ErrBareCreateForExistingPeer, args.DeviceID)
	default:
		intent.Kind = "new"
	}

	return intent, nil
}
