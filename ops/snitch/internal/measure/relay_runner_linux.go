//go:build linux

package measure

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
)

// sockCountDefault — Linux. Shells out to `ss` and counts ESTABLISHED
// sockets to (host, port) over the given proto. Returns 0 on any error
// (probe is best-effort).
//
// We use `ss -Hnt` (no header, numeric, tcp) or `-Hnu` for UDP. We do
// NOT pass `-p` (process info) — it requires CAP_NET_ADMIN, and we
// only need socket counts. Filter via `dst host:port` so we don't have
// to grep. Removing `-p` lets the systemd unit drop the cap from its
// AmbientCapabilities (do-no-harm posture review).
func sockCountDefault(ctx context.Context, host string, port int, proto string) (int, error) {
	flag := "-Hnt"
	if proto == "udp" {
		flag = "-Hnu"
	}
	dst := fmt.Sprintf("dst %s:%d", host, port)
	// #nosec G204 -- literal command "ss"; flag is a fixed-set local
	// constant ("-Hnt"/"-Hnu"); host + port are operator-controlled
	// probe inputs, not attacker-controlled. No shell interpolation.
	cmd := exec.CommandContext(ctx, "ss", flag, dst)
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	return parseSSCount(string(out)), nil
}

// portReachableDefault — Linux. Tests reachability via `nc -z` (TCP) or
// `nc -zu` (UDP). nc returns 0 on success. Wall-clock-bounded by ctx.
//
// Caveat: UDP reachability is best-effort — connectionless protocols
// can't definitively confirm the server received the packet. We treat
// "no ICMP unreachable" as "reachable," matching nc's behavior.
func portReachableDefault(ctx context.Context, host string, port int, proto string) (bool, error) {
	args := []string{"-z", "-w", "2"}
	if proto == "udp" {
		args = append(args, "-u")
	}
	args = append(args, host, strconv.Itoa(port))
	// #nosec G204 -- literal command "nc"; args is a fixed-set list of
	// flag literals + numeric port + operator-controlled host string.
	// No shell interpolation.
	cmd := exec.CommandContext(ctx, "nc", args...)
	if err := cmd.Run(); err != nil {
		// non-zero exit = unreachable; not an error from our caller's
		// perspective. We only return true error if exec itself failed.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
