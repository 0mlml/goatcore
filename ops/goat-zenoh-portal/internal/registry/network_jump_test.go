package registry

// ADR 1131 — the NETWORK-JUMP node-class.
//
// The property under test is the one the ADR turns on: NETWORK-JUMP is
// infrastructure, and it is NOT delegable. The delegation in this design sits
// on the *people* who drive the jump host (the site-scoped network_admin
// entitlement, ADR 1113 §3), never on the peer itself — a site admin who may
// enroll a switch must not thereby be able to enroll the box that reaches every
// switch.

import "testing"

func TestNetworkJumpIsInfraAndNotDelegable(t *testing.T) {
	if !ValidNodeClass("NETWORK-JUMP") {
		t.Fatal("ValidNodeClass(NETWORK-JUMP) = false, want true")
	}
	if !IsInfraNodeClass("NETWORK-JUMP") {
		t.Error("IsInfraNodeClass(NETWORK-JUMP) = false — it is the egress for bearer-gear administration and must stay infra-classified")
	}
	if IsDelegableInfraNodeClass("NETWORK-JUMP") {
		t.Error("IsDelegableInfraNodeClass(NETWORK-JUMP) = true — the peer is operator-enrolled (ADR 1131 §2); only the people driving it are delegated")
	}
	if !IsEnrollableNodeClass("NETWORK-JUMP") {
		t.Error("IsEnrollableNodeClass(NETWORK-JUMP) = false — the operator must be able to mint one")
	}
	if IsGearNodeClass("NETWORK-JUMP") {
		t.Error("IsGearNodeClass(NETWORK-JUMP) = true — gear classes are rule-A fabric sources (ADR 1116); this peer must never be one")
	}
}

// NETWORK and NETWORK-JUMP are adjacent names for deliberately different things:
// one is the switch, the other is the only box allowed to log into the switch.
// Conflating them re-creates the over-grant ADR 1113 removed, so assert they
// stay distinct rather than trusting the names to keep them apart.
func TestNetworkAndNetworkJumpAreDistinctClasses(t *testing.T) {
	if IsDelegableInfraNodeClass("NETWORK") == IsDelegableInfraNodeClass("NETWORK-JUMP") {
		t.Error("NETWORK and NETWORK-JUMP have the same delegability — NETWORK is delegable (ADR 1113), NETWORK-JUMP is not (ADR 1131)")
	}
}
