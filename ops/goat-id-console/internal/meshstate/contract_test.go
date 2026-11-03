package meshstate

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ops/lifecycle/fixtures/hold-records-bad-lines.jsonl is read by BOTH
// readers' test suites (this one and ops/scripts/test_lifecycle_sweep.py)
// and both must get the same answer: 2 records, 10 bad lines, the blank
// line neither. The numbers are duplicated on purpose — a change to
// either reader that moves them here must move them there too.
const (
	sharedBadLineFixture = "../../../lifecycle/fixtures/hold-records-bad-lines.jsonl"
	sharedFixtureRecords = 2
	sharedFixtureBad     = 10
)

func TestLoadHoldRecords_SharedBadLineFixtureMatchesTheSweep(t *testing.T) {
	recs, err := LoadHoldRecords(sharedBadLineFixture)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(recs.All) != sharedFixtureRecords || recs.Skipped != sharedFixtureBad {
		t.Fatalf("records=%d skipped=%d, want %d/%d (lines %v)", len(recs.All), recs.Skipped, sharedFixtureRecords, sharedFixtureBad, recs.SkippedLines)
	}
	wantLines := []int{2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	if fmt.Sprint(recs.SkippedLines) != fmt.Sprint(wantLines) {
		t.Fatalf("skipped lines = %v, want %v", recs.SkippedLines, wantLines)
	}
	if recs.All[0].PeerID != "good-dormancy-0001" || recs.All[1].PeerID != "good-releasing-0002" {
		t.Fatalf("records = %+v", recs.All)
	}
	if recs.All[1].Phase != "releasing" {
		t.Fatalf("phase = %q, want releasing", recs.All[1].Phase)
	}
}

// Each shape the sweep's _is_bad_hold_record rejects is rejected here
// too, one at a time, so a future divergence names the shape.
func TestParseHoldRecordLine_SweepParityShapes(t *testing.T) {
	base := `{"v":1,"lane":"%s","site":"zagros","peer_id":"p1","peer_name":"peer-ann-zagros-2026-09-01","handle":null,"entered_at":"2026-09-01T10:00:00Z","hold_group":"%s","prior_groups":[],"actor":"sweep"%s}`
	cases := []struct {
		name  string
		lane  string
		group string
		extra string
		ok    bool
	}{
		{"dormancy on PROBATION", "dormancy", "ZAGROS-PROBATION-DEVELOPER", "", true},
		{"security on QUARANTINED", "security", "ZAGROS-QUARANTINED", "", true},
		{"releasing phase", "dormancy", "ZAGROS-PROBATION-DEVELOPER", `,"phase":"releasing"`, true},
		{"lane outside enum", "vacation", "ZAGROS-PROBATION-DEVELOPER", "", false},
		{"dormancy on QUARANTINED", "dormancy", "ZAGROS-QUARANTINED", "", false},
		{"security on PROBATION", "security", "ZAGROS-PROBATION-DEVELOPER", "", false},
		{"phase other than releasing", "dormancy", "ZAGROS-PROBATION-DEVELOPER", `,"phase":"held"`, false},
		{"phase null", "dormancy", "ZAGROS-PROBATION-DEVELOPER", `,"phase":null`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			line := strings.Replace(strings.Replace(strings.Replace(base, "%s", c.lane, 1), "%s", c.group, 1), "%s", c.extra, 1)
			_, ok := parseHoldRecordLine(line)
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v for %s", ok, c.ok, line)
			}
		})
	}
	// prior_groups must be a list of strings.
	for _, pg := range []string{`"ZAGROS-DEVELOPER"`, `null`, `[1,2]`, `{"a":1}`} {
		line := `{"v":1,"lane":"dormancy","site":"zagros","peer_id":"p1","peer_name":"n","handle":null,"entered_at":"2026-09-01T10:00:00Z","hold_group":"ZAGROS-PROBATION-DEVELOPER","prior_groups":` + pg + `,"actor":"sweep"}`
		if _, ok := parseHoldRecordLine(line); ok {
			t.Fatalf("prior_groups=%s accepted, want bad line", pg)
		}
	}
}

// One over-long line is one bad line; the good lines around it load and
// the section is not blanked for every member.
func TestLoadHoldRecords_OverlongLineIsOneBadLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hold-records.jsonl")
	huge := `{"v":1,"lane":"dormancy","site":"zagros","peer_id":"huge","peer_name":"` + strings.Repeat("x", maxHoldRecordLine+16) + `","handle":null,"entered_at":"2026-09-01T10:00:00Z","hold_group":"ZAGROS-PROBATION-DEVELOPER","prior_groups":[],"actor":"sweep"}`
	content := good("p1") + "\n" + huge + "\n" + good("p2") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	recs, err := LoadHoldRecords(path)
	if err != nil {
		t.Fatalf("load: %v (an over-long line must not fail the load)", err)
	}
	if len(recs.All) != 2 || recs.Skipped != 1 || len(recs.SkippedLines) != 1 || recs.SkippedLines[0] != 2 {
		t.Fatalf("records=%d skipped=%d lines=%v, want 2/1/[2]", len(recs.All), recs.Skipped, recs.SkippedLines)
	}
	// Last line without a trailing newline still loads.
	if err := os.WriteFile(path, []byte(good("p1")+"\n"+good("p2")), 0o600); err != nil {
		t.Fatal(err)
	}
	recs, err = LoadHoldRecords(path)
	if err != nil || len(recs.All) != 2 || recs.Skipped != 0 {
		t.Fatalf("no trailing newline: records=%d skipped=%d err=%v", len(recs.All), recs.Skipped, err)
	}
}

// A record reaches a member's page by peer ownership only. A record whose
// handle field names this member but whose peer belongs to somebody else
// stays off the page; a record with a null handle whose peer IS this
// member's lands on it.
func TestResolve_RecordsMatchByPeerIDNeverByHandle(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	known := map[string]struct{}{"dave": {}, "dave-2": {}}
	dave := "dave"
	// The sweep once recorded dave-2's peer under handle "dave".
	wrongHandle := HoldRecord{Version: 1, Lane: "dormancy", Site: "zagros", PeerID: "p-dave-2",
		PeerName: "peer-dave-2-usareur-af-zagros-2026-08-04", Handle: &dave,
		EnteredAt: now.Add(-72 * time.Hour), HoldGroup: "ZAGROS-PROBATION-DEVELOPER",
		PriorGroups: []string{"ZAGROS-DEVELOPER"}, Actor: "sweep"}
	// dave's own peer, recorded with handle null (ambiguous name).
	ownNullHandle := HoldRecord{Version: 1, Lane: "dormancy", Site: "zagros", PeerID: "p-dave",
		PeerName: "peer-dave-acme-zagros-2026-08-04", Handle: nil,
		EnteredAt: now.Add(-48 * time.Hour), HoldGroup: "ZAGROS-PROBATION-DEVELOPER",
		PriorGroups: []string{"ZAGROS-DEVELOPER"}, Actor: "sweep"}
	peers := []Peer{
		{ID: "p-dave-2", Name: wrongHandle.PeerName, Groups: []Group{{Name: "ZAGROS-PROBATION-DEVELOPER"}}},
		{ID: "p-dave", Name: ownNullHandle.PeerName, Groups: []Group{{Name: "ZAGROS-PROBATION-DEVELOPER"}}},
	}
	av := Resolve(ResolveInput{
		Handle: "dave", Site: "zagros", Known: known,
		Records: Records{All: []HoldRecord{wrongHandle, ownNullHandle}, DuplicatePeerIDs: map[string]bool{}},
		Peers:   peers, Now: now,
	})
	if len(av.Peers) != 1 || av.Peers[0].ID != "p-dave" || av.Peers[0].Verdict != "on_hold" || !av.Peers[0].HasRecord {
		t.Fatalf("dave's page = %+v, want exactly his own held peer", av.Peers)
	}
	if av.Inconsistent {
		t.Fatalf("Inconsistent = true: %s", av.InconsistentDetail)
	}
	av2 := Resolve(ResolveInput{
		Handle: "dave-2", Site: "zagros", Known: known,
		Records: Records{All: []HoldRecord{wrongHandle, ownNullHandle}, DuplicatePeerIDs: map[string]bool{}},
		Peers:   peers, Now: now,
	})
	if len(av2.Peers) != 1 || av2.Peers[0].ID != "p-dave-2" || av2.Peers[0].Verdict != "on_hold" {
		t.Fatalf("dave-2's page = %+v, want his own held peer despite the record's handle", av2.Peers)
	}
}

// Two hold groups at once is a contradiction, with and without a record,
// and both groups are named. PROBATION sorts before QUARANTINED, so a
// first-match rule would have called a quarantined peer a dormancy hold.
func TestResolve_TwoHoldGroupsIsInconsistent(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	known := map[string]struct{}{"alice": {}}
	peer := Peer{ID: "p-alice", Name: "peer-alice-zagros-2026-09-01", Groups: []Group{
		{Name: "All"}, {Name: "ZAGROS-QUARANTINED"}, {Name: "ZAGROS-PROBATION-DEVELOPER"},
	}}
	check := func(t *testing.T, av AccessView) {
		t.Helper()
		if len(av.Peers) != 1 || av.Peers[0].Verdict != "inconsistent" || !av.Inconsistent {
			t.Fatalf("got %+v, want inconsistent", av.Peers)
		}
		d := av.Peers[0].VerdictDetail
		if !strings.Contains(d, "ZAGROS-QUARANTINED") || !strings.Contains(d, "ZAGROS-PROBATION-DEVELOPER") || !strings.Contains(d, "more than one hold group") {
			t.Fatalf("detail = %q, want both hold groups named", d)
		}
	}
	t.Run("no record", func(t *testing.T) {
		check(t, Resolve(ResolveInput{Handle: "alice", Site: "zagros", Known: known,
			Records: Records{DuplicatePeerIDs: map[string]bool{}}, Peers: []Peer{peer}, Now: now}))
	})
	t.Run("record names QUARANTINED", func(t *testing.T) {
		rec := HoldRecord{Version: 1, Lane: "security", Site: "zagros", PeerID: peer.ID, PeerName: peer.Name,
			EnteredAt: now.Add(-24 * time.Hour), HoldGroup: "ZAGROS-QUARANTINED", PriorGroups: []string{"ZAGROS-DEVELOPER"}, Actor: "jane-admin"}
		check(t, Resolve(ResolveInput{Handle: "alice", Site: "zagros", Known: known,
			Records: Records{All: []HoldRecord{rec}, DuplicatePeerIDs: map[string]bool{}}, Peers: []Peer{peer}, Now: now}))
	})
	t.Run("record names PROBATION", func(t *testing.T) {
		rec := HoldRecord{Version: 1, Lane: "dormancy", Site: "zagros", PeerID: peer.ID, PeerName: peer.Name,
			EnteredAt: now.Add(-24 * time.Hour), HoldGroup: "ZAGROS-PROBATION-DEVELOPER", PriorGroups: []string{"ZAGROS-DEVELOPER"}, Actor: "sweep"}
		check(t, Resolve(ResolveInput{Handle: "alice", Site: "zagros", Known: known,
			Records: Records{All: []HoldRecord{rec}, DuplicatePeerIDs: map[string]bool{}}, Peers: []Peer{peer}, Now: now}))
	})
}

// A record mid-release is its own state whatever the live shape looks
// like, and the detail tells the admin to run the release again.
func TestResolve_ReleasingRecordIsNotGradedAsAHold(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	known := map[string]struct{}{"alice": {}}
	rec := HoldRecord{Version: 1, Lane: "dormancy", Site: "zagros", PeerID: "p-alice", PeerName: "peer-alice-zagros-2026-09-01",
		EnteredAt: now.Add(-24 * time.Hour), HoldGroup: "ZAGROS-PROBATION-DEVELOPER", PriorGroups: []string{"ZAGROS-DEVELOPER"},
		Actor: "sweep", Phase: "releasing"}
	shapes := map[string][]Group{
		"prior back, hold group still on": {{Name: "ZAGROS-DEVELOPER"}, {Name: "ZAGROS-PROBATION-DEVELOPER"}},
		"hold group gone, prior not yet":  {},
		"still held":                      {{Name: "ZAGROS-PROBATION-DEVELOPER"}},
	}
	for name, groups := range shapes {
		t.Run(name, func(t *testing.T) {
			av := Resolve(ResolveInput{Handle: "alice", Site: "zagros", Known: known,
				Records: Records{All: []HoldRecord{rec}, DuplicatePeerIDs: map[string]bool{}},
				Peers:   []Peer{{ID: rec.PeerID, Name: rec.PeerName, Groups: groups}}, Now: now})
			if len(av.Peers) != 1 || av.Peers[0].Verdict != "releasing" || av.Peers[0].Phase != "releasing" {
				t.Fatalf("got %+v, want releasing", av.Peers)
			}
			if av.Inconsistent {
				t.Fatalf("Inconsistent = true for a release in progress: %s", av.InconsistentDetail)
			}
			if !strings.Contains(av.Peers[0].VerdictDetail, "--lane release") {
				t.Fatalf("detail = %q, want the re-run command", av.Peers[0].VerdictDetail)
			}
		})
	}
}

// An expiry the API sent but the client could not parse never reads as
// valid.
func TestResolve_UnreadableExpiryIsNeverValid(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	av := Resolve(ResolveInput{Handle: "alice", Site: "zagros", Known: map[string]struct{}{"alice": {}},
		Records: Records{DuplicatePeerIDs: map[string]bool{}},
		Keys:    []SetupKey{{ID: "k", Name: "peer-alice-zagros-2026-09-01", Valid: true, ExpiresUnreadable: true}}, Now: now})
	if len(av.Keys) != 1 || av.Keys[0].Status != "expiry unreadable" {
		t.Fatalf("keys = %+v, want status 'expiry unreadable'", av.Keys)
	}
}

// When the handle roster cannot be read, OwnsName is a bare prefix test
// and dave's page would claim dave-2's peer, record and key. Nothing is
// attributed; the live halves render unavailable and name the cause.
func TestResolve_RosterFailureAttributesNothing(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	rec := HoldRecord{Version: 1, Lane: "dormancy", Site: "zagros", PeerID: "p-dave-2", PeerName: "peer-dave-2-acme-zagros-2026-08-04",
		EnteredAt: now.Add(-24 * time.Hour), HoldGroup: "ZAGROS-PROBATION-DEVELOPER", PriorGroups: []string{"ZAGROS-DEVELOPER"}, Actor: "sweep"}
	av := Resolve(ResolveInput{Handle: "dave", Site: "zagros", Known: map[string]struct{}{},
		KnownErr: context.DeadlineExceeded,
		Records:  Records{All: []HoldRecord{rec}, DuplicatePeerIDs: map[string]bool{}},
		Peers:    []Peer{{ID: "p-dave-2", Name: rec.PeerName, Groups: []Group{{Name: "ZAGROS-PROBATION-DEVELOPER"}}}},
		Keys:     []SetupKey{{ID: "k", Name: "peer-dave-2-acme-zagros-2026-08-04", Valid: true}}, Now: now})
	if av.KnownHandlesError == "" || av.MeshAvailable || av.KeysAvailable {
		t.Fatalf("roster failure: KnownHandlesError=%q MeshAvailable=%v KeysAvailable=%v", av.KnownHandlesError, av.MeshAvailable, av.KeysAvailable)
	}
	if len(av.Peers) != 0 || len(av.Keys) != 0 || av.NoKeyForHandle {
		t.Fatalf("attributed under a partial roster: peers=%+v keys=%+v", av.Peers, av.Keys)
	}
	if !strings.Contains(av.MeshError, "roster") {
		t.Fatalf("MeshError = %q, want the roster named", av.MeshError)
	}
}

// Any group beside the hold group is a contradiction, whatever its
// prefix or case (the sweep's rule), and hold groups compare
// case-insensitively.
func TestResolve_AnyOtherGroupBesideHoldIsInconsistent(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	known := map[string]struct{}{"alice": {}}
	for _, extra := range []string{"printers", "SITE_ONE-SENSOR", "zagros-developer"} {
		t.Run(extra, func(t *testing.T) {
			peer := Peer{ID: "p", Name: "peer-alice-zagros-2026-09-01", Groups: []Group{{Name: "All"}, {Name: "zagros-probation-developer"}, {Name: extra}}}
			av := Resolve(ResolveInput{Handle: "alice", Site: "zagros", Known: known,
				Records: Records{DuplicatePeerIDs: map[string]bool{}}, Peers: []Peer{peer}, Now: now})
			if len(av.Peers) != 1 || av.Peers[0].Verdict != "inconsistent" || !strings.Contains(av.Peers[0].VerdictDetail, extra) {
				t.Fatalf("got %+v, want inconsistent naming %s", av.Peers, extra)
			}
		})
	}
	// Lower-case hold group beside nothing else is a clean hold.
	peer := Peer{ID: "p", Name: "peer-alice-zagros-2026-09-01", Groups: []Group{{Name: "All"}, {Name: "zagros-probation-developer"}}}
	rec := HoldRecord{Version: 1, Lane: "dormancy", Site: "zagros", PeerID: "p", PeerName: peer.Name, EnteredAt: now.Add(-time.Hour),
		HoldGroup: "ZAGROS-PROBATION-DEVELOPER", PriorGroups: []string{"ZAGROS-DEVELOPER"}, Actor: "sweep"}
	av := Resolve(ResolveInput{Handle: "alice", Site: "zagros", Known: known,
		Records: Records{All: []HoldRecord{rec}, DuplicatePeerIDs: map[string]bool{}}, Peers: []Peer{peer}, Now: now})
	if len(av.Peers) != 1 || av.Peers[0].Verdict != "on_hold" {
		t.Fatalf("case-insensitive hold group: %+v", av.Peers)
	}
}

func TestClientSetupKeys_UnparsableExpiryIsFlagged(t *testing.T) {
	cli := &Client{MgmtURL: "https://netbird.example.test", Token: "t",
		HTTP: &http.Client{Transport: staticJSONRoundTripper{body: `[{"id":"k-1","name":"peer-alice-zagros-2026-09-01","used_times":0,"revoked":false,"valid":true,"expires":"next tuesday","state":"active"}]`}}}
	keys, err := cli.SetupKeys(context.Background())
	if err != nil || len(keys) != 1 {
		t.Fatalf("keys=%+v err=%v", keys, err)
	}
	if !keys[0].ExpiresUnreadable || !keys[0].Expires.IsZero() {
		t.Fatalf("key = %+v, want ExpiresUnreadable with zero Expires", keys[0])
	}
}

// An orphan record (its peer_id is no live peer) is attributed by
// peer_name under the same roster-disambiguated rule as live peers, so it
// surfaces as a contradiction on the right page and on no other.
func TestResolve_OrphanRecordAttributedByNameOwnership(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	known := map[string]struct{}{"dave": {}, "dave-2": {}}
	orphan := HoldRecord{Version: 1, Lane: "dormancy", Site: "zagros", PeerID: "p-gone",
		PeerName: "peer-dave-2-acme-zagros-2026-08-04", EnteredAt: now.Add(-24 * time.Hour),
		HoldGroup: "ZAGROS-PROBATION-DEVELOPER", PriorGroups: []string{"ZAGROS-DEVELOPER"}, Actor: "sweep"}
	in := func(h string) ResolveInput {
		return ResolveInput{Handle: h, Site: "zagros", Known: known,
			Records: Records{All: []HoldRecord{orphan}, DuplicatePeerIDs: map[string]bool{}},
			Peers:   []Peer{{ID: "p-other", Name: "peer-dave-acme-zagros-2026-08-04"}}, Now: now}
	}
	av := Resolve(in("dave-2"))
	if len(av.Peers) != 1 || av.Peers[0].Verdict != "inconsistent" || !strings.Contains(av.Peers[0].VerdictDetail, "mesh does not list") {
		t.Fatalf("dave-2: %+v", av.Peers)
	}
	av = Resolve(in("dave"))
	for _, p := range av.Peers {
		if p.ID == "p-gone" {
			t.Fatalf("dave's page shows dave-2's orphan record: %+v", av.Peers)
		}
	}
}
