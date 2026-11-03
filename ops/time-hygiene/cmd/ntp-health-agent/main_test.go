package main

import (
	"testing"
	"time"
)

// 50B.7 — jitteredInterval should produce values clustered around `base`
// with each individual sample within ±10% AND the spread (max-min) of N
// samples observably non-zero (i.e. real jitter, not a constant).
func TestJitteredIntervalStaysWithinTenPercent(t *testing.T) {
	base := 10 * time.Second
	const n = 1000
	var (
		minSeen = base * 2
		maxSeen time.Duration
		sum     int64
	)
	for i := 0; i < n; i++ {
		got := jitteredInterval(base)
		if got < minSeen {
			minSeen = got
		}
		if got > maxSeen {
			maxSeen = got
		}
		sum += int64(got)
		if got < (base-base/10) || got > (base+base/10) {
			t.Errorf("sample %d = %v, outside [%v, %v]", i, got, base-base/10, base+base/10)
		}
	}
	mean := time.Duration(sum / n)
	// Mean should be within 1% of base over 1000 samples (uniform distribution).
	if mean < base-base/100 || mean > base+base/100 {
		t.Errorf("mean = %v over %d samples, expected ~%v", mean, n, base)
	}
	// Spread should be approximately 2 * 10% of base (uniform across ±10%).
	// We loosen to "at least 80% of theoretical max" to avoid flakiness.
	expectedSpread := 2 * base / 10
	gotSpread := maxSeen - minSeen
	if gotSpread < (expectedSpread*8)/10 {
		t.Errorf("spread = %v over %d samples, expected ≥%v", gotSpread, n, (expectedSpread*8)/10)
	}
}

func TestJitteredIntervalEdgeCases(t *testing.T) {
	if got := jitteredInterval(0); got != 0 {
		t.Errorf("zero base: got %v, want 0", got)
	}
	if got := jitteredInterval(-time.Second); got != -time.Second {
		t.Errorf("negative base: got %v, want -1s", got)
	}
	// 5ns base: 10% is 0ns truncated, so jitter should be 0.
	if got := jitteredInterval(5 * time.Nanosecond); got != 5*time.Nanosecond {
		t.Errorf("tiny base: got %v, want 5ns", got)
	}
}

func TestMinInt(t *testing.T) {
	if got := minInt(1, 2); got != 1 {
		t.Errorf("min(1,2) = %d, want 1", got)
	}
	if got := minInt(2, 1); got != 1 {
		t.Errorf("min(2,1) = %d, want 1", got)
	}
	if got := minInt(3, 3); got != 3 {
		t.Errorf("min(3,3) = %d, want 3", got)
	}
}
