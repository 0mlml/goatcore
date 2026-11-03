package peerset

import (
	"errors"
	"testing"
	"time"
)

func TestValidate_FreshlyMigratedV1Passes(t *testing.T) {
	ps, err := Load("testdata/v1-flat.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := ps.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidate_WrongSchemaVersion(t *testing.T) {
	ps := emptyV2()
	ps.SchemaVersion = 99
	if err := ps.Validate(); !errors.Is(err, ErrSchemaInvalid) {
		t.Fatalf("err = %v, want ErrSchemaInvalid", err)
	}
}

func TestValidate_DuplicateNameInActiveSet(t *testing.T) {
	ps := emptyV2()
	ps.Peers = []Peer{
		{Name: "a", CurrentPubkey: "P1", WGCP0Address: "198.18.0.1"},
		{Name: "a", CurrentPubkey: "P2", WGCP0Address: "198.18.0.2"},
	}
	if err := ps.Validate(); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("err = %v, want ErrDuplicateName", err)
	}
}

func TestValidate_DuplicatePubkey(t *testing.T) {
	ps := emptyV2()
	ps.Peers = []Peer{
		{Name: "a", CurrentPubkey: "P", WGCP0Address: "198.18.0.1"},
		{Name: "b", CurrentPubkey: "P", WGCP0Address: "198.18.0.2"},
	}
	if err := ps.Validate(); !errors.Is(err, ErrDuplicatePubkey) {
		t.Fatalf("err = %v, want ErrDuplicatePubkey", err)
	}
}

func TestValidate_DuplicateAddress(t *testing.T) {
	ps := emptyV2()
	ps.Peers = []Peer{
		{Name: "a", CurrentPubkey: "P1", WGCP0Address: "198.18.0.1"},
		{Name: "b", CurrentPubkey: "P2", WGCP0Address: "198.18.0.1"},
	}
	if err := ps.Validate(); !errors.Is(err, ErrDuplicateAddress) {
		t.Fatalf("err = %v, want ErrDuplicateAddress", err)
	}
}

func TestValidate_DeprecatedMissingRemovedAt(t *testing.T) {
	ps := emptyV2()
	ps.DeprecatedPeers = []DeprecatedPeer{
		{Peer: Peer{Name: "ghost", CurrentPubkey: "P", WGCP0Address: "X"}},
	}
	if err := ps.Validate(); !errors.Is(err, ErrSchemaInvalid) {
		t.Fatalf("err = %v, want ErrSchemaInvalid", err)
	}
}

func TestValidate_HistoryEntryMissingTimestamp(t *testing.T) {
	ps := emptyV2()
	ps.Peers = []Peer{{
		Name: "a", CurrentPubkey: "P", WGCP0Address: "198.18.0.1",
		PubkeyHistory: []PubkeyHistoryEntry{{Pubkey: "OLD"}}, // RotatedAt zero
	}}
	if err := ps.Validate(); !errors.Is(err, ErrSchemaInvalid) {
		t.Fatalf("err = %v, want ErrSchemaInvalid", err)
	}
}

func TestValidate_HistoryEntryEmptyPubkey(t *testing.T) {
	ps := emptyV2()
	ps.Peers = []Peer{{
		Name: "a", CurrentPubkey: "P", WGCP0Address: "198.18.0.1",
		PubkeyHistory: []PubkeyHistoryEntry{{RotatedAt: time.Now()}},
	}}
	if err := ps.Validate(); !errors.Is(err, ErrSchemaInvalid) {
		t.Fatalf("err = %v, want ErrSchemaInvalid", err)
	}
}

func TestValidate_RelayDuplicateName(t *testing.T) {
	ps := emptyV2()
	ps.Relays = []Relay{{Name: "relay-rla"}, {Name: "relay-rla"}}
	if err := ps.Validate(); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("err = %v, want ErrDuplicateName", err)
	}
}

func TestValidate_ActiveForwarderMissingTier(t *testing.T) {
	ps := emptyV2()
	ps.ActiveForwarder = &ActiveForwarder{Host: "relay-rla"}
	if err := ps.Validate(); !errors.Is(err, ErrSchemaInvalid) {
		t.Fatalf("err = %v, want ErrSchemaInvalid", err)
	}
}

func TestValidate_EmptyEnvelopePasses(t *testing.T) {
	if err := emptyV2().Validate(); err != nil {
		t.Fatalf("empty envelope Validate: %v", err)
	}
}
