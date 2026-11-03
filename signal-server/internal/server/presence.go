package server

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	federationpb "github.com/usareur-af/goatcore/signal-server/gen/federation"
)

// presenceState implements ADR 1108 presence-sync (push model).
//
// Each server periodically PUSHES a full snapshot of its locally-registered
// peer keys to its federation peers (a change to the local registry kicks an
// earlier, debounced push). A receiver records key -> owning-node, so relay()
// can forward a cross-shard message DIRECTLY to the owner instead of
// broadcasting to every peer.
//
// Presence is an eventually-consistent PROJECTION, never an authority: an
// unknown key — or a direct forward that misses because the snapshot was stale —
// falls back to Broadcast, so correctness never depends on presence being fresh
// or even present. With it disabled (the default) or empty, relay() behaves
// exactly as the pre-1108 broadcast-to-all path.
//
// The outbound push reuses the federation ClientConn (so the shared-token auth
// interceptor and TLS apply unchanged, ADR 1085 Amendment 1). The push does NOT
// go through the Forward circuit breaker — a presence hiccup must not trip
// message delivery, nor vice versa.
type presenceState struct {
	enabled  bool
	node     string
	interval time.Duration
	fed      *federationClient
	reg      *registry
	metrics  *metrics
	logger   *slog.Logger

	localEpoch atomic.Uint64
	kick       chan struct{}
	stop       chan struct{}
	stopOnce   sync.Once

	mu          sync.RWMutex
	ownerByKey  map[string]string // remote peer key -> owning node name
	epochByNode map[string]uint64 // per-node last-applied snapshot epoch
	nodeByAddr  map[string]string // outbound peer addr -> its node (from PresenceAck)
}

func newPresenceState(enabled bool, node string, interval time.Duration, fed *federationClient, reg *registry, m *metrics, logger *slog.Logger) *presenceState {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	return &presenceState{
		enabled:     enabled,
		node:        node,
		interval:    interval,
		fed:         fed,
		reg:         reg,
		metrics:     m,
		logger:      logger,
		kick:        make(chan struct{}, 1),
		stop:        make(chan struct{}),
		ownerByKey:  make(map[string]string),
		epochByNode: make(map[string]uint64),
		nodeByAddr:  make(map[string]string),
	}
}

// localChanged signals that the local registry changed so the pusher sends a
// fresh snapshot soon (debounced). Non-blocking and cheap; safe on a nil/off
// receiver.
func (p *presenceState) localChanged() {
	if p == nil || !p.enabled {
		return
	}
	select {
	case p.kick <- struct{}{}:
	default:
	}
}

// Stop ends the pusher goroutine. Idempotent.
func (p *presenceState) Stop() {
	if p == nil {
		return
	}
	p.stopOnce.Do(func() { close(p.stop) })
}

// run is the pusher goroutine. Push a full snapshot on each interval tick, and
// on a local change after a short debounce so a reconnect burst coalesces into
// one push. No-op if presence is off or there are no federation peers.
func (p *presenceState) run() {
	if p == nil || !p.enabled || p.fed == nil || len(p.fed.peers) == 0 {
		return
	}
	t := time.NewTicker(p.interval)
	defer t.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-t.C:
			p.pushSnapshot()
		case <-p.kick:
			// debounce a burst of register/unregister into one push
			select {
			case <-p.stop:
				return
			case <-time.After(200 * time.Millisecond):
			}
			p.pushSnapshot()
		}
	}
}

func (p *presenceState) pushSnapshot() {
	keys := p.reg.keys()
	epoch := p.localEpoch.Add(1)
	delta := &federationpb.PresenceDelta{Node: p.node, Full: true, Added: keys, Epoch: epoch}
	for _, peer := range p.fed.peers {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		ack, err := peer.rpc.PresenceUpdate(ctx, delta)
		cancel()
		if err != nil {
			if p.metrics != nil {
				p.metrics.presencePushTotal.WithLabelValues("error").Inc()
			}
			continue
		}
		if p.metrics != nil {
			p.metrics.presencePushTotal.WithLabelValues("ok").Inc()
		}
		if n := ack.GetNode(); n != "" {
			p.mu.Lock()
			p.nodeByAddr[peer.addr] = n
			p.mu.Unlock()
		}
	}
}

// applyInbound records a peer's pushed presence (called from the inbound
// PresenceUpdate handler). Full snapshots replace all keys owned by that node;
// stale epochs are dropped.
func (p *presenceState) applyInbound(d *federationpb.PresenceDelta) {
	if p == nil || !p.enabled || d == nil || d.GetNode() == "" {
		return
	}
	node := d.GetNode()
	p.mu.Lock()
	defer p.mu.Unlock()
	if d.GetEpoch() < p.epochByNode[node] {
		return // stale snapshot; ignore
	}
	p.epochByNode[node] = d.GetEpoch()
	if d.GetFull() {
		for k, owner := range p.ownerByKey {
			if owner == node {
				delete(p.ownerByKey, k)
			}
		}
		for _, k := range d.GetAdded() {
			p.ownerByKey[k] = node
		}
	} else {
		for _, k := range d.GetAdded() {
			p.ownerByKey[k] = node
		}
		for _, k := range d.GetRemoved() {
			if p.ownerByKey[k] == node {
				delete(p.ownerByKey, k)
			}
		}
	}
	if p.metrics != nil {
		p.metrics.presenceKeys.Set(float64(len(p.ownerByKey)))
	}
}

// ownerAddr returns the outbound federation-peer address that presence says owns
// key, if both the key's owning node AND that node's address are known.
func (p *presenceState) ownerAddr(key string) (string, bool) {
	if p == nil || !p.enabled {
		return "", false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	node, ok := p.ownerByKey[key]
	if !ok {
		return "", false
	}
	for addr, n := range p.nodeByAddr {
		if n == node {
			return addr, true
		}
	}
	return "", false
}
