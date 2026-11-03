package server

import (
	"io"
	"log/slog"
	"testing"
	"time"

	federationpb "github.com/usareur-af/goatcore/signal-server/gen/federation"
)

// TestPresenceOwnerResolution covers the ADR-1108 presence projection: a full
// snapshot from a peer maps its keys to that node; the address<->node link
// (learned from PresenceAck) resolves a key to the outbound peer address; newer
// snapshots replace, stale epochs are dropped, and a disabled tracker never
// resolves (so relay() always falls back to broadcast).
func TestPresenceOwnerResolution(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	p := newPresenceState(true, "A", time.Second, nil, nil, nil, logger)

	// The pusher would learn this from B's PresenceAck; set it directly here.
	p.mu.Lock()
	p.nodeByAddr["b:10001"] = "B"
	p.mu.Unlock()

	// B pushes a full snapshot: it owns k1, k2 at epoch 1.
	p.applyInbound(&federationpb.PresenceDelta{Node: "B", Full: true, Added: []string{"k1", "k2"}, Epoch: 1})
	if addr, ok := p.ownerAddr("k1"); !ok || addr != "b:10001" {
		t.Fatalf("ownerAddr(k1) = %q,%v; want b:10001,true", addr, ok)
	}
	if _, ok := p.ownerAddr("unknown"); ok {
		t.Fatal("ownerAddr resolved an owner for an unknown key")
	}

	// Newer snapshot: B now owns only k2 → k1 must be gone (full = replace).
	p.applyInbound(&federationpb.PresenceDelta{Node: "B", Full: true, Added: []string{"k2"}, Epoch: 2})
	if _, ok := p.ownerAddr("k1"); ok {
		t.Fatal("k1 still resolves after a newer snapshot dropped it")
	}
	if _, ok := p.ownerAddr("k2"); !ok {
		t.Fatal("k2 lost after a snapshot that still contained it")
	}

	// Stale epoch must be ignored.
	p.applyInbound(&federationpb.PresenceDelta{Node: "B", Full: true, Added: []string{"k1", "k2"}, Epoch: 1})
	if _, ok := p.ownerAddr("k1"); ok {
		t.Fatal("a stale-epoch snapshot was applied")
	}

	// Disabled tracker never resolves — relay() stays on pure broadcast.
	off := newPresenceState(false, "A", time.Second, nil, nil, nil, logger)
	off.applyInbound(&federationpb.PresenceDelta{Node: "B", Full: true, Added: []string{"k1"}, Epoch: 1})
	if _, ok := off.ownerAddr("k1"); ok {
		t.Fatal("disabled presence resolved an owner")
	}
}
