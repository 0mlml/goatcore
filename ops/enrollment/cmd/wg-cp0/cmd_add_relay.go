package main

// cmd_add_relay.go — `wg-cp0 add-relay` subcommand owned by Session
// C of Block 22R.2.
//
// Adds a new relay listener to the peerset's relays[] list. Pure
// state mutation; does NOT update any peer bundles. The operator
// follow-up to propagate the new relay to existing peers is
// `wg-cp0 update-endpoints --all-peers --add-relay <relay-name>
// --apply`, which Session C also owns. This split-and-follow-up
// shape matches the design doc §4.1 + §4.2 examples:
//
//   wg-cp0 add-relay relay-rld \
//       --endpoint relay-rld.netbird-prod.goat.example:51821 \
//       --pubkey <base64-32-byte-pubkey> \
//       --address 198.18.0.4 \
//       --site rld \
//       --tier relay_servers \
//       --apply
//
// Idempotency: re-running the same command after success is a no-op
// (existing relay with the same name + same field values; the
// subcommand prints "= KEEP" and exits 0). Re-running with different
// fields refuses to clobber by default; `--replace` lets the operator
// overwrite intentionally.

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/usareur-af/goatcore/ops/enrollment/peerset"
)

// cmdAddRelay implements the `wg-cp0 add-relay` flow.
//
// Dry-run-by-default; `--apply` commits.
//
// Acceptance:
//   - state path is pure peerset mutation (Relays slice append /
//     replace), no bundle-create call, no live wire-state mutation;
//   - re-running with identical fields is a no-op (PlanKeep, exit 0);
//   - re-running with different fields without --replace fails loud
//     (exit 1, error names the conflicting field);
//   - dry-run output names the relay and its endpoint so an operator
//     can scan the preview and confirm.
func cmdAddRelay(args []string, stdout, stderr io.Writer) int {
	accept := map[string]bool{
		"path":     true,
		"endpoint": true,
		"pubkey":   true,
		"address":  true,
		"site":     true,
		"tier":     true,
		"apply":    true,
		"replace":  true,
	}
	f, pos, err := parseFlagsAddRelay(args, accept)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 add-relay: %v\n", err)
		return 2
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, "wg-cp0 add-relay: requires exactly one positional arg <relay-name>")
		return 2
	}
	name := pos[0]
	if err := validateAddRelayArgs(name, f); err != nil {
		fmt.Fprintf(stderr, "wg-cp0 add-relay: %v\n", err)
		return 2
	}

	ps, err := peerset.Load(f.path)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 add-relay: load %s: %v\n", f.path, err)
		return 1
	}

	candidate := peerset.Relay{
		Name:          name,
		Site:          f.site,
		Tier:          f.tier,
		Endpoint:      f.endpoint,
		CurrentPubkey: f.pubkey,
		MeshAddress:   f.address,
	}

	op, prior, err := classifyRelayAdd(ps, candidate, f.replace)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 add-relay: %v\n", err)
		return 1
	}

	plan := buildAddRelayPlan(f.path, candidate, op, prior)
	printPlan(stdout, plan)

	if op == PlanKeep {
		fmt.Fprintln(stdout, "No changes required; relay already present with matching fields.")
		return 0
	}
	if !f.apply {
		printApplyHint(stdout, true)
		return 0
	}

	if err := commitAddRelay(ps, candidate, op); err != nil {
		fmt.Fprintf(stderr, "wg-cp0 add-relay: commit: %v\n", err)
		return 1
	}
	if err := ps.Save(f.path); err != nil {
		fmt.Fprintf(stderr, "wg-cp0 add-relay: save %s: %v\n", f.path, err)
		return 1
	}
	printApplyCompleted(stdout, fmt.Sprintf("add-relay %s (%s)", name, opLabel(op)))
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Follow-up: propagate the new relay to existing peers' bundles with")
	fmt.Fprintf(stdout, "  wg-cp0 update-endpoints --all-peers --add-relay %s --apply\n", name)
	return 0
}

// addRelayFlags collects the flag values for the add-relay subcommand.
type addRelayFlags struct {
	path     string
	endpoint string
	pubkey   string
	address  string
	site     string
	tier     string
	apply    bool
	replace  bool
}

// parseFlagsAddRelay is the add-relay-specific flag parser. parseFlags
// in main.go was scoped for the read-only subcommands and didn't
// accept the mutating-subcommand-specific flags; rather than widen
// that helper (which would couple A-owned read-only paths to C-owned
// mutating paths), we maintain a local parser per mutating subcommand.
func parseFlagsAddRelay(args []string, accept map[string]bool) (addRelayFlags, []string, error) {
	f := addRelayFlags{path: defaultPath}
	positional := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") && !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
			continue
		}
		name := strings.TrimLeft(a, "-")
		var val string
		boolFlag := name == "apply" || name == "replace"
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
		case "endpoint":
			f.endpoint = val
		case "pubkey":
			f.pubkey = val
		case "address":
			f.address = val
		case "site":
			f.site = val
		case "tier":
			f.tier = val
		case "apply":
			f.apply = true
		case "replace":
			f.replace = true
		}
	}
	return f, positional, nil
}

// validateAddRelayArgs enforces the required-flag contract before any
// peerset I/O. Returns a single combined error if multiple flags are
// missing so the operator fixes everything at once.
func validateAddRelayArgs(name string, f addRelayFlags) error {
	if name == "" {
		return errors.New("relay name (positional) is required")
	}
	missing := []string{}
	if f.endpoint == "" {
		missing = append(missing, "--endpoint")
	}
	if f.pubkey == "" {
		missing = append(missing, "--pubkey")
	}
	if f.address == "" {
		missing = append(missing, "--address")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required flags: %s", strings.Join(missing, ", "))
	}
	// Pubkey shape check: canonical base64-32-byte Curve25519. Reuses
	// the relays-file normalizer so both call sites agree on the
	// accepted form.
	if _, err := normalizeRelayPubkey(f.pubkey); err != nil {
		return fmt.Errorf("--pubkey: %w", err)
	}
	return nil
}

// classifyRelayAdd decides whether the candidate add results in
// PlanAdd, PlanUpdate, or PlanKeep. The prior return value is the
// existing relay row when one matches by name (used by the plan
// printer to render the per-field diff).
//
// Returns an error when the candidate's name matches an existing
// relay AND fields differ AND --replace was not passed.
func classifyRelayAdd(ps *peerset.PeerSet, cand peerset.Relay, replace bool) (PlanOp, *peerset.Relay, error) {
	prior, ok := ps.Relay(cand.Name)
	if !ok {
		return PlanAdd, nil, nil
	}
	if relaysEqual(*prior, cand) {
		return PlanKeep, prior, nil
	}
	if !replace {
		return PlanUpdate, prior, fmt.Errorf(
			"relay %q already present with different fields; re-run with --replace to overwrite (or remove via a separate ceremony)",
			cand.Name,
		)
	}
	return PlanUpdate, prior, nil
}

// relaysEqual compares two Relay records by every load-bearing field.
// peerset's exported types are flat structs; a deep-equal over the
// six fields we care about is the simplest correct comparison.
func relaysEqual(a, b peerset.Relay) bool {
	return a.Name == b.Name &&
		a.Site == b.Site &&
		a.Tier == b.Tier &&
		a.Endpoint == b.Endpoint &&
		a.CurrentPubkey == b.CurrentPubkey &&
		a.MeshAddress == b.MeshAddress
}

// buildAddRelayPlan renders the dry-run preview for the operation.
func buildAddRelayPlan(path string, cand peerset.Relay, op PlanOp, prior *peerset.Relay) Plan {
	plan := Plan{
		Command:     fmt.Sprintf("wg-cp0 add-relay %s", cand.Name),
		StateTarget: path,
	}
	switch op {
	case PlanAdd:
		plan.State = []PlanLine{{
			Op:      PlanAdd,
			Subject: fmt.Sprintf("relays[]: %s", cand.Name),
			Detail:  formatRelayDetail(cand),
		}}
	case PlanUpdate:
		detail := formatRelayUpdateDetail(prior, cand)
		plan.State = []PlanLine{{
			Op:      PlanUpdate,
			Subject: fmt.Sprintf("relays[]: %s", cand.Name),
			Detail:  detail,
		}}
	case PlanKeep:
		plan.State = []PlanLine{{
			Op:      PlanKeep,
			Subject: fmt.Sprintf("relays[]: %s", cand.Name),
			Detail:  "all fields match",
		}}
	}
	return plan
}

// formatRelayDetail returns a single-line summary suitable for the
// add-path PlanLine.Detail. Includes the load-bearing fields an
// operator wants to confirm at-a-glance.
func formatRelayDetail(r peerset.Relay) string {
	parts := []string{
		"endpoint=" + r.Endpoint,
		"address=" + r.MeshAddress,
	}
	if r.Site != "" {
		parts = append(parts, "site="+r.Site)
	}
	if r.Tier != "" {
		parts = append(parts, "tier="+r.Tier)
	}
	parts = append(parts, "pubkey="+truncate(r.CurrentPubkey, 22))
	return strings.Join(parts, " ")
}

// formatRelayUpdateDetail shows the field-by-field diff between the
// prior and candidate relays so an operator can see which fields
// would change.
func formatRelayUpdateDetail(prior *peerset.Relay, cand peerset.Relay) string {
	if prior == nil {
		return formatRelayDetail(cand)
	}
	pairs := []string{}
	if prior.Endpoint != cand.Endpoint {
		pairs = append(pairs, fmt.Sprintf("endpoint: %s → %s", prior.Endpoint, cand.Endpoint))
	}
	if prior.MeshAddress != cand.MeshAddress {
		pairs = append(pairs, fmt.Sprintf("address: %s → %s", prior.MeshAddress, cand.MeshAddress))
	}
	if prior.Site != cand.Site {
		pairs = append(pairs, fmt.Sprintf("site: %s → %s", emptyDash(prior.Site), emptyDash(cand.Site)))
	}
	if prior.Tier != cand.Tier {
		pairs = append(pairs, fmt.Sprintf("tier: %s → %s", emptyDash(prior.Tier), emptyDash(cand.Tier)))
	}
	if prior.CurrentPubkey != cand.CurrentPubkey {
		pairs = append(pairs, fmt.Sprintf("pubkey: %s → %s",
			truncate(prior.CurrentPubkey, 22), truncate(cand.CurrentPubkey, 22)))
	}
	if len(pairs) == 0 {
		return "no diff (rerun classification bug)"
	}
	return strings.Join(pairs, "\n")
}

// emptyDash renders "-" for empty strings to keep the diff output
// scannable (otherwise an empty → non-empty transition looks like
// " → vega" which is harder to parse).
func emptyDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// commitAddRelay mutates the peerset to apply the planned operation.
// Operates directly on ps.Relays because peerset (22R.1) does not
// expose an UpsertRelay() method; the orchestrator's apply step is
// the canonical caller. Calls ps.Validate() before returning so an
// invariant violation surfaces here rather than at Save().
func commitAddRelay(ps *peerset.PeerSet, cand peerset.Relay, op PlanOp) error {
	switch op {
	case PlanAdd:
		ps.Relays = append(ps.Relays, cand)
	case PlanUpdate:
		for i := range ps.Relays {
			if ps.Relays[i].Name == cand.Name {
				ps.Relays[i] = cand
				break
			}
		}
	case PlanKeep:
		return nil
	default:
		return fmt.Errorf("commitAddRelay: unexpected op %v", op)
	}
	return ps.Validate()
}

// opLabel returns the human-readable verb for an op (used in the
// post-apply confirmation line).
func opLabel(op PlanOp) string {
	switch op {
	case PlanAdd:
		return "added"
	case PlanUpdate:
		return "updated"
	case PlanRemove:
		return "removed"
	case PlanKeep:
		return "unchanged"
	default:
		return "applied"
	}
}
