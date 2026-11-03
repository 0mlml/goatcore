package netbirdkeys

import "testing"

// `auto_peer_name` is a PREFIX: the patched mgmt image renders the joining peer
// as "<auto_peer_name>-<hostname>", so a machine minted as "machine-<handle>"
// never appears as "<handle>". CutMachine must not match on exact equality
// against the bare handle: that finds nothing, deletes the setup key and reports
// a clean mesh cut while the peer keeps its groups and its reach.
func TestOwnedPeerName(t *testing.T) {
	cases := []struct {
		name    string // the peer name on the mesh
		handle  string // the registry machine handle
		want    bool
		because string
	}{
		{"machine-svn-gateway-one-1-gateway-one-pi-1", "svn-gateway-one-1", true,
			"the shape the mgmt image actually produces"},
		{"machine-octo-pod-test-machine-octo-pod-test-machine", "octo-pod-test-machine", true,
			"handle and hostname identical, so the handle appears twice"},
		{"machine-alpha-baxter-1-alpha-baxter-1", "alpha-baxter-1", true,
			"same shape, different machine"},
		{"gateway-one-svk-001", "gateway-one-svk-001", true,
			"renamed to the bare handle by hand — the documented workaround, still has to cut"},
		{"machine-pod-svk-001", "pod-svk-001", true,
			"auto_peer_name with no hostname appended"},

		// The reason the trailing hyphen is in the implementation.
		{"machine-gateway-one-11-thor", "gateway-one-1", false,
			"handle gateway-one-1 must NOT claim gateway-one-11 — cutting the wrong machine is worse than not cutting"},
		{"machine-svn-gateway-one-10-x", "svn-gateway-one-1", false,
			"same trap one digit further out"},

		{"guestshell", "gateway-one-svk-001", false, "an unrelated peer is not owned"},
		{"", "gateway-one-svk-001", false, "empty name owns nothing"},
	}

	for _, c := range cases {
		t.Run(c.name+"/"+c.handle, func(t *testing.T) {
			got := ownedPeerName(c.name, "machine-"+c.handle, c.handle)
			if got != c.want {
				t.Errorf("ownedPeerName(%q, machine-%s, %s) = %v, want %v — %s",
					c.name, c.handle, c.handle, got, c.want, c.because)
			}
		})
	}
}
