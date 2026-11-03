package measure

import (
	"testing"
	"time"
)

func TestNetworkCache_TTL(t *testing.T) {
	c := NewNetworkCache()
	if c.PublicIPTTL != 6*time.Hour {
		t.Errorf("default PublicIPTTL = %v; want 6h", c.PublicIPTTL)
	}
}

// Cache-trigger logic is exercised at the integration level (the
// CachingNetwork closure shells out to detectInterface / lookupARP /
// detectPublicIP which exec the OS); a focused unit test would have
// to inject every helper. The trigger logic itself is small enough
// to read at a glance — we cover the easy invariants here.
func TestNetworkCache_Empty(t *testing.T) {
	c := NewNetworkCache()
	if c.hasLast {
		t.Error("fresh cache should have hasLast=false")
	}
	if c.lastInterface != "" || c.lastGatewayMAC != "" || c.lastSubnet != "" {
		t.Error("fresh cache should have empty trigger fields")
	}
}
