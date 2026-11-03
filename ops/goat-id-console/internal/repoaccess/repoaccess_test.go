package repoaccess

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func cfg() *Config {
	return &Config{
		Version:      1,
		DefaultOwner: "usareur-af",
		Rules: []Rule{
			{
				Name:  "developers-moon-pod",
				Match: Match{Tier: []string{"developer", "platform-admin"}},
				Grant: []Grant{{Repo: "zagros-moon-pod", Permission: "pull"}},
			},
			{
				Name:  "developer-examples",
				Match: Match{Tier: []string{"developer"}},
				Grant: []Grant{{Repo: "hello-galaxy-access-mgmt", Permission: "pull"}},
			},
			{
				Name:  "platform-admin-infra",
				Match: Match{Tier: []string{"platform-admin"}},
				Grant: []Grant{{Repo: "usareur-af-infra", Permission: "push"}},
			},
			{
				// Stronger grant on a repo another rule also grants weakly — dedupe
				// must keep the strongest (admin > pull).
				Name:  "moon-pod-maintainers",
				Match: Match{Tier: []string{"platform-admin"}},
				Grant: []Grant{{Repo: "zagros-moon-pod", Permission: "admin"}},
			},
			{
				Name:  "coalition-partner-shared",
				Match: Match{Tier: []string{"developer"}, AffiliationKind: []string{"partner"}, Coalition: []string{"member"}},
				Grant: []Grant{{Repo: "coalition/coalition-shared", Permission: "pull"}},
			},
			{
				Name:  "law-enforcement-tools",
				Match: Match{Tier: []string{"developer"}, Sector: []string{"law-enforcement"}},
				Grant: []Grant{{Repo: "le-tools", Permission: "pull"}},
			},
		},
	}
}

func TestGrants_noGitHubUsername_empty(t *testing.T) {
	got := cfg().Grants(Subject{Tier: "developer"})
	if len(got) != 0 {
		t.Fatalf("expected no grants without a github username, got %v", got)
	}
}

func TestGrants_tierMatchSingle(t *testing.T) {
	got := cfg().Grants(Subject{Tier: "developer", GitHubUsername: "octocat"})
	want := []ResolvedGrant{
		{Owner: "usareur-af", Repo: "zagros-moon-pod", Permission: "pull"},
		{Owner: "usareur-af", Repo: "hello-galaxy-access-mgmt", Permission: "pull"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("developer grants:\n got %v\nwant %v", got, want)
	}
}

func TestGrants_multiRuleUnionAndDedupeStrongest(t *testing.T) {
	// platform-admin matches developers-moon-pod (pull), platform-admin-infra
	// (push), and moon-pod-maintainers (admin). zagros-moon-pod must dedupe to
	// admin (the strongest), NOT pull.
	got := cfg().Grants(Subject{Tier: "platform-admin", GitHubUsername: "octocat"})
	want := []ResolvedGrant{
		{Owner: "usareur-af", Repo: "zagros-moon-pod", Permission: "admin"},
		{Owner: "usareur-af", Repo: "usareur-af-infra", Permission: "push"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("platform-admin grants:\n got %v\nwant %v", got, want)
	}
}

func TestGrants_affiliationKindAndCoalitionAND(t *testing.T) {
	// A developer partner who is a member of the active framework gets the
	// coalition repo on top.
	got := cfg().Grants(Subject{
		Tier: "developer", AffiliationKind: "partner", Coalition: "member", GitHubUsername: "octocat",
	})
	want := []ResolvedGrant{
		{Owner: "usareur-af", Repo: "zagros-moon-pod", Permission: "pull"},
		{Owner: "usareur-af", Repo: "hello-galaxy-access-mgmt", Permission: "pull"},
		{Owner: "coalition", Repo: "coalition-shared", Permission: "pull"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("member partner developer grants:\n got %v\nwant %v", got, want)
	}

	// A partner developer who is NOT a member (partner standing) must NOT get
	// the coalition repo (coalition is an AND key).
	got = cfg().Grants(Subject{
		Tier: "developer", AffiliationKind: "partner", Coalition: "partner", GitHubUsername: "octocat",
	})
	for _, g := range got {
		if g.Repo == "coalition-shared" {
			t.Fatalf("non-member partner must not receive the coalition repo, got %v", got)
		}
	}
}

func TestGrants_sectorScopedMatch(t *testing.T) {
	// A law-enforcement developer gets the le-tools repo (sector AND tier).
	got := cfg().Grants(Subject{
		Tier: "developer", Sector: "law-enforcement", GitHubUsername: "octocat",
	})
	var haveLE bool
	for _, g := range got {
		if g.Repo == "le-tools" {
			haveLE = true
		}
	}
	if !haveLE {
		t.Fatalf("law-enforcement developer should receive le-tools, got %v", got)
	}

	// A military-sector developer must NOT get the le-tools repo (sector is an
	// AND key that must match).
	got = cfg().Grants(Subject{
		Tier: "developer", Sector: "military", GitHubUsername: "octocat",
	})
	for _, g := range got {
		if g.Repo == "le-tools" {
			t.Fatalf("military-sector developer must not receive le-tools, got %v", got)
		}
	}
}

func TestGrants_ownerDefaulting(t *testing.T) {
	c := &Config{
		DefaultOwner: "usareur-af",
		Rules: []Rule{{
			Match: Match{},
			Grant: []Grant{
				{Repo: "bare-repo", Permission: "pull"},          // owner defaults
				{Repo: "OtherOrg/qualified", Permission: "push"}, // owner explicit
			},
		}},
	}
	got := c.Grants(Subject{Tier: "browse", GitHubUsername: "octocat"})
	want := []ResolvedGrant{
		{Owner: "usareur-af", Repo: "bare-repo", Permission: "pull"},
		{Owner: "OtherOrg", Repo: "qualified", Permission: "push"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("owner defaulting:\n got %v\nwant %v", got, want)
	}
}

func TestLoad_missingFileReturnsNilNil(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("missing file should not error, got %v", err)
	}
	if c != nil {
		t.Fatalf("missing file should return nil config, got %+v", c)
	}
}

func TestLoad_parsesPolicy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "repo-access.yaml")
	yaml := `version: 1
default_owner: usareur-af
rules:
  - name: developers-moon-pod
    match:
      tier: [developer, platform-admin]
    grant:
      - repo: zagros-moon-pod
        permission: pull
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c == nil || c.DefaultOwner != "usareur-af" || len(c.Rules) != 1 {
		t.Fatalf("unexpected parsed config: %+v", c)
	}
	got := c.Grants(Subject{Tier: "developer", GitHubUsername: "octocat"})
	want := []ResolvedGrant{{Owner: "usareur-af", Repo: "zagros-moon-pod", Permission: "pull"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("grants from loaded config:\n got %v\nwant %v", got, want)
	}
}

func TestLoad_malformedYAMLErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte("version: 1\nrules: [ this: is: not valid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected an error for malformed YAML")
	}
}

// --- audibles / Effective ----------------------------------------------------

// findEff returns the effective grant for owner/repo (case-insensitive), or nil.
func findEff(gs []EffectiveGrant, owner, repo string) *EffectiveGrant {
	for i := range gs {
		if strings.EqualFold(gs[i].Owner, owner) && strings.EqualFold(gs[i].Repo, repo) {
			return &gs[i]
		}
	}
	return nil
}

func TestEffective_policyOnly(t *testing.T) {
	c := cfg()
	s := Subject{Tier: "developer", GitHubUsername: "octocat"}
	got := c.Effective(s, Overrides{})
	// Every effective row must be present with Source=policy and no audibles.
	if g := findEff(got, "usareur-af", "zagros-moon-pod"); g == nil || g.Source != SourcePolicy {
		t.Fatalf("policy grant missing/wrong source: %+v", got)
	}
	for _, g := range got {
		if g.Source != SourcePolicy {
			t.Fatalf("unexpected non-policy source with empty overrides: %+v", g)
		}
	}
}

func TestEffective_addNew(t *testing.T) {
	c := cfg()
	s := Subject{Tier: "developer", GitHubUsername: "octocat"}
	ov := Overrides{Add: []Grant{{Repo: "usareur-af/extra-repo", Permission: "push"}}}
	got := c.Effective(s, ov)
	g := findEff(got, "usareur-af", "extra-repo")
	if g == nil {
		t.Fatalf("added repo missing: %+v", got)
	}
	if g.Permission != "push" || g.Source != SourceAudible {
		t.Fatalf("added repo wrong perm/source: %+v", g)
	}
}

func TestEffective_addRaisesPermission(t *testing.T) {
	c := cfg()
	// developer gets zagros-moon-pod at pull from policy; audible raises to push.
	s := Subject{Tier: "developer", GitHubUsername: "octocat"}
	ov := Overrides{Add: []Grant{{Repo: "zagros-moon-pod", Permission: "push"}}}
	got := c.Effective(s, ov)
	g := findEff(got, "usareur-af", "zagros-moon-pod")
	if g == nil || g.Permission != "push" {
		t.Fatalf("raise did not take: %+v", g)
	}
	if g.Source != SourceAudible {
		t.Fatalf("a raised permission should be tagged audible: %+v", g)
	}
	// A weaker add must NOT lower the permission, and must stay policy-sourced.
	ovWeak := Overrides{Add: []Grant{{Repo: "usareur-af-infra", Permission: "pull"}}}
	sp := Subject{Tier: "platform-admin", GitHubUsername: "octocat"}
	gotW := c.Effective(sp, ovWeak)
	gw := findEff(gotW, "usareur-af", "usareur-af-infra")
	if gw == nil || gw.Permission != "push" || gw.Source != SourcePolicy {
		t.Fatalf("a weaker add wrongly changed the row: %+v", gw)
	}
}

func TestEffective_removeBeatsPolicy(t *testing.T) {
	c := cfg()
	s := Subject{Tier: "developer", GitHubUsername: "octocat"}
	ov := Overrides{Remove: []RepoRef{{Repo: "zagros-moon-pod"}}}
	got := c.Effective(s, ov)
	if g := findEff(got, "usareur-af", "zagros-moon-pod"); g != nil {
		t.Fatalf("remove should drop the policy grant: %+v", got)
	}
}

func TestEffective_removeBeatsAdd(t *testing.T) {
	c := cfg()
	s := Subject{Tier: "developer", GitHubUsername: "octocat"}
	ov := Overrides{
		Add:    []Grant{{Repo: "usareur-af/extra-repo", Permission: "push"}},
		Remove: []RepoRef{{Repo: "usareur-af/extra-repo"}},
	}
	got := c.Effective(s, ov)
	if g := findEff(got, "usareur-af", "extra-repo"); g != nil {
		t.Fatalf("remove should win over add: %+v", got)
	}
}

func TestEffective_nilConfigAddOnly(t *testing.T) {
	var c *Config // no policy file
	s := Subject{Tier: "developer", GitHubUsername: "octocat"}
	ov := Overrides{Add: []Grant{{Repo: "usareur-af/solo-repo", Permission: "maintain"}}}
	got := c.Effective(s, ov)
	if len(got) != 1 {
		t.Fatalf("nil config should yield only the audible adds, got %+v", got)
	}
	g := got[0]
	if g.Owner != "usareur-af" || g.Repo != "solo-repo" || g.Permission != "maintain" || g.Source != SourceAudible {
		t.Fatalf("nil-config add wrong: %+v", g)
	}
}

func TestParseOverrides(t *testing.T) {
	// "" and "{}" → empty, no error.
	for _, s := range []string{"", "{}", "  "} {
		ov, err := ParseOverrides(s)
		if err != nil {
			t.Fatalf("ParseOverrides(%q) errored: %v", s, err)
		}
		if len(ov.Add) != 0 || len(ov.Remove) != 0 {
			t.Fatalf("ParseOverrides(%q) not empty: %+v", s, ov)
		}
	}
	// A well-formed object parses.
	ov, err := ParseOverrides(`{"add":[{"repo":"a/b","permission":"push"}],"remove":[{"repo":"c/d"}]}`)
	if err != nil {
		t.Fatalf("valid parse errored: %v", err)
	}
	if len(ov.Add) != 1 || ov.Add[0].Repo != "a/b" || len(ov.Remove) != 1 || ov.Remove[0].Repo != "c/d" {
		t.Fatalf("valid parse wrong: %+v", ov)
	}
	// Malformed JSON is an error.
	if _, err := ParseOverrides(`{"add": [ this is not json`); err == nil {
		t.Fatal("expected an error for malformed override JSON")
	}
}
