package handlers

import (
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/permissions"
)

func vocabServer() *Server {
	return &Server{Permissions: &permissions.Matrix{Groups: []permissions.Group{
		{Name: "PLATFORM-ADMIN"}, {Name: "ADMINS"}, {Name: "IAM-ADMIN"},
		{Name: "COMPLIANCE"}, {Name: "GRAFANA-READONLY"},
	}}}
}

func TestValidateGroupVocabulary_acceptsMatrixAndDataGrammar(t *testing.T) {
	s := vocabServer()
	ok := [][]string{
		nil,
		{"PLATFORM-ADMIN"},
		{"IAM-ADMIN", "GRAFANA-READONLY"},
		{"data.view.all"},
		{"data.view.org:acme-corp"},
		{"data.view.producer:jane"},
		{"data.write.producer:jane"},
		{"ADMINS", "data.view.all", ""},
	}
	for _, gs := range ok {
		if err := s.validateGroupVocabulary(gs); err != nil {
			t.Errorf("validateGroupVocabulary(%v) = %v, want nil", gs, err)
		}
	}
}

func TestValidateGroupVocabulary_meshRoleGetsTrapMessage(t *testing.T) {
	s := vocabServer()
	// The exact mistake from the org117x batch.
	err := s.validateGroupVocabulary([]string{"DEVELOPER"})
	if err == nil {
		t.Fatal("DEVELOPER must be refused on the tenant realm")
	}
	for _, m := range []string{"mesh role", "onboarding", "PLATFORM-ADMIN"} {
		if !strings.Contains(err.Error(), m) {
			t.Errorf("mesh-trap message missing %q: %s", m, err)
		}
	}
	if err := s.validateGroupVocabulary([]string{"user"}); err == nil {
		t.Error("mesh-role match must be case-insensitive (user → USER)")
	}
}

func TestValidateGroupVocabulary_unknownListsValidSet(t *testing.T) {
	s := vocabServer()
	err := s.validateGroupVocabulary([]string{"zagros"})
	if err == nil {
		t.Fatal("unknown group must be refused")
	}
	for _, m := range []string{"zagros", "Valid login groups", "data.view.all"} {
		if !strings.Contains(err.Error(), m) {
			t.Errorf("unknown-group message missing %q: %s", m, err)
		}
	}
}

func TestValidateGroupVocabulary_nilMatrixStillTrapsMeshAndGrammar(t *testing.T) {
	s := &Server{}
	if err := s.validateGroupVocabulary([]string{"DEVELOPER"}); err == nil {
		t.Error("mesh trap must hold without a matrix")
	}
	if err := s.validateGroupVocabulary([]string{"data.view.org:acme"}); err != nil {
		t.Errorf("data grammar must pass without a matrix: %v", err)
	}
	// Without a matrix the valid set is unknowable — unknown uppercase is refused
	// with the pointer message rather than silently accepted.
	if err := s.validateGroupVocabulary([]string{"SOMETHING-ELSE"}); err == nil {
		t.Error("unknown group must be refused even without a matrix")
	}
}

// The login-invite mint runs the same fence — the live mistake was
// pasting invitee EMAIL ADDRESSES into preset_groups.
func TestValidateGroupVocabulary_emailShapedValuesRefused(t *testing.T) {
	s := vocabServer()
	err := s.validateGroupVocabulary([]string{"pat.example.ctr@partner.example"})
	if err == nil {
		t.Fatal("email-shaped preset group must be refused")
	}
	if !strings.Contains(err.Error(), "Unknown group") {
		t.Errorf("email-shaped value should report as unknown group: %s", err)
	}
}
