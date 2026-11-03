//go:build !linux

package measure

import (
	"context"

	"github.com/usareur-af/goatcore/ops/snitch/internal/types"
)

// CountDialerOutcomesJournalctl — non-Linux fallback. Always returns
// (zero, false) so RunProbe drops the race-dialer field rather than
// emit misleading zeros.
//
// Block 61H rationale. macOS netbird runs under launchd; logs land in
// `log show --predicate 'subsystem == "io.netbird"'`. Windows netbird
// runs as a service and writes to the Windows Event Log. Both surfaces
// are doable but the parsing shape is meaningfully different from
// journalctl, and the operational value of the race-dialer counters
// on a non-Linux peer is much lower (those peers are typically
// operator workstations, not the high-uptime mesh peers where
// transport thrashing matters most). Defer to a future per-OS log
// reader if dashboard demand justifies the work.
func CountDialerOutcomesJournalctl(_ context.Context, _ int) (types.RaceDialerCounters, bool) {
	return types.RaceDialerCounters{}, false
}
