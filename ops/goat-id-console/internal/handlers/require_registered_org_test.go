package handlers

import (
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/config"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/orgstore"
)

// ADR 1171 decision 7. This gate refuses to approve a person carrying an
// organisation the registry does not hold, and ships OFF because arming it
// against a registry that is not yet populated refuses every onboarding.

func mfWithOrg(id string) *onboardstore.Proposal {
	return &onboardstore.Proposal{ManifestJSON: `{"affiliation":{"id":"` + id + `"}}`}
}

func TestRequireRegisteredOrgIsOffByDefault(t *testing.T) {
	s := newOnboardTestServer(t)
	withOrgs(t, s, true) // registry present but the flag is not set
	if s.Cfg.RequireRegisteredOrg {
		t.Fatal("the flag must default false — armed fleet-wide it refuses every onboarding on a goatnet whose registry is not yet populated")
	}
	if e := s.requireRegisteredOrg(mfWithOrg("nowhere-org")); e != nil {
		t.Errorf("with the flag off, an unregistered org must pass: %q", e.msg)
	}
}

func TestRequireRegisteredOrgRefusesAnUnregisteredOrgWhenArmed(t *testing.T) {
	s := newOnboardTestServer(t)
	orgs := withOrgs(t, s, true)
	s.Cfg.RequireRegisteredOrg = true

	e := s.requireRegisteredOrg(mfWithOrg("demo-org-odt"))
	if e == nil {
		t.Fatal("an unregistered org must be refused when the goatnet has armed the gate")
	}
	// The refusal has to name the way out: the admin is one click from fixing it.
	if !strings.Contains(e.msg, "create action") {
		t.Errorf("refusal should point at the create action, got %q", e.msg)
	}
	// And it must say the facts are the admin's, not the candidate's (ADR 1171).
	if !strings.Contains(e.msg, "typed by you") {
		t.Errorf("refusal should say whose facts these are, got %q", e.msg)
	}

	// Register it, and the same proposal passes.
	if _, err := orgs.CreateOrg(orgstore.Org{ID: "demo-org-odt", Provenance: "operator-verified"}); err != nil {
		t.Fatal(err)
	}
	if e := s.requireRegisteredOrg(mfWithOrg("demo-org-odt")); e != nil {
		t.Errorf("a registered org must pass: %q", e.msg)
	}
}

func TestRequireRegisteredOrgRefusesAProposalNamingNoOrg(t *testing.T) {
	s := newOnboardTestServer(t)
	withOrgs(t, s, true)
	s.Cfg.RequireRegisteredOrg = true
	p := &onboardstore.Proposal{ManifestJSON: `{"affiliation":{}}`}
	if e := s.requireRegisteredOrg(p); e == nil {
		t.Error("no organisation at all must refuse too — an armed goatnet requires one")
	}
}

func TestRequireRegisteredOrgIsInertWithoutARegistry(t *testing.T) {
	// A goatnet that armed the flag but has no org store must not refuse every
	// approval on a nil dereference; the flag is meaningless without the store.
	s := newOnboardTestServer(t)
	s.Cfg = &config.Config{RequireRegisteredOrg: true}
	s.Orgs = nil
	if e := s.requireRegisteredOrg(mfWithOrg("anything")); e != nil {
		t.Errorf("no registry means the gate cannot apply, got %q", e.msg)
	}
}
