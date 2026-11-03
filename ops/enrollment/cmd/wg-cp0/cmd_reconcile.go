// cmd_reconcile.go — `wg-cp0 reconcile` + `wg-cp0 diff` subcommands.
//
// reconcile is the Session A flagship subcommand: walk the full
// declared PeerSet against live state, produce the union diff. Dry-
// run by default; --apply commits. --remove flag is the only way to
// drop peers (upsert is the default to preserve the F-114-class
// failure-mode-elimination promise of ADR 0322 Property 3).
//
// diff is a thin alias for `reconcile --dry-run` that exits 1 when
// the live state diverges from the declared state — operator-friendly
// for cron + observability paths.

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/usareur-af/goatcore/ops/enrollment/peerset"
)

// reconcileFlags is the parsed flag set for `wg-cp0 reconcile`.
type reconcileFlags struct {
	path        string
	site        string
	apply       bool
	noAdd       bool
	allowRemove bool
	asJSON      bool
	noColor     bool
	color       bool
}

// parseReconcileFlags walks the argument list. Local rather than
// reusing parseFlags() so the reconcile-specific flag set is explicit.
func parseReconcileFlags(args []string) (reconcileFlags, []string, error) {
	f := reconcileFlags{path: defaultPath}
	positional := []string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		// Boolean flags first.
		switch a {
		case "--apply":
			f.apply = true
			continue
		case "--no-add":
			f.noAdd = true
			continue
		case "--remove":
			f.allowRemove = true
			continue
		case "--json":
			f.asJSON = true
			continue
		case "--no-color":
			f.noColor = true
			continue
		case "--color":
			f.color = true
			continue
		case "-h", "--help":
			return f, nil, errHelpRequested
		}
		// Value-taking flags.
		if !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
			continue
		}
		name := strings.TrimLeft(a, "-")
		var val string
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			val = name[eq+1:]
			name = name[:eq]
		} else {
			if i+1 >= len(args) {
				return f, nil, fmt.Errorf("flag --%s requires a value", name)
			}
			val = args[i+1]
			i++
		}
		switch name {
		case "path":
			f.path = val
		case "site":
			f.site = val
		default:
			return f, nil, fmt.Errorf("unknown flag --%s", name)
		}
	}
	return f, positional, nil
}

var errHelpRequested = errors.New("help requested")

// cmdReconcile implements `wg-cp0 reconcile`.
func cmdReconcile(args []string, stdout, stderr io.Writer) int {
	f, pos, err := parseReconcileFlags(args)
	if errors.Is(err, errHelpRequested) {
		if _, err := fmt.Fprint(stdout, reconcileUsage); err != nil {
			return 1
		}
		return 0
	}
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 reconcile: %v\n%s", err, reconcileUsage)
		return 2
	}
	if len(pos) > 0 {
		fmt.Fprintf(stderr, "wg-cp0 reconcile: unexpected positional %v\n", pos)
		return 2
	}

	intent := ReconcileIntent{
		Site:        f.site,
		AllowAdd:    !f.noAdd,
		AllowRemove: f.allowRemove,
	}

	live, relayMutator := buildIOLayer(f.apply)
	var audit AuditEmitter = NoopAuditEmitter{} // dry-run never writes audit
	if f.apply {
		audit = pickAuditEmitter()
	}

	applier, err := NewApplier(ApplierOptions{
		PeerSetPath: f.path,
		Live:        live,
		Relay:       relayMutator,
		Audit:       audit,
		Stdout:      stdout,
	})
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 reconcile: %v\n", err)
		return 1
	}

	plan, err := applier.Plan(context.Background(), intent)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 reconcile: plan: %v\n", err)
		return 1
	}

	// Always print the plan first — operator sees what would happen
	// before any wire move lands.
	if err := FormatPlan(plan, FormatOptions{
		JSON:    f.asJSON,
		Color:   f.color,
		NoColor: f.noColor,
		Out:     stdout,
	}); err != nil {
		fmt.Fprintf(stderr, "wg-cp0 reconcile: format: %v\n", err)
		return 1
	}

	if !f.apply {
		return 0
	}

	result, err := applier.Apply(context.Background(), plan)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 reconcile: apply: %v\n", err)
		if result != nil {
			fmt.Fprintln(stderr, summarizeApply(result))
		}
		return 1
	}
	fmt.Fprintln(stdout, summarizeApply(result))
	return 0
}

// cmdDiff implements `wg-cp0 diff`. Equivalent to reconcile --dry-run
// with a clean exit-code contract: 0 = clean (live matches declared),
// 1 = drift detected. Useful in cron / observability.
func cmdDiff(args []string, stdout, stderr io.Writer) int {
	// Disallow --apply on diff to avoid operator surprise.
	for _, a := range args {
		if a == "--apply" {
			fmt.Fprintln(stderr, "wg-cp0 diff: --apply is not valid on diff; use `wg-cp0 reconcile --apply`")
			return 2
		}
	}
	f, pos, err := parseReconcileFlags(args)
	if errors.Is(err, errHelpRequested) {
		if _, err := fmt.Fprint(stdout, diffUsage); err != nil {
			return 1
		}
		return 0
	}
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 diff: %v\n", err)
		return 2
	}
	if len(pos) > 0 {
		fmt.Fprintf(stderr, "wg-cp0 diff: unexpected positional %v\n", pos)
		return 2
	}

	intent := ReconcileIntent{
		Site:        f.site,
		AllowAdd:    !f.noAdd,
		AllowRemove: f.allowRemove,
	}
	live, _ := buildIOLayer(false)
	applier, err := NewApplier(ApplierOptions{
		PeerSetPath: f.path,
		Live:        live,
		Audit:       NoopAuditEmitter{},
		Stdout:      stdout,
	})
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 diff: %v\n", err)
		return 1
	}
	plan, err := applier.Plan(context.Background(), intent)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 diff: plan: %v\n", err)
		return 1
	}
	if err := FormatPlan(plan, FormatOptions{
		JSON:    f.asJSON,
		Color:   f.color,
		NoColor: f.noColor,
		Out:     stdout,
	}); err != nil {
		fmt.Fprintf(stderr, "wg-cp0 diff: format: %v\n", err)
		return 1
	}
	if plan.IsEmpty() {
		return 0
	}
	return 1
}

// buildIOLayer constructs the LiveStateReader + (when needsMutator)
// the production RelayMutator. Returns (nil, nil) when WG_CP0_OFFLINE
// is set so unit tests can dry-run against an empty live state
// without SSH dependencies.
func buildIOLayer(needsMutator bool) (LiveStateReader, RelayMutator) {
	if os.Getenv("WG_CP0_OFFLINE") != "" {
		return &MockLiveStateReader{State: LiveState{PerRelay: map[string]*RelayLiveState{}}}, nil
	}
	live := NewSSHLiveStateReader(SSHReaderOptions{})
	if !needsMutator {
		return live, nil
	}
	mutator := NewSSHRelayMutator(SSHReaderOptions{})
	return live, mutator
}

// pickAuditEmitter returns the FileAuditEmitter pointed at the env-
// overridable default path. WG_CP0_AUDIT_PATH wins over the default.
func pickAuditEmitter() AuditEmitter {
	path := os.Getenv("WG_CP0_AUDIT_PATH")
	return NewFileAuditEmitter(path)
}

// summarizeApply renders one operator-readable line for the apply
// result, plus a per-step rollup when failures occurred.
func summarizeApply(r *ApplyResult) string {
	if r == nil {
		return "wg-cp0 apply: nil result"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "wg-cp0 apply: %s — %d step(s)", r.Outcome, len(r.PerStep))
	if r.Err != nil {
		fmt.Fprintf(&b, " — %v", r.Err)
	}
	if r.Outcome != "ok" {
		for _, s := range r.PerStep {
			if s.Outcome != "ok" {
				fmt.Fprintf(&b, "\n  ! %s [%s] %s", s.Kind, s.Outcome, s.Err)
			}
		}
	}
	return b.String()
}

// planReconcile is the Session A planner. Walks the declared PeerSet
// against the live state on each relay and produces the union diff +
// the Mutation list to land it. The Mutation list is ordered:
//
//  1. StateWriteMutation (always first; the PeerSet write happens
//     before any wire move so a partial-fleet apply still leaves
//     the declared intent committed and the operator can re-run).
//  2. Per-relay peer adds (RelayAddPeerMutation).
//  3. Per-relay peer rotates (RelayRotatePubkeyMutation).
//  4. Per-relay peer removes (RelayRemovePeerMutation) — only when
//     the ReconcileIntent has AllowRemove=true.
//  5. Per-relay wg-quick save (RelayPersistMutation) — one per
//     relay that received any peer mutation, ordered last so the
//     in-memory changes are durable across reboots.
//
// Unreachable relays produce a RelayLiveDiff with Unreachable=true
// in the LivePlan and no Mutation in the list (the operator sees the
// skip in the dry-run and decides whether to proceed with partial
// coverage).
func planReconcile(env *PlanEnv, intent Intent) (*ApplyPlan, error) {
	ri, ok := intent.(ReconcileIntent)
	if !ok {
		return nil, fmt.Errorf("planReconcile: wrong intent type %T", intent)
	}
	plan := &ApplyPlan{
		StatePlan: &PeerSetDiff{},
		LivePlan:  &LiveStateDiff{},
	}

	// Filter declared peers by site if requested.
	declaredPeers := env.PeerSet.Peers
	declaredRelays := env.PeerSet.Relays
	if ri.Site != "" {
		filtered := make([]peerset.Peer, 0, len(declaredPeers))
		for _, p := range declaredPeers {
			if p.Site == ri.Site {
				filtered = append(filtered, p)
			}
		}
		declaredPeers = filtered
		// Relays-by-site is similarly filtered, but reconcile still
		// needs to touch every relay (a peer in site vega has an
		// allowlist entry on relay-rla). For the v1 reconcile we
		// fan out across every declared relay regardless of site; a
		// future refinement could prune.
	}

	// Build a quick lookup from pubkey -> declared peer name.
	declaredByPubkey := make(map[string]peerset.Peer, len(declaredPeers))
	for _, p := range declaredPeers {
		declaredByPubkey[p.CurrentPubkey] = p
	}

	// Track which peers + relays the dry-run touched.
	var mutations []Mutation
	// Always first: the state write. The reconcile path produces a
	// PeerSet that is identical to the loaded one (reconcile only
	// changes live state, not the JSON-of-record), so the
	// StateWriteMutation is a no-op write that stamps Metadata.
	// We omit it from the mutation list for pure-reconcile to avoid
	// a no-op disk write; planners that actually mutate the PeerSet
	// (onboard, rotate, remove) insert it themselves.

	relayMutators := map[string][]Mutation{} // by relay name

	// Walk every declared relay; produce the per-relay live diff +
	// the corresponding Mutations.
	for i := range declaredRelays {
		relay := declaredRelays[i]
		rlive, hasLive := env.Live.PerRelay[relay.Name]
		rd := RelayLiveDiff{Relay: relay.Name}
		if !hasLive {
			rd.Unreachable = true
			rd.UnreachableErr = "relay not in live-state snapshot (reader skipped or fleet not yet read)"
			plan.LivePlan.PerRelay = append(plan.LivePlan.PerRelay, rd)
			continue
		}
		if !rlive.Reachable {
			rd.Unreachable = true
			rd.UnreachableErr = rlive.UnreachableErr
			plan.LivePlan.PerRelay = append(plan.LivePlan.PerRelay, rd)
			continue
		}

		// For each declared peer: is the relay's pubkey row aligned?
		declaredOnRelay := map[string]struct{}{} // pubkey set the relay SHOULD hold for this site
		for _, p := range declaredPeers {
			declaredOnRelay[p.CurrentPubkey] = struct{}{}
			allowed := joinAllowedIPs(p)
			lp, present := rlive.Peers[p.CurrentPubkey]
			if !present {
				// Missing — check whether an old pubkey for this name
				// is present (rotation case).
				oldPubkey := findOldPubkey(p, rlive.Peers)
				if oldPubkey != "" {
					rd.WouldRotate = append(rd.WouldRotate, LivePeerOp{
						PeerName:   p.Name,
						OldPubkey:  oldPubkey,
						NewPubkey:  p.CurrentPubkey,
						AllowedIPs: allowed,
					})
					if env.Mutator != nil {
						relayMutators[relay.Name] = append(relayMutators[relay.Name],
							&RelayRotatePubkeyMutation{
								Relay:      relay,
								PeerName:   p.Name,
								OldPubkey:  oldPubkey,
								NewPubkey:  p.CurrentPubkey,
								AllowedIPs: allowed,
								Mutator:    env.Mutator,
							})
					}
					continue
				}
				if ri.AllowAdd {
					rd.WouldAdd = append(rd.WouldAdd, LivePeerOp{
						PeerName:   p.Name,
						NewPubkey:  p.CurrentPubkey,
						AllowedIPs: allowed,
					})
					if env.Mutator != nil {
						relayMutators[relay.Name] = append(relayMutators[relay.Name],
							&RelayAddPeerMutation{
								Relay:      relay,
								PeerName:   p.Name,
								Pubkey:     p.CurrentPubkey,
								AllowedIPs: allowed,
								Mutator:    env.Mutator,
							})
					}
				}
				continue
			}
			// Present — check that allowed-ips matches.
			if !allowedIPsEqual(lp.AllowedIPs, allowed) {
				// Wire-side allowed-ips drift; surfaced as an add op
				// (wg set with the same pubkey overwrites allowed-ips
				// atomically).
				rd.WouldAdd = append(rd.WouldAdd, LivePeerOp{
					PeerName:   p.Name,
					NewPubkey:  p.CurrentPubkey,
					AllowedIPs: allowed,
				})
				if env.Mutator != nil {
					relayMutators[relay.Name] = append(relayMutators[relay.Name],
						&RelayAddPeerMutation{
							Relay:      relay,
							PeerName:   p.Name,
							Pubkey:     p.CurrentPubkey,
							AllowedIPs: allowed,
							Mutator:    env.Mutator,
						})
				}
				continue
			}
			rd.Unchanged = append(rd.Unchanged, LivePeerOp{
				PeerName:   p.Name,
				NewPubkey:  p.CurrentPubkey,
				AllowedIPs: lp.AllowedIPs,
			})
		}

		// Now scan the live peers for entries that are NOT in the
		// declared set + NOT in any peer's pubkey history (so we
		// don't flag a not-yet-cleaned-up-old-key as a rogue peer).
		for pubkey := range rlive.Peers {
			if _, ok := declaredOnRelay[pubkey]; ok {
				continue
			}
			if findPeerByPubkeyHistory(env.PeerSet, pubkey) != "" {
				// It's an old pubkey for a known peer; declared peer
				// has a different CurrentPubkey, so the rotation
				// handling above already queued the rotate op.
				continue
			}
			// Truly unknown peer on the relay.
			if !ri.AllowRemove {
				continue
			}
			rd.WouldRemove = append(rd.WouldRemove, LivePeerOp{
				PeerName:  "(unknown)",
				OldPubkey: pubkey,
			})
			if env.Mutator != nil {
				relayMutators[relay.Name] = append(relayMutators[relay.Name],
					&RelayRemovePeerMutation{
						Relay:    relay,
						PeerName: "(unknown)",
						Pubkey:   pubkey,
						Mutator:  env.Mutator,
					})
			}
		}

		plan.LivePlan.PerRelay = append(plan.LivePlan.PerRelay, rd)
	}

	// Fold the per-relay mutators into the linear plan + cap each
	// relay's group with a Persist call. Relay order is declared-list
	// order so the apply hits the same relay sequence the operator
	// sees in the dry-run.
	for _, relay := range declaredRelays {
		ms, ok := relayMutators[relay.Name]
		if !ok || len(ms) == 0 {
			continue
		}
		mutations = append(mutations, ms...)
		if env.Mutator != nil {
			mutations = append(mutations, &RelayPersistMutation{Relay: relay, Mutator: env.Mutator})
		}
	}

	// Reconcile does not mutate the PeerSet; StatePlan is therefore
	// empty for a pure reconcile. Unchanged is populated so the
	// formatter can show "KEEP N" if desired.
	plan.StatePlan.Unchanged = peersToDeltas(declaredPeers)
	plan.Mutations = mutations
	return plan, nil
}

// reconcileUsage is the operator-facing help text for reconcile.
const reconcileUsage = `usage:
  wg-cp0 reconcile [--site <site>] [--apply] [--no-add] [--remove]
                   [--path <p>] [--json] [--color | --no-color]

Sync live wire state to match the declared PeerSet.

  --site <site>      restrict to peers + relays at one site
  --apply            commit the plan (dry-run by default)
  --no-add           do not add declared peers missing from relays
  --remove           remove relay-side peers absent from the declaration
  --path <p>         override the JSON-of-record path
  --json             machine-readable output
  --color/--no-color force color on/off (default: auto by tty)

  default --path: ` + defaultPath + `
`

// diffUsage is the operator-facing help text for diff.
const diffUsage = `usage:
  wg-cp0 diff [--site <site>] [--no-add] [--remove] [--path <p>] [--json]

Read-only: PeerSet vs live state. Exits 0 if clean, 1 if drift.
Useful in cron / observability.
`

// joinAllowedIPs returns the canonical AllowedIPs string for a peer:
// "<peer>/32,<fleet-subnet>" per ADR 0321. Peers may carry extra
// CIDRs in Peer.AllowedIPsExtra; those are appended after the fleet
// subnet.
func joinAllowedIPs(p peerset.Peer) string {
	parts := []string{p.WGCP0Address + "/32"}
	hasFleetSubnet := false
	for _, e := range p.AllowedIPsExtra {
		if e == "198.18.0.0/24" {
			hasFleetSubnet = true
		}
		parts = append(parts, e)
	}
	if !hasFleetSubnet {
		// Default: include the fleet subnet so the canonical pair per
		// ADR 0321 holds. Inserted in position 1 so the resulting
		// string is "<peer>/32,198.18.0.0/24,<extras>".
		parts = append(parts[:1], append([]string{"198.18.0.0/24"}, parts[1:]...)...)
	}
	return strings.Join(parts, ",")
}

// findOldPubkey returns the most-recent old pubkey for the declared
// peer that is currently present on the relay, or "" when none. Used
// by reconcile to detect the rotation case (old key present, new key
// missing).
func findOldPubkey(p peerset.Peer, livePeers map[string]LivePeer) string {
	// Walk history newest-to-oldest so the most recent rotation is
	// the one we re-pair against.
	for i := len(p.PubkeyHistory) - 1; i >= 0; i-- {
		if _, present := livePeers[p.PubkeyHistory[i].Pubkey]; present {
			return p.PubkeyHistory[i].Pubkey
		}
	}
	return ""
}

// findPeerByPubkeyHistory returns the peer name whose history
// contains the given pubkey, or "" when none. Used by reconcile to
// avoid flagging a not-yet-cleaned-up-old-key as a rogue peer.
func findPeerByPubkeyHistory(ps *peerset.PeerSet, pubkey string) string {
	for _, p := range ps.Peers {
		for _, h := range p.PubkeyHistory {
			if h.Pubkey == pubkey {
				return p.Name
			}
		}
	}
	return ""
}

// allowedIPsEqual normalizes two AllowedIPs strings for comparison.
// Both wg-show and the planner produce sorted-list strings, but
// historic bundles may have entries in a different order; treat as a
// set comparison to avoid false drift signals.
func allowedIPsEqual(a, b string) bool {
	if a == b {
		return true
	}
	as := splitAllowedIPsSet(a)
	bs := splitAllowedIPsSet(b)
	if len(as) != len(bs) {
		return false
	}
	for k := range as {
		if _, ok := bs[k]; !ok {
			return false
		}
	}
	return true
}

func splitAllowedIPsSet(s string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" && part != "(none)" {
			out[part] = struct{}{}
		}
	}
	return out
}

// peersToDeltas converts a []peerset.Peer to []PeerDelta with Before
// == After == self (used for the Unchanged section of StatePlan).
func peersToDeltas(in []peerset.Peer) []PeerDelta {
	out := make([]PeerDelta, 0, len(in))
	for i := range in {
		p := in[i]
		out = append(out, PeerDelta{Name: p.Name, Before: &p, After: &p})
	}
	return out
}
