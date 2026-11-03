package handlers

import (
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

func TestRecipientFromProposal_developerIsPublisherNotAdmin(t *testing.T) {
	p := &onboardstore.Proposal{
		Handle: "user8x", Email: "user8x@example.org", Tier: "developer",
		ManifestJSON: `{"identity":{"name":"Joshua Belke"},"netbird":{"auto_groups":["DEVELOPER","OBSERVER"]},"zenoh":{"enabled":true}}`,
	}
	r := recipientFromProposal(p)
	if r.Name != "Joshua Belke" {
		t.Errorf("name = %q", r.Name)
	}
	if !r.FabricAccess {
		t.Error("zenoh-enabled manifest should grant fabric access")
	}
	if !r.Publisher {
		t.Error("developer with zenoh enabled + DEVELOPER group should be a publisher")
	}
	if r.AdminConsole {
		t.Error("developer must not get the admin console")
	}
	if len(r.Groups) != 2 {
		t.Errorf("groups = %v", r.Groups)
	}
}

func TestRecipientFromProposal_observerNotPublisher(t *testing.T) {
	p := &onboardstore.Proposal{
		Handle: "obs", Tier: "browse",
		ManifestJSON: `{"identity":{"name":"Obs"},"netbird":{"auto_groups":["OBSERVER"]},"zenoh":{"enabled":true}}`,
	}
	r := recipientFromProposal(p)
	if r.Publisher {
		t.Error("a pure OBSERVER is not a publisher even with a zenoh slot")
	}
	if !r.FabricAccess {
		t.Error("an OBSERVER with a zenoh slot still has fabric access (read/subscribe)")
	}
}

func TestRecipientFromProposal_adminTierGetsConsole(t *testing.T) {
	for _, tier := range []string{"iam-admin", "platform-admin"} {
		p := &onboardstore.Proposal{Handle: "a", Tier: tier, ManifestJSON: `{}`}
		if !recipientFromProposal(p).AdminConsole {
			t.Errorf("tier %q should grant the admin console", tier)
		}
	}
}
