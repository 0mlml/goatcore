package onboardstore

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// SetProposalGitHub is the one post-mint manifest write: only identity.github
// changes, only on an onboarded or enrolled row, and the rest of the manifest
// (integers included) survives byte-for-byte in meaning.
func TestSetProposalGitHub(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)
	manifest := `{"identity":{"handle":"x","email":"x@y"},"netbird":{"ttl_days":7,"usage_limit":5}}`

	mk := func(id, status string) {
		t.Helper()
		p := &Proposal{ID: id, Sponsor: "s", Env: "zagros", Handle: id, Email: id + "@x", Status: ProposalProposed, ManifestJSON: manifest}
		if err := st.CreateProposal(ctx, p); err != nil {
			t.Fatal(err)
		}
		if status != ProposalProposed {
			if err := st.DecideProposal(ctx, id, status, "t", ""); err != nil {
				t.Fatal(err)
			}
		}
	}

	mk("prop-on", ProposalOnboarded)
	before, err := st.SetProposalGitHub(ctx, "prop-on", "octocat")
	if err != nil || before != "" {
		t.Fatalf("first set: before=%q err=%v", before, err)
	}
	got, _ := st.GetProposal(ctx, "prop-on")
	if !strings.Contains(got.ManifestJSON, `"github":"octocat"`) ||
		!strings.Contains(got.ManifestJSON, `"ttl_days":7`) || !strings.Contains(got.ManifestJSON, `"usage_limit":5`) {
		t.Fatalf("manifest after set = %s", got.ManifestJSON)
	}
	before, err = st.SetProposalGitHub(ctx, "prop-on", "")
	if err != nil || before != "octocat" {
		t.Fatalf("clear: before=%q err=%v", before, err)
	}
	got, _ = st.GetProposal(ctx, "prop-on")
	if strings.Contains(got.ManifestJSON, "github") {
		t.Fatalf("clear left the key: %s", got.ManifestJSON)
	}

	mk("prop-en", ProposalEnrolled)
	if _, err := st.SetProposalGitHub(ctx, "prop-en", "late-dev"); err != nil {
		t.Fatalf("enrolled: %v", err)
	}

	for _, status := range []string{ProposalProposed, ProposalApproved, ProposalRejected} {
		id := "prop-" + status
		mk(id, status)
		if _, err := st.SetProposalGitHub(ctx, id, "nope"); !errors.Is(err, ErrGitHubNotEditable) {
			t.Fatalf("%s: err=%v, want ErrGitHubNotEditable", status, err)
		}
	}
	if _, err := st.SetProposalGitHub(ctx, "prop-missing", "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: err=%v, want ErrNotFound", err)
	}
}
