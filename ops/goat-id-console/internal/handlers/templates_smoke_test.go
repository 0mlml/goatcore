package handlers

import (
	"bytes"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/config"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/meshstate"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/permissions"
	"github.com/usareur-af/goatcore/ops/goat-id-console/web"
)

// Render-execute the templates the group-vocabulary guardrails touched, the
// same way Server.render does (clone base, re-parse the page's "content" into
// it, execute "layout"), with GroupOptions both set and empty — a field typo
// in a template fails at execute time, not parse time.
func TestTemplates_userFormsRenderWithGroupOptions(t *testing.T) {
	base, err := loadTemplates()
	if err != nil {
		t.Fatalf("loadTemplates: %v", err)
	}
	opts := []permissions.Group{{Name: "PLATFORM-ADMIN", Description: "platform admin"}, {Name: "GRAFANA-READONLY"}}
	cases := []struct {
		page string
		vars *pageVars
		must []string
	}{
		{"user_new", &pageVars{GroupOptions: opts}, []string{"LOGIN identity only", "/onboarding/invite", "PLATFORM-ADMIN", "groups_extra", "not</strong> set here"}},
		{"user_new", &pageVars{}, []string{"Initial groups (comma-separated)"}},
		{"user_detail", &pageVars{User: &userDetail{ID: "x", GroupsCSV: "ADMINS"}, GroupOptions: opts}, []string{"NetBird-plane", "PLATFORM-ADMIN"}},
		{"user_detail", &pageVars{User: &userDetail{ID: "x"}}, []string{"ADMINS, GRAFANA-READONLY"}},
	}
	for _, c := range cases {
		tm, err := base.Clone()
		if err != nil {
			t.Fatalf("clone: %v", err)
		}
		data, err := fs.ReadFile(web.FS, "templates/"+c.page+".html")
		if err != nil {
			t.Fatalf("read %s: %v", c.page, err)
		}
		if _, err := tm.New(c.page).Parse(string(data)); err != nil {
			t.Fatalf("parse %s: %v", c.page, err)
		}
		var b strings.Builder
		if err := tm.ExecuteTemplate(&b, "layout", c.vars); err != nil {
			t.Fatalf("execute %s: %v", c.page, err)
		}
		for _, m := range c.must {
			if !strings.Contains(b.String(), m) {
				t.Errorf("%s render missing %q", c.page, m)
			}
		}
	}
}

// users_list carries the reframed action buttons — render-execute it too.
func TestTemplates_usersListRendersOnboardFirst(t *testing.T) {
	base, err := loadTemplates()
	if err != nil {
		t.Fatalf("loadTemplates: %v", err)
	}
	tm, err := base.Clone()
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	data, err := fs.ReadFile(web.FS, "templates/users_list.html")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if _, err := tm.New("users_list").Parse(string(data)); err != nil {
		t.Fatalf("parse: %v", err)
	}
	var b strings.Builder
	if err := tm.ExecuteTemplate(&b, "layout", &pageVars{}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := b.String()
	if !strings.Contains(out, "Onboard someone (invite)") || !strings.Contains(out, "/onboarding/invite") {
		t.Error("users list must lead with the onboarding invite action")
	}
	if !strings.Contains(out, "New login only") {
		t.Error("users list must reframe the raw create as login-only")
	}
}

// invites_list carries the login-invite decoder + vocabulary hint.
func TestTemplates_invitesListRendersLoginOnlyDecoder(t *testing.T) {
	base, err := loadTemplates()
	if err != nil {
		t.Fatalf("loadTemplates: %v", err)
	}
	tm, err := base.Clone()
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	data, err := fs.ReadFile(web.FS, "templates/invites_list.html")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if _, err := tm.New("invites_list").Parse(string(data)); err != nil {
		t.Fatalf("parse: %v", err)
	}
	var b strings.Builder
	if err := tm.ExecuteTemplate(&b, "layout", &pageVars{}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := b.String()
	for _, m := range []string{"LOGIN invite", "/onboarding/invite", "NOT emails"} {
		if !strings.Contains(out, m) {
			t.Errorf("invites list missing %q", m)
		}
	}
}

// user_detail gained the role-reference action. Render-execute it.
//
// docs_onboarding used to be asserted here too. That template is gone: the
// page was a second copy of the guide's admin material that drifted out of
// date, and /docs/onboarding is now a redirect to the guide. Its content is
// asserted in the welcome package's guide tests instead.
func TestTemplates_docsPageAndReferenceButton(t *testing.T) {
	base, err := loadTemplates()
	if err != nil {
		t.Fatalf("loadTemplates: %v", err)
	}
	cases := []struct {
		page string
		vars *pageVars
		must []string
	}{
		{"user_detail", &pageVars{User: &userDetail{ID: "x"}}, []string{"Email role reference", "/email-reference"}},
	}
	for _, c := range cases {
		tm, err := base.Clone()
		if err != nil {
			t.Fatalf("clone: %v", err)
		}
		data, err := fs.ReadFile(web.FS, "templates/"+c.page+".html")
		if err != nil {
			t.Fatalf("read %s: %v", c.page, err)
		}
		if _, err := tm.New(c.page).Parse(string(data)); err != nil {
			t.Fatalf("parse %s: %v", c.page, err)
		}
		var b strings.Builder
		if err := tm.ExecuteTemplate(&b, "layout", c.vars); err != nil {
			t.Fatalf("execute %s: %v", c.page, err)
		}
		for _, m := range c.must {
			if !strings.Contains(b.String(), m) {
				t.Errorf("%s render missing %q", c.page, m)
			}
		}
	}
}

// TestUserDetailTemplate_AccessStatesSmoke renders the user_detail template
// for a few AccessView shapes to catch template errors early.
func TestUserDetailTemplate_AccessStatesSmoke(t *testing.T) {
	base, err := loadTemplates()
	if err != nil {
		t.Fatalf("loadTemplates: %v", err)
	}

	data, err := fs.ReadFile(web.FS, "templates/user_detail.html")
	if err != nil {
		t.Fatalf("read user_detail: %v", err)
	}

	tmpl, err := base.Clone()
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	if _, err := tmpl.New("user_detail").Parse(string(data)); err != nil {
		t.Fatalf("parse user_detail: %v", err)
	}

	cases := []meshstate.AccessView{
		{HandleError: "onboarding store is not configured"},
		{Handle: "alice", Site: "zagros"},
		{
			Handle:           "alice",
			Site:             "zagros",
			RecordsAvailable: false,
			RecordsError:     "HOLD_RECORDS_PATH is unset",
			MeshAvailable:    false,
			MeshError:        "NetBird mesh state unavailable — ENROLL_WATCH_NB_TOKEN is unset",
		},
		{
			Handle:           "alice",
			Site:             "zagros",
			RecordsAvailable: true,
			MeshAvailable:    true,
			Peers: []meshstate.PeerView{{
				ID:         "p-1",
				Name:       "peer-alice-zagros-2026-09-01",
				Lane:       "dormancy",
				HoldGroup:  "ZAGROS-PROBATION-DEVELOPER",
				EnteredAt:  "2026-09-01T10:00:00Z",
				DaysOnHold: "7.0",
				Verdict:    "on_hold",
			}},
			KeysAvailable: true,
			Keys: []meshstate.KeyView{{
				ID:        "k-1",
				Name:      "peer-alice-zagros-2026-09-01",
				UsedTimes: 1,
				Status:    "valid",
			}},
		},
		{
			Handle:             "alice",
			Site:               "zagros",
			RecordsAvailable:   true,
			MeshAvailable:      true,
			Inconsistent:       true,
			InconsistentDetail: "record names a peer the mesh does not list",
			Peers: []meshstate.PeerView{{
				ID:      "p-2",
				Name:    "peer-alice-hold-zagros",
				Verdict: "inconsistent",
			}},
		},
		{
			Handle:           "alice",
			Site:             "zagros",
			RecordsAvailable: true,
			MeshAvailable:    true,
			Peers: []meshstate.PeerView{{
				ID:        "p-3",
				Name:      "peer-alice-security-zagros",
				HoldGroup: "ZAGROS-QUARANTINED",
				Verdict:   "on_hold_no_record",
			}},
		},
		{
			Handle:           "alice",
			Site:             "zagros",
			RecordsAvailable: true,
			MeshAvailable:    true,
			Peers: []meshstate.PeerView{{
				ID:        "p-4",
				Name:      "peer-alice-healthy-zagros",
				Groups:    []string{"ZAGROS-DEVELOPER"},
				Connected: true,
				LastSeen:  "2026-09-10T10:00:00Z",
				Verdict:   "not_on_hold",
			}},
		},
		{
			// Mesh unavailable: no record can be attributed, so no
			// peer rows; the section says why.
			Handle:           "alice",
			Site:             "zagros",
			RecordsAvailable: true,
			MeshAvailable:    false,
			MeshError:        "timeout",
		},
		{
			// A release in progress renders as its own state, and a
			// partial handle roster is said out loud.
			Handle:            "alice",
			Site:              "zagros",
			KnownHandlesError: "list handles: store closed",
			RecordsAvailable:  true,
			MeshAvailable:     true,
			Peers: []meshstate.PeerView{{
				ID:            "p-5",
				Name:          "peer-alice-releasing-zagros",
				HasRecord:     true,
				Lane:          "dormancy",
				Phase:         "releasing",
				HoldGroup:     "ZAGROS-PROBATION-DEVELOPER",
				Verdict:       "releasing",
				VerdictDetail: "release in progress; run the release again",
			}},
		},
		{
			Handle:           "alice",
			Site:             "zagros",
			RecordsAvailable: false,
			RecordsError:     "HOLD_RECORDS_PATH is unset",
			MeshAvailable:    true,
			Peers: []meshstate.PeerView{{
				ID:            "p-6",
				Name:          "peer-alice-holdgroup-zagros",
				HoldGroup:     "ZAGROS-PROBATION-DEVELOPER",
				Verdict:       "in_hold_group",
				VerdictDetail: "peer in hold group ZAGROS-PROBATION-DEVELOPER; hold record unavailable, lane unknown",
				State:         meshstate.HoldStateHeld,
			}},
		},
		{
			// Bad lines skipped and counted (ops/lifecycle/README.md): the
			// section names the count and the lines, and still renders.
			Handle:              "alice",
			Site:                "zagros",
			RecordsAvailable:    true,
			RecordsSkipped:      2,
			RecordsSkippedLines: []int{3, 7},
			MeshAvailable:       true,
		},
	}

	for i, av := range cases {
		caseIndex := i
		avCopy := av
		t.Run(funcNameForAccessCase(caseIndex), func(t *testing.T) {
			var buf bytes.Buffer
			pv := &pageVars{
				Title: "User detail",
				IdP:   config.IdP{Key: "tenant", Label: "Tenant"},
				User: &userDetail{
					ID:              "id-1",
					Email:           "user@example.test",
					Name:            "User",
					State:           "active",
					CreatedAt:       time.Now().UTC().Format(time.RFC3339),
					Groups:          []string{"ADMINS"},
					GroupsCSV:       "ADMINS",
					HasSecondFactor: true,
					Access:          avCopy,
				},
			}
			if err := tmpl.ExecuteTemplate(&buf, "layout", pv); err != nil {
				t.Fatalf("ExecuteTemplate: %v", err)
			}

			out := buf.String()
			if avCopy.RecordsSkipped > 0 {
				if !strings.Contains(out, "2 bad hold-record line(s) skipped") || !strings.Contains(out, "line(s) 3, 7") {
					t.Errorf("Access render missing the skipped-line count for case %d", caseIndex)
				}
			} else if strings.Contains(out, "bad hold-record line") {
				t.Errorf("Access render shows a skipped-line badge with nothing skipped, case %d", caseIndex)
			}
			for _, peer := range avCopy.Peers {
				if peer.Verdict == "in_hold_group" {
					// Shared define: an unreadable record file renders "kind
					// unknown" beside "held", never a guessed lane.
					if !strings.Contains(out, ">kind unknown</span>") || !strings.Contains(out, ">held</span>") {
						t.Errorf("Access render missing the kind-unknown/held badges for case %d", caseIndex)
					}
					break
				}
			}
		})
	}
}

func funcNameForAccessCase(i int) string {
	return fmt.Sprintf("case_%d", i)
}

// The onboarding page renders the proposal panel through one if/else-if chain
// per status, and Go templates fail at EXECUTE time: a field the view does not
// carry stops the page at that panel. The Failed panel did exactly that on
// zagros (#1491): it read $p.Note before proposalView had a Note, so every
// Failed candidate's page ended after its heading, with no note and no Retry.
// Render the page for a proposal in every status and require the whole page.
func TestTemplates_onboardingRendersEveryStatus(t *testing.T) {
	const note = "runner writeback: netbird setup-key mint refused (403)"
	statuses := []string{
		onboardstore.ProposalProposed,
		onboardstore.ProposalSponsorApproved,
		onboardstore.ProposalApproved,
		onboardstore.ProposalRejected,
		onboardstore.ProposalOnboarding,
		onboardstore.ProposalOnboarded,
		onboardstore.ProposalFailed,
		onboardstore.ProposalEnrolled,
		onboardstore.ProposalArchived,
		onboardstore.ProposalOffboardRequested,
		onboardstore.ProposalOffboarding,
		onboardstore.ProposalOffboardFailed,
		onboardstore.ProposalOffboarded,
	}
	for _, status := range statuses {
		t.Run(status, func(t *testing.T) {
			out := renderPage(t, "onboarding", &pageVars{
				CSRFToken:     "nonce-123",
				OnboardStatus: status,
				Proposal: &proposalView{
					ID: "prop-smoke-1", Handle: "smoke-handle", Env: "zagros",
					Status: status, Note: note,
				},
			})
			if !strings.Contains(out, "</html>") {
				t.Fatalf("%s: page cut off before </html>", status)
			}
			switch status {
			case onboardstore.ProposalFailed:
				for _, m := range []string{note, `action="/onboarding/prop-smoke-1/retry"`, "Retry"} {
					if !strings.Contains(out, m) {
						t.Errorf("failed panel missing %q", m)
					}
				}
			case onboardstore.ProposalOffboardFailed:
				for _, m := range []string{note, `action="/onboarding/prop-smoke-1/offboard-cut"`} {
					if !strings.Contains(out, m) {
						t.Errorf("offboard-failed panel missing %q", m)
					}
				}
			}
		})
	}
}
