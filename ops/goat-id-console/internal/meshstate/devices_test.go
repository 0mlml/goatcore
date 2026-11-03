package meshstate

import "testing"

func TestDevicesForServerOnly(t *testing.T) {
	peers := []Peer{
		{Name: "peer-user228x-zagros-2026-10-05-sccm-srv16", OS: "Oracle Linux Server 8.10", Connected: true},
		{Name: "peer-someone-zagros-2026-10-05-laptop", OS: "Microsoft Windows 11 Pro"},
	}
	got := DevicesFor(peers, "user228x", map[string]struct{}{"user228x": {}, "someone": {}}, nil)
	if len(got.Peers) != 1 || got.Peers[0].OS != "Oracle Linux Server 8.10" {
		t.Fatalf("peers = %+v, want the one server peer", got.Peers)
	}
	if !got.ServerOnly {
		t.Fatal("a person whose only peer is a Linux server must read ServerOnly")
	}
}

func TestDevicesForWithWorkstation(t *testing.T) {
	peers := []Peer{
		{Name: "peer-alice-zagros-2026-10-05-srv", OS: "Ubuntu 24.04"},
		{Name: "peer-alice-zagros-2026-10-05-mac", OS: "Darwin"},
	}
	got := DevicesFor(peers, "alice", map[string]struct{}{"alice": {}}, nil)
	if got.ServerOnly || len(got.Peers) != 2 {
		t.Fatalf("got %+v, want two peers and not ServerOnly", got)
	}
}

func TestDevicesForNoPeers(t *testing.T) {
	got := DevicesFor(nil, "bob", nil, nil)
	if got.ServerOnly || len(got.Peers) != 0 {
		t.Fatalf("no peers must not read ServerOnly: %+v", got)
	}
}

func TestDevicesForLongerHandleClaimsName(t *testing.T) {
	peers := []Peer{{Name: "peer-ann-marie-zagros-x", OS: "Ubuntu"}}
	got := DevicesFor(peers, "ann", map[string]struct{}{"ann": {}, "ann-marie": {}}, nil)
	if len(got.Peers) != 0 {
		t.Fatalf("a longer known handle owns the name; got %+v", got.Peers)
	}
}

func TestBrowserCapableOS(t *testing.T) {
	for os, want := range map[string]bool{
		"Microsoft Windows 11 Pro": true, "Darwin": true, "iOS": true, "Android": true,
		"Oracle Linux Server 8.10": false, "Ubuntu 24.04.1 LTS": false, "": false,
	} {
		if got := BrowserCapableOS(os); got != want {
			t.Errorf("BrowserCapableOS(%q) = %v, want %v", os, got, want)
		}
	}
}
