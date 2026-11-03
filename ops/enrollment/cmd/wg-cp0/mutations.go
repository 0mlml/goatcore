// mutations.go — concrete Mutation implementations.
//
// The Applier executes mutations in plan order. Each Mutation
// implementation captures the deps it needs at construction time
// (RelayMutator, peerset.Save closure, etc.) so the Applier itself
// stays a simple driver.
//
// These primitives are reusable by Sessions B + C as well — the
// onboard / rotate / remove / endpoint-update / ttl-update / add-relay
// planners all compose against the same wire-side primitives.

package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/usareur-af/goatcore/ops/enrollment/peerset"
)

// StateWriteMutation persists a new PeerSet to disk via the peerset
// accessor library's atomic write path. Always the first mutation in
// a plan; failure here aborts the apply (Pre-flight validates the
// in-memory shape, Apply writes to a .tmp then atomic-renames).
type StateWriteMutation struct {
	Path       string
	NewPeerSet *peerset.PeerSet
	Describer  string // operator-readable summary, e.g. "write peerset.json (+1 peer)"
}

// Kind implements Mutation.
func (m *StateWriteMutation) Kind() string { return "state-write" }

// Describe implements Mutation.
func (m *StateWriteMutation) Describe() string {
	if m.Describer != "" {
		return m.Describer
	}
	return "write peerset.json"
}

// PreFlight runs Validate on the in-memory PeerSet so a schema
// violation surfaces before any wire move.
func (m *StateWriteMutation) PreFlight(_ context.Context) error {
	if m.NewPeerSet == nil {
		return errors.New("state-write: NewPeerSet is nil")
	}
	if m.Path == "" {
		return errors.New("state-write: Path is empty")
	}
	return m.NewPeerSet.Validate()
}

// Apply writes the PeerSet atomically.
func (m *StateWriteMutation) Apply(_ context.Context) error {
	return m.NewPeerSet.Save(m.Path)
}

// RelayAddPeerMutation runs `wg set wg-cp0 peer <pubkey> allowed-ips
// <ips>` on one relay via the RelayMutator. Idempotent on the relay
// side (wg set rewrites the peer's row).
type RelayAddPeerMutation struct {
	Relay      peerset.Relay
	PeerName   string
	Pubkey     string
	AllowedIPs string
	Keepalive  int
	Mutator    RelayMutator
}

// Kind implements Mutation.
func (m *RelayAddPeerMutation) Kind() string { return "relay-add" }

// Describe implements Mutation.
func (m *RelayAddPeerMutation) Describe() string {
	return fmt.Sprintf("wg set wg-cp0 peer %s allowed-ips %s on %s (%s)",
		truncate(m.Pubkey, 22), m.AllowedIPs, m.Relay.Name, m.PeerName)
}

// PreFlight checks required fields.
func (m *RelayAddPeerMutation) PreFlight(_ context.Context) error {
	if m.Pubkey == "" {
		return errors.New("relay-add: empty pubkey")
	}
	if m.AllowedIPs == "" {
		return errors.New("relay-add: empty allowed-ips")
	}
	if m.Mutator == nil {
		return errors.New("relay-add: nil RelayMutator")
	}
	return nil
}

// Apply executes the wire-side AddPeer call.
func (m *RelayAddPeerMutation) Apply(ctx context.Context) error {
	keepalive := m.Keepalive
	if keepalive <= 0 {
		keepalive = 25 // matches the surgical playbook default
	}
	return m.Mutator.AddPeer(ctx, m.Relay, m.Pubkey, m.AllowedIPs, keepalive)
}

// RelayRotatePubkeyMutation runs the two-step transactional rotation
// on one relay: remove the old pubkey, then add the new with the same
// allowed-ips. Failure between steps leaves the relay in a recoverable
// state (operator re-runs reconcile; the next plan adds the new key).
type RelayRotatePubkeyMutation struct {
	Relay      peerset.Relay
	PeerName   string
	OldPubkey  string
	NewPubkey  string
	AllowedIPs string
	Keepalive  int
	Mutator    RelayMutator
}

// Kind implements Mutation.
func (m *RelayRotatePubkeyMutation) Kind() string { return "relay-rotate" }

// Describe implements Mutation.
func (m *RelayRotatePubkeyMutation) Describe() string {
	return fmt.Sprintf("wg set wg-cp0 peer %s remove; wg set wg-cp0 peer %s allowed-ips %s on %s (%s)",
		truncate(m.OldPubkey, 22), truncate(m.NewPubkey, 22), m.AllowedIPs, m.Relay.Name, m.PeerName)
}

// PreFlight checks required fields.
func (m *RelayRotatePubkeyMutation) PreFlight(_ context.Context) error {
	if m.OldPubkey == "" {
		return errors.New("relay-rotate: empty old pubkey")
	}
	if m.NewPubkey == "" {
		return errors.New("relay-rotate: empty new pubkey")
	}
	if m.OldPubkey == m.NewPubkey {
		return errors.New("relay-rotate: old == new pubkey (no rotation)")
	}
	if m.AllowedIPs == "" {
		return errors.New("relay-rotate: empty allowed-ips")
	}
	if m.Mutator == nil {
		return errors.New("relay-rotate: nil RelayMutator")
	}
	return nil
}

// Apply executes Remove(old) then Add(new). Returns nil only when
// both succeed; partial state surfaces as the error from whichever
// step failed.
func (m *RelayRotatePubkeyMutation) Apply(ctx context.Context) error {
	if err := m.Mutator.RemovePeer(ctx, m.Relay, m.OldPubkey); err != nil {
		return fmt.Errorf("rotate %s on %s: remove old: %w", m.PeerName, m.Relay.Name, err)
	}
	keepalive := m.Keepalive
	if keepalive <= 0 {
		keepalive = 25
	}
	if err := m.Mutator.AddPeer(ctx, m.Relay, m.NewPubkey, m.AllowedIPs, keepalive); err != nil {
		return fmt.Errorf("rotate %s on %s: add new: %w", m.PeerName, m.Relay.Name, err)
	}
	return nil
}

// RelayRemovePeerMutation runs `wg set wg-cp0 peer <pubkey> remove`
// on one relay. Idempotent (removing an absent peer succeeds on
// recent wg versions).
type RelayRemovePeerMutation struct {
	Relay    peerset.Relay
	PeerName string
	Pubkey   string
	Mutator  RelayMutator
}

// Kind implements Mutation.
func (m *RelayRemovePeerMutation) Kind() string { return "relay-remove" }

// Describe implements Mutation.
func (m *RelayRemovePeerMutation) Describe() string {
	return fmt.Sprintf("wg set wg-cp0 peer %s remove on %s (%s)",
		truncate(m.Pubkey, 22), m.Relay.Name, m.PeerName)
}

// PreFlight checks required fields.
func (m *RelayRemovePeerMutation) PreFlight(_ context.Context) error {
	if m.Pubkey == "" {
		return errors.New("relay-remove: empty pubkey")
	}
	if m.Mutator == nil {
		return errors.New("relay-remove: nil RelayMutator")
	}
	return nil
}

// Apply executes the wire-side RemovePeer call.
func (m *RelayRemovePeerMutation) Apply(ctx context.Context) error {
	return m.Mutator.RemovePeer(ctx, m.Relay, m.Pubkey)
}

// RelayPersistMutation runs `wg-quick save wg-cp0` on one relay so
// the in-memory wg set changes survive reboot. One per relay per
// apply, ordered after every peer mutation on that relay.
type RelayPersistMutation struct {
	Relay   peerset.Relay
	Mutator RelayMutator
}

// Kind implements Mutation.
func (m *RelayPersistMutation) Kind() string { return "relay-persist" }

// Describe implements Mutation.
func (m *RelayPersistMutation) Describe() string {
	return fmt.Sprintf("wg-quick save wg-cp0 on %s", m.Relay.Name)
}

// PreFlight checks required fields.
func (m *RelayPersistMutation) PreFlight(_ context.Context) error {
	if m.Mutator == nil {
		return errors.New("relay-persist: nil RelayMutator")
	}
	return nil
}

// Apply executes wg-quick save.
func (m *RelayPersistMutation) Apply(ctx context.Context) error {
	return m.Mutator.PersistConfig(ctx, m.Relay)
}
