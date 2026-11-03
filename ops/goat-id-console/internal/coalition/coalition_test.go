package coalition

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// canonicalRegistry is the repo's real registry, four levels up from this
// package. The parity test reads it rather than a fixture on purpose: a
// hand-written fixture can agree with this code forever while the real file
// drifts.
func canonicalRegistry(t *testing.T) *Registry {
	t.Helper()
	path := filepath.Join("..", "..", "..", "identity", "coalition-frameworks.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("canonical registry not reachable from the test tree: %v", err)
	}
	reg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(%s): %v", path, err)
	}
	return reg
}

func day(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse(DateFormat, s)
	if err != nil {
		t.Fatalf("bad test date %q: %v", s, err)
	}
	return d
}

// TestRuleParityWithCanonicalRegistry pins the resolution rule against the real
// registry. Every case here is a fact stated in coalition-frameworks.yaml, so
// this test fails closed both on a rule regression here and on an unreviewed
// edit to the registry's membership dates.
func TestRuleParityWithCanonicalRegistry(t *testing.T) {
	reg := canonicalRegistry(t)

	cases := []struct {
		name   string
		nation string
		asOf   string
		want   string
	}{
		// The case that prompted this surface: a partner-nation org whose
		// nation is in no framework member list.
		{"UKR is not a NATO member", "UKR", "2026-08-17", StandingPartner},
		// Founders.
		{"USA member", "USA", "2026-08-17", StandingMember},
		// Effective-dated accession, both sides of the date.
		{"FIN member after accession", "FIN", "2026-08-17", StandingMember},
		{"FIN partner the day before accession", "FIN", "2023-04-03", StandingPartner},
		{"SWE partner in 2023", "SWE", "2023-06-01", StandingPartner},
		{"SWE member after 2024-03-07", "SWE", "2024-03-07", StandingMember},
		// Alliance bodies are members by definition, not by nation row.
		{"NATO marker is a member", "NATO", "2026-08-17", StandingMember},
		// Case + whitespace folding.
		{"lowercase nation folds", "ukr", "2026-08-17", StandingPartner},
		{"padded nation folds", "  usa  ", "2026-08-17", StandingMember},
		// The fail-to-partner defaults.
		{"unknown nation defaults to partner", "ZZZ", "2026-08-17", StandingPartner},
		{"empty nation defaults to partner", "", "2026-08-17", StandingPartner},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := reg.Resolve(c.nation, "", day(t, c.asOf))
			if got.Standing != c.want {
				t.Errorf("Resolve(%q, default, %s).Standing = %q, want %q (basis: %s)",
					c.nation, c.asOf, got.Standing, c.want, got.Basis)
			}
			if got.Basis == "" {
				t.Errorf("Resolve(%q) returned no basis — the page would render a bare standing", c.nation)
			}
		})
	}
}

// TestResolveCarriesFrameworkContext checks the fields the page renders beside
// the standing, so an operator can see WHICH framework the answer is relative
// to and which lane membership would unlock.
func TestResolveCarriesFrameworkContext(t *testing.T) {
	reg := canonicalRegistry(t)
	got := reg.Resolve("UKR", "", day(t, "2026-08-17"))

	if got.Framework == "" {
		t.Error("Framework is empty — the standing would be unattributable")
	}
	if got.FrameworkName == "" {
		t.Error("FrameworkName is empty — the page has nothing human to show")
	}
	if got.AsOf.IsZero() {
		t.Error("AsOf is zero — a derived standing must carry the date it was derived for")
	}
}

// TestMembershipRangeEndDate covers the withdrawal half of the effective-dated
// range. No nation in the live registry has left a framework, so this uses a
// synthetic registry — the rule must still be right the day it happens.
func TestMembershipRangeEndDate(t *testing.T) {
	reg := &Registry{
		DefaultFramework: "testfw",
		Frameworks: map[string]Framework{
			"testfw": {
				DisplayName: "Test Framework",
				Members: []Member{
					{Nation: "AAA", Name: "Aaa", MemberSince: "2000-01-01", MemberUntil: "2026-01-01"},
				},
			},
		},
	}

	if got := reg.Resolve("AAA", "", day(t, "2025-12-31")); got.Standing != StandingMember {
		t.Errorf("day before member_until: got %q, want member (basis: %s)", got.Standing, got.Basis)
	}
	// member_until is exclusive — the nation is a partner ON that date.
	if got := reg.Resolve("AAA", "", day(t, "2026-01-01")); got.Standing != StandingPartner {
		t.Errorf("on member_until: got %q, want partner (basis: %s)", got.Standing, got.Basis)
	}
	if got := reg.Resolve("AAA", "", day(t, "2026-08-17")); got.Standing != StandingPartner {
		t.Errorf("after member_until: got %q, want partner (basis: %s)", got.Standing, got.Basis)
	}
}

func TestUnknownFrameworkFailsToPartner(t *testing.T) {
	reg := canonicalRegistry(t)
	got := reg.Resolve("USA", "no-such-framework", day(t, "2026-08-17"))
	if got.Standing != StandingPartner {
		t.Errorf("unknown framework: got %q, want partner", got.Standing)
	}
	if got.Basis == "" {
		t.Error("unknown framework produced no basis")
	}
}

func TestLoadRejectsMissingAndEmpty(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "absent.yaml")); err == nil {
		t.Error("Load on a missing file must error, not return an empty registry")
	}

	empty := filepath.Join(t.TempDir(), "empty.yaml")
	if err := os.WriteFile(empty, []byte("default_framework: nato\nframeworks: {}\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Load(empty); err == nil {
		t.Error("Load on a frameworkless registry must error — silent 'nobody is a member' is the failure mode")
	}
}
