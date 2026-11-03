package welcome_test

import (
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/welcome"
)

// The escalated admin tiers grant the admin console; nothing else does. This is
// the rule the console's recipientFromProposal and tools/reissue-preview now
// BOTH read, so it is locked here rather than in either caller.
func TestTierGrantsAdminConsole(t *testing.T) {
	for _, tc := range []struct {
		tier string
		want bool
	}{
		{"platform-admin", true},
		{"iam-admin", true},
		{"PLATFORM-ADMIN", true}, // case-insensitive: operators type it by hand
		{" platform-admin ", true},
		{"developer", false},
		{"browse", false},
		{"", false},
		{"admin", false}, // not a tier in the vocabulary; must not pass
	} {
		if got := welcome.TierGrantsAdminConsole(tc.tier); got != tc.want {
			t.Errorf("TierGrantsAdminConsole(%q) = %v, want %v", tc.tier, got, tc.want)
		}
	}
}

// A platform-admin's package must NOT carry the "admin surfaces are not part of
// your tier" line, and MUST carry the operator-key self-service link — the two
// halves that were wrong when the tool could not express AdminConsole.
func TestAdminTierPackageOffersOperatorKey(t *testing.T) {
	cat := welcome.DeriveCatalog("zagros-prod", "https://mgmt.example",
		"https://get.example", "https://portal.example")
	rcpt := welcome.Recipient{
		Name: "A", Handle: "a", Email: "a@example.test", Tier: "platform-admin",
		Groups: []string{"PLATFORM-ADMIN", "DEVELOPER"},
	}
	rcpt.AdminConsole = welcome.TierGrantsAdminConsole(rcpt.Tier)
	rcpt.AdminOperator = rcpt.AdminConsole

	body := welcome.Render(cat, rcpt,
		welcome.Artifacts{SetupKey: "K", RecoveryLink: "https://r.example", RecoveryCode: "1"})

	if strings.Contains(body, "are not part of") {
		t.Error("admin package disclaims admin surfaces the tier actually grants")
	}
	if !strings.Contains(body, "/operator-key") {
		t.Error("admin package omits the operator-key self-service link")
	}
}

// A non-admin tier keeps the disclaimer and gets no operator-key link.
func TestNonAdminTierKeepsDisclaimer(t *testing.T) {
	cat := welcome.DeriveCatalog("zagros-prod", "https://mgmt.example",
		"https://get.example", "https://portal.example")
	rcpt := welcome.Recipient{
		Name: "B", Handle: "b", Email: "b@example.test", Tier: "developer",
		Groups: []string{"DEVELOPER"},
	}
	rcpt.AdminConsole = welcome.TierGrantsAdminConsole(rcpt.Tier)
	rcpt.AdminOperator = rcpt.AdminConsole

	body := welcome.Render(cat, rcpt,
		welcome.Artifacts{SetupKey: "K", RecoveryLink: "https://r.example", RecoveryCode: "1"})

	if strings.Contains(body, "/operator-key") {
		t.Error("non-admin package offers an operator key")
	}
}

// The two subjects are distinct and neither announces a correction for a
// first-time package.
func TestSubjects(t *testing.T) {
	g := "ZAGROS-PROD (production data fabric)"
	if welcome.SubjectFresh(g) == welcome.SubjectReissue(g) {
		t.Fatal("fresh and reissue subjects are identical")
	}
	if strings.Contains(welcome.SubjectFresh(g), "Corrected") {
		t.Error("a first-time package announces itself as a correction")
	}
	if !strings.Contains(welcome.SubjectReissue(g), "replaces the earlier email") {
		t.Error("reissue subject lost its replacement notice")
	}
}
