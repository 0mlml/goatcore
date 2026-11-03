//go:build darwin

package measure

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"time"
)

// sockCountDefault — Darwin. Shells out to `lsof -nP -i <proto>:<port>`
// and counts active connections to (host, port) over the given proto.
//
// macOS has no analog to `ss`; lsof is the closest cross-version-stable
// tool. The output is `<command> <pid> <user> <fd> <type> <device>
// <size> <node> <name>` where `<name>` is `local-host:local-port->remote-host:remote-port`
// for ESTABLISHED TCP and `local-host:local-port` for UDP. We match
// remote = host:port (or host:port with no arrow for UDP listening).
//
// Returns 0 on any error (probe is best-effort).
func sockCountDefault(ctx context.Context, host string, port int, proto string) (int, error) {
	protoFlag := "TCP"
	if proto == "udp" {
		protoFlag = "UDP"
	}
	// gosec G204 false-positive: protoFlag is one of {"TCP","UDP"}, host
	// is a relay address from operator-controlled management.json, port
	// is a numeric int. All bounded; passed as one argv element to lsof.
	cmd := exec.CommandContext(ctx, "lsof", "-nP", "-i", fmt.Sprintf("%s@%s:%d", protoFlag, host, port)) //nolint:gosec // see comment above
	out, err := cmd.Output()
	if err != nil {
		// nilerr intentional: lsof exits non-zero with no matches; that's
		// a verdict ("zero connections"), not an internal failure.
		return 0, nil //nolint:nilerr // see comment above
	}
	return parseLsofCount(string(out), host, port), nil
}

// portReachableDefault — Darwin. Pure-Go TCP dial; for UDP it falls back
// to a brief WriteTo + read attempt. macOS ships nc (BSD variant) but
// the flag set is incompatible with Linux nc; using net.Dial sidesteps
// that and works the same way on macOS + Windows.
func portReachableDefault(ctx context.Context, host string, port int, proto string) (bool, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(2 * time.Second)
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	// noctx false-positive: net.DialTimeout takes its own deadline driven
	// from the caller-provided ctx. Switching to (*net.Dialer).DialContext
	// would change behavior on intermediate ctx-cancel during the dial,
	// which we don't want here (the probe is bounded; we want it to
	// complete or hard-deadline). Suppressed for both UDP + TCP branches.
	if proto == "udp" {
		// Best-effort: open a UDP socket and send 1 byte. Most relays
		// drop unsolicited UDP silently (which is "reachable" by nc's
		// definition — no ICMP unreachable returned). We treat any
		// non-error write as reachable.
		conn, err := net.DialTimeout("udp", addr, time.Until(deadline)) //nolint:noctx // see comment above
		if err != nil {
			return false, nil //nolint:nilerr // dial failure = unreachable verdict
		}
		defer func() { _ = conn.Close() }() //nolint:errcheck // reachability is determined before socket cleanup
		if err := conn.SetWriteDeadline(deadline); err != nil {
			return false, fmt.Errorf("set UDP write deadline: %w", err)
		}
		if _, werr := conn.Write([]byte{0}); werr != nil {
			return false, nil //nolint:nilerr // write failure = unreachable verdict
		}
		return true, nil
	}
	// TCP: SYN + RST suffices.
	conn, err := net.DialTimeout("tcp", addr, time.Until(deadline)) //nolint:noctx // see comment above
	if err != nil {
		return false, nil //nolint:nilerr // dial failure = unreachable verdict
	}
	_ = conn.Close() //nolint:errcheck // reachability is determined before socket cleanup
	return true, nil
}
