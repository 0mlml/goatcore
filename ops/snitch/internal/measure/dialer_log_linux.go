//go:build linux

package measure

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/usareur-af/goatcore/ops/snitch/internal/types"
)

// CountDialerOutcomesJournalctl shells out to `journalctl -u netbird
// --since "<window> ago"` and counts dialer outcomes from the netbird
// daemon's stderr. This is the production wrapper used by RunProbe via
// WithDialerCounters on Linux.
//
// Returns (counters, true) on success and (zero, false) when the log
// is unreadable — the caller drops the field rather than emitting
// misleading zeros.
//
// Why journalctl rather than reading /var/log/netbird/client.log:
// netbird-on-systemd writes to journald by default; the file path is
// only present on netbird-in-docker. journalctl unifies both.
func CountDialerOutcomesJournalctl(ctx context.Context, windowSeconds int) (types.RaceDialerCounters, bool) {
	since := fmt.Sprintf("%d seconds ago", windowSeconds)
	// #nosec G204 -- literal command "journalctl"; all args except
	// `since` are string constants. `since` is `%d seconds ago` with
	// a numeric int input — no shell interpolation possible.
	cmd := exec.CommandContext(ctx, "journalctl", "-u", "netbird", "--since", since, "--no-pager")
	out, err := cmd.Output()
	if err != nil {
		return types.RaceDialerCounters{}, false
	}
	return CountDialerOutcomes(strings.NewReader(string(out)), windowSeconds), true
}
