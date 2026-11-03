package handlers

// The cross-stage candidate index behind the find box on /onboarding (#1306).
//
// The review page lists one stage per tab, so "where is this person" used to
// cost a click through every tab. Instead of a search endpoint, the page now
// renders a hidden, sponsor-scoped index of every candidate across every stage
// on each load, and web/static/onboard-find.js filters it in the browser the
// way the users list filters its table. Nothing here touches HTTP or the
// store: the handler hands in the two all-status pulls it already makes for
// the tab badges and the Invited tab, and gets back rows for the template.
//
// Scoping happens HERE, before a row exists, never in the template or the
// script: a sponsor session's index holds only their own candidates, so view
// source shows them nothing the tabs would not (ADR 1072).

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

// onboardFindRow is one entry of the index. Stage is the ?status= the detail
// pane needs to open the row: "invited" for an invite, the proposal's own
// status otherwise, because handleOnboarding picks the pane by status.
type onboardFindRow struct {
	ID         string
	Stage      string
	StageLabel string
	StateLabel string // invites only: "" while open, else expired | revoked | archived
	Title      string // proposal: manifest identity.name, else handle; invite: candidate email, else note, else id
	Email      string
	Handle     string // proposals only
	Sponsor    string
	Env        string
	Changed    int64  // unix seconds of the last recorded state change; the sort key
	ChangedOn  string // Changed as YYYY-MM-DD, "" when unknown
	Filter     string // the data-filter haystack the script matches against
}

// findIndexMax bounds the index. Trial-scale goatnets hold a few hundred
// rows; past the bound the page says so instead of dropping rows silently.
const findIndexMax = 2000

// buildFindIndex turns the all-status pulls into index rows: sponsor-scoped,
// submitted invites left out (their proposal row represents them), newest
// state change first, ties broken by id so the order is stable.
func buildFindIndex(proposals []onboardstore.Proposal, invites []onboardstore.Invite, sponsorKeys map[string]struct{}) ([]onboardFindRow, bool) {
	rows := make([]onboardFindRow, 0, len(proposals)+len(invites))
	for _, p := range proposals {
		if sponsorKeys != nil && !sponsorAllows(sponsorKeys, p.Sponsor) {
			continue
		}
		rows = append(rows, findRowFromProposal(p))
	}
	for _, inv := range invites {
		if inv.Status == onboardstore.InviteSubmitted {
			continue
		}
		if sponsorKeys != nil && !sponsorAllows(sponsorKeys, inv.Sponsor) {
			continue
		}
		rows = append(rows, findRowFromInvite(inv))
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Changed != rows[j].Changed {
			return rows[i].Changed > rows[j].Changed
		}
		return rows[i].ID < rows[j].ID
	})
	if len(rows) > findIndexMax {
		return rows[:findIndexMax], true
	}
	return rows, false
}

func findRowFromProposal(p onboardstore.Proposal) onboardFindRow {
	name, github := manifestIdentity(p.ManifestJSON)
	changed := proposalChangedAt(p)
	return onboardFindRow{
		ID:         p.ID,
		Stage:      p.Status,
		StageLabel: stageLabel(p.Status),
		Title:      firstNonEmpty(name, p.Handle, p.Email, p.ID),
		Email:      p.Email,
		Handle:     p.Handle,
		Sponsor:    firstNonEmpty(p.SponsorName, p.Sponsor),
		Env:        p.Env,
		Changed:    changed,
		ChangedOn:  dayOf(changed),
		Filter:     findFilter(p.ID, p.Email, name, p.Handle, github),
	}
}

// findRowFromInvite expects the row as ListInvites returns it, with a past-TTL
// 'invited' already folded to expired, so Status is the display state.
func findRowFromInvite(inv onboardstore.Invite) onboardFindRow {
	state := ""
	switch inv.Status {
	case onboardstore.InviteExpired, onboardstore.InviteRevoked, onboardstore.InviteArchived:
		state = inv.Status
	}
	changed := inviteChangedAt(inv)
	return onboardFindRow{
		ID:         inv.ID,
		Stage:      onboardStageInvited,
		StageLabel: stageLabel(onboardStageInvited),
		StateLabel: state,
		Title:      firstNonEmpty(inv.CandidateEmail, inv.Note, inv.ID),
		Email:      inv.CandidateEmail,
		Sponsor:    firstNonEmpty(inv.SponsorName, inv.Sponsor),
		Env:        inv.Env,
		Changed:    changed,
		ChangedOn:  dayOf(changed),
		Filter:     findFilter(inv.ID, inv.CandidateEmail, inv.Note),
	}
}

// stageLabel is the tab wording for a status; an unknown key reads as itself
// rather than vanishing, so a new status still shows up in the index.
func stageLabel(key string) string {
	for _, d := range onboardStageDefs {
		if d.Key == key {
			return d.Label
		}
	}
	return key
}

// manifestIdentity reads the two person fields that live only in the
// manifest. An assembled manifest carries them as identity.name and
// identity.github. A draft (unregistered org, incomplete intake) still holds
// the intake record onboardstore.AssembleIntakeRecord wrote, where the
// candidate's own answers sit under candidate_submission.name and
// candidate_submission.github_username; each field falls back to that on its
// own. A manifest that is empty or unreadable yields neither; the row still
// indexes by handle and email, which are columns, so a broken manifest
// narrows a match but never hides a person.
func manifestIdentity(manifestJSON string) (name, github string) {
	if strings.TrimSpace(manifestJSON) == "" {
		return "", ""
	}
	var m struct {
		Identity struct {
			Name   string `json:"name"`
			GitHub string `json:"github"`
		} `json:"identity"`
		Submission struct {
			Name   string `json:"name"`
			GitHub string `json:"github_username"`
		} `json:"candidate_submission"`
	}
	if err := json.Unmarshal([]byte(manifestJSON), &m); err != nil {
		return "", ""
	}
	name = strings.TrimSpace(firstNonEmpty(m.Identity.Name, m.Submission.Name))
	github = strings.TrimSpace(firstNonEmpty(m.Identity.GitHub, m.Submission.GitHub))
	return name, github
}

// findFilter joins the searchable values into one haystack: trimmed, empties
// dropped, duplicates folded case-insensitively, single spaces between.
func findFilter(parts ...string) string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		v := strings.TrimSpace(p)
		if v == "" {
			continue
		}
		k := strings.ToLower(v)
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, v)
	}
	return strings.Join(out, " ")
}

// proposalChangedAt is the last recorded state change: the decision, else the
// sponsor's vouch, else the submission.
func proposalChangedAt(p onboardstore.Proposal) int64 {
	if p.DecidedAt != nil && *p.DecidedAt > 0 {
		return *p.DecidedAt
	}
	if p.SponsorApprovedAt > 0 {
		return p.SponsorApprovedAt
	}
	return p.CreatedAt
}

// inviteChangedAt is the last recorded state change of an invite: archived,
// else revoked, else the expiry that folded it, else the send, else the mint.
func inviteChangedAt(inv onboardstore.Invite) int64 {
	if inv.ArchivedAt != nil && *inv.ArchivedAt > 0 {
		return *inv.ArchivedAt
	}
	if inv.RevokedAt != nil && *inv.RevokedAt > 0 {
		return *inv.RevokedAt
	}
	if inv.Status == onboardstore.InviteExpired && inv.ExpiresAt > 0 {
		return inv.ExpiresAt
	}
	if inv.SentAt != nil && *inv.SentAt > 0 {
		return *inv.SentAt
	}
	return inv.CreatedAt
}

func dayOf(unix int64) string {
	if unix <= 0 {
		return ""
	}
	return time.Unix(unix, 0).UTC().Format("2006-01-02")
}
