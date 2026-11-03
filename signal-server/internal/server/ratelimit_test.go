package server

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// fixedClock returns a clock function whose value the test controls via the
// returned pointer, so token-bucket refill is exercised without wall-clock
// sleeps.
func fixedClock() (*time.Time, func() time.Time) {
	now := time.Unix(0, 0)
	return &now, func() time.Time { return now }
}

// TestRateLimiterRefillAndBreach walks a bucket through burst → drain →
// breach → refill → admit, asserting the token accounting at each step.
func TestRateLimiterRefillAndBreach(t *testing.T) {
	clk, nowFn := fixedClock()
	rl := newSenderRateLimiter(prometheus.NewRegistry(), "n", 60, 2) // 60/min = 1 token/s, burst 2
	rl.now = nowFn

	// Fresh sender starts with a full bucket (burst=2): two immediate admits.
	if ok, _ := rl.allow("s"); !ok {
		t.Fatal("first offer should admit from a full bucket")
	}
	if ok, _ := rl.allow("s"); !ok {
		t.Fatal("second offer should admit (burst=2)")
	}

	// Bucket empty: third offer breaches with the base 5s hint.
	ok, retryAfter := rl.allow("s")
	if ok {
		t.Fatal("third offer should breach (bucket drained)")
	}
	if retryAfter != 5*time.Second {
		t.Fatalf("first breach retry-after = %v, want 5s", retryAfter)
	}

	// One second later, exactly one token has refilled → one admit, which
	// also resets the consecutive-breach counter.
	*clk = clk.Add(time.Second)
	if ok, _ := rl.allow("s"); !ok {
		t.Fatal("offer after 1s refill should admit")
	}

	// Bucket empty again; because the prior admit reset the counter, this
	// breach is back to the 5s base rather than the doubled 10s.
	ok, retryAfter = rl.allow("s")
	if ok {
		t.Fatal("offer should breach again (bucket drained)")
	}
	if retryAfter != 5*time.Second {
		t.Fatalf("retry-after after an intervening admit = %v, want reset to 5s", retryAfter)
	}
}

// TestConsecutiveBreachBackoff asserts the retry-after hint doubles from 5s
// and caps at 80s across consecutive breaches with no intervening admit.
func TestConsecutiveBreachBackoff(t *testing.T) {
	_, nowFn := fixedClock()
	reg := prometheus.NewRegistry()
	rl := newSenderRateLimiter(reg, "n", 60, 1) // burst 1
	rl.now = nowFn

	// Consume the single token so every subsequent offer (clock frozen)
	// breaches.
	if ok, _ := rl.allow("s"); !ok {
		t.Fatal("first offer should admit")
	}

	want := []time.Duration{5, 10, 20, 40, 80, 80}
	for i, w := range want {
		ok, retryAfter := rl.allow("s")
		if ok {
			t.Fatalf("breach %d: offer admitted, want breach", i+1)
		}
		if retryAfter != w*time.Second {
			t.Fatalf("breach %d: retry-after = %v, want %v", i+1, retryAfter, w*time.Second)
		}
	}

	if got := testutil.ToFloat64(rl.limited); got != float64(len(want)) {
		t.Fatalf("rate_limited_total = %v, want %d", got, len(want))
	}
}

// TestRetryAfterForBreach pins the pure doubling+cap function independent of
// the bucket, including the 0-guard and the clamp boundary.
func TestRetryAfterForBreach(t *testing.T) {
	cases := map[uint]time.Duration{
		0:  5 * time.Second, // guarded to n=1
		1:  5 * time.Second,
		2:  10 * time.Second,
		3:  20 * time.Second,
		4:  40 * time.Second,
		5:  80 * time.Second,
		6:  80 * time.Second,
		99: 80 * time.Second, // no overflow past the clamp
	}
	for n, want := range cases {
		if got := retryAfterForBreach(n); got != want {
			t.Errorf("retryAfterForBreach(%d) = %v, want %v", n, got, want)
		}
	}
}

// TestManyDistinctSendersNotThrottled is the mass-reconnect sizing guarantee:
// the bucket is per-sender, so many peers each firing a full burst are all
// admitted. A healthy Thor-reboot reconnect looks like this — it must not
// throttle. (The storm, by contrast, is a few senders draining their own
// buckets; TestConsecutiveBreachBackoff covers that.)
func TestManyDistinctSendersNotThrottled(t *testing.T) {
	_, nowFn := fixedClock() // clock frozen: no refill, so this tests burst alone
	rl := newSenderRateLimiter(prometheus.NewRegistry(), "n", 30, 60)
	rl.now = nowFn

	const senders, burst = 200, 60
	for s := 0; s < senders; s++ {
		key := fmt.Sprintf("peer-%d", s)
		for i := 0; i < burst; i++ {
			if ok, _ := rl.allow(key); !ok {
				t.Fatalf("sender %d offer %d throttled; per-sender burst must cover mass reconnect", s, i)
			}
		}
	}
	if got := testutil.ToFloat64(rl.limited); got != 0 {
		t.Fatalf("rate_limited_total = %v, want 0 (no sender should breach within burst)", got)
	}
}

// TestRateLimiterDisabled confirms the zero-value (default) limiter is a
// pass-through: every offer admits with a zero hint.
func TestRateLimiterDisabled(t *testing.T) {
	rl := newSenderRateLimiter(prometheus.NewRegistry(), "n", 0, 0)
	for i := 0; i < 1000; i++ {
		if ok, retryAfter := rl.allow("s"); !ok || retryAfter != 0 {
			t.Fatalf("disabled limiter breached at offer %d (ok=%v retryAfter=%v)", i, ok, retryAfter)
		}
	}
}
