package handlers

import (
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/kratos"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

func identityWith(email, name string, groups []string) *kratos.Identity {
	gs := make([]interface{}, 0, len(groups))
	for _, g := range groups {
		gs = append(gs, g)
	}
	return &kratos.Identity{ID: "id-1", Traits: map[string]interface{}{
		"email": email, "name": name, "groups": gs,
	}}
}

func TestGroupsGrantAdminConsole(t *testing.T) {
	cases := []struct {
		groups []string
		want   bool
	}{
		{[]string{"PLATFORM-ADMIN"}, true},
		{[]string{"ADMINS"}, true}, // legacy alias still admits
		{[]string{"iam-admin"}, true},
		{[]string{"GRAFANA-READONLY", "data.view.all"}, false},
		{nil, false},
	}
	for _, c := range cases {
		if got := groupsGrantAdminConsole(c.groups); got != c.want {
			t.Errorf("groupsGrantAdminConsole(%v) = %v, want %v", c.groups, got, c.want)
		}
	}
}

func TestTierForGroups(t *testing.T) {
	cases := []struct {
		groups    []string
		onboarded string
		want      string
	}{
		{[]string{"PLATFORM-ADMIN"}, "developer", "platform-admin"},
		{[]string{"ADMINS"}, "developer", "platform-admin"},
		{[]string{"IAM-ADMIN"}, "browse", "iam-admin"},
		// admin group removed from an escalated tier → don't keep the label
		{[]string{"GRAFANA-READONLY"}, "platform-admin", "participant"},
		// non-escalated onboarded tier stands
		{[]string{"data.view.all"}, "developer", "developer"},
	}
	for _, c := range cases {
		if got := tierForGroups(c.groups, c.onboarded); got != c.want {
			t.Errorf("tierForGroups(%v, %q) = %q, want %q", c.groups, c.onboarded, got, c.want)
		}
	}
}

func TestAccessChangeRecipient_noProposalDegradesGracefully(t *testing.T) {
	id := identityWith("m@example.org", "Morgan", []string{"GRAFANA-READONLY"})
	r := accessChangeRecipient(id, []string{"GRAFANA-READONLY", "PLATFORM-ADMIN"}, nil)
	if r.Name != "Morgan" || r.Email != "m@example.org" {
		t.Errorf("recipient identity fields wrong: %+v", r)
	}
	if !r.AdminConsole || !r.AdminOperator {
		t.Errorf("escalation to PLATFORM-ADMIN must grant admin sections: %+v", r)
	}
	if r.Tier != "platform-admin" {
		t.Errorf("tier = %q, want platform-admin", r.Tier)
	}
	// No proposal → never fabricate fabric access.
	if r.FabricAccess || r.Publisher {
		t.Errorf("no proposal must mean no fabricated fabric access: %+v", r)
	}
}

func TestAccessChangeRecipient_proposalEnrichesButGroupsDecideConsole(t *testing.T) {
	id := identityWith("j@example.org", "", []string{"PLATFORM-ADMIN"})
	prop := &onboardstore.Proposal{
		Handle: "jane", Email: "j@example.org", Tier: "developer", Status: onboardstore.ProposalOnboarded,
		ManifestJSON: `{"identity":{"name":"Jane D"},"netbird":{"auto_groups":["DEVELOPER","OBSERVER"]},"zenoh":{"enabled":true}}`,
	}
	// Reduction: PLATFORM-ADMIN removed → console access gone, tier falls back.
	r := accessChangeRecipient(id, []string{"GRAFANA-READONLY"}, prop)
	if r.AdminConsole || r.AdminOperator {
		t.Errorf("reduction must drop admin sections: %+v", r)
	}
	if r.Tier != "developer" {
		t.Errorf("tier = %q, want developer (onboarded tier stands)", r.Tier)
	}
	if !r.FabricAccess || !r.Publisher {
		t.Errorf("proposal-derived fabric capabilities must survive: %+v", r)
	}
	if r.Handle != "jane" || r.Name != "Jane D" {
		t.Errorf("proposal enrichment missing: %+v", r)
	}
}

// sendAccessChangeEmail must be a safe no-op with no mailer configured — the
// patch path calls it unconditionally.
func TestSendAccessChangeEmail_noMailerIsNoop(t *testing.T) {
	s := &Server{}
	s.sendAccessChangeEmail(nil, tenantIdPKey, identityWith("a@b.c", "A", nil), []string{"PLATFORM-ADMIN"})
}
