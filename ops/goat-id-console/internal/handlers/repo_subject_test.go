package handlers

import (
	"path/filepath"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/coalition"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/config"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

func regOrSkip(t *testing.T) *coalition.Registry {
	t.Helper()
	reg, err := coalition.Load(filepath.Join("..", "..", "..", "identity", "coalition-frameworks.yaml"))
	if err != nil {
		t.Skipf("canonical registry unavailable: %v", err)
	}
	return reg
}

func propWith(aff string) *onboardstore.Proposal {
	return &onboardstore.Proposal{
		ID: "p1", Tier: "developer",
		ManifestJSON: `{"identity":{"github":"someone"},"affiliation":` + aff + `}`,
	}
}

// The case that prompted the derivation: org4x is kind=vendor, nation
// UKR, and carries NO coalition — like 253 of 276 live proposals. A policy rule
// keyed on coalition must still reach them.
func TestRepoSubject_derivesCoalitionFromNation(t *testing.T) {
	s, ok := repoSubjectWith(propWith(`{"id":"demoforge","kind":"vendor","nation":"UKR"}`), regOrSkip(t), "")
	if !ok {
		t.Fatal("subject should resolve")
	}
	if s.Coalition != "partner" {
		t.Errorf("Coalition = %q, want partner derived from UKR", s.Coalition)
	}
	if s.AffiliationKind != "vendor" {
		t.Errorf("kind = %q — derivation must not disturb the recorded kind", s.AffiliationKind)
	}
}

func TestRepoSubject_derivesMemberForAFrameworkNation(t *testing.T) {
	s, _ := repoSubjectWith(propWith(`{"id":"org124x","kind":"partner","nation":"SVN"}`), regOrSkip(t), "")
	if s.Coalition != "member" {
		t.Errorf("Coalition = %q, want member derived from SVN", s.Coalition)
	}
}

// An explicit value is a human's statement and must win over a computation.
func TestRepoSubject_explicitCoalitionWins(t *testing.T) {
	s, _ := repoSubjectWith(propWith(`{"id":"x","kind":"partner","nation":"SVN","coalition":"partner"}`), regOrSkip(t), "")
	if s.Coalition != "partner" {
		t.Errorf("Coalition = %q, want the explicit partner, not the derived member", s.Coalition)
	}
}

// No nation, or no registry, must leave it blank rather than guess.
func TestRepoSubject_noNationLeavesCoalitionBlank(t *testing.T) {
	s, _ := repoSubjectWith(propWith(`{"id":"x","kind":"vendor"}`), regOrSkip(t), "")
	if s.Coalition != "" {
		t.Errorf("Coalition = %q, want empty — nothing to derive from", s.Coalition)
	}
	s2, _ := repoSubjectWith(propWith(`{"id":"x","kind":"vendor","nation":"UKR"}`), nil, "")
	if s2.Coalition != "" {
		t.Errorf("Coalition = %q, want empty when no registry is loaded", s2.Coalition)
	}
}

// The preview endpoint and the grant path must answer identically about the
// same person. They did not: the grant derived coalition standing and the
// preview did not, so an operator reading /repo-access saw a set the mint
// would not produce. Pinned because the two are separate call sites and
// nothing else would notice them drifting apart again.
func TestRepoSubject_previewAndGrantAgree(t *testing.T) {
	reg := regOrSkip(t)
	s := &Server{Coalition: reg, Cfg: &config.Config{}}
	p := propWith(`{"id":"demoforge","kind":"vendor","nation":"UKR"}`)

	viaServer, ok1 := s.repoSubjectDerived(p)
	viaDirect, ok2 := repoSubjectWith(p, reg, "")
	if !ok1 || !ok2 {
		t.Fatal("both paths should resolve")
	}
	if viaServer != viaDirect {
		t.Errorf("the two subject paths disagree:\n  server %+v\n  direct %+v", viaServer, viaDirect)
	}
	if viaServer.Coalition != "partner" {
		t.Errorf("Coalition = %q, want partner — the preview must see what the grant sees", viaServer.Coalition)
	}
}
