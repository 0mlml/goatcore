package main

// Block 22R.2 / Session B — remove subcommand.
//
//   wg-cp0 remove <peer-name> --reason "<text>" [--apply]
//
// Soft-deletes a peer: moves the row from peers[] to
// deprecated_peers[] with the operator-supplied reason and timestamp.
// Live-state side: removes the peer's pubkey from each relay's
// allowlist. Hard-deletion (purge from deprecated_peers[]) is
// deliberately NOT supported here — that stays a manual operator
// action against the JSON-of-record, with the runbook pointer
// surfaced in the help text.
//
// The dry-run preview prominently displays the removal list so the
// operator sees the WOULD REMOVE block before any mutation. Partial-input
// rewrites that silently drop peers cannot happen, because the operator must
// say "remove" explicitly and sees the consequence in the preview before --apply.

import (
	"errors"
	"fmt"
	"io"

	"github.com/usareur-af/goatcore/ops/enrollment/peerset"
)

var removeAccept = map[string]bool{
	"reason": true,
}

func cmdRemove(args []string, stdout, stderr io.Writer) int {
	la, pos, err := parseLifecycleFlags(args, removeAccept)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 remove: %v\n", err)
		return 2
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, "wg-cp0 remove: requires exactly one positional arg <peer-name>")
		return 2
	}
	la.PeerName = pos[0]

	if err := validateRemoveArgs(la); err != nil {
		fmt.Fprintf(stderr, "wg-cp0 remove: %v\n", err)
		return 2
	}

	ps, err := loadOrEmpty(la.Path)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 remove: %v\n", err)
		return 1
	}

	plan, err := buildRemovePlan(ps, la)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 remove: %v\n", err)
		return 1
	}

	emitPreview(stdout, plan, la.Apply)

	if !la.Apply {
		return 0
	}

	if err := applyLifecyclePlan(ps, plan, stdout); err != nil {
		if errors.Is(err, errSessionAPending) {
			fmt.Fprintf(stderr, "\nwg-cp0 remove: %v\n", err)
			fmt.Fprintf(stderr, "\nLegacy path until Session A lands:\n")
			fmt.Fprintf(stderr, "  1. Manually edit %s — move %s row to deprecated_peers[]\n", la.Path, la.PeerName)
			fmt.Fprintf(stderr, "  2. Fire ops/ansible/wg-cp0-peer-surgical-update.yml with peer_old_pubkey=<from peerset> peer_new_pubkey=\"\" to remove from each relay\n")
			return 1
		}
		fmt.Fprintf(stderr, "wg-cp0 remove: %v\n", err)
		return 1
	}
	return 0
}

func validateRemoveArgs(la *lifecycleArgs) error {
	if la.PeerName == "" {
		return errors.New("peer-name is required (positional argument)")
	}
	if la.Reason == "" {
		return errors.New("--reason \"<text>\" is required (removal must carry a reason for the audit log)")
	}
	return nil
}

func buildRemovePlan(ps *peerset.PeerSet, la *lifecycleArgs) (*lifecyclePlan, error) {
	peer, ok := ps.Lookup(la.PeerName)
	if !ok {
		// Surface a useful misdirection-catch.
		if _, deprecated := ps.LookupDeprecated(la.PeerName); deprecated {
			return nil, fmt.Errorf("peer %q already in deprecated_peers[]; nothing to remove (use --include-deprecated on `wg-cp0 list` to see it)",
				la.PeerName)
		}
		return nil, fmt.Errorf("peer %q not in active set; nothing to remove", la.PeerName)
	}

	plan := &lifecyclePlan{
		Op:             "remove",
		PeerName:       la.PeerName,
		StateRemove:    peer,
		RelayRemove:    []string{peer.CurrentPubkey},
		AffectedRelays: affectedRelaysFromPeerset(ps),
		NotesForOp: []string{
			"this is a SOFT-DELETE: the row moves to deprecated_peers[] with reason + timestamp; the row is recoverable via 'wg-cp0 resurrect'.",
			"hard-deletion (purge from deprecated_peers[]) is intentionally out of scope; that stays a manual operator action.",
		},
	}
	return plan, nil
}
