package main

// Block 22R.2 / Session B — shared helpers for the four per-peer
// lifecycle subcommands (onboard, rotate, remove, resurrect).
//
// Each subcommand follows the same shape:
//
//   1. Parse flags into a typed *lifecycleArgs.
//   2. Validate args (presence, format, mutual-exclusion).
//   3. Load the peerset from --path (default ops/enrollment/
//      wg-cp0-bundle-allowlist.json).
//   4. Compute a *lifecyclePlan describing what would change in the
//      state (peerset) plane and what would change in the apply plane
//      (relay-side allowlists, bundle delivery, etc.).
//   5. Emit the dry-run preview to stdout in the structured shape
//      from docs/design/wg-cp0-declarative-substrate.md §3.1.
//   6. If --apply, hand the plan to the orchestrator's Applier
//      (Session A's substrate, queued on track wg-cp0-orchestrator-
//      reconcile-core). Until A's interface lands, --apply emits a
//      structural-pending error so dry-run remains operator-usable.
//
// The four subcommands are deliberately thin: every mutating decision
// goes through peerset.Mutator functions whose invariants (collision
// rejection, fail-loud on missing fields, atomic write) are already
// covered by 22R.1 unit tests; the subcommand surface composes them.

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/usareur-af/goatcore/ops/enrollment/peerset"
)

// lifecycleArgs is the typed parsed-flag surface shared by the four
// lifecycle subcommands. Subcommand-specific fields are zero-valued
// when not relevant (Reason on onboard; WGCP0Address on rotate; etc.).
type lifecycleArgs struct {
	// PeerName is the positional argument: the peer being mutated.
	PeerName string

	// Path is the peerset JSON-of-record path (default = defaultPath
	// in main.go, ops/enrollment/wg-cp0-bundle-allowlist.json).
	Path string

	// Apply controls mutation. Dry-run-by-default; --apply commits.
	Apply bool

	// Onboard-only fields.
	Site         string // --site
	Role         string // --role
	WGCP0Address string // --wg-cp0-address (CIDR, e.g. 198.18.0.10/24)
	BundleOut    string // --bundle-out

	// Rotate-only fields.
	Reason string // --reason
	// BundleOut also used on rotate (operator re-delivers).

	// Remove-only field.
	// Reason is reused for remove ("why is this peer leaving the fleet").

	// Resurrect takes no extra flags beyond Path + Apply.
}

// lifecyclePlan captures the WOULD shape of a subcommand run before
// mutation. The dry-run formatter walks the fields; the apply path
// passes the plan to Session A's Applier.
type lifecyclePlan struct {
	// Op is one of "onboard", "rotate", "remove", "resurrect" — the
	// operator-visible command name. Used by the formatter to title
	// the preview.
	Op string

	// PeerName is the peer the plan is about.
	PeerName string

	// State-plane changes (peerset.json mutations).
	StateAdd      *peerset.Peer       // new peer to be appended to peers[]
	StateRemove   *peerset.Peer       // peer to be moved to deprecated_peers[]
	StateRotate   *rotatePlanDetail   // rotation specifics
	StateResurrect *peerset.Peer      // peer to be moved out of deprecated_peers[] back into peers[]

	// Apply-plane changes (relay-side allowlist + bundle delivery).
	RelayAdd       []string // pubkeys to push onto relay allowlists
	RelayRemove    []string // pubkeys to remove from relay allowlists
	BundleMint     bool     // does --apply mint a fresh bundle?
	BundleOutPath  string   // where the bundle would land
	NotesForOp     []string // free-text operator hints surfaced in dry-run

	// AffectedRelays is the relay-name list the live mutation step
	// would target. Session A's LiveState gives the authoritative
	// roster; this slice is populated from peerset.Relays for the
	// dry-run preview when LiveState isn't yet wired.
	AffectedRelays []string
}

// rotatePlanDetail captures the rotation-specific shape: which peer,
// what pubkey is being retired (the prior CurrentPubkey), and the
// operator-supplied reason for the audit log.
type rotatePlanDetail struct {
	PeerName       string
	PriorPubkey    string // base64
	Reason         string
	// NewPubkey is intentionally absent at plan time: bundle-create
	// mints during --apply, so the dry-run preview shows
	// "<will-be-generated>" rather than promising a value the run
	// hasn't produced yet.
}

// errSessionAPending is the structural sentinel emitted by --apply
// paths until Session A's Applier interface lands on track
// wg-cp0-orchestrator-reconcile-core. Dry-run is fully usable today;
// --apply produces this error with a clear remediation pointer.
var errSessionAPending = errors.New(
	"--apply requires Session A's Applier interface (cmd/wg-cp0/applier.go), " +
		"queued on track 'wg-cp0-orchestrator-reconcile-core' — until that lands, " +
		"this subcommand is dry-run-only; the peerset mutation path through " +
		"peerset.Upsert/RotatePubkey/Remove/Resurrect is wired and tested, while " +
		"the relay-side fan-out is what waits on A's substrate; re-run without " +
		"--apply to preview the plan or check docs/project/active-work/tracks/ for A's status")

// commonLifecycleFlags returns the flag-accept map every lifecycle
// subcommand shares: --path, --apply, plus subcommand-specific extras
// the caller merges in.
func commonLifecycleFlags() map[string]bool {
	return map[string]bool{
		"path":  true,
		"apply": true,
	}
}

// loadOrEmpty wraps peerset.Load with a uniform error envelope. A
// missing file is silently treated as an empty v2 envelope by the
// library, matching the operator UX where "no peerset yet" should not
// block a first onboard.
func loadOrEmpty(path string) (*peerset.PeerSet, error) {
	ps, err := peerset.Load(path)
	if err != nil {
		return nil, fmt.Errorf("load peerset %s: %w", path, err)
	}
	return ps, nil
}

// emitPreview writes the structured dry-run preview to w. The shape
// follows docs/design/wg-cp0-declarative-substrate.md §3.1 — title +
// state plan + apply plan + affected-relays + operator notes.
func emitPreview(w io.Writer, plan *lifecyclePlan, apply bool) {
	verb := "PLAN"
	if apply {
		verb = "APPLY (pending Session A; --apply will fail with substrate-pending error)"
	}
	fmt.Fprintf(w, "%s — wg-cp0 %s %s\n\n", verb, plan.Op, plan.PeerName)

	// State-plane changes — what the JSON-of-record gains/loses.
	if hasStateChange(plan) {
		fmt.Fprintln(w, "State plan (would mutate peerset JSON):")
		if plan.StateAdd != nil {
			p := plan.StateAdd
			fmt.Fprintf(w, "  + ADD peers[]: %s (%s) role=%s site=%s\n", p.Name, p.WGCP0Address, dashOr(p.Role), dashOr(p.Site))
			if p.CurrentPubkey != "" {
				fmt.Fprintf(w, "                 current_pubkey=%s\n", truncate(p.CurrentPubkey, 22))
			}
			if p.BundleExpiry != nil {
				fmt.Fprintf(w, "                 bundle_expiry=%s\n", p.BundleExpiry.Format(time.RFC3339))
			}
		}
		if plan.StateRotate != nil {
			r := plan.StateRotate
			fmt.Fprintf(w, "  ~ ROTATE peer %s\n", r.PeerName)
			fmt.Fprintf(w, "                 prior_pubkey=%s → new=<will-be-generated>\n", truncate(r.PriorPubkey, 22))
			fmt.Fprintf(w, "                 reason=%q (appended to pubkey_history[])\n", r.Reason)
		}
		if plan.StateRemove != nil {
			p := plan.StateRemove
			fmt.Fprintf(w, "  - REMOVE peers[]: %s (%s)\n", p.Name, p.WGCP0Address)
			fmt.Fprintf(w, "                 moves to deprecated_peers[]; can be restored via 'wg-cp0 resurrect %s'\n", p.Name)
		}
		if plan.StateResurrect != nil {
			p := plan.StateResurrect
			fmt.Fprintf(w, "  + RESURRECT peers[]: %s (%s) role=%s site=%s\n", p.Name, p.WGCP0Address, dashOr(p.Role), dashOr(p.Site))
			fmt.Fprintf(w, "                 moves from deprecated_peers[] back to peers[]\n")
		}
		fmt.Fprintln(w)
	}

	// Apply-plane changes — what the live wire state would gain/lose.
	if hasApplyChange(plan) {
		fmt.Fprintln(w, "Apply plan (would mutate live wire state):")
		for _, pk := range plan.RelayAdd {
			fmt.Fprintf(w, "  + On each relay-side wg-cp0 listener:\n")
			fmt.Fprintf(w, "      wg set wg-cp0 peer %s allowed-ips %s\n", truncate(pk, 22), peerAllowedIPs(plan))
		}
		for _, pk := range plan.RelayRemove {
			fmt.Fprintf(w, "  - On each relay-side wg-cp0 listener:\n")
			fmt.Fprintf(w, "      wg set wg-cp0 peer %s remove\n", truncate(pk, 22))
		}
		if plan.BundleMint {
			fmt.Fprintf(w, "  + Mint bundle at %s\n", plan.BundleOutPath)
			fmt.Fprintf(w, "  + Operator-fired: deliver bundle to device, run wg-cp0-bundle-apply\n")
		}
		fmt.Fprintln(w)
	}

	// Affected relays.
	if len(plan.AffectedRelays) > 0 {
		fmt.Fprintf(w, "Affected relays: %s\n", strings.Join(plan.AffectedRelays, ", "))
		fmt.Fprintln(w)
	}

	// Operator notes — surface hints for unusual or important shapes.
	if len(plan.NotesForOp) > 0 {
		fmt.Fprintln(w, "Notes:")
		for _, n := range plan.NotesForOp {
			fmt.Fprintf(w, "  * %s\n", n)
		}
		fmt.Fprintln(w)
	}

	if !apply {
		fmt.Fprintln(w, "Re-run with --apply to execute. (--apply currently pending Session A's Applier.)")
	}
}

func hasStateChange(p *lifecyclePlan) bool {
	return p.StateAdd != nil || p.StateRotate != nil || p.StateRemove != nil || p.StateResurrect != nil
}

func hasApplyChange(p *lifecyclePlan) bool {
	return len(p.RelayAdd) > 0 || len(p.RelayRemove) > 0 || p.BundleMint
}

// peerAllowedIPs returns the canonical AllowedIPs line a relay-side
// wg-set command would carry: <wg_cp0_address>/32 — fleet-subnet
// catchall lives at the device-side per ADR 0321.
func peerAllowedIPs(p *lifecyclePlan) string {
	switch {
	case p.StateAdd != nil && p.StateAdd.WGCP0Address != "":
		return p.StateAdd.WGCP0Address + "/32"
	case p.StateResurrect != nil && p.StateResurrect.WGCP0Address != "":
		return p.StateResurrect.WGCP0Address + "/32"
	default:
		return "<from peerset>"
	}
}

// affectedRelaysFromPeerset returns the relay-name slice the apply
// step would target. Until Session A's LiveState wires the
// authoritative roster, this falls back to peerset.Relays[] —
// which is empty in today's deployments (Phase A of 22R.1 didn't
// populate it), so the slice is typically empty.
func affectedRelaysFromPeerset(ps *peerset.PeerSet) []string {
	out := make([]string, 0, len(ps.Relays))
	for _, r := range ps.Relays {
		out = append(out, r.Name)
	}
	return out
}

// dashOr returns "-" for empty strings; lets the preview formatter
// stay readable without if-empty branches at each call site.
func dashOr(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// applyLifecyclePlan is the seam where Session A's Applier will
// take over. Today it returns errSessionAPending; once A's interface
// lands, this function calls applier.Apply(plan) with the necessary
// translation between *lifecyclePlan and applier.Plan.
//
// Keeping the seam in a single function makes the A-lands follow-up
// commit a one-line wiring change rather than a sweep across four
// subcommand files.
var applyLifecyclePlan = func(_ *peerset.PeerSet, _ *lifecyclePlan, _ io.Writer) error {
	return errSessionAPending
}

// derivePubkeyPlaceholder is a deliberate non-implementation. Onboard's
// dry-run preview can't promise a specific pubkey value (the bundle is
// minted at --apply time). The placeholder string communicates this
// to the operator scanning the preview.
func derivePubkeyPlaceholder() string {
	return "<will-be-generated>"
}

// parseLifecycleFlags is the shared flag walker. It consumes the
// common --path / --apply flags plus any subcommand-specific flags
// the caller registered in extra. Unknown flags surface as errors.
// Positional args are returned in the second slice (callers validate
// arity).
func parseLifecycleFlags(args []string, extra map[string]bool) (*lifecycleArgs, []string, error) {
	accept := commonLifecycleFlags()
	for k, v := range extra {
		accept[k] = v
	}

	la := &lifecycleArgs{Path: defaultPath}
	positional := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") && !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
			continue
		}
		name := strings.TrimLeft(a, "-")
		var val string
		valueExpected := name != "apply"
		switch {
		case strings.ContainsRune(name, '='):
			eq := strings.IndexByte(name, '=')
			val = name[eq+1:]
			name = name[:eq]
		case valueExpected:
			// Two-arg form: `--path /tmp/p`. Use takeArg's combined
			// access + bounds check so gosec G602's flow analysis is
			// satisfied (a single-site indexed access on a slice it
			// can prove is in-bounds).
			v, ok := takeArg(args, i+1)
			if !ok {
				return nil, nil, fmt.Errorf("flag --%s requires a value", name)
			}
			val = v
			i++
		}
		if !accept[name] {
			return nil, nil, fmt.Errorf("unknown flag --%s", name)
		}
		switch name {
		case "path":
			la.Path = val
		case "apply":
			la.Apply = true
		case "site":
			la.Site = val
		case "role":
			la.Role = val
		case "wg-cp0-address":
			la.WGCP0Address = val
		case "bundle-out":
			la.BundleOut = val
		case "reason":
			la.Reason = val
		}
	}
	return la, positional, nil
}

// takeArg returns args[i] when i is in-bounds. The two-value return
// shape replaces the slice-index + len-check idiom so gosec G602's
// data-flow analysis sees a single safe access site.
func takeArg(args []string, i int) (string, bool) {
	if i < 0 || i >= len(args) {
		return "", false
	}
	return args[i], true
}

// _ avoids "imported and not used" for base64 if a future change
// drops its only consumer; keeping the import line stable across
// refactors. Same shape as cmd/bundle-create/main.go uses for json.
var _ = base64.StdEncoding
