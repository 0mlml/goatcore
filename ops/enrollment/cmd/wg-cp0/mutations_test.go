// mutations_test.go — direct exercise of every concrete Mutation
// type. These are the primitives Sessions B + C compose against, so
// each Kind/Describe/PreFlight/Apply triple has its own unit test.

package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/enrollment/peerset"
)

func TestStateWriteMutation_Lifecycle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ps.json")
	ps := &peerset.PeerSet{
		SchemaVersion: peerset.SchemaVersion,
		Peers: []peerset.Peer{
			{Name: "alpha", WGCP0Address: "198.18.0.10", CurrentPubkey: "PK_ALPHA"},
		},
	}
	m := &StateWriteMutation{Path: path, NewPeerSet: ps, Describer: "test write"}
	if m.Kind() != "state-write" {
		t.Errorf("Kind = %q", m.Kind())
	}
	if !strings.Contains(m.Describe(), "test write") {
		t.Errorf("Describe = %q", m.Describe())
	}
	if err := m.PreFlight(context.Background()); err != nil {
		t.Fatalf("PreFlight: %v", err)
	}
	if err := m.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// File should exist and validate.
	loaded, err := peerset.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if loaded.Peers[0].Name != "alpha" {
		t.Errorf("Peers[0].Name = %q", loaded.Peers[0].Name)
	}
}

func TestStateWriteMutation_DefaultDescribe(t *testing.T) {
	m := &StateWriteMutation{Path: "/tmp/x", NewPeerSet: &peerset.PeerSet{SchemaVersion: peerset.SchemaVersion}}
	if m.Describe() == "" {
		t.Error("default Describe should return non-empty")
	}
}

func TestStateWriteMutation_PreFlight_NilPS(t *testing.T) {
	m := &StateWriteMutation{Path: "/x"}
	if err := m.PreFlight(context.Background()); err == nil {
		t.Error("nil NewPeerSet should fail PreFlight")
	}
}

func TestStateWriteMutation_PreFlight_EmptyPath(t *testing.T) {
	m := &StateWriteMutation{NewPeerSet: &peerset.PeerSet{SchemaVersion: peerset.SchemaVersion}}
	if err := m.PreFlight(context.Background()); err == nil {
		t.Error("empty path should fail PreFlight")
	}
}

func TestStateWriteMutation_PreFlight_InvalidSchema(t *testing.T) {
	// PeerSet with no schema version should fail Validate.
	m := &StateWriteMutation{Path: "/x", NewPeerSet: &peerset.PeerSet{}}
	if err := m.PreFlight(context.Background()); err == nil {
		t.Error("invalid schema should fail PreFlight")
	}
}

func TestRelayAddPeerMutation_Lifecycle(t *testing.T) {
	relay := peerset.Relay{Name: "relay-rla"}
	mock := &MockRelayMutator{}
	m := &RelayAddPeerMutation{
		Relay: relay, PeerName: "alpha", Pubkey: "PK_ALPHA",
		AllowedIPs: "198.18.0.10/32", Keepalive: 25, Mutator: mock,
	}
	if m.Kind() != "relay-add" {
		t.Errorf("Kind = %q", m.Kind())
	}
	if !strings.Contains(m.Describe(), "alpha") {
		t.Errorf("Describe = %q", m.Describe())
	}
	if err := m.PreFlight(context.Background()); err != nil {
		t.Fatalf("PreFlight: %v", err)
	}
	if err := m.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(mock.AddCalls) != 1 || mock.AddCalls[0].Keepalive != 25 {
		t.Errorf("AddCalls = %+v", mock.AddCalls)
	}
}

func TestRelayAddPeerMutation_DefaultKeepalive(t *testing.T) {
	relay := peerset.Relay{Name: "r"}
	mock := &MockRelayMutator{}
	m := &RelayAddPeerMutation{
		Relay: relay, PeerName: "x", Pubkey: "PK",
		AllowedIPs: "1/32", Mutator: mock, // Keepalive=0 → default 25
	}
	_ = m.Apply(context.Background())
	if mock.AddCalls[0].Keepalive != 25 {
		t.Errorf("default keepalive = %d, want 25", mock.AddCalls[0].Keepalive)
	}
}

func TestRelayAddPeerMutation_PreFlight_Errors(t *testing.T) {
	cases := []struct {
		name string
		m    *RelayAddPeerMutation
	}{
		{"empty pubkey", &RelayAddPeerMutation{Pubkey: "", AllowedIPs: "1/32", Mutator: &MockRelayMutator{}}},
		{"empty allowed-ips", &RelayAddPeerMutation{Pubkey: "PK", AllowedIPs: "", Mutator: &MockRelayMutator{}}},
		{"nil mutator", &RelayAddPeerMutation{Pubkey: "PK", AllowedIPs: "1/32", Mutator: nil}},
	}
	for _, c := range cases {
		if err := c.m.PreFlight(context.Background()); err == nil {
			t.Errorf("%s: PreFlight should have failed", c.name)
		}
	}
}

func TestRelayRotatePubkeyMutation_Lifecycle(t *testing.T) {
	relay := peerset.Relay{Name: "relay-rla"}
	mock := &MockRelayMutator{}
	m := &RelayRotatePubkeyMutation{
		Relay: relay, PeerName: "alpha",
		OldPubkey: "PK_OLD", NewPubkey: "PK_NEW",
		AllowedIPs: "198.18.0.10/32", Keepalive: 25, Mutator: mock,
	}
	if m.Kind() != "relay-rotate" {
		t.Errorf("Kind = %q", m.Kind())
	}
	if !strings.Contains(m.Describe(), "alpha") {
		t.Errorf("Describe = %q", m.Describe())
	}
	if err := m.PreFlight(context.Background()); err != nil {
		t.Fatalf("PreFlight: %v", err)
	}
	if err := m.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(mock.RemoveCalls) != 1 || mock.RemoveCalls[0].Pubkey != "PK_OLD" {
		t.Errorf("RemoveCalls = %+v", mock.RemoveCalls)
	}
	if len(mock.AddCalls) != 1 || mock.AddCalls[0].Pubkey != "PK_NEW" {
		t.Errorf("AddCalls = %+v", mock.AddCalls)
	}
}

func TestRelayRotatePubkeyMutation_RemoveFailsBeforeAdd(t *testing.T) {
	mock := &MockRelayMutator{RemoveErr: errors.New("rm fail")}
	m := &RelayRotatePubkeyMutation{
		OldPubkey: "PK_OLD", NewPubkey: "PK_NEW",
		AllowedIPs: "1/32", Mutator: mock,
	}
	err := m.Apply(context.Background())
	if err == nil {
		t.Fatal("expected error from remove")
	}
	if len(mock.AddCalls) != 0 {
		t.Errorf("Add should not have been called after Remove failed")
	}
}

func TestRelayRotatePubkeyMutation_AddFailureAfterRemove(t *testing.T) {
	mock := &MockRelayMutator{AddErr: errors.New("add fail")}
	m := &RelayRotatePubkeyMutation{
		OldPubkey: "PK_OLD", NewPubkey: "PK_NEW",
		AllowedIPs: "1/32", Mutator: mock,
	}
	err := m.Apply(context.Background())
	if err == nil {
		t.Fatal("expected error from add")
	}
	if !strings.Contains(err.Error(), "add new") {
		t.Errorf("err = %v", err)
	}
}

func TestRelayRotatePubkeyMutation_PreFlight_Errors(t *testing.T) {
	cases := []struct {
		name string
		m    *RelayRotatePubkeyMutation
	}{
		{"empty old", &RelayRotatePubkeyMutation{NewPubkey: "N", AllowedIPs: "1/32", Mutator: &MockRelayMutator{}}},
		{"empty new", &RelayRotatePubkeyMutation{OldPubkey: "O", AllowedIPs: "1/32", Mutator: &MockRelayMutator{}}},
		{"same keys", &RelayRotatePubkeyMutation{OldPubkey: "X", NewPubkey: "X", AllowedIPs: "1/32", Mutator: &MockRelayMutator{}}},
		{"empty ips", &RelayRotatePubkeyMutation{OldPubkey: "O", NewPubkey: "N", AllowedIPs: "", Mutator: &MockRelayMutator{}}},
		{"nil mutator", &RelayRotatePubkeyMutation{OldPubkey: "O", NewPubkey: "N", AllowedIPs: "1/32"}},
	}
	for _, c := range cases {
		if err := c.m.PreFlight(context.Background()); err == nil {
			t.Errorf("%s: PreFlight should fail", c.name)
		}
	}
}

func TestRelayRemovePeerMutation_Lifecycle(t *testing.T) {
	relay := peerset.Relay{Name: "relay-rla"}
	mock := &MockRelayMutator{}
	m := &RelayRemovePeerMutation{Relay: relay, PeerName: "alpha", Pubkey: "PK_ALPHA", Mutator: mock}
	if m.Kind() != "relay-remove" {
		t.Errorf("Kind = %q", m.Kind())
	}
	if !strings.Contains(m.Describe(), "alpha") {
		t.Errorf("Describe = %q", m.Describe())
	}
	if err := m.PreFlight(context.Background()); err != nil {
		t.Fatalf("PreFlight: %v", err)
	}
	if err := m.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(mock.RemoveCalls) != 1 || mock.RemoveCalls[0].Pubkey != "PK_ALPHA" {
		t.Errorf("RemoveCalls = %+v", mock.RemoveCalls)
	}
}

func TestRelayRemovePeerMutation_PreFlight_Errors(t *testing.T) {
	cases := []*RelayRemovePeerMutation{
		{Pubkey: "", Mutator: &MockRelayMutator{}},
		{Pubkey: "PK", Mutator: nil},
	}
	for i, c := range cases {
		if err := c.PreFlight(context.Background()); err == nil {
			t.Errorf("case %d: PreFlight should fail", i)
		}
	}
}

func TestRelayPersistMutation_Lifecycle(t *testing.T) {
	relay := peerset.Relay{Name: "relay-rla"}
	mock := &MockRelayMutator{}
	m := &RelayPersistMutation{Relay: relay, Mutator: mock}
	if m.Kind() != "relay-persist" {
		t.Errorf("Kind = %q", m.Kind())
	}
	if !strings.Contains(m.Describe(), "relay-rla") {
		t.Errorf("Describe = %q", m.Describe())
	}
	if err := m.PreFlight(context.Background()); err != nil {
		t.Fatalf("PreFlight: %v", err)
	}
	if err := m.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(mock.PersistCalls) != 1 || mock.PersistCalls[0] != "relay-rla" {
		t.Errorf("PersistCalls = %+v", mock.PersistCalls)
	}
}

func TestRelayPersistMutation_PreFlight_NilMutator(t *testing.T) {
	m := &RelayPersistMutation{Relay: peerset.Relay{Name: "r"}}
	if err := m.PreFlight(context.Background()); err == nil {
		t.Error("nil mutator should fail PreFlight")
	}
}
