//go:build !linux

// Block 61H. Non-Linux platforms always return (zero, false) from
// CountDialerOutcomesJournalctl by construction. This test pins that
// invariant — adding a real macOS / Windows log reader in the future
// will require changing the test alongside.

package measure

import (
	"context"
	"testing"
	"time"

	"github.com/usareur-af/goatcore/ops/snitch/internal/types"
)

func TestCountDialerOutcomesJournalctl_NonLinux(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	counters, ok := CountDialerOutcomesJournalctl(ctx, 60)
	if ok {
		t.Errorf("expected ok=false on non-Linux, got ok=true")
	}
	if counters != (types.RaceDialerCounters{}) {
		t.Errorf("expected zero RaceDialerCounters, got %+v", counters)
	}
}
