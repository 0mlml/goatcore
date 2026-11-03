package handlers

import (
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/meshstate"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

// #1586: the Onboarded and Enrolled cards list the person's mesh peers with
// their OS, and warn when every peer is a server — that person reads
// "enrolled" but cannot open the mesh-only login link from a workstation.
func TestTemplates_onboardingDevicesBlock(t *testing.T) {
	for _, status := range []string{onboardstore.ProposalOnboarded, onboardstore.ProposalEnrolled} {
		serverOnly := renderPage(t, "onboarding", &pageVars{
			CSRFToken: "nonce", OnboardStatus: status,
			Proposal: &proposalView{ID: "p1", Handle: "mz", Env: "zagros", Status: status,
				Devices: &meshstate.DeviceSummary{
					Peers:      []meshstate.DevicePeer{{Name: "peer-mz-zagros-srv16", OS: "Oracle Linux Server 8.10", Connected: true}},
					ServerOnly: true,
				}},
		})
		for _, m := range []string{"Devices on the mesh", "peer-mz-zagros-srv16", "Oracle Linux Server 8.10", "Only a server is enrolled"} {
			if !strings.Contains(serverOnly, m) {
				t.Errorf("%s card missing %q", status, m)
			}
		}

		withDesktop := renderPage(t, "onboarding", &pageVars{
			CSRFToken: "nonce", OnboardStatus: status,
			Proposal: &proposalView{ID: "p1", Handle: "mz", Env: "zagros", Status: status,
				Devices: &meshstate.DeviceSummary{Peers: []meshstate.DevicePeer{{Name: "peer-mz-zagros-lap", OS: "Microsoft Windows 11 Pro"}}}},
		})
		if strings.Contains(withDesktop, "Only a server is enrolled") {
			t.Errorf("%s card warns although a Windows peer is enrolled", status)
		}

		unavailable := renderPage(t, "onboarding", &pageVars{
			CSRFToken: "nonce", OnboardStatus: status,
			Proposal: &proposalView{ID: "p1", Handle: "mz", Env: "zagros", Status: status, DevicesErr: "mesh device list unavailable: token unset"},
		})
		if !strings.Contains(unavailable, "mesh device list unavailable") {
			t.Errorf("%s card does not say why devices are unknown", status)
		}
	}
}
