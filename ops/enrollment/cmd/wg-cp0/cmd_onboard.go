package main

// Block 22R.2 / Session B — onboard subcommand.
//
//   wg-cp0 onboard <peer-name>
//     --site <site> --role <role> --wg-cp0-address <ip>/24
//     [--bundle-out <path>] [--apply] [--path <peerset.json>]
//
// Adds a new peer to the peerset (state plane), schedules a relay-side
// allowlist push (apply plane), and (with --apply --bundle-out)
// triggers a bundle mint. The dry-run shape per
// docs/design/wg-cp0-declarative-substrate.md §4.2 example "Add a new
// mgmt-host Orin."
//
// Bundle minting is delegated to the bundle-create CLI invocation via
// an out-of-band operator step at this commit (Session B does NOT
// re-implement the offline-CA bundle ceremony inside the orchestrator).
// The dry-run preview tells the operator the next-step commands.

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/usareur-af/goatcore/ops/enrollment/peerset"
)

// onboardAccept lists the subcommand-specific flags the shared
// parseLifecycleFlags walker should accept on top of --path / --apply.
var onboardAccept = map[string]bool{
	"site":           true,
	"role":           true,
	"wg-cp0-address": true,
	"bundle-out":     true,
}

func cmdOnboard(args []string, stdout, stderr io.Writer) int {
	la, pos, err := parseLifecycleFlags(args, onboardAccept)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 onboard: %v\n", err)
		return 2
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, "wg-cp0 onboard: requires exactly one positional arg <peer-name>")
		return 2
	}
	la.PeerName = pos[0]

	if err := validateOnboardArgs(la); err != nil {
		fmt.Fprintf(stderr, "wg-cp0 onboard: %v\n", err)
		return 2
	}

	ps, err := loadOrEmpty(la.Path)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 onboard: %v\n", err)
		return 1
	}

	plan, err := buildOnboardPlan(ps, la)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 onboard: %v\n", err)
		return 1
	}

	emitPreview(stdout, plan, la.Apply)

	if !la.Apply {
		return 0
	}

	// State-plane mutation: insert the new peer into peerset.json.
	// peerset.Upsert enforces collision rejection (name / pubkey /
	// address) — the operator sees the error if --wg-cp0-address
	// collides with an existing peer.
	//
	// At onboard-dry-run we don't promise a pubkey, but --apply needs
	// one for the peerset row. Today's path: bundle-create owns
	// keypair minting; the orchestrator delegates by writing a
	// placeholder pubkey and instructing the operator to fire
	// bundle-create against this peerset, which will replace the
	// placeholder via its --update-allowlist path.
	//
	// Until A's Applier lands, the actual write of the new peer row
	// is deliberately deferred to bundle-create's --update-allowlist
	// invocation (so the orchestrator stays idempotent with the
	// existing bundle-create ceremony — re-running this onboard then
	// running bundle-create against the same peerset doesn't produce
	// two rows). Session B's apply-path skeleton currently surfaces
	// this as a structural-pending error so the operator hears about
	// the substrate gap instead of silently no-op'ing.
	if err := applyLifecyclePlan(ps, plan, stdout); err != nil {
		// errSessionAPending is a structural-pending signal until
		// Session A's Applier lands. Emit a short follow-on hint
		// describing the bundle-create command the operator can
		// fire today for the same effect (legacy path).
		if errors.Is(err, errSessionAPending) {
			fmt.Fprintf(stderr, "\nwg-cp0 onboard: %v\n", err)
			fmt.Fprintf(stderr, "\nLegacy path until Session A lands:\n")
			fmt.Fprintf(stderr, "  bundle-create \\\n")
			fmt.Fprintf(stderr, "    --site %s --device-id %s --wg-cp0-address %s \\\n",
				la.Site, la.PeerName, la.WGCP0Address)
			fmt.Fprintf(stderr, "    --update-allowlist %s \\\n", la.Path)
			fmt.Fprintf(stderr, "    --out %s\n", la.BundleOut)
			return 1
		}
		fmt.Fprintf(stderr, "wg-cp0 onboard: %v\n", err)
		return 1
	}
	return 0
}

// validateOnboardArgs checks the four required onboard inputs +
// the CIDR shape of --wg-cp0-address.
func validateOnboardArgs(la *lifecycleArgs) error {
	if la.PeerName == "" {
		return errors.New("peer-name is required (positional argument)")
	}
	if la.Site == "" {
		return errors.New("--site is required")
	}
	if la.Role == "" {
		return errors.New("--role is required")
	}
	if la.WGCP0Address == "" {
		return errors.New("--wg-cp0-address is required")
	}
	if !strings.Contains(la.WGCP0Address, "/") {
		return fmt.Errorf("--wg-cp0-address must be a CIDR (e.g. 198.18.0.10/24); got %q", la.WGCP0Address)
	}
	if _, _, err := net.ParseCIDR(la.WGCP0Address); err != nil {
		return fmt.Errorf("--wg-cp0-address parse: %w", err)
	}
	if la.Apply && la.BundleOut == "" {
		return errors.New("--apply requires --bundle-out <path> so the minted bundle has a destination")
	}
	return nil
}

// buildOnboardPlan computes the WOULD shape of an onboard against
// the loaded peerset. Caller has already validated args.
func buildOnboardPlan(ps *peerset.PeerSet, la *lifecycleArgs) (*lifecyclePlan, error) {
	// Fail-loud against re-onboarding an active peer. The right tool
	// for that is `wg-cp0 rotate` (replace key) or no-op (peer
	// already there).
	if _, exists := ps.Lookup(la.PeerName); exists {
		return nil, fmt.Errorf("peer %q already in active set; use 'wg-cp0 rotate %s' to roll the key or remove first",
			la.PeerName, la.PeerName)
	}
	// Resurrection of a deprecated peer goes through the resurrect
	// subcommand explicitly so the operator sees the deprecated-row
	// metadata before re-activating.
	if _, deprecated := ps.LookupDeprecated(la.PeerName); deprecated {
		return nil, fmt.Errorf("peer %q is in deprecated_peers[]; use 'wg-cp0 resurrect %s' to bring it back",
			la.PeerName, la.PeerName)
	}

	addr := addrIPOnly(la.WGCP0Address)

	// Address-collision check is enforced by peerset.Upsert too, but
	// preview the failure shape at plan time so dry-run is honest.
	for _, p := range ps.Peers {
		if p.WGCP0Address == addr {
			return nil, fmt.Errorf("--wg-cp0-address %s already held by active peer %q (rotate or pick another address)",
				addr, p.Name)
		}
	}

	newPeer := &peerset.Peer{
		Name:          la.PeerName,
		WGCP0Address:  addr,
		CurrentPubkey: derivePubkeyPlaceholder(),
		Role:          la.Role,
		Site:          la.Site,
	}

	plan := &lifecyclePlan{
		Op:             "onboard",
		PeerName:       la.PeerName,
		StateAdd:       newPeer,
		RelayAdd:       []string{derivePubkeyPlaceholder()},
		BundleMint:     la.BundleOut != "",
		BundleOutPath:  la.BundleOut,
		AffectedRelays: affectedRelaysFromPeerset(ps),
		NotesForOp: []string{
			"current_pubkey is generated by bundle-create at --apply time; the dry-run shows a placeholder.",
		},
	}

	if la.Apply && la.BundleOut == "" {
		plan.NotesForOp = append(plan.NotesForOp,
			"--apply without --bundle-out skips bundle minting; operator must fire bundle-create separately.")
	}
	return plan, nil
}

// addrIPOnly strips the prefix length from a CIDR string. Same shape
// as bundle-create's addressIPOnly; duplicated here to keep cmd/wg-cp0
// self-contained (no cross-binary internal-helper import).
func addrIPOnly(cidr string) string {
	if i := strings.Index(cidr, "/"); i >= 0 {
		return cidr[:i]
	}
	return cidr
}
