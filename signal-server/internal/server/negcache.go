// negcache.go — ADR 1081 Layer 2: recipient-offline negative cache.
//
// The expensive part of a relay miss is not the local registry lookup — it's
// the federation fan-out that follows it: every miss broadcasts a Forward to
// every peer signal server (200ms per-peer deadline) only to confirm the
// recipient is offline everywhere. During the storm every one of
// the flooded offers paid that cost and emitted a relay-miss log line.
//
// The negative cache remembers "recipient X was offline" for a short TTL. A
// subsequent Send to X is short-circuited at the top of relay(): no registry
// deliver, no federation broadcast, no per-message log line — just a cheap
// map hit and a shed counter. The client-visible contract is unchanged: a
// cached miss returns the same OK/empty response an uncached miss would (the
// F-105 silent-drop contract), so the vanilla daemon health probe is
// unaffected.
//
// Purge-on-register is the correctness anchor: the moment recipient X
// registers a stream at THIS server (ConnectStream), its entry is dropped, so
// a healthy mass-reconnect makes every reconnecting peer immediately
// reachable again — the cache never delays delivery to a peer that is back.
//
// Known bound: purge only fires for LOCAL registration. If X re-registers at
// a PEER site within the TTL, this server keeps skipping the federation hop to
// X for up to TTL seconds. With the default 20s TTL that is a bounded, storm-
// only staleness (the cache is default-off) on first cross-site contact;
// netbird clients retry, so it self-heals. Cross-site purge would need
// federation signaling and is deliberately out of scope.
//
// Default OFF: ttl <= 0 makes every method a no-op. Wired from the
// SIGNAL_NEGATIVE_CACHE_TTL env var via cmd/signal-server + server.Config.

package server

import (
	"sync"
	"time"
)

// negativeCache maps a recipient WireGuard public key to the time its
// offline mark expires. Lazy expiry: an entry is dropped when it is read
// past expiry or when the recipient registers. The live set is bounded by
// the peer count (scope envelope: ≤6000 fleet-wide), so no background reaper
// is needed.
type negativeCache struct {
	ttl time.Duration // 0 or negative disables the cache
	now func() time.Time

	mu  sync.Mutex
	off map[string]time.Time
}

// newNegativeCache builds a cache with the given TTL. ttl <= 0 disables it.
func newNegativeCache(ttl time.Duration) *negativeCache {
	return &negativeCache{
		ttl: ttl,
		now: time.Now,
		off: make(map[string]time.Time),
	}
}

// enabled reports whether the cache is active.
func (nc *negativeCache) enabled() bool { return nc != nil && nc.ttl > 0 }

// offline reports whether recipient is currently marked offline. A read past
// expiry drops the entry and reports false (lazy expiry). Disabled cache
// always reports false.
func (nc *negativeCache) offline(recipient string) bool {
	if !nc.enabled() {
		return false
	}
	nc.mu.Lock()
	defer nc.mu.Unlock()
	exp, ok := nc.off[recipient]
	if !ok {
		return false
	}
	if !nc.now().Before(exp) { // now >= exp: expired
		delete(nc.off, recipient)
		return false
	}
	return true
}

// record marks recipient offline for the configured TTL. Called only on a
// genuine full miss (local + federation both absent). Disabled cache no-ops.
func (nc *negativeCache) record(recipient string) {
	if !nc.enabled() {
		return
	}
	nc.mu.Lock()
	defer nc.mu.Unlock()
	nc.off[recipient] = nc.now().Add(nc.ttl)
}

// purge drops any offline mark for recipient. Called the moment the recipient
// registers a stream, so a reconnecting peer is immediately reachable again.
// Disabled cache no-ops.
func (nc *negativeCache) purge(recipient string) {
	if !nc.enabled() {
		return
	}
	nc.mu.Lock()
	defer nc.mu.Unlock()
	delete(nc.off, recipient)
}
