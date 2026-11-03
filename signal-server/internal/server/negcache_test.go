package server

import (
	"testing"
	"time"
)

// TestNegativeCacheTTLAndPurge covers the lazy-expiry lifecycle and the
// purge-on-register clear.
func TestNegativeCacheTTLAndPurge(t *testing.T) {
	clk, nowFn := fixedClock()
	nc := newNegativeCache(20 * time.Second)
	nc.now = nowFn

	if nc.offline("x") {
		t.Fatal("empty cache should not report offline")
	}

	nc.record("x")
	if !nc.offline("x") {
		t.Fatal("recorded key should report offline")
	}

	// Still within TTL.
	*clk = clk.Add(19 * time.Second)
	if !nc.offline("x") {
		t.Fatal("key should still be offline 19s into a 20s TTL")
	}

	// Past TTL: lazy expiry drops it.
	*clk = clk.Add(2 * time.Second) // 21s total
	if nc.offline("x") {
		t.Fatal("key should expire at TTL")
	}
	if _, ok := nc.off["x"]; ok {
		t.Fatal("expired entry should be deleted by the offline() read")
	}

	// Purge clears a live entry immediately (the purge-on-register path).
	nc.record("y")
	if !nc.offline("y") {
		t.Fatal("y should be offline after record")
	}
	nc.purge("y")
	if nc.offline("y") {
		t.Fatal("purge should clear the offline mark")
	}
}

// TestNegativeCacheDisabled confirms the zero-TTL (default) cache is inert:
// record is a no-op and offline always reports false, so the F-105
// silent-drop path is unchanged when the mechanism is off.
func TestNegativeCacheDisabled(t *testing.T) {
	nc := newNegativeCache(0)
	nc.record("x")
	if nc.offline("x") {
		t.Fatal("disabled cache must always report not-offline")
	}
	nc.purge("x") // must not panic
}
