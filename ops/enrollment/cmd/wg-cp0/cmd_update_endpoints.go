package main

// cmd_update_endpoints.go — `wg-cp0 update-endpoints` subcommand
// owned by Session C of Block 22R.2.
//
// Updates the relay endpoint list in a peer's bundle without rotating
// the device's wg-cp0 Curve25519 keypair. The canonical use case is
// Block 22Q SJC tier bundle redistribution: a new relay listener
// joined the substrate via `wg-cp0 add-relay`; every existing peer
// now needs a fresh bundle.cbor carrying the new endpoint so the
// device's wg-cp0.conf includes the new relay as a known_endpoint.
//
// Per the ADR 0322 Property 4 invariant ("no gratuitous rotation for
// additive-metadata operations"), this subcommand passes
// `--reuse-keypair-from` to bundle-create unconditionally. The
// "REUSING KEYPAIR" line in the dry-run preview is prominent so the
// operator confirms no rotation is happening.
//
// Usage:
//
//   wg-cp0 update-endpoints <peer-name> \
//       [--add-relay <name>] [--remove-relay <name>] \
//       [--set-active-forwarder <name>] [--apply] [--out-dir <path>]
//
//   wg-cp0 update-endpoints --all-peers \
//       [--add-relay <name>] [--remove-relay <name>] \
//       [--set-active-forwarder <name>] [--apply] [--out-dir <path>]
//
// Endpoint-list semantics:
//   The default endpoint list for the re-minted bundle is the full
//   peerset.Relays[] slice. --add-relay and --remove-relay filter that
//   slice: --add-relay is a no-op if the named relay is already in the
//   list (the field is "ensure-present"); --remove-relay drops the
//   named relay from the per-peer bundle. Both flags may repeat.
//
// Active-forwarder:
//   --set-active-forwarder names which relay should be the preferred
//   forwarder. Currently this updates peerset.ActiveForwarder; the
//   embedded bundle representation of preferred-forwarder is a
//   downstream Block 22P concern that the bundle-create CLI does not
//   yet surface as a flag, so the bundle re-mint carries the relays
//   list and the operator-side wg-cp0-bootstrap.yml subsequently honors
//   the JSON-of-record's ActiveForwarder.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/usareur-af/goatcore/ops/enrollment/peerset"
)

type updateEndpointsFlags struct {
	path                string
	addRelays           []string
	removeRelays        []string
	setActiveForwarder  string
	apply               bool
	allPeers            bool
	outDir              string
}

func cmdUpdateEndpoints(args []string, stdout, stderr io.Writer) int {
	accept := map[string]bool{
		"path":                  true,
		"add-relay":             true,
		"remove-relay":          true,
		"set-active-forwarder":  true,
		"apply":                 true,
		"all-peers":             true,
		"out-dir":               true,
	}
	f, pos, err := parseFlagsUpdateEndpoints(args, accept)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 update-endpoints: %v\n", err)
		return 2
	}
	ps, err := peerset.Load(f.path)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 update-endpoints: load %s: %v\n", f.path, err)
		return 1
	}

	targets, err := resolveEndpointTargets(ps, pos, f.allPeers)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 update-endpoints: %v\n", err)
		return 1
	}
	if len(targets) == 0 {
		fmt.Fprintln(stdout, "No peers selected; nothing to do.")
		return 0
	}

	relays, err := filterRelays(ps, f.addRelays, f.removeRelays)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 update-endpoints: %v\n", err)
		return 1
	}
	if len(relays) == 0 {
		fmt.Fprintln(stderr, "wg-cp0 update-endpoints: post-filter relay set is empty; refusing to mint endpoint-less bundles")
		return 1
	}

	if f.setActiveForwarder != "" {
		if _, ok := relayByName(relays, f.setActiveForwarder); !ok {
			fmt.Fprintf(stderr, "wg-cp0 update-endpoints: --set-active-forwarder %q is not in the post-filter relay set\n", f.setActiveForwarder)
			return 1
		}
	}

	plan := buildUpdateEndpointsPlan(f, pos, targets, relays)
	printPlan(stdout, plan)

	if !f.apply {
		printApplyHint(stdout, true)
		return 0
	}

	if err := commitUpdateEndpoints(stdout, stderr, ps, f, targets, relays); err != nil {
		fmt.Fprintf(stderr, "wg-cp0 update-endpoints: %v\n", err)
		return 1
	}

	summary := fmt.Sprintf("update-endpoints (%d peer%s, %d relay%s)",
		len(targets), pluralS(len(targets)), len(relays), pluralS(len(relays)))
	printApplyCompleted(stdout, summary)
	return 0
}

// parseFlagsUpdateEndpoints handles the repeated --add-relay /
// --remove-relay flag semantics that the read-only main.go parser
// doesn't accommodate. Both flags accumulate; the rest match the
// other mutating subcommands' shape.
func parseFlagsUpdateEndpoints(args []string, accept map[string]bool) (updateEndpointsFlags, []string, error) {
	f := updateEndpointsFlags{path: defaultPath}
	positional := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") && !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
			continue
		}
		name := strings.TrimLeft(a, "-")
		var val string
		boolFlag := name == "apply" || name == "all-peers"
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			val = name[eq+1:]
			name = name[:eq]
		} else if !boolFlag {
			if i+1 >= len(args) {
				return f, nil, fmt.Errorf("flag --%s requires a value", name)
			}
			// #nosec G602 — guarded by the bounds check above.
			val = args[i+1]
			i++
		}
		if !accept[name] {
			return f, nil, fmt.Errorf("unknown flag --%s", name)
		}
		switch name {
		case "path":
			f.path = val
		case "add-relay":
			f.addRelays = append(f.addRelays, val)
		case "remove-relay":
			f.removeRelays = append(f.removeRelays, val)
		case "set-active-forwarder":
			f.setActiveForwarder = val
		case "apply":
			f.apply = true
		case "all-peers":
			f.allPeers = true
		case "out-dir":
			f.outDir = val
		}
	}
	return f, positional, nil
}

// resolveEndpointTargets is the endpoint-subcommand analog of the
// TTL subcommand's helper. Single-peer mode requires a positional
// name; --all-peers walks every active peer.
func resolveEndpointTargets(ps *peerset.PeerSet, positional []string, allPeers bool) ([]peerset.Peer, error) {
	if allPeers {
		if len(positional) > 0 {
			return nil, fmt.Errorf("--all-peers conflicts with positional peer name(s) %v", positional)
		}
		out := make([]peerset.Peer, 0, len(ps.Peers))
		out = append(out, ps.Peers...)
		return out, nil
	}
	if len(positional) != 1 {
		return nil, fmt.Errorf("either pass exactly one positional peer name or use --all-peers (got %v)", positional)
	}
	p, ok := ps.Lookup(positional[0])
	if !ok {
		return nil, fmt.Errorf("peer %q not found in active set", positional[0])
	}
	return []peerset.Peer{*p}, nil
}

// filterRelays applies --add-relay / --remove-relay to the peerset's
// relays slice. --add-relay is ensure-present (no-op if already in);
// --remove-relay drops the named entry from the per-peer bundle's
// endpoint list. Returns an error if a named --add-relay is not in
// the peerset (we don't synthesize relay rows; the operator should
// run `wg-cp0 add-relay` first).
func filterRelays(ps *peerset.PeerSet, addRelays, removeRelays []string) ([]peerset.Relay, error) {
	missing := []string{}
	for _, name := range addRelays {
		if _, ok := ps.Relay(name); !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("--add-relay names not in peerset: %s (run `wg-cp0 add-relay <name> --apply` first)",
			strings.Join(missing, ", "))
	}

	removeSet := make(map[string]struct{}, len(removeRelays))
	for _, r := range removeRelays {
		removeSet[r] = struct{}{}
	}

	out := make([]peerset.Relay, 0, len(ps.Relays))
	for _, r := range ps.Relays {
		if _, drop := removeSet[r.Name]; drop {
			continue
		}
		out = append(out, r)
	}
	// --add-relay ensure-present semantics: walk the requested adds
	// and append any that aren't already in the post-remove output
	// (the typical case is "operator wants to re-mint with the new
	// relay alongside everything that's already there"; if --add-relay
	// names a relay that's also in --remove-relay, we honor add and
	// drop the remove for that name).
	present := make(map[string]struct{}, len(out))
	for _, r := range out {
		present[r.Name] = struct{}{}
	}
	for _, name := range addRelays {
		if _, ok := present[name]; ok {
			continue
		}
		r, _ := ps.Relay(name)
		out = append(out, *r)
		present[name] = struct{}{}
	}
	return out, nil
}

// relayByName returns the relay matching the given name from the
// passed slice (not the peerset). Used to verify --set-active-
// forwarder names a relay that survives the filter pass.
func relayByName(relays []peerset.Relay, name string) (peerset.Relay, bool) {
	for _, r := range relays {
		if r.Name == name {
			return r, true
		}
	}
	return peerset.Relay{}, false
}

// buildUpdateEndpointsPlan renders the dry-run preview.
func buildUpdateEndpointsPlan(f updateEndpointsFlags, positional []string, targets []peerset.Peer, relays []peerset.Relay) Plan {
	cmd := "wg-cp0 update-endpoints"
	if f.allPeers {
		cmd += " --all-peers"
	} else if len(positional) > 0 {
		cmd += " " + positional[0]
	}
	for _, a := range f.addRelays {
		cmd += " --add-relay " + a
	}
	for _, r := range f.removeRelays {
		cmd += " --remove-relay " + r
	}
	if f.setActiveForwarder != "" {
		cmd += " --set-active-forwarder " + f.setActiveForwarder
	}

	relayNames := make([]string, 0, len(relays))
	for _, r := range relays {
		relayNames = append(relayNames, r.Name)
	}
	endpointDetail := "relays=[" + strings.Join(relayNames, ", ") + "]"

	stateLines := make([]PlanLine, 0)
	applyLines := make([]PlanLine, 0, len(targets))
	for _, p := range targets {
		applyLines = append(applyLines, PlanLine{
			Op:      PlanReuseKeypair,
			Subject: p.Name,
			Detail:  "re-mint bundle.cbor via bundle-create --reuse-keypair-from " + p.Name + "\n" + endpointDetail,
		})
	}

	if f.setActiveForwarder != "" {
		var prior string
		if f.allPeers {
			prior = "(set by --all-peers run)"
		} else if len(positional) > 0 {
			prior = positional[0] + " peer"
		}
		_ = prior
		stateLines = append(stateLines, PlanLine{
			Op:      PlanUpdate,
			Subject: "active_forwarder",
			Detail:  "host → " + f.setActiveForwarder,
		})
	}

	notes := []string{
		fmt.Sprintf("REUSING KEYPAIR for %d peer(s) — additive metadata only; no rotation.", len(targets)),
	}
	if len(f.addRelays) > 0 {
		notes = append(notes, fmt.Sprintf("Adding relay(s) to bundles: %s", strings.Join(f.addRelays, ", ")))
	}
	if len(f.removeRelays) > 0 {
		notes = append(notes, fmt.Sprintf("Removing relay(s) from bundles: %s", strings.Join(f.removeRelays, ", ")))
	}

	return Plan{
		Command:     cmd,
		StateTarget: f.path,
		State:       stateLines,
		Apply:       applyLines,
		Notes:       notes,
	}
}

// commitUpdateEndpoints drives the apply phase. Per-peer bundle re-
// mint via bundle-create; if --set-active-forwarder was passed, the
// peerset's ActiveForwarder field is updated after all bundles
// succeed (state commit gated on the live mutations landing).
func commitUpdateEndpoints(stdout, stderr io.Writer, ps *peerset.PeerSet, f updateEndpointsFlags, targets []peerset.Peer, relays []peerset.Relay) error {
	if len(targets) == 0 {
		return errors.New("no targets resolved (internal: classify-then-commit out of sync)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	now := time.Now().UTC()
	errs := []string{}
	for _, p := range targets {
		fmt.Fprintf(stdout, "→ re-minting bundle for %s (keypair reused, %d endpoint%s)\n",
			p.Name, len(relays), pluralS(len(relays)))
		opts := invokeOpts{
			Peer:    p,
			Relays:  relays,
			OutPath: computeDefaultOutPath(p, f.outDir, now),
		}
		if _, err := invokeBundleCreate(ctx, opts, f.path, stdout, stderr); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", p.Name, err))
			fmt.Fprintf(stderr, "  ! failed for %s: %v\n", p.Name, err)
			continue
		}
		fmt.Fprintf(stdout, "  ✓ %s\n", opts.OutPath)
	}
	if len(errs) > 0 {
		return fmt.Errorf("update-endpoints failed for %d peer(s):\n  %s", len(errs), strings.Join(errs, "\n  "))
	}

	// Apply the state-only ActiveForwarder change after the live
	// mutations succeed. The peerset reload ensures we layer on top
	// of bundle-create's write-throughs.
	if f.setActiveForwarder != "" {
		reloaded, err := peerset.Load(f.path)
		if err != nil {
			return fmt.Errorf("reload before active-forwarder update: %w", err)
		}
		r, _ := relayByName(relays, f.setActiveForwarder)
		tier := r.Tier
		if tier == "" {
			// peerset.Validate requires both tier + host on
			// ActiveForwarder; fall back to a sentinel rather than
			// silently emitting an invalid envelope.
			tier = "relay_servers"
		}
		reloaded.ActiveForwarder = &peerset.ActiveForwarder{
			Tier: tier,
			Host: f.setActiveForwarder,
		}
		if err := reloaded.Validate(); err != nil {
			return fmt.Errorf("active-forwarder update would violate schema invariants: %w", err)
		}
		if err := reloaded.Save(f.path); err != nil {
			return fmt.Errorf("save active-forwarder update: %w", err)
		}
		fmt.Fprintf(stdout, "  ✓ active_forwarder.host = %s (tier=%s)\n", f.setActiveForwarder, tier)
	}
	_ = ps // explicit-keep until A's reconcile pipeline absorbs the post-commit verification
	return nil
}

// pluralS returns "s" when n != 1, otherwise "". Tiny helper that
// keeps the operator-facing output grammatical.
func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
