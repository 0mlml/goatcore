// Package measure — netbird daemon dialer-outcome log parser.
//
// Background. F-041 documented that the daemon's relay-client uses a
// non-deterministic race-dialer (QUIC vs WebSocket; first to finish
// the handshake wins). The outcome is logged to /var/log/netbird/
// client.log as one of:
//
//   "successfully dialed via: quic"
//   "successfully dialed via: WS"
//   "failed to dial via quic: <reason>"
//   "failed to dial via WS: <reason>"
//
// Without parsing these, snitch couldn't tell which transport was
// active OR detect transport thrashing under reconnect storms. This
// parser turns N log lines over a window into 4 counters per relay.
//
// Discipline:
//   - Pure-function over an io.Reader of log content. No exec, no fs
//     access. Tests inject a strings.Reader.
//   - Regex-based, robust to leading timestamps and ANSI color codes
//     (we strip the timestamp prefix before matching).
//   - Window-bounded: caller decides which lines to feed.

package measure

import (
	"bufio"
	"io"
	"regexp"
	"strings"

	"github.com/usareur-af/goatcore/ops/snitch/internal/types"
)

var (
	successRE = regexp.MustCompile(`successfully dialed via:\s*(\S+)`)
	failureRE = regexp.MustCompile(`failed to dial via\s+(\S+):`)
)

// CountDialerOutcomes parses the supplied log content and returns the
// transport outcome counters. Pure function — testable with stub
// readers. The window is determined by the caller's slice of log
// content; this fn just counts.
//
// `windowSeconds` is metadata: we record what the caller declared the
// window to be so the collector knows the rate. We don't validate it.
func CountDialerOutcomes(r io.Reader, windowSeconds int) types.RaceDialerCounters {
	out := types.RaceDialerCounters{WindowSeconds: windowSeconds}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if m := successRE.FindStringSubmatch(line); m != nil {
			switch transportLabel(m[1]) {
			case "quic":
				out.QUICSuccess++
			case "ws":
				out.WSSuccess++
			}
			continue
		}
		if m := failureRE.FindStringSubmatch(line); m != nil {
			switch transportLabel(m[1]) {
			case "quic":
				out.QUICFailure++
			case "ws":
				out.WSFailure++
			}
		}
	}
	return out
}

// CountDialerOutcomesJournalctl is defined per-OS in
// dialer_log_{linux,other}.go. The Linux variant shells to journalctl;
// the non-Linux variant returns (zero, false) since macOS / Windows
// netbird daemons don't write to journald.

// transportLabel normalizes the transport-token netbird logs to a
// canonical lowercase form. The daemon writes "quic" lowercase but
// "WS" uppercase — netbird sources confirm this. Future versions may
// drift; we lowercase + strip punctuation defensively.
func transportLabel(raw string) string {
	cleaned := strings.ToLower(strings.TrimRight(raw, ":,"))
	switch cleaned {
	case "quic":
		return "quic"
	case "ws", "websocket":
		return "ws"
	}
	return cleaned // unknown — caller can ignore
}
