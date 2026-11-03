package server

import (
	"errors"
	"sync"

	signalpb "github.com/usareur-af/goatcore/signal-server/gen/signalexchange"
)

// errUnknownPeer is returned by registry.deliver when the target key isn't
// registered at this signal server. The caller (Send / ConnectStream's
// inbound pump) uses this to decide whether to try the federation path.
var errUnknownPeer = errors.New("peer not registered here")

// errOutboundFull is returned when a peer's outbound channel is saturated.
// Upstream Netbird signal drops in this case; we match that behavior rather
// than blocking inbound traffic from other peers.
var errOutboundFull = errors.New("peer outbound channel full")

// peerStream is one half of a duplex stream.
//
// outbound carries messages the server wants to deliver to the registered
// peer; the ConnectStream handler pumps it to the gRPC stream.
//
// evicted is closed exactly once by register() when a later registration
// for the same key takes over. The ConnectStream handler selects on it
// alongside outbound and exits when it fires.
//
// Design note: we deliberately do NOT close `outbound` on eviction. A
// concurrent deliver() that read the ps pointer before eviction might
// still try to send on it after eviction — sending on a closed channel
// panics and would crash the server. Keeping outbound open lets that
// send either succeed (harmlessly, nobody will read it) or hit the
// select default (queue full); the old peerStream is GC'd once the
// evicted-stream handler returns.
type peerStream struct {
	outbound chan *signalpb.EncryptedMessage
	evicted  chan struct{}
}

// registry is the in-memory map of WireGuard public key → peer stream.
// Two guarantees:
//   - Only one stream per key at a time; re-registration evicts the previous
//     stream (upstream Netbird behavior; clients re-register after reconnect).
//   - Eviction closes the displaced stream's outbound channel so the serving
//     goroutine returns promptly instead of dangling.
type registry struct {
	mu    sync.RWMutex
	peers map[string]*peerStream
}

func newRegistry() *registry {
	return &registry{peers: make(map[string]*peerStream)}
}

// keys returns a snapshot of the currently-registered peer keys — the local
// presence set pushed to federation peers (ADR 1108).
func (r *registry) keys() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.peers))
	for k := range r.peers {
		out = append(out, k)
	}
	return out
}

// register returns a fresh peerStream for key and evicts any previous one
// by closing its `evicted` channel. The outbound channel buffer is
// intentionally small — a peer that can't keep up drops messages
// rather than blocking the server.
func (r *registry) register(key string) *peerStream {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.peers[key]; ok {
		close(old.evicted)
	}
	ps := &peerStream{
		outbound: make(chan *signalpb.EncryptedMessage, 16),
		evicted:  make(chan struct{}),
	}
	r.peers[key] = ps
	return ps
}

// unregister removes the entry only if ps is still the current registration
// for key. This avoids a race where a quick re-register followed by the old
// goroutine's deferred unregister would evict the new (valid) stream.
func (r *registry) unregister(key string, ps *peerStream) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.peers[key] == ps {
		delete(r.peers, key)
	}
}

// deliver attempts a non-blocking send to the target peer's outbound channel.
// Returns errUnknownPeer if the key isn't registered here, errOutboundFull if
// the channel is saturated.
func (r *registry) deliver(key string, msg *signalpb.EncryptedMessage) error {
	r.mu.RLock()
	ps, ok := r.peers[key]
	r.mu.RUnlock()
	if !ok {
		return errUnknownPeer
	}
	select {
	case ps.outbound <- msg:
		return nil
	default:
		return errOutboundFull
	}
}

// count returns the number of peers currently registered. Exposed for the
// Prometheus connected-peer gauge.
func (r *registry) count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.peers)
}
