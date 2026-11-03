package welcome

import (
	"regexp"
	"strings"
	"testing"
)

var testRelays = []string{"rels://relay-a.example.net:33073", "rels://relay-b.example.net:33073"}
var testStun = []string{"stun:relay-a.example.net:3478", "stun:relay-b.example.net:3478"}

func TestNetworkRequirementsFrom_splitsControlAndData(t *testing.T) {
	n, ok := NetworkRequirementsFrom("https://mgmt.example.net", testRelays, testStun)
	if !ok {
		t.Fatal("expected ok")
	}
	if n.ControlHost != "mgmt.example.net" || strings.Join(n.ControlPorts, ",") != "TCP 443" {
		t.Fatalf("control = %q %v", n.ControlHost, n.ControlPorts)
	}
	if got := strings.Join(n.DataHosts, ","); got != "relay-a.example.net,relay-b.example.net" {
		t.Fatalf("data hosts = %s", got)
	}
	if got := strings.Join(n.DataPorts, ","); got != "TCP 33073,UDP 3478,UDP 33073" {
		t.Fatalf("data ports = %s", got)
	}
	if got := n.TestCommand(); got != "nc -zv relay-a.example.net 33073" {
		t.Fatalf("test = %s", got)
	}
}

func TestNetworkRequirementsFrom_noRelaysIsNotOK(t *testing.T) {
	if _, ok := NetworkRequirementsFrom("https://mgmt.example.net", nil, nil); ok {
		t.Fatal("a goatnet with no relay list must not claim a complete answer")
	}
}

func TestEmailLines_shortAndComplete(t *testing.T) {
	n, _ := NetworkRequirementsFrom("https://mgmt.example.net", testRelays, testStun)
	lines := n.EmailLines("zagros", "https://intake.example.net/network-requirements")
	if len(lines) > 9 {
		t.Fatalf("network block is %d lines; keep it short", len(lines))
	}
	body := strings.Join(lines, "\n")
	for _, want := range []string{"relay-a.example.net", "TCP 33073", "nc -zv", "0 peers", "/network-requirements"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
	for _, l := range lines {
		if len(l) > 80 {
			t.Errorf("line over 80 columns: %q", l)
		}
	}
}

var internalAddr = regexp.MustCompile(`\b(100\.(6[4-9]|[7-9][0-9]|1[01][0-9]|12[0-7])\.\d+\.\d+|10\.\d+\.\d+\.\d+|192\.168\.\d+\.\d+|172\.(1[6-9]|2[0-9]|3[01])\.\d+\.\d+)\b`)

func TestNetworkRequirementsPage_namesAndPortsOnly(t *testing.T) {
	n, ok := NetworkRequirementsFrom("https://mgmt.example.net", testRelays, testStun)
	html, err := RenderNetworkRequirementsPage("zagros", n, ok)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"relay-b.example.net", "TCP 33073", "UDP 3478", "nc -zv relay-a.example.net 33073", "0 peers"} {
		if !strings.Contains(html, want) {
			t.Errorf("page missing %q", want)
		}
	}
	if m := internalAddr.FindString(html); m != "" {
		t.Fatalf("public page carries an internal address: %s", m)
	}
}

func TestNetworkPageURL(t *testing.T) {
	if got := NetworkPageURL("https://intake.example.net/onboard.html?t=abc"); got != "https://intake.example.net/network-requirements" {
		t.Fatalf("got %s", got)
	}
	if got := NetworkPageURL("not a url"); got != "" {
		t.Fatalf("got %s", got)
	}
}
