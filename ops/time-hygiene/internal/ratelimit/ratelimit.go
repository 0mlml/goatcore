// Package ratelimit provides token-bucket rate limiting with two layers:
//
//   - PeerLimiter: keyed-by-peer-ID limiter with LRU bound. Each peer gets its
//     own token bucket sized to (max, window); a single misbehaving peer can
//     only exhaust its own bucket, not the global pool.
//
//   - Bucket: the underlying single-bucket token-bucket primitive. Re-exposed
//     so the admit-sidecar can also keep a fleet-wide backstop limiter at a
//     higher cap, defending against many-peer-coordinated abuse.
//
// Block 50B.7 — Per-peer rate limiter.
//
// LRU eviction is required: an attacker can otherwise drive memory growth by
// rotating peer IDs. When the LRU is full and a new peer arrives, the
// least-recently-used bucket is evicted; the attacker effectively forces
// honest infrequent peers to start fresh, which is acceptable (their bucket
// would have refilled anyway).
//
// Concurrency: PeerLimiter.Allow is safe for concurrent use. The underlying
// hashicorp/golang-lru/v2 cache uses an internal mutex; per-bucket Allow
// holds its own mutex.
package ratelimit

import (
	"fmt"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

// Bucket is a thread-safe token-bucket rate limiter. Tokens refill at rate
// max/window; each Allow() consumes one token. Returns an error when empty.
type Bucket struct {
	mu       sync.Mutex
	tokens   float64
	maxTok   float64
	rate     float64 // tokens per nanosecond
	window   time.Duration
	lastTick time.Time
}

// NewBucket builds a bucket with `max` tokens refilling over `window`.
// The bucket starts full so a freshly created peer is not penalized.
func NewBucket(max int, window time.Duration) *Bucket {
	if max <= 0 {
		max = 1
	}
	if window <= 0 {
		window = time.Second
	}
	return &Bucket{
		tokens:   float64(max),
		maxTok:   float64(max),
		rate:     float64(max) / float64(window.Nanoseconds()),
		window:   window,
		lastTick: time.Now(),
	}
}

// Allow consumes one token from the bucket. Returns nil on success, an
// error describing the bucket state otherwise.
func (b *Bucket) Allow() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastTick).Nanoseconds()
	b.lastTick = now

	b.tokens += float64(elapsed) * b.rate
	if b.tokens > b.maxTok {
		b.tokens = b.maxTok
	}

	if b.tokens < 1 {
		return fmt.Errorf("rate limit reached (%.2f/%.0f tokens, window %s)",
			b.tokens, b.maxTok, b.window.String())
	}
	b.tokens--
	return nil
}

// PeerLimiter is a keyed token-bucket limiter with LRU eviction. Each unique
// peer ID gets its own bucket sized to (max, window). When the LRU cap is
// reached, the least-recently-used peer's bucket is evicted.
type PeerLimiter struct {
	cache  *lru.Cache[string, *Bucket]
	max    int
	window time.Duration
}

// NewPeerLimiter builds a per-peer limiter holding up to `lruCap` buckets,
// each sized to (max, window). lruCap must be ≥1; max and window must be > 0.
func NewPeerLimiter(lruCap, max int, window time.Duration) (*PeerLimiter, error) {
	if lruCap < 1 {
		return nil, fmt.Errorf("lruCap must be ≥1, got %d", lruCap)
	}
	if max <= 0 {
		return nil, fmt.Errorf("max must be >0, got %d", max)
	}
	if window <= 0 {
		return nil, fmt.Errorf("window must be >0, got %s", window)
	}
	cache, err := lru.New[string, *Bucket](lruCap)
	if err != nil {
		return nil, fmt.Errorf("lru.New: %w", err)
	}
	return &PeerLimiter{cache: cache, max: max, window: window}, nil
}

// Allow consumes one token from the bucket associated with peerID, creating
// the bucket on first use. Returns nil on success, an error otherwise.
//
// Two callers with the same peerID share the same bucket and contend on its
// internal mutex; callers with distinct peerIDs proceed in parallel.
func (p *PeerLimiter) Allow(peerID string) error {
	b, ok := p.cache.Get(peerID)
	if !ok {
		// Create a fresh bucket. Race-friendly: if another goroutine added
		// one between our Get miss and Add, ContainsOrAdd makes us reuse it.
		nb := NewBucket(p.max, p.window)
		actual, _, evicted := p.cache.PeekOrAdd(peerID, nb)
		_ = actual
		_ = evicted
		// PeekOrAdd does not return the existing value; re-Get to obtain it.
		b, _ = p.cache.Get(peerID)
		if b == nil {
			b = nb // pathological cap=0 shouldn't happen given constructor guard
		}
	}
	return b.Allow()
}

// Len returns the current number of distinct peers tracked. Useful for
// metrics and tests.
func (p *PeerLimiter) Len() int {
	return p.cache.Len()
}
