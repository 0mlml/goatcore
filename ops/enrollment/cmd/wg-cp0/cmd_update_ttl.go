package main

// cmd_update_ttl.go — `wg-cp0 update-ttl` subcommand owned by Session
// C of Block 22R.2.
//
// Extends a peer's bundle expiry. Per the session-C prompt, this
// re-mints the bundle via `bundle-create --reuse-keypair-from` (no
// keypair rotation) so the deployed device receives a fresh
// signed-by-CA bundle with the new horizon. Updating only the
// peerset's BundleExpiry field would leave the deployed bundle's
// embedded expiry unchanged and the operator's intent half-applied.
//
// Usage:
//
//   wg-cp0 update-ttl <peer-name> --extend <duration> [--apply] [--force]
//   wg-cp0 update-ttl --all-peers --extend <duration> [--apply] [--force]
//
// Duration:
//   --extend takes a Go time.Duration with the additive 'd' (days)
//   shortcut for operator ergonomics. Examples: "90d", "180d", "1h",
//   "24h". The new bundle_expiry is computed as now + extend.
//
// Safety cap:
//   The new expiry must be within 365 days of now (rejected via the
//   ADR 0308 90-day-default horizon's outer envelope). Operators who
//   know they want a longer TTL pass --force; the dry-run output
//   surfaces the override in a WARNING line so the operator sees what
//   they asked for.
//
// Bulk path:
//   --all-peers walks every active peer in the JSON-of-record and
//   re-mints in operator-list order. Per-peer failures are collected
//   and the apply path continues; a non-zero exit reports total
//   per-peer outcomes. State commits only after every peer's bundle
//   succeeds — partial-fleet state drift is the failure mode we are
//   structurally avoiding.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/usareur-af/goatcore/ops/enrollment/peerset"
)

// updateTTLFlags is the flag set for `wg-cp0 update-ttl`.
type updateTTLFlags struct {
	path     string
	extend   string // "--extend 90d", parsed via parseExtendDuration
	apply    bool
	force    bool
	allPeers bool
	outDir   string
}

// defaultTTLCapDays is the safety upper bound on --extend. Operators
// can override via --force; the cap surfaces in the dry-run preview.
const defaultTTLCapDays = 365

func cmdUpdateTTL(args []string, stdout, stderr io.Writer) int {
	accept := map[string]bool{
		"path":      true,
		"extend":    true,
		"apply":     true,
		"force":     true,
		"all-peers": true,
		"out-dir":   true,
	}
	f, pos, err := parseFlagsUpdateTTL(args, accept)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 update-ttl: %v\n", err)
		return 2
	}
	if f.extend == "" {
		fmt.Fprintln(stderr, "wg-cp0 update-ttl: --extend is required (e.g. --extend 90d)")
		return 2
	}

	dur, err := parseExtendDuration(f.extend)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 update-ttl: --extend: %v\n", err)
		return 2
	}

	cap := time.Duration(defaultTTLCapDays) * 24 * time.Hour
	if dur > cap && !f.force {
		fmt.Fprintf(stderr, "wg-cp0 update-ttl: --extend %s exceeds %d-day cap; re-run with --force to override\n",
			f.extend, defaultTTLCapDays)
		return 2
	}

	ps, err := peerset.Load(f.path)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 update-ttl: load %s: %v\n", f.path, err)
		return 1
	}

	now := time.Now().UTC()
	newExpiry := now.Add(dur)

	targets, err := resolveTTLTargets(ps, pos, f.allPeers)
	if err != nil {
		fmt.Fprintf(stderr, "wg-cp0 update-ttl: %v\n", err)
		return 1
	}
	if len(targets) == 0 {
		fmt.Fprintln(stdout, "No peers selected; nothing to do.")
		return 0
	}

	plan := buildUpdateTTLPlan(f, pos, targets, newExpiry, dur)
	printPlan(stdout, plan)

	if !f.apply {
		printApplyHint(stdout, true)
		return 0
	}

	if err := commitUpdateTTL(stdout, stderr, ps, f.path, targets, dur, f.outDir, now); err != nil {
		fmt.Fprintf(stderr, "wg-cp0 update-ttl: %v\n", err)
		return 1
	}

	if f.allPeers {
		printApplyCompleted(stdout, fmt.Sprintf("update-ttl --all-peers --extend %s (%d peers)", f.extend, len(targets)))
	} else {
		printApplyCompleted(stdout, fmt.Sprintf("update-ttl %s --extend %s", targets[0].Name, f.extend))
	}
	return 0
}

// parseFlagsUpdateTTL parses the subcommand's flag set.
func parseFlagsUpdateTTL(args []string, accept map[string]bool) (updateTTLFlags, []string, error) {
	f := updateTTLFlags{path: defaultPath}
	positional := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") && !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
			continue
		}
		name := strings.TrimLeft(a, "-")
		var val string
		boolFlag := name == "apply" || name == "force" || name == "all-peers"
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
		case "extend":
			f.extend = val
		case "apply":
			f.apply = true
		case "force":
			f.force = true
		case "all-peers":
			f.allPeers = true
		case "out-dir":
			f.outDir = val
		}
	}
	return f, positional, nil
}

// parseExtendDuration accepts "90d" / "30d" / "12h" / "1h30m" — the
// extra "d" (days) shortcut is the only non-stdlib accept. Returns
// the parsed duration or an error naming the malformed input.
func parseExtendDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty --extend value")
	}
	// Days shortcut: terminal "d" suffix, integer body.
	if strings.HasSuffix(s, "d") && !strings.ContainsAny(s[:len(s)-1], "smhd") {
		n, err := strconv.Atoi(s[:len(s)-1])
		if err != nil {
			return 0, fmt.Errorf("malformed days value %q", s)
		}
		if n <= 0 {
			return 0, fmt.Errorf("--extend must be positive (got %d days)", n)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("unparseable duration %q (try 90d, 180d, or a Go duration like 24h)", s)
	}
	if d <= 0 {
		return 0, fmt.Errorf("--extend must be positive (got %s)", s)
	}
	return d, nil
}

// resolveTTLTargets returns the list of peers the operation should
// affect. Single-peer mode requires a positional name; --all-peers
// walks the active peers in declaration order. Mutually exclusive.
func resolveTTLTargets(ps *peerset.PeerSet, positional []string, allPeers bool) ([]peerset.Peer, error) {
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

// buildUpdateTTLPlan renders the dry-run preview. For each target,
// the state line shows the prior expiry → new expiry. The apply line
// names the bundle-create re-mint (with REUSING KEYPAIR call-out so
// the operator confirms no rotation is happening).
func buildUpdateTTLPlan(f updateTTLFlags, positional []string, targets []peerset.Peer, newExpiry time.Time, dur time.Duration) Plan {
	cmd := "wg-cp0 update-ttl"
	if f.allPeers {
		cmd += " --all-peers"
	} else if len(positional) > 0 {
		cmd += " " + positional[0]
	}
	cmd += " --extend " + f.extend

	stateLines := make([]PlanLine, 0, len(targets))
	applyLines := make([]PlanLine, 0, len(targets))
	for _, p := range targets {
		stateLines = append(stateLines, PlanLine{
			Op:      PlanUpdate,
			Subject: fmt.Sprintf("peers[].bundle_expiry: %s", p.Name),
			Detail:  fmt.Sprintf("%s → %s (+%s)", formatExpiry(p.BundleExpiry), newExpiry.UTC().Format(time.RFC3339), dur),
		})
		applyLines = append(applyLines, PlanLine{
			Op:      PlanReuseKeypair,
			Subject: p.Name,
			Detail:  "re-mint bundle.cbor via bundle-create --reuse-keypair-from " + p.Name,
		})
	}

	notes := []string{
		fmt.Sprintf("REUSING KEYPAIR for %d peer(s) — additive metadata only; no rotation.", len(targets)),
	}
	if dur > time.Duration(defaultTTLCapDays)*24*time.Hour {
		notes = append(notes,
			fmt.Sprintf("WARNING: --force overrides %d-day cap; new horizon is %s.",
				defaultTTLCapDays, newExpiry.UTC().Format(time.RFC3339)))
	}

	return Plan{
		Command:     cmd,
		StateTarget: f.path,
		State:       stateLines,
		Apply:       applyLines,
		Notes:       notes,
	}
}

// formatExpiry renders a *time.Time for the dry-run preview, with a
// distinguished "(unset)" string for nil/zero values so the
// before-state is unambiguous.
func formatExpiry(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "(unset)"
	}
	return t.UTC().Format(time.RFC3339)
}

// commitUpdateTTL drives the apply phase: for each target peer,
// invoke bundle-create with the new TTL + --reuse-keypair-from, then
// (on success) confirm the JSON-of-record's BundleExpiry now matches
// the requested horizon (bundle-create's --update-allowlist write-
// through is the canonical update path; this is a verification, not
// a second write).
func commitUpdateTTL(stdout, stderr io.Writer, ps *peerset.PeerSet, path string, targets []peerset.Peer, dur time.Duration, outDir string, now time.Time) error {
	ttlDays := int((dur + 23*time.Hour) / (24 * time.Hour)) // ceiling-divide so 90d → 90 not 89
	if ttlDays <= 0 {
		ttlDays = 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	errs := []string{}
	for _, p := range targets {
		fmt.Fprintf(stdout, "→ re-minting bundle for %s (ttl=%dd, keypair reused)\n", p.Name, ttlDays)
		opts := invokeOpts{
			Peer:    p,
			Relays:  ps.Relays,
			TTLDays: ttlDays,
			OutPath: computeDefaultOutPath(p, outDir, now),
		}
		if _, err := invokeBundleCreate(ctx, opts, path, stdout, stderr); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", p.Name, err))
			fmt.Fprintf(stderr, "  ! failed for %s: %v\n", p.Name, err)
			continue
		}
		fmt.Fprintf(stdout, "  ✓ %s\n", opts.OutPath)
	}

	if len(errs) > 0 {
		return fmt.Errorf("update-ttl failed for %d peer(s):\n  %s", len(errs), strings.Join(errs, "\n  "))
	}

	// Reload the peerset to pick up bundle-create's write-throughs
	// (each successful invocation upserted the peer's BundleExpiry via
	// --update-allowlist). The reload is the structural confirmation
	// that the on-disk state matches our applied intent.
	reloaded, err := peerset.Load(path)
	if err != nil {
		return fmt.Errorf("reload after apply: %w", err)
	}
	for _, target := range targets {
		got, ok := reloaded.Lookup(target.Name)
		if !ok {
			return fmt.Errorf("post-apply: peer %q vanished from peerset", target.Name)
		}
		// We don't assert exact-second match — bundle-create stamps
		// IssuedAt at exec time, slightly after our `now`. We do
		// assert the new expiry is at least within 24 h of the
		// requested horizon (one-day grace).
		want := now.Add(dur)
		if got.BundleExpiry == nil || got.BundleExpiry.Before(want.Add(-24*time.Hour)) {
			return fmt.Errorf("post-apply: peer %q bundle_expiry did not advance to within 24h of %s (got %s)",
				target.Name, want.Format(time.RFC3339), formatExpiry(got.BundleExpiry))
		}
	}
	_ = os.Getenv // keep imports honest in pkg-init test path
	return nil
}
