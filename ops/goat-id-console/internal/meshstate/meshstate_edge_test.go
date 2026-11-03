package meshstate

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

const goodLine = `{"v":1,"lane":"security","site":"zagros","peer_id":"%s","peer_name":"peer-%s-zagros-2026-09-01","handle":"%s","entered_at":"2026-09-01T00:00:00Z","hold_group":"ZAGROS-QUARANTINED","prior_groups":["ZAGROS-DEVELOPER"],"actor":"jane-admin"}`

func good(peer string) string { return fmt.Sprintf(goodLine, peer, peer, peer) }

// Contract (ops/lifecycle/README.md "A bad line is skipped, never fatal,
// and always counted"): a bad line is skipped and counted, the good
// lines around it still load, and blank lines are neither.
func TestLoadHoldRecords_BadLinesAreSkippedAndCounted(t *testing.T) {
	tmp := t.TempDir()
	path := tmp + "/bad.jsonl"

	cases := []struct {
		name         string
		bad          string
		wantSkipped  int
		wantSkipLine []int
		loads        bool // the "bad" line is in fact acceptable and loads
	}{
		{name: "blank line is ignored, not counted", bad: "   ", wantSkipped: 0},
		{name: "not json", bad: `{"v":1,"peer_id":"p1"`, wantSkipped: 1, wantSkipLine: []int{2}},
		{name: "not an object", bad: `[1,2,3]`, wantSkipped: 1, wantSkipLine: []int{2}},
		{name: "empty peer_id", bad: strings.Replace(good("p1"), `"peer_id":"p1"`, `"peer_id":"   "`, 1), wantSkipped: 1, wantSkipLine: []int{2}},
		{name: "unsupported version", bad: strings.Replace(good("p1"), `"v":1`, `"v":2`, 1), wantSkipped: 1, wantSkipLine: []int{2}},
		{name: "required field missing (entered_at)", bad: strings.Replace(good("p1"), `"entered_at":"2026-09-01T00:00:00Z",`, ``, 1), wantSkipped: 1, wantSkipLine: []int{2}},
		{name: "entered_at unparsable", bad: strings.Replace(good("p1"), `2026-09-01T00:00:00Z`, `yesterday`, 1), wantSkipped: 1, wantSkipLine: []int{2}},
		{name: "prior_groups wrong type", bad: strings.Replace(good("p1"), `["ZAGROS-DEVELOPER"]`, `"ZAGROS-DEVELOPER"`, 1), wantSkipped: 1, wantSkipLine: []int{2}},
		{name: "handle null is allowed", bad: strings.Replace(good("p1"), `"handle":"p1"`, `"handle":null`, 1), wantSkipped: 0, loads: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := good("p0") + "\n" + tc.bad + "\n" + good("p2") + "\n"
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			recs, err := LoadHoldRecords(path)
			if err != nil {
				t.Fatalf("a bad line must never fail the load; got %v", err)
			}
			if recs.Skipped != tc.wantSkipped {
				t.Fatalf("Skipped = %d, want %d", recs.Skipped, tc.wantSkipped)
			}
			if fmt.Sprint(recs.SkippedLines) != fmt.Sprint(tc.wantSkipLine) {
				t.Fatalf("SkippedLines = %v, want %v", recs.SkippedLines, tc.wantSkipLine)
			}
			wantAll := 2
			if tc.loads {
				wantAll = 3
			}
			if len(recs.All) != wantAll {
				t.Fatalf("got %d records, want %d (good lines around a bad one still load)", len(recs.All), wantAll)
			}
			if recs.All[0].PeerID != "p0" || recs.All[len(recs.All)-1].PeerID != "p2" {
				t.Fatalf("records = %+v", recs.All)
			}
		})
	}
}

// The unreadable-file case is the only error the loader returns.
func TestLoadHoldRecords_MissingFileIsAnError(t *testing.T) {
	if _, err := LoadHoldRecords(t.TempDir() + "/absent.jsonl"); err == nil {
		t.Fatal("expected an error for an absent file")
	}
}

// holdRecordRequiredFields must match the schema's "required" list, or
// the console and the sweep would disagree on what a bad line is.
func TestHoldRecordRequiredFieldsMatchSchema(t *testing.T) {
	b, err := os.ReadFile("../../../lifecycle/schema/hold-record.v1.schema.json")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	var schema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(b, &schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	want := append([]string(nil), schema.Required...)
	got := append([]string(nil), holdRecordRequiredFields...)
	sort.Strings(want)
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("required fields drifted: code %v, schema %v", got, want)
	}
}

// The skip count reaches the view when records loaded, and is absent
// when they did not.
func TestResolve_SurfacesSkippedLineCount(t *testing.T) {
	av := Resolve(ResolveInput{
		Handle: "alice", Site: "zagros", Known: map[string]struct{}{"alice": {}},
		Records: Records{DuplicatePeerIDs: map[string]bool{}, Skipped: 2, SkippedLines: []int{3, 7}},
		Now:     time.Now(),
	})
	if !av.RecordsAvailable || av.RecordsSkipped != 2 || fmt.Sprint(av.RecordsSkippedLines) != "[3 7]" {
		t.Fatalf("got available=%v skipped=%d lines=%v", av.RecordsAvailable, av.RecordsSkipped, av.RecordsSkippedLines)
	}
	av = Resolve(ResolveInput{
		Handle: "alice", Site: "zagros", Known: map[string]struct{}{"alice": {}},
		Records: Records{Skipped: 2}, RecordsErr: fmt.Errorf("boom"), Now: time.Now(),
	})
	if av.RecordsAvailable || av.RecordsSkipped != 0 {
		t.Fatalf("unavailable records must not report a skip count; got %+v", av)
	}
}

func TestLoadHoldRecords_DuplicatePeerIDs(t *testing.T) {
	tmp := t.TempDir()
	path := tmp + "/dups.jsonl"

	content := good("p1") + "\n" + good("p2") + "\n" + good("p1") + "\n"

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	recs, err := LoadHoldRecords(path)
	if err != nil {
		t.Fatalf("LoadHoldRecords: %v", err)
	}
	if len(recs.All) != 3 {
		t.Fatalf("got %d records, want 3", len(recs.All))
	}
	if !recs.DuplicatePeerIDs["p1"] {
		t.Fatalf("DuplicatePeerIDs[p1] = false, want true")
	}
	if recs.DuplicatePeerIDs["p2"] {
		t.Fatalf("DuplicatePeerIDs[p2] = true, want false")
	}
}

// A peer that holds a hold group AND any other live group on its site is
// contradictory whether or not a hold record exists (issue #1041 item 4).
// Neither branch may pick a side.
func TestResolve_HoldGroupBesideOtherSiteGroupsIsInconsistent(t *testing.T) {
	known := map[string]struct{}{"alice": {}}
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	peer := Peer{
		ID:   "p-alice",
		Name: "peer-alice-zagros-2026-09-01",
		Groups: []Group{
			{Name: "All"},
			{Name: "ZAGROS-QUARANTINED"},
			{Name: "ZAGROS-DEVELOPER"},
			{Name: "OTHERSITE-DEVELOPER"}, // another site's group: a contradiction too (sweep rule)
		},
	}

	t.Run("no record", func(t *testing.T) {
		av := Resolve(ResolveInput{
			Handle: "alice", Site: "zagros", Known: known,
			Records: Records{DuplicatePeerIDs: map[string]bool{}},
			Peers:   []Peer{peer}, Now: now,
		})
		if len(av.Peers) != 1 || av.Peers[0].Verdict != "inconsistent" || !av.Inconsistent {
			t.Fatalf("got %+v, want inconsistent", av.Peers)
		}
		if !strings.Contains(av.Peers[0].VerdictDetail, "ZAGROS-DEVELOPER") || !strings.Contains(av.Peers[0].VerdictDetail, "OTHERSITE-DEVELOPER") {
			t.Fatalf("detail = %q, want both extra groups named", av.Peers[0].VerdictDetail)
		}
	})

	t.Run("record whose prior list does not name the extra group", func(t *testing.T) {
		h := "alice"
		rec := HoldRecord{Version: 1, Lane: "security", Site: "zagros", PeerID: peer.ID, PeerName: peer.Name,
			Handle: &h, EnteredAt: now.Add(-48 * time.Hour), HoldGroup: "ZAGROS-QUARANTINED",
			PriorGroups: []string{"ZAGROS-OBSERVER"}, Actor: "jane-admin"}
		av := Resolve(ResolveInput{
			Handle: "alice", Site: "zagros", Known: known,
			Records: Records{All: []HoldRecord{rec}, DuplicatePeerIDs: map[string]bool{}},
			Peers:   []Peer{peer}, Now: now,
		})
		if len(av.Peers) != 1 || av.Peers[0].Verdict != "inconsistent" || !av.Inconsistent {
			t.Fatalf("got %+v, want inconsistent", av.Peers)
		}
	})
}
