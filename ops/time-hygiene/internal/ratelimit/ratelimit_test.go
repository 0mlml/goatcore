package ratelimit

import (
	"sync"
	"testing"
	"time"
)

func TestBucketAllowsUpToCap(t *testing.T) {
	b := NewBucket(5, 24*time.Hour)
	for i := 0; i < 5; i++ {
		if err := b.Allow(); err != nil {
			t.Fatalf("Allow #%d before exhaustion: %v", i+1, err)
		}
	}
	if err := b.Allow(); err == nil {
		t.Error("Allow after exhausting 5/5 tokens: expected error, got nil")
	}
}

func TestBucketRefills(t *testing.T) {
	b := NewBucket(1, 100*time.Millisecond)
	if err := b.Allow(); err != nil {
		t.Fatalf("Allow first: %v", err)
	}
	if err := b.Allow(); err == nil {
		t.Error("Allow second immediately: expected exhaustion, got nil")
	}
	time.Sleep(150 * time.Millisecond)
	if err := b.Allow(); err != nil {
		t.Errorf("Allow after refill: %v", err)
	}
}

func TestBucketDoesNotOverfill(t *testing.T) {
	b := NewBucket(3, 100*time.Millisecond)
	time.Sleep(500 * time.Millisecond)
	allowed := 0
	for i := 0; i < 100; i++ {
		if err := b.Allow(); err != nil {
			break
		}
		allowed++
	}
	if allowed > 3 {
		t.Errorf("bucket allowed %d in burst; cap is 3 — overfill bug", allowed)
	}
}

func TestBucketConcurrent(t *testing.T) {
	b := NewBucket(50, 24*time.Hour)
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := b.Allow(); err == nil {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if allowed > 50 {
		t.Errorf("concurrent allowed %d, cap is 50 — race in bucket", allowed)
	}
	if allowed < 1 {
		t.Errorf("concurrent allowed %d, expected at least 1", allowed)
	}
}

func TestPeerLimiterConstructorRejectsBadInputs(t *testing.T) {
	cases := []struct {
		name              string
		lruCap, max       int
		window            time.Duration
		wantErr           bool
	}{
		{"good", 4, 5, time.Hour, false},
		{"lru zero", 0, 5, time.Hour, true},
		{"max zero", 4, 0, time.Hour, true},
		{"window zero", 4, 5, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewPeerLimiter(tc.lruCap, tc.max, tc.window)
			if (err != nil) != tc.wantErr {
				t.Errorf("err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestPeerLimiterSeparateBucketsPerPeer(t *testing.T) {
	pl, err := NewPeerLimiter(16, 3, 24*time.Hour)
	if err != nil {
		t.Fatalf("NewPeerLimiter: %v", err)
	}

	// Peer A exhausts its bucket; peer B must still be allowed.
	for i := 0; i < 3; i++ {
		if err := pl.Allow("peer-a"); err != nil {
			t.Fatalf("peer-a Allow #%d: %v", i+1, err)
		}
	}
	if err := pl.Allow("peer-a"); err == nil {
		t.Error("peer-a 4th Allow: expected exhausted, got nil")
	}
	for i := 0; i < 3; i++ {
		if err := pl.Allow("peer-b"); err != nil {
			t.Fatalf("peer-b Allow #%d: %v (must not be affected by peer-a)", i+1, err)
		}
	}
}

func TestPeerLimiterSaturationDoesNotLeakAcrossPeers(t *testing.T) {
	pl, err := NewPeerLimiter(64, 2, 24*time.Hour)
	if err != nil {
		t.Fatalf("NewPeerLimiter: %v", err)
	}
	// 50 peers, each gets 2 admits.
	for i := 0; i < 50; i++ {
		peer := "p-" + itoa(i)
		for j := 0; j < 2; j++ {
			if err := pl.Allow(peer); err != nil {
				t.Fatalf("peer %s Allow #%d: %v", peer, j+1, err)
			}
		}
		if err := pl.Allow(peer); err == nil {
			t.Errorf("peer %s 3rd Allow: expected exhausted", peer)
		}
	}
}

func TestPeerLimiterLRUEviction(t *testing.T) {
	pl, err := NewPeerLimiter(4, 1, 24*time.Hour)
	if err != nil {
		t.Fatalf("NewPeerLimiter: %v", err)
	}
	// Fill cache.
	for i, peer := range []string{"a", "b", "c", "d"} {
		if err := pl.Allow(peer); err != nil {
			t.Fatalf("%d Allow %s: %v", i, peer, err)
		}
	}
	// All four buckets should now be exhausted.
	if got := pl.Len(); got != 4 {
		t.Errorf("Len after fill = %d, want 4", got)
	}
	if err := pl.Allow("a"); err == nil {
		t.Error("peer a should be exhausted before eviction")
	}
	// Add a 5th peer; LRU should evict 'b' (a was just touched again).
	if err := pl.Allow("e"); err != nil {
		t.Fatalf("Allow e: %v", err)
	}
	if got := pl.Len(); got != 4 {
		t.Errorf("Len after eviction = %d, want 4", got)
	}
	// 'b' should now be a fresh bucket — Allow should succeed.
	if err := pl.Allow("b"); err != nil {
		t.Errorf("Allow b after eviction: expected fresh bucket, got %v", err)
	}
}

func TestPeerLimiterConcurrent(t *testing.T) {
	pl, err := NewPeerLimiter(64, 5, 24*time.Hour)
	if err != nil {
		t.Fatalf("NewPeerLimiter: %v", err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 200)
	for i := 0; i < 200; i++ {
		wg.Add(1)
		// Two peers: each should accumulate exactly 5 allowed.
		peer := "concurrent-" + itoa(i%2)
		go func() {
			defer wg.Done()
			results <- pl.Allow(peer)
		}()
	}
	wg.Wait()
	close(results)
	allowed := 0
	for r := range results {
		if r == nil {
			allowed++
		}
	}
	// 2 peers × 5 tokens each = exactly 10 admits.
	if allowed != 10 {
		t.Errorf("concurrent allowed=%d, want 10 (2 peers × 5)", allowed)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
