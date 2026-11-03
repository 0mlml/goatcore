// live_state_test.go — unit tests for the wg-show parser + mock
// reader. SSH-backed paths are covered by the integration test.

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/enrollment/peerset"
)

func TestParseWGShowDump_Happy(t *testing.T) {
	// First line: interface row (skipped).
	// Subsequent lines: peers.
	dump := strings.Join([]string{
		"iiiiiiii\t(none)\t51821\toff",
		"PK_ALPHA\t(none)\t-\t198.18.0.10/32,198.18.0.0/24\t1715800000\t1024\t512\t25",
		"PK_BRAVO\t(none)\t-\t198.18.0.11/32\t0\t0\t0\toff",
		"",
	}, "\n")

	peers, err := ParseWGShowDump(strings.NewReader(dump))
	if err != nil {
		t.Fatalf("ParseWGShowDump: %v", err)
	}
	if len(peers) != 2 {
		t.Fatalf("peer count = %d, want 2", len(peers))
	}
	alpha, ok := peers["PK_ALPHA"]
	if !ok {
		t.Fatal("PK_ALPHA missing")
	}
	if alpha.AllowedIPs != "198.18.0.10/32,198.18.0.0/24" {
		t.Errorf("alpha.AllowedIPs = %q", alpha.AllowedIPs)
	}
	if alpha.TransferRxBytes != 1024 || alpha.TransferTxBytes != 512 {
		t.Errorf("alpha transfer = %d/%d", alpha.TransferRxBytes, alpha.TransferTxBytes)
	}
	if alpha.LastHandshake.IsZero() {
		t.Error("alpha.LastHandshake should be non-zero")
	}
	bravo := peers["PK_BRAVO"]
	if !bravo.LastHandshake.IsZero() {
		t.Errorf("bravo.LastHandshake should be zero (never handshaked); got %v", bravo.LastHandshake)
	}
}

func TestParseWGShowDump_EmptyDump(t *testing.T) {
	peers, err := ParseWGShowDump(strings.NewReader(""))
	if err != nil {
		t.Fatalf("ParseWGShowDump: %v", err)
	}
	if len(peers) != 0 {
		t.Errorf("peers = %d, want 0", len(peers))
	}
}

func TestParseWGShowDump_OnlyInterfaceRow(t *testing.T) {
	// Just the header row, no peers.
	peers, err := ParseWGShowDump(strings.NewReader("iiiiiiii\t(none)\t51821\toff\n"))
	if err != nil {
		t.Fatalf("ParseWGShowDump: %v", err)
	}
	if len(peers) != 0 {
		t.Errorf("peers = %d, want 0", len(peers))
	}
}

func TestParseWGShowDump_ShortRow_Errors(t *testing.T) {
	dump := "iiiiiiii\t(none)\t51821\toff\nshort"
	_, err := ParseWGShowDump(strings.NewReader(dump))
	if err == nil {
		t.Fatal("expected error on short row")
	}
}

func TestParseWGShowDump_EmptyPubkey_Errors(t *testing.T) {
	dump := "iiiiiiii\t(none)\t51821\toff\n\t(none)\t-\t198.18.0.10/32\t0\t0\t0\toff"
	_, err := ParseWGShowDump(strings.NewReader(dump))
	if err == nil {
		t.Fatal("expected error on empty pubkey row")
	}
}

func TestMockLiveStateReader_RecordsCalls(t *testing.T) {
	mock := &MockLiveStateReader{State: LiveState{PerRelay: map[string]*RelayLiveState{}}}
	relays := []peerset.Relay{{Name: "relay-rla"}, {Name: "relay-rlb"}}
	state, err := mock.Read(context.Background(), relays)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(state.PerRelay) != 0 {
		t.Errorf("PerRelay = %d, want 0", len(state.PerRelay))
	}
	if len(mock.Calls) != 1 || len(mock.Calls[0]) != 2 {
		t.Errorf("Calls = %+v", mock.Calls)
	}
}

func TestMockLiveStateReader_Err(t *testing.T) {
	mock := &MockLiveStateReader{Err: errors.New("boom")}
	_, err := mock.Read(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v", err)
	}
}

func TestMockRelayMutator_RecordsCalls(t *testing.T) {
	mock := &MockRelayMutator{}
	relay := peerset.Relay{Name: "relay-rla"}
	if err := mock.AddPeer(context.Background(), relay, "PK_X", "198.18.0.10/32", 25); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}
	if err := mock.RemovePeer(context.Background(), relay, "PK_X"); err != nil {
		t.Fatalf("RemovePeer: %v", err)
	}
	if err := mock.PersistConfig(context.Background(), relay); err != nil {
		t.Fatalf("PersistConfig: %v", err)
	}
	if len(mock.AddCalls) != 1 || mock.AddCalls[0].Pubkey != "PK_X" {
		t.Errorf("AddCalls = %+v", mock.AddCalls)
	}
	if len(mock.RemoveCalls) != 1 || mock.RemoveCalls[0].Pubkey != "PK_X" {
		t.Errorf("RemoveCalls = %+v", mock.RemoveCalls)
	}
	if len(mock.PersistCalls) != 1 || mock.PersistCalls[0] != "relay-rla" {
		t.Errorf("PersistCalls = %+v", mock.PersistCalls)
	}
}

func TestMockRelayMutator_Errors(t *testing.T) {
	mock := &MockRelayMutator{
		AddErr:     errors.New("add boom"),
		RemoveErr:  errors.New("rm boom"),
		PersistErr: errors.New("persist boom"),
	}
	relay := peerset.Relay{Name: "r"}
	if err := mock.AddPeer(context.Background(), relay, "k", "i", 25); err == nil {
		t.Error("AddPeer should error")
	}
	if err := mock.RemovePeer(context.Background(), relay, "k"); err == nil {
		t.Error("RemovePeer should error")
	}
	if err := mock.PersistConfig(context.Background(), relay); err == nil {
		t.Error("PersistConfig should error")
	}
}

func TestDefaultEndpointFromRelay(t *testing.T) {
	r := peerset.Relay{MeshAddress: "198.18.0.2"}
	got := defaultEndpointFromRelay(r)
	if got != "198.18.0.2:22" {
		t.Errorf("got %q, want 198.18.0.2:22", got)
	}
	r.MeshAddress = ""
	got = defaultEndpointFromRelay(r)
	if got != "" {
		t.Errorf("empty mesh_address should yield empty endpoint; got %q", got)
	}
}

func TestNewSSHLiveStateReader_Defaults(t *testing.T) {
	r := NewSSHLiveStateReader(SSHReaderOptions{})
	if r.opts.Username != "ubuntu" {
		t.Errorf("Username = %q, want ubuntu", r.opts.Username)
	}
	if r.opts.Concurrency != 4 {
		t.Errorf("Concurrency = %d, want 4", r.opts.Concurrency)
	}
	if r.opts.PerRelayTimeout != 5_000_000_000 { // 5s
		t.Errorf("PerRelayTimeout = %v, want 5s", r.opts.PerRelayTimeout)
	}
}

func TestSSHLiveStateReader_NoRelays_ReturnsEmpty(t *testing.T) {
	r := NewSSHLiveStateReader(SSHReaderOptions{})
	state, err := r.Read(context.Background(), nil)
	if err != nil {
		t.Fatalf("Read with no relays: %v", err)
	}
	if len(state.PerRelay) != 0 {
		t.Errorf("PerRelay = %d, want 0", len(state.PerRelay))
	}
}

func TestSSHLiveStateReader_BogusAgentSocket_Errors(t *testing.T) {
	// Force a definitively-broken agent socket path so the test does
	// not depend on the developer's $SSH_AUTH_SOCK being set or unset.
	r := NewSSHLiveStateReader(SSHReaderOptions{AgentSocket: "/nonexistent/path/to/agent.sock"})
	_, err := r.Read(context.Background(), []peerset.Relay{{Name: "relay-x", MeshAddress: "10.0.0.1"}})
	if err == nil {
		t.Fatal("expected agent dial error")
	}
	if !strings.Contains(err.Error(), "ssh agent") {
		t.Errorf("err = %v", err)
	}
}

func TestSSHRelayMutator_EmptyMeshAddress_Errors(t *testing.T) {
	m := NewSSHRelayMutator(SSHReaderOptions{})
	err := m.AddPeer(context.Background(), peerset.Relay{Name: "r"}, "PK", "1/32", 25)
	if err == nil || !strings.Contains(err.Error(), "no usable SSH endpoint") {
		t.Errorf("err = %v", err)
	}
	err = m.RemovePeer(context.Background(), peerset.Relay{Name: "r"}, "PK")
	if err == nil || !strings.Contains(err.Error(), "no usable SSH endpoint") {
		t.Errorf("err = %v", err)
	}
	err = m.PersistConfig(context.Background(), peerset.Relay{Name: "r"})
	if err == nil || !strings.Contains(err.Error(), "no usable SSH endpoint") {
		t.Errorf("err = %v", err)
	}
}

func TestSSHRelayMutator_BogusAgent_Errors(t *testing.T) {
	// Same pattern as the reader test — a known-bad agent socket
	// path forces the agent-load failure regardless of dev env state.
	m := NewSSHRelayMutator(SSHReaderOptions{AgentSocket: "/nonexistent/path/to/agent.sock"})
	err := m.AddPeer(context.Background(), peerset.Relay{Name: "r", MeshAddress: "10.0.0.1"}, "PK", "1/32", 25)
	if err == nil || !strings.Contains(err.Error(), "ssh agent") {
		t.Errorf("err = %v", err)
	}
}

func TestNewSSHRelayMutator_Defaults(t *testing.T) {
	m := NewSSHRelayMutator(SSHReaderOptions{})
	if m.opts.Username != "ubuntu" {
		t.Errorf("Username = %q", m.opts.Username)
	}
	if m.opts.PerRelayTimeout == 0 {
		t.Error("PerRelayTimeout should default")
	}
}

func TestShellEscape(t *testing.T) {
	cases := map[string]string{
		"simple": "'simple'",
		"":       "''",
		"a b c":  "'a b c'",
		"with'q": "'with'\\''q'",
	}
	for in, want := range cases {
		got := shellEscape(in)
		if got != want {
			t.Errorf("shellEscape(%q) = %q, want %q", in, got, want)
		}
	}
}
