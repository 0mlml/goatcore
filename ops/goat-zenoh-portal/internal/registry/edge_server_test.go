package registry

// ADR 1216 — the EDGE-SERVER node-class.
//
// EDGE-SERVER is a named server that directly supports a site's edge devices.
// It takes NETWORK's shape exactly, including the ADR 1113 delegation: a site
// network admin may enroll one for their own site. The neighbouring classes a
// site admin must NOT be able to enroll — BREAKGLASS and NETWORK-JUMP — stay
// operator-gated, and this test is what fails if the delegable set widens to
// them alongside EDGE-SERVER.

import "testing"

func TestEdgeServerIsInfraDelegableAndMapped(t *testing.T) {
	if !ValidNodeClass("EDGE-SERVER") {
		t.Fatal("ValidNodeClass(EDGE-SERVER) = false, want true")
	}
	if !IsInfraNodeClass("EDGE-SERVER") {
		t.Error("IsInfraNodeClass(EDGE-SERVER) = false — it shapes the site substrate and must stay infra-classified")
	}
	if !IsDelegableInfraNodeClass("EDGE-SERVER") {
		t.Error("IsDelegableInfraNodeClass(EDGE-SERVER) = false — ADR 1216 puts it beside NETWORK under the ADR 1113 entitlement")
	}
	if !IsEnrollableNodeClass("EDGE-SERVER") {
		t.Error("IsEnrollableNodeClass(EDGE-SERVER) = false")
	}
	if IsGearNodeClass("EDGE-SERVER") {
		t.Error("IsGearNodeClass(EDGE-SERVER) = true — gear classes are rule-A fabric sources (ADR 1116); EDGE-SERVER holds no fabric ports")
	}
	got, err := MeshGroupsForNodeClass("EDGE-SERVER")
	if err != nil || len(got) != 1 || got[0] != "EDGE-SERVER" {
		t.Errorf("MeshGroupsForNodeClass(EDGE-SERVER) = %v, %v; want [EDGE-SERVER]", got, err)
	}
	mg, err := MeshGroupsForMachine("EDGE-SERVER", nil)
	if err != nil || len(mg) != 1 || mg[0] != "EDGE-SERVER" {
		t.Errorf("MeshGroupsForMachine(EDGE-SERVER) = %v, %v; want [EDGE-SERVER]", mg, err)
	}
}

func TestDelegationStopsAtNetworkAndEdgeServer(t *testing.T) {
	for _, nc := range []string{"BREAKGLASS", "NETWORK-JUMP", "MGMT-HOST", "ZENOH", "SNITCH", "SERVICES"} {
		if IsDelegableInfraNodeClass(nc) {
			t.Errorf("IsDelegableInfraNodeClass(%q) = true — ADR 1216 delegates EDGE-SERVER only, beside NETWORK", nc)
		}
	}
}
