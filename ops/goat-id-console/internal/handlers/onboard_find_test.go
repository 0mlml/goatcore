package handlers

// The cross-stage find index (#1306) is a pure function over the two pulls the
// page already makes. These tests pin what a row says, the order rows come in,
// and the one rule that matters for safety: a sponsor's index never holds a
// row that is not theirs.

import (
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

func i64(v int64) *int64 { return &v }

func TestBuildFindIndex_OrdersByChangedDesc(t *testing.T) {
	rows, truncated := buildFindIndex([]onboardstore.Proposal{
		{ID: "prop-b", CreatedAt: 100, Status: "proposed"},
		{ID: "prop-a", CreatedAt: 100, Status: "proposed"},
		{ID: "prop-c", CreatedAt: 50, DecidedAt: i64(300), Status: "approved"},
		{ID: "prop-d", CreatedAt: 60, SponsorApprovedAt: 200, Status: "sponsor_approved"},
	}, nil, nil)
	if truncated {
		t.Fatal("four rows must not truncate")
	}
	got := make([]string, 0, len(rows))
	for _, r := range rows {
		got = append(got, r.ID)
	}
	want := "prop-c prop-d prop-a prop-b" // decided > vouched > created; ties by id
	if strings.Join(got, " ") != want {
		t.Errorf("order = %q, want %q", strings.Join(got, " "), want)
	}
}

func TestBuildFindIndex_StageLabelsCoverEveryDef(t *testing.T) {
	for _, d := range onboardStageDefs {
		if got := stageLabel(d.Key); got != d.Label {
			t.Errorf("stageLabel(%q) = %q, want %q", d.Key, got, d.Label)
		}
	}
	if got := stageLabel("brand_new_status"); got != "brand_new_status" {
		t.Errorf("unknown key = %q, want the key itself", got)
	}
}

func TestBuildFindIndex_TitleAndFilterFromManifest(t *testing.T) {
	rows, _ := buildFindIndex([]onboardstore.Proposal{
		{ID: "prop-1", Handle: "jane-doe", Email: "jane@example.org", Status: "enrolled",
			ManifestJSON: `{"identity":{"name":"Jane Doe","github":"jane-doe-gh"}}`},
		{ID: "prop-2", Handle: "john-roe", Email: "john@example.org", Status: "archived", ManifestJSON: "{}"},
		{ID: "prop-3", Handle: "kim-poe", Email: "kim@example.org", Status: "failed", ManifestJSON: "{not json"},
	}, nil, nil)
	byID := map[string]onboardFindRow{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	if r := byID["prop-1"]; r.Title != "Jane Doe" || !strings.Contains(r.Filter, "Jane Doe") || !strings.Contains(r.Filter, "jane-doe-gh") {
		t.Errorf("manifest row = %+v, want the name as title and name+github in the filter", r)
	}
	if r := byID["prop-2"]; r.Title != "john-roe" || r.Filter != "prop-2 john@example.org john-roe" {
		t.Errorf("empty-manifest row = %+v, want handle as title and no manifest fields", r)
	}
	if r := byID["prop-3"]; r.Title != "kim-poe" || !strings.Contains(r.Filter, "kim@example.org") {
		t.Errorf("malformed-manifest row = %+v, want handle and email to survive", r)
	}
	if byID["prop-1"].StageLabel != "Enrolled" || byID["prop-1"].Stage != "enrolled" {
		t.Errorf("stage = %q/%q, want enrolled/Enrolled", byID["prop-1"].Stage, byID["prop-1"].StageLabel)
	}
}

func TestBuildFindIndex_InviteRows(t *testing.T) {
	rows, _ := buildFindIndex(nil, []onboardstore.Invite{
		{ID: "inv-open", Status: onboardstore.InviteInvited, CandidateEmail: "amy@example.org", Note: "Amy, liaison", CreatedAt: 10},
		{ID: "inv-exp", Status: onboardstore.InviteExpired, Note: "Ben", ExpiresAt: 20, CreatedAt: 5},
		{ID: "inv-rev", Status: onboardstore.InviteRevoked, RevokedAt: i64(30), CreatedAt: 5},
		{ID: "inv-arc", Status: onboardstore.InviteArchived, ArchivedAt: i64(40), CandidateEmail: "cat@example.org", CreatedAt: 5},
		{ID: "inv-sub", Status: onboardstore.InviteSubmitted, CandidateEmail: "dan@example.org", CreatedAt: 99},
	}, nil)
	byID := map[string]onboardFindRow{}
	for _, r := range rows {
		byID[r.ID] = r
		if r.Stage != onboardStageInvited || r.StageLabel != "Invited" {
			t.Errorf("%s stage = %q/%q, want invited/Invited", r.ID, r.Stage, r.StageLabel)
		}
	}
	if _, has := byID["inv-sub"]; has {
		t.Error("a submitted invite is indexed; its proposal row already represents it")
	}
	if r := byID["inv-open"]; r.StateLabel != "" || r.Title != "amy@example.org" || !strings.Contains(r.Filter, "Amy, liaison") {
		t.Errorf("open invite = %+v", r)
	}
	if r := byID["inv-exp"]; r.StateLabel != "expired" || r.Title != "Ben" || r.Changed != 20 {
		t.Errorf("expired invite = %+v, want the expiry as the change time and the note as title", r)
	}
	if r := byID["inv-rev"]; r.StateLabel != "revoked" || r.Title != "inv-rev" || r.Changed != 30 {
		t.Errorf("revoked invite = %+v, want the id as the last-resort title", r)
	}
	if r := byID["inv-arc"]; r.StateLabel != "archived" || r.Changed != 40 || r.Email != "cat@example.org" {
		t.Errorf("archived invite = %+v", r)
	}
	if rows[0].ID != "inv-arc" {
		t.Errorf("first row = %s, want the most recently changed (inv-arc)", rows[0].ID)
	}
}

func TestBuildFindIndex_SponsorScoped(t *testing.T) {
	proposals := []onboardstore.Proposal{
		{ID: "prop-own-a", Sponsor: "sponsora@example.org", Status: "proposed", CreatedAt: 3},
		{ID: "prop-own-b", Sponsor: "0ff404dd", Status: "archived", CreatedAt: 2},
		{ID: "prop-own-c", Sponsor: "Pat-Example", Status: "failed", CreatedAt: 1},
		{ID: "prop-foreign", Sponsor: "someone-else@example.org", Status: "proposed", CreatedAt: 9},
	}
	invites := []onboardstore.Invite{
		{ID: "inv-own", Sponsor: "sponsora@example.org", Status: onboardstore.InviteInvited, CreatedAt: 4},
		{ID: "inv-foreign", Sponsor: "other-prefix", Status: onboardstore.InviteInvited, CreatedAt: 8},
	}
	keys := sponsorKeySet("0ff404dd", "pat-example", "sponsora@example.org")
	rows, _ := buildFindIndex(proposals, invites, keys)
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
		if strings.Contains(r.ID, "foreign") {
			t.Errorf("foreign row %s reached a sponsor's index", r.ID)
		}
	}
	if len(rows) != 4 {
		t.Errorf("sponsor index = %v, want the four own rows", ids)
	}
	all, _ := buildFindIndex(proposals, invites, nil)
	if len(all) != 6 {
		t.Errorf("admin index holds %d rows, want all 6", len(all))
	}
}

func TestBuildFindIndex_Truncates(t *testing.T) {
	proposals := make([]onboardstore.Proposal, 0, findIndexMax+1)
	for i := 0; i <= findIndexMax; i++ {
		proposals = append(proposals, onboardstore.Proposal{ID: "prop-" + strings.Repeat("x", 3) + itoa(i), CreatedAt: int64(i), Status: "enrolled"})
	}
	rows, truncated := buildFindIndex(proposals, nil, nil)
	if !truncated || len(rows) != findIndexMax {
		t.Fatalf("truncated=%v len=%d, want true/%d", truncated, len(rows), findIndexMax)
	}
	if rows[0].Changed != int64(findIndexMax) {
		t.Errorf("newest row dropped: first Changed = %d, want %d", rows[0].Changed, findIndexMax)
	}
}

func TestFindFilter_DedupesAndTrims(t *testing.T) {
	got := findFilter(" prop-1 ", "", "Jane@Example.org", "jane@example.org", "jane-doe", "jane-doe")
	if got != "prop-1 Jane@Example.org jane-doe" {
		t.Errorf("findFilter = %q", got)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// A draft still holds the intake record, not an assembled manifest, so its
// name and GitHub login sit under candidate_submission. Each field falls back
// on its own; identity.* wins when both are present.
func TestManifestIdentity_AssembledOrDraft(t *testing.T) {
	cases := []struct{ name, manifest, wantName, wantGitHub string }{
		{"assembled", `{"identity":{"name":"Jane Doe","github":"jane-gh"}}`, "Jane Doe", "jane-gh"},
		{"draft", `{"schema_version":"candidate-intake/v-draft","prebound":{"tier":"developer"},` +
			`"candidate_submission":{"name":" Alice Smith ","email":"alice.smith@example.org","github_username":"alice-gh"}}`,
			"Alice Smith", "alice-gh"},
		{"identity wins per field", `{"identity":{"name":"Jane Doe","github":""},"candidate_submission":{"name":"Other","github_username":"jane-gh"}}`,
			"Jane Doe", "jane-gh"},
		{"draft without the fields", `{"prebound":{"tier":"developer"},"candidate_submission":{"email":"x@example.org"}}`, "", ""},
		{"empty", "", "", ""},
		{"blank", "   ", "", ""},
		{"malformed", "{not json", "", ""},
	}
	for _, c := range cases {
		name, github := manifestIdentity(c.manifest)
		if name != c.wantName || github != c.wantGitHub {
			t.Errorf("%s: manifestIdentity = (%q, %q), want (%q, %q)", c.name, name, github, c.wantName, c.wantGitHub)
		}
	}
}
