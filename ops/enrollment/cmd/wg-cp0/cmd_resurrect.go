package main

// Block 22R.2 / Session B — resurrect subcommand.
//
//   wg-cp0 resurrect <peer-name> [--apply]
//
// Inverse of remove: pulls a peer back from deprecated_peers[] into
// peers[]. Useful for "we accidentally removed an active peer; put it
// back." Requires --apply for the state mutation (dry-run by default
// shows what would happen).
//
// The resurrected peer keeps its full prior record — CurrentPubkey,
// PubkeyHistory, all metadata — so re-adding the peer to relay-side
// allowlists is unambiguous (uses the same pubkey the operator
// previously distributed bundles for).

import (
	"errors"
	"fmt"
	"io"

	"github.com/usareur-af/goatcore/ops/enrollment/peerset"
)

// resurrect takes no subcommand-specific flags beyond the shared
// --path / --apply. The variable exists for parity with other
// cmd_*.go files and to make the parser signature uniform.
var resurrectAccept = map[string]bool{}

func cmdResurrect(args []string, stdout, stderr io.Writer) int {
	la, pos, err := parseLifecycleFlags(args, resurrectAccept)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 resurrect: %v\n", err)
		return 2
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, "wg-cp0 resurrect: requires exactly one positional arg <peer-name>")
		return 2
	}
	la.PeerName = pos[0]

	if la.PeerName == "" {
		fmt.Fprintln(stderr, "wg-cp0 resurrect: peer-name cannot be empty")
		return 2
	}

	ps, err := loadOrEmpty(la.Path)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 resurrect: %v\n", err)
		return 1
	}

	plan, err := buildResurrectPlan(ps, la)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 resurrect: %v\n", err)
		return 1
	}

	emitPreview(stdout, plan, la.Apply)

	if !la.Apply {
		return 0
	}

	if err := applyLifecyclePlan(ps, plan, stdout); err != nil {
		if errors.Is(err, errSessionAPending) {
			fmt.Fprintf(stderr, "\nwg-cp0 resurrect: %v\n", err)
			fmt.Fprintf(stderr, "\nLegacy path until Session A lands:\n")
			fmt.Fprintf(stderr, "  1. Manually edit %s — move %s row from deprecated_peers[] back to peers[]\n", la.Path, la.PeerName)
			fmt.Fprintf(stderr, "  2. Fire ops/ansible/wg-cp0-peer-surgical-update.yml with peer_new_pubkey=<from peerset> to re-add on each relay\n")
			return 1
		}
		fmt.Fprintf(stderr, "wg-cp0 resurrect: %v\n", err)
		return 1
	}
	return 0
}

func buildResurrectPlan(ps *peerset.PeerSet, la *lifecycleArgs) (*lifecyclePlan, error) {
	dep, ok := ps.LookupDeprecated(la.PeerName)
	if !ok {
		// Misdirection-catch: maybe the peer is active already.
		if _, active := ps.Lookup(la.PeerName); active {
			return nil, fmt.Errorf("peer %q is already in active set; nothing to resurrect", la.PeerName)
		}
		return nil, fmt.Errorf("peer %q not in deprecated_peers[]; nothing to resurrect (use 'wg-cp0 onboard' for a fresh peer)",
			la.PeerName)
	}

	// peerset.Resurrect enforces collision rejection (name/pubkey/
	// address) against the active set. Preview that failure shape at
	// plan time so dry-run is honest about what would fail.
	for _, p := range ps.Peers {
		switch {
		case p.Name == dep.Name:
			return nil, fmt.Errorf("cannot resurrect %q: an active peer with that name already exists", dep.Name)
		case p.CurrentPubkey == dep.CurrentPubkey:
			return nil, fmt.Errorf("cannot resurrect %q: its pubkey is held by active peer %q", dep.Name, p.Name)
		case p.WGCP0Address == dep.WGCP0Address:
			return nil, fmt.Errorf("cannot resurrect %q: its address %s is held by active peer %q", dep.Name, dep.WGCP0Address, p.Name)
		}
	}

	// Build a peer snapshot for the preview. Resurrect reuses every
	// field the deprecated row carried, including PubkeyHistory.
	restored := dep.Peer

	plan := &lifecyclePlan{
		Op:             "resurrect",
		PeerName:       la.PeerName,
		StateResurrect: &restored,
		RelayAdd:       []string{restored.CurrentPubkey},
		AffectedRelays: affectedRelaysFromPeerset(ps),
		NotesForOp: []string{
			fmt.Sprintf("peer was removed at %s, reason: %q", dep.RemovedAt.Format("2006-01-02T15:04:05Z"), dep.RemovedReason),
			"pubkey + pubkey_history[] preserved verbatim from the deprecated row; relay-side allowlist push uses the same key the operator previously distributed.",
		},
	}
	return plan, nil
}
