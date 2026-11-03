// diff_formatter_test.go — unit tests for the dry-run renderer.

package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/enrollment/peerset"
)

func TestFormatPlan_NilPlan_EmitsPlaceholder(t *testing.T) {
	var buf bytes.Buffer
	if err := FormatPlan(nil, FormatOptions{Out: &buf}); err != nil {
		t.Fatalf("FormatPlan: %v", err)
	}
	if !strings.Contains(buf.String(), "empty plan") {
		t.Errorf("got %q, want mention of empty plan", buf.String())
	}
}

func TestFormatPlan_EmptyPlan(t *testing.T) {
	var buf bytes.Buffer
	plan := &ApplyPlan{Intent: ReconcileIntent{}, DryRun: true}
	if err := FormatPlan(plan, FormatOptions{Out: &buf, NoColor: true}); err != nil {
		t.Fatalf("FormatPlan: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "wg-cp0 reconcile") {
		t.Errorf("header missing: %q", out)
	}
	if !strings.Contains(out, "no changes") {
		t.Errorf("clean-state message missing: %q", out)
	}
}

func TestFormatPlan_AddAndRemove_TextHasSections(t *testing.T) {
	peerA := peerset.Peer{Name: "alpha", WGCP0Address: "198.18.0.10", CurrentPubkey: "PK_ALPHA"}
	peerB := peerset.Peer{Name: "bravo", WGCP0Address: "198.18.0.11", CurrentPubkey: "PK_BRAVO"}
	plan := &ApplyPlan{
		Intent: ReconcileIntent{},
		DryRun: true,
		StatePlan: &PeerSetDiff{
			Added:   []PeerDelta{{Name: "alpha", After: &peerA}},
			Removed: []PeerDelta{{Name: "bravo", Before: &peerB, Reason: "decommissioned"}},
		},
		LivePlan: &LiveStateDiff{
			PerRelay: []RelayLiveDiff{
				{Relay: "relay-rla", WouldAdd: []LivePeerOp{{PeerName: "alpha", NewPubkey: "PK_ALPHA", AllowedIPs: "198.18.0.10/32"}}},
			},
		},
	}
	var buf bytes.Buffer
	if err := FormatPlan(plan, FormatOptions{Out: &buf, NoColor: true}); err != nil {
		t.Fatalf("FormatPlan: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"WOULD ADD", "WOULD REMOVE", "relay relay-rla", "alpha", "bravo", "Summary:"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n--- got ---\n%s", want, out)
		}
	}
}

func TestFormatPlan_UnreachableRelay_Surface(t *testing.T) {
	plan := &ApplyPlan{
		Intent: ReconcileIntent{},
		DryRun: true,
		LivePlan: &LiveStateDiff{
			PerRelay: []RelayLiveDiff{
				{Relay: "relay-rla", Unreachable: true, UnreachableErr: "dial timeout"},
			},
		},
	}
	var buf bytes.Buffer
	if err := FormatPlan(plan, FormatOptions{Out: &buf, NoColor: true}); err != nil {
		t.Fatalf("FormatPlan: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "UNREACHABLE") || !strings.Contains(out, "dial timeout") {
		t.Errorf("unreachable surface missing: %q", out)
	}
}

func TestFormatPlan_JSON_Stable(t *testing.T) {
	peerA := peerset.Peer{Name: "alpha", WGCP0Address: "198.18.0.10", CurrentPubkey: "PK_ALPHA"}
	plan := &ApplyPlan{
		Intent: ReconcileIntent{},
		DryRun: true,
		StatePlan: &PeerSetDiff{
			Added: []PeerDelta{{Name: "alpha", After: &peerA}},
		},
		LivePlan: &LiveStateDiff{
			PerRelay: []RelayLiveDiff{
				{Relay: "relay-rla", WouldAdd: []LivePeerOp{{PeerName: "alpha", NewPubkey: "PK_ALPHA"}}},
			},
		},
	}
	var buf bytes.Buffer
	if err := FormatPlan(plan, FormatOptions{Out: &buf, JSON: true}); err != nil {
		t.Fatalf("FormatPlan: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json parse: %v\n%s", err, buf.String())
	}
	if got["intent_kind"] != "reconcile" {
		t.Errorf("intent_kind = %v", got["intent_kind"])
	}
	if got["dry_run"] != true {
		t.Errorf("dry_run = %v", got["dry_run"])
	}
	state := got["state_plan"].(map[string]any)
	added := state["added"].([]any)
	if len(added) != 1 {
		t.Errorf("added rows = %d, want 1", len(added))
	}
}

func TestShouldColor_NoColorWins(t *testing.T) {
	var buf bytes.Buffer
	if shouldColor(&buf, FormatOptions{Color: true, NoColor: true}) {
		t.Error("NoColor must beat Color")
	}
}

func TestShouldColor_ExplicitColor(t *testing.T) {
	var buf bytes.Buffer
	if !shouldColor(&buf, FormatOptions{Color: true}) {
		t.Error("explicit Color should win on non-tty writer")
	}
}

func TestShouldColor_DefaultOnNonTTY(t *testing.T) {
	var buf bytes.Buffer
	if shouldColor(&buf, FormatOptions{}) {
		t.Error("default on non-tty should be no-color")
	}
}

func TestFormatPlan_Color_EmitsANSI(t *testing.T) {
	plan := &ApplyPlan{
		Intent: ReconcileIntent{},
		StatePlan: &PeerSetDiff{
			Added: []PeerDelta{{Name: "x", After: &peerset.Peer{Name: "x", WGCP0Address: "1.2.3.4", CurrentPubkey: "PK"}}},
		},
	}
	var buf bytes.Buffer
	if err := FormatPlan(plan, FormatOptions{Out: &buf, Color: true}); err != nil {
		t.Fatalf("FormatPlan: %v", err)
	}
	if !strings.Contains(buf.String(), "\x1b[") {
		t.Errorf("expected ANSI escapes; got %q", buf.String())
	}
}

func TestFormatPlan_Summary_CountsRollup(t *testing.T) {
	plan := &ApplyPlan{
		Intent: ReconcileIntent{},
		StatePlan: &PeerSetDiff{
			Added:   []PeerDelta{{Name: "a"}},
			Rotated: []PeerDelta{{Name: "b"}, {Name: "c"}},
		},
	}
	var buf bytes.Buffer
	_ = FormatPlan(plan, FormatOptions{Out: &buf, NoColor: true})
	if !strings.Contains(buf.String(), "1 add, 2 rotate") {
		t.Errorf("summary mismatch: %q", buf.String())
	}
}

func TestFormatPlan_RelayAdded_TextSection(t *testing.T) {
	plan := &ApplyPlan{
		Intent: ReconcileIntent{},
		StatePlan: &PeerSetDiff{
			RelaysAdded: []RelayDelta{
				{Name: "relay-rld", After: &peerset.Relay{Name: "relay-rld", Site: "rld", Tier: "goat-relay", Endpoint: "goat-relay-rld:51821"}},
			},
		},
	}
	var buf bytes.Buffer
	_ = FormatPlan(plan, FormatOptions{Out: &buf, NoColor: true})
	out := buf.String()
	for _, want := range []string{"WOULD ADD RELAY", "relay-rld", "site=rld"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
}

func TestFormatPlan_RelayAdded_SkipsAfterNil(t *testing.T) {
	plan := &ApplyPlan{
		Intent: ReconcileIntent{},
		StatePlan: &PeerSetDiff{
			RelaysAdded: []RelayDelta{{Name: "phantom"}}, // After=nil
		},
	}
	var buf bytes.Buffer
	_ = FormatPlan(plan, FormatOptions{Out: &buf, NoColor: true})
	if strings.Contains(buf.String(), "phantom") {
		t.Errorf("phantom relay with nil After should be skipped; got %q", buf.String())
	}
}

func TestFormatPlan_KeepCount_Visible(t *testing.T) {
	plan := &ApplyPlan{
		Intent: ReconcileIntent{},
		StatePlan: &PeerSetDiff{
			Added: []PeerDelta{{Name: "x", After: &peerset.Peer{Name: "x"}}},
			Unchanged: []PeerDelta{
				{Name: "a"}, {Name: "b"}, {Name: "c"},
			},
		},
	}
	var buf bytes.Buffer
	_ = FormatPlan(plan, FormatOptions{Out: &buf, NoColor: true})
	if !strings.Contains(buf.String(), "WOULD KEEP: 3 peers") {
		t.Errorf("KEEP rollup missing: %q", buf.String())
	}
}

func TestFormatDeltaLine_Rotate(t *testing.T) {
	before := peerset.Peer{Name: "x", WGCP0Address: "1.2.3.4", CurrentPubkey: "PK_OLD"}
	after := peerset.Peer{Name: "x", WGCP0Address: "1.2.3.4", CurrentPubkey: "PK_NEW"}
	d := PeerDelta{Name: "x", Before: &before, After: &after, Reason: "post-incident"}
	line := formatDeltaLine("WOULD ROTATE", d)
	if !strings.Contains(line, "PK_OLD") || !strings.Contains(line, "PK_NEW") || !strings.Contains(line, "post-incident") {
		t.Errorf("rotate line missing fields: %q", line)
	}
}

func TestFormatDeltaLine_EmptyDelta(t *testing.T) {
	d := PeerDelta{Name: "x"} // both Before + After nil
	line := formatDeltaLine("WOULD KEEP", d)
	if !strings.Contains(line, "empty delta") {
		t.Errorf("expected empty-delta placeholder; got %q", line)
	}
}

func TestFormatPlan_JSON_UnreachableRelay(t *testing.T) {
	plan := &ApplyPlan{
		Intent: ReconcileIntent{},
		DryRun: true,
		LivePlan: &LiveStateDiff{
			PerRelay: []RelayLiveDiff{
				{Relay: "relay-rla", Unreachable: true, UnreachableErr: "dial timeout"},
			},
		},
	}
	var buf bytes.Buffer
	if err := FormatPlan(plan, FormatOptions{Out: &buf, JSON: true}); err != nil {
		t.Fatalf("FormatPlan: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "\"unreachable\": true") {
		t.Errorf("unreachable=true missing in JSON: %q", out)
	}
	if !strings.Contains(out, "dial timeout") {
		t.Errorf("dial timeout err missing: %q", out)
	}
}
