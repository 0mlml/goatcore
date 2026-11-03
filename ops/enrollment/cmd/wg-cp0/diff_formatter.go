// diff_formatter.go — dry-run preview renderer.
//
// The structural catch for partial-input rewrites (an operator with a
// partial input view fires a tool whose default is to rewrite-not-
// reconcile, and the silent-drop is invisible until the next reboot)
// is the *visibility* of removals in the preview. The formatter's job
// is to make that visibility legible in five seconds, in both a tty
// session and in scripted output.
//
// Two output modes:
//
//   - Text (default): banner + one section per change kind, sorted
//     for stability. Color-coded when stdout is a tty (red for
//     removes, green for adds, yellow for rotates, dim for keeps).
//
//   - JSON (--json): the same content rendered as a stable JSON
//     object so cron / observability / CI can consume it without
//     string parsing.
//
// Both branches share one walker over PeerSetDiff + LiveStateDiff so
// renderer drift between formats is structurally impossible.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"

	"golang.org/x/term"
)

// FormatOptions configures the diff formatter.
type FormatOptions struct {
	// JSON forces JSON output regardless of tty detection.
	JSON bool

	// Color forces color on; otherwise the formatter checks if Out
	// is a tty via golang.org/x/term.IsTerminal and color-on when so.
	Color bool

	// NoColor forces color off even on a tty. Takes priority over Color.
	NoColor bool

	// Out is the destination writer. nil defaults to os.Stdout.
	Out io.Writer
}

// FormatPlan renders an ApplyPlan to opts.Out (or os.Stdout when nil).
// Returns nil on success; the only failure mode is the writer
// rejecting bytes, which is surfaced to the caller.
func FormatPlan(plan *ApplyPlan, opts FormatOptions) error {
	w := opts.Out
	if w == nil {
		w = os.Stdout
	}
	if opts.JSON {
		return formatPlanJSON(plan, w)
	}
	useColor := shouldColor(w, opts)
	return formatPlanText(plan, w, useColor)
}

// shouldColor decides whether to emit ANSI color escapes based on
// (a) opts.NoColor, (b) opts.Color, (c) whether w is a tty.
func shouldColor(w io.Writer, opts FormatOptions) bool {
	if opts.NoColor {
		return false
	}
	if opts.Color {
		return true
	}
	if f, ok := w.(interface{ Fd() uintptr }); ok {
		return term.IsTerminal(int(f.Fd()))
	}
	return false
}

// ANSI color helpers — applied conditionally per shouldColor.
const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiBlue   = "\x1b[34m"
	ansiCyan   = "\x1b[36m"
)

func paint(useColor bool, color, s string) string {
	if !useColor {
		return s
	}
	return color + s + ansiReset
}

func formatPlanText(plan *ApplyPlan, w io.Writer, useColor bool) error {
	if plan == nil {
		_, err := fmt.Fprintln(w, "(empty plan)")
		return err
	}
	intentKind := "(unknown)"
	if plan.Intent != nil {
		intentKind = plan.Intent.Kind()
	}
	header := fmt.Sprintf("PLAN — wg-cp0 %s", intentKind)
	if _, err := fmt.Fprintln(w, paint(useColor, ansiBold, header)); err != nil {
		return err
	}
	if plan.IsEmpty() {
		_, err := fmt.Fprintln(w, paint(useColor, ansiDim, "  (no changes — live state matches declared intent)"))
		return err
	}

	// State plan section
	if d := plan.StatePlan; d != nil && !d.IsEmpty() {
		if _, err := fmt.Fprintln(w, paint(useColor, ansiBold, "\nState plan (peerset.json delta):")); err != nil {
			return err
		}
		emitDeltaList(w, "WOULD ADD", d.Added, ansiGreen, useColor)
		emitDeltaList(w, "WOULD ROTATE", d.Rotated, ansiYellow, useColor)
		emitDeltaList(w, "WOULD UPDATE ENDPOINTS", d.EndpointsUpdated, ansiCyan, useColor)
		emitDeltaList(w, "WOULD UPDATE TTL", d.TTLUpdated, ansiCyan, useColor)
		emitDeltaList(w, "WOULD REMOVE", d.Removed, ansiRed, useColor)
		emitRelayList(w, "WOULD ADD RELAY", d.RelaysAdded, ansiGreen, useColor)
		// Unchanged is informational; only show count to keep the
		// preview scannable. Operators who want detail run `wg-cp0
		// list`.
		if len(d.Unchanged) > 0 {
			line := fmt.Sprintf("  WOULD KEEP: %d peers unchanged", len(d.Unchanged))
			if _, err := fmt.Fprintln(w, paint(useColor, ansiDim, line)); err != nil {
				return err
			}
		}
	}

	// Live plan section
	if d := plan.LivePlan; d != nil && !d.IsEmpty() {
		if _, err := fmt.Fprintln(w, paint(useColor, ansiBold, "\nApply plan (relay-side wg-set fan-out):")); err != nil {
			return err
		}
		// Sort relays by name for deterministic output.
		relays := append([]RelayLiveDiff(nil), d.PerRelay...)
		sort.Slice(relays, func(i, j int) bool { return relays[i].Relay < relays[j].Relay })
		for _, rd := range relays {
			if rd.Unreachable {
				line := fmt.Sprintf("  ! relay %s — UNREACHABLE (%s)", rd.Relay, rd.UnreachableErr)
				if _, err := fmt.Fprintln(w, paint(useColor, ansiRed, line)); err != nil {
					return err
				}
				continue
			}
			if rd.IsEmpty() {
				continue
			}
			if _, err := fmt.Fprintf(w, "  %s:\n", paint(useColor, ansiBold, "relay "+rd.Relay)); err != nil {
				return err
			}
			for _, op := range rd.WouldAdd {
				if _, err := fmt.Fprintln(w, paint(useColor, ansiGreen,
					fmt.Sprintf("    + wg set peer %s allowed-ips %s   (%s)", truncate(op.NewPubkey, 22), op.AllowedIPs, op.PeerName))); err != nil {
					return err
				}
			}
			for _, op := range rd.WouldRotate {
				if _, err := fmt.Fprintln(w, paint(useColor, ansiYellow,
					fmt.Sprintf("    ~ wg set peer %s remove; wg set peer %s allowed-ips %s   (%s)",
						truncate(op.OldPubkey, 22), truncate(op.NewPubkey, 22), op.AllowedIPs, op.PeerName))); err != nil {
					return err
				}
			}
			for _, op := range rd.WouldRemove {
				if _, err := fmt.Fprintln(w, paint(useColor, ansiRed,
					fmt.Sprintf("    - wg set peer %s remove   (%s)", truncate(op.OldPubkey, 22), op.PeerName))); err != nil {
					return err
				}
			}
		}
	}

	// Summary line.
	summary := SummarizePlan(plan)
	summaryStr := fmt.Sprintf("\nSummary: %d add, %d rotate, %d remove, %d endpoint-update, %d ttl-update, %d relay-add",
		summary.Adds, summary.Rotates, summary.Removes, summary.EndpointUpdates, summary.TTLUpdates, summary.RelaysAdded)
	if _, err := fmt.Fprintln(w, paint(useColor, ansiBold, summaryStr)); err != nil {
		return err
	}
	if plan.DryRun {
		_, err := fmt.Fprintln(w, paint(useColor, ansiDim, "Re-run with --apply to execute."))
		return err
	}
	return nil
}

// emitDeltaList writes one labeled section of PeerDeltas. Sorts by
// name for stability. Silent when the slice is empty.
func emitDeltaList(w io.Writer, label string, list []PeerDelta, color string, useColor bool) {
	if len(list) == 0 {
		return
	}
	xs := append([]PeerDelta(nil), list...)
	sort.Slice(xs, func(i, j int) bool { return xs[i].Name < xs[j].Name })
	for _, d := range xs {
		line := formatDeltaLine(label, d)
		_, _ = fmt.Fprintln(w, paint(useColor, color, line))
	}
}

func emitRelayList(w io.Writer, label string, list []RelayDelta, color string, useColor bool) {
	if len(list) == 0 {
		return
	}
	xs := append([]RelayDelta(nil), list...)
	sort.Slice(xs, func(i, j int) bool { return xs[i].Name < xs[j].Name })
	for _, d := range xs {
		after := d.After
		if after == nil {
			continue
		}
		line := fmt.Sprintf("  %s  relay %s  site=%s  tier=%s  endpoint=%s", label, d.Name, after.Site, after.Tier, after.Endpoint)
		_, _ = fmt.Fprintln(w, paint(useColor, color, line))
	}
}

// formatDeltaLine renders one PeerDelta as a single line. The
// before/after pair is the source of truth; the label tells the
// reader what kind of change this is.
func formatDeltaLine(label string, d PeerDelta) string {
	switch {
	case d.After != nil && d.Before == nil:
		// add
		return fmt.Sprintf("  %s  %s (%s)  role=%s  site=%s  pubkey=%s",
			label, d.Name, d.After.WGCP0Address, dash(d.After.Role), dash(d.After.Site), truncate(d.After.CurrentPubkey, 22))
	case d.After == nil && d.Before != nil:
		// remove
		return fmt.Sprintf("  %s  %s (%s)  role=%s  reason=%q",
			label, d.Name, d.Before.WGCP0Address, dash(d.Before.Role), d.Reason)
	case d.After != nil && d.Before != nil:
		// rotate / endpoint-update / ttl-update
		oldKey := truncate(d.Before.CurrentPubkey, 22)
		newKey := truncate(d.After.CurrentPubkey, 22)
		return fmt.Sprintf("  %s  %s (%s)  pubkey: %s → %s  reason=%q",
			label, d.Name, d.After.WGCP0Address, oldKey, newKey, d.Reason)
	default:
		return fmt.Sprintf("  %s  %s  (empty delta)", label, d.Name)
	}
}

// peerDeltaWire is the JSON-on-the-wire shape for one PeerDelta row.
// Lives at package scope so the formatPlanJSON wire types + the
// toWire helper share one definition.
type peerDeltaWire struct {
	Name   string `json:"name"`
	Before any    `json:"before,omitempty"`
	After  any    `json:"after,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// stateWire is the JSON shape for the StatePlan section.
type stateWire struct {
	Added            []peerDeltaWire `json:"added"`
	Removed          []peerDeltaWire `json:"removed"`
	Rotated          []peerDeltaWire `json:"rotated"`
	EndpointsUpdated []peerDeltaWire `json:"endpoints_updated"`
	TTLUpdated       []peerDeltaWire `json:"ttl_updated"`
	RelaysAdded      []RelayDelta    `json:"relays_added"`
	Unchanged        int             `json:"unchanged_count"`
}

// relayLiveWire is the JSON shape for one per-relay live-plan row.
type relayLiveWire struct {
	Relay          string       `json:"relay"`
	WouldAdd       []LivePeerOp `json:"would_add"`
	WouldRotate    []LivePeerOp `json:"would_rotate"`
	WouldRemove    []LivePeerOp `json:"would_remove"`
	Unreachable    bool         `json:"unreachable"`
	UnreachableErr string       `json:"unreachable_err,omitempty"`
}

// planWire is the top-level JSON shape FormatPlan emits with --json.
type planWire struct {
	IntentKind    string          `json:"intent_kind"`
	DryRun        bool            `json:"dry_run"`
	PlannedAt     string          `json:"planned_at,omitempty"`
	AppliedAt     string          `json:"applied_at,omitempty"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	StatePlan     *stateWire      `json:"state_plan,omitempty"`
	LivePlan      []relayLiveWire `json:"live_plan,omitempty"`
	Summary       PlanSummary     `json:"summary"`
}

// formatPlanJSON renders an ApplyPlan as one structured JSON object.
// The shape is deliberately stable + tagged so a future audit-log
// consumer can pin against the schema.
func formatPlanJSON(plan *ApplyPlan, w io.Writer) error {
	out := planWire{DryRun: plan.DryRun, CorrelationID: plan.CorrelationID, Summary: SummarizePlan(plan)}
	if plan.Intent != nil {
		out.IntentKind = plan.Intent.Kind()
	}
	if !plan.PlannedAt.IsZero() {
		out.PlannedAt = plan.PlannedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	if !plan.AppliedAt.IsZero() {
		out.AppliedAt = plan.AppliedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	if d := plan.StatePlan; d != nil {
		out.StatePlan = &stateWire{
			Added:            toWire(d.Added),
			Removed:          toWire(d.Removed),
			Rotated:          toWire(d.Rotated),
			EndpointsUpdated: toWire(d.EndpointsUpdated),
			TTLUpdated:       toWire(d.TTLUpdated),
			RelaysAdded:      d.RelaysAdded,
			Unchanged:        len(d.Unchanged),
		}
	}
	if d := plan.LivePlan; d != nil {
		relays := append([]RelayLiveDiff(nil), d.PerRelay...)
		sort.Slice(relays, func(i, j int) bool { return relays[i].Relay < relays[j].Relay })
		for _, rd := range relays {
			out.LivePlan = append(out.LivePlan, relayLiveWire{
				Relay:          rd.Relay,
				WouldAdd:       rd.WouldAdd,
				WouldRotate:    rd.WouldRotate,
				WouldRemove:    rd.WouldRemove,
				Unreachable:    rd.Unreachable,
				UnreachableErr: rd.UnreachableErr,
			})
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// toWire maps a slice of PeerDelta to the JSON wire shape, sorted by
// name for stable output. Centralized so the four call sites in
// formatPlanJSON stay one-line and the before/after-nil rules are
// written once.
func toWire(in []PeerDelta) []peerDeltaWire {
	out := make([]peerDeltaWire, 0, len(in))
	xs := append([]PeerDelta(nil), in...)
	sort.Slice(xs, func(i, j int) bool { return xs[i].Name < xs[j].Name })
	for _, d := range xs {
		row := peerDeltaWire{Name: d.Name, Reason: d.Reason}
		if d.Before != nil {
			row.Before = d.Before
		}
		if d.After != nil {
			row.After = d.After
		}
		out = append(out, row)
	}
	return out
}
