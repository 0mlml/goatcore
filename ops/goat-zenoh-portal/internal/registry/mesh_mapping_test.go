package registry

import "testing"

// Every class the mint accepts must also derive mesh groups, or the operator
// learns it at enrolment time as a no_mesh_mapping refusal (#1404, where
// NETWORK-JUMP was enrollable and unmapped). The deliberate exceptions are
// PLATFORM, which derives from its workloads, and the legacy PROCESSOR, which
// has no group on purpose (see nodeClassMeshGroups).
func TestEveryEnrollableClassHasAMeshDerivation(t *testing.T) {
	unmappedByDesign := map[string]bool{"PLATFORM": true, "PROCESSOR": true}
	classes := []map[string]bool{infraNodeClasses, gearNodeClasses, functionNodeClasses}
	for _, set := range classes {
		for nc := range set {
			if !IsEnrollableNodeClass(nc) || unmappedByDesign[nc] {
				continue
			}
			if _, err := MeshGroupsForNodeClass(nc); err != nil {
				t.Errorf("%s is enrollable but has no mesh derivation: %v", nc, err)
			}
		}
	}
}

func TestPodMoonAreMintableGearWithCanonicalGroups(t *testing.T) {
	for _, nodeClass := range []string{"POD", "MOON"} {
		t.Run(nodeClass, func(t *testing.T) {
			if !ValidNodeClass(nodeClass) {
				t.Fatalf("%s is not a valid node class", nodeClass)
			}
			if !IsGearNodeClass(nodeClass) {
				t.Fatalf("%s is not a gear node class", nodeClass)
			}
			if !IsEnrollableNodeClass(nodeClass) {
				t.Fatalf("%s is not enrollable", nodeClass)
			}
			if IsInfraNodeClass(nodeClass) {
				t.Fatalf("%s is infrastructure-gated, want custodian self-service", nodeClass)
			}
			if IsFunctionWorkloadClass(nodeClass) {
				t.Fatalf("%s is a standalone trust-boundary class, not a PLATFORM workload", nodeClass)
			}

			groups, err := MeshGroupsForNodeClass(nodeClass)
			if err != nil {
				t.Fatalf("MeshGroupsForNodeClass(%s): %v", nodeClass, err)
			}
			if len(groups) != 1 || groups[0] != nodeClass {
				t.Fatalf("MeshGroupsForNodeClass(%s) = %v, want [%s]", nodeClass, groups, nodeClass)
			}
		})
	}
}
