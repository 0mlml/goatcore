package main

// Block 22R.2 / Session B — rotate subcommand.
//
//   wg-cp0 rotate <peer-name> --reason "<text>" [--bundle-out <path>] [--apply]
//
// Rotates an active peer's Curve25519 keypair. Calls
// peerset.RotatePubkey with the new pubkey + reason; the old pubkey
// is appended to pubkey_history[]. Dry-run-by-default.
//
// Fail-loud rules:
//   - --reason is required (rotations must carry an audit string).
//   - Peer must be in active set (not deprecated; not absent).
//   - --apply requires --bundle-out so the operator has a target for
//     the freshly-minted bundle.cbor.

import (
	"errors"
	"fmt"
	"io"

	"github.com/usareur-af/goatcore/ops/enrollment/peerset"
)

var rotateAccept = map[string]bool{
	"reason":     true,
	"bundle-out": true,
}

func cmdRotate(args []string, stdout, stderr io.Writer) int {
	la, pos, err := parseLifecycleFlags(args, rotateAccept)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 rotate: %v\n", err)
		return 2
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, "wg-cp0 rotate: requires exactly one positional arg <peer-name>")
		return 2
	}
	la.PeerName = pos[0]

	if err := validateRotateArgs(la); err != nil {
		fmt.Fprintf(stderr, "wg-cp0 rotate: %v\n", err)
		return 2
	}

	ps, err := loadOrEmpty(la.Path)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 rotate: %v\n", err)
		return 1
	}

	plan, err := buildRotatePlan(ps, la)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 rotate: %v\n", err)
		return 1
	}

	emitPreview(stdout, plan, la.Apply)

	if !la.Apply {
		return 0
	}

	// Apply path: hand the plan to Session A's Applier. The Applier
	// owns the transactional remove-old + add-new on each relay; the
	// peerset.RotatePubkey call is sequenced inside the Applier so
	// state-plane and apply-plane stay aligned (partial-step apply
	// failure leaves state ahead of live, which subsequent reconcile
	// heals — see design doc §3.2).
	if err := applyLifecyclePlan(ps, plan, stdout); err != nil {
		if errors.Is(err, errSessionAPending) {
			fmt.Fprintf(stderr, "\nwg-cp0 rotate: %v\n", err)
			fmt.Fprintf(stderr, "\nLegacy path until Session A lands:\n")
			fmt.Fprintf(stderr, "  bundle-create \\\n")
			fmt.Fprintf(stderr, "    --device-id %s --rotate-keypair --reason %q \\\n", la.PeerName, la.Reason)
			fmt.Fprintf(stderr, "    --update-allowlist %s --out %s \\\n", la.Path, la.BundleOut)
			fmt.Fprintf(stderr, "    --site <site> --wg-cp0-address <addr>/24 \\\n")
			fmt.Fprintf(stderr, "    --wg-cp0-relays <relays.json> --acl-groups <g1,g2> \\\n")
			fmt.Fprintf(stderr, "    --ca-id <id> --ca-key <key.age> --age-identity <id.txt>\n")
			return 1
		}
		fmt.Fprintf(stderr, "wg-cp0 rotate: %v\n", err)
		return 1
	}
	return 0
}

func validateRotateArgs(la *lifecycleArgs) error {
	if la.PeerName == "" {
		return errors.New("peer-name is required (positional argument)")
	}
	if la.Reason == "" {
		return errors.New("--reason \"<text>\" is required (rotation must carry an audit string)")
	}
	if la.Apply && la.BundleOut == "" {
		return errors.New("--apply requires --bundle-out <path> so the minted bundle has a destination")
	}
	return nil
}

func buildRotatePlan(ps *peerset.PeerSet, la *lifecycleArgs) (*lifecyclePlan, error) {
	peer, ok := ps.Lookup(la.PeerName)
	if !ok {
		// Helpful misdirection-catch: maybe the operator meant a
		// deprecated peer (typo: rotate vs resurrect-then-rotate).
		if _, deprecated := ps.LookupDeprecated(la.PeerName); deprecated {
			return nil, fmt.Errorf("peer %q is in deprecated_peers[]; rotate is for active peers — use 'wg-cp0 resurrect %s' first",
				la.PeerName, la.PeerName)
		}
		return nil, fmt.Errorf("peer %q not in active set; use 'wg-cp0 onboard' for a new peer", la.PeerName)
	}

	priorPubkey := peer.CurrentPubkey

	plan := &lifecyclePlan{
		Op:       "rotate",
		PeerName: la.PeerName,
		StateRotate: &rotatePlanDetail{
			PeerName:    la.PeerName,
			PriorPubkey: priorPubkey,
			Reason:      la.Reason,
		},
		RelayRemove:    []string{priorPubkey},
		RelayAdd:       []string{derivePubkeyPlaceholder()},
		BundleMint:     la.BundleOut != "",
		BundleOutPath:  la.BundleOut,
		AffectedRelays: affectedRelaysFromPeerset(ps),
		NotesForOp: []string{
			"rotation is transactional: prior pubkey is removed AND new pubkey is added on every relay; partial-step failure surfaces as a recovery hint.",
			"audit reason flows into peerset pubkey_history[] AND (when Session A lands) the wg-cp0-ceremony-audit.jsonl.",
		},
	}
	return plan, nil
}
