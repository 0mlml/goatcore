// Block 61H — pure-function parsers for the per-OS socket-count probes.
//
// Why a separate file (no build tags). The exec calls in
// relay_runner_{linux,darwin,windows}.go are platform-specific by
// construction, but the parsers that turn their stdout into a count are
// just text manipulation — keeping them out of the per-OS files lets
// them run as unit tests on any developer host (a Mac developer can
// still verify the Windows netstat parser via canned input).

package measure

import (
	"fmt"
	"strings"
)

// parseSSCount counts non-empty rows in `ss -Hnt dst host:port` (or -Hnu)
// output. Linux production runner.
//
// Sample output:
//
//	ESTAB 0  0  192.168.1.10:54321  10.0.0.1:443
//	ESTAB 0  0  192.168.1.10:54322  10.0.0.1:443
//
// (header suppressed by -H). Each non-empty line is one socket.
func parseSSCount(out string) int {
	count := 0
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

// parseLsofCount counts established connections to (host, port) in
// `lsof -nP -i <PROTO>@<host>:<port>` output. Darwin production runner.
//
// Sample output:
//
//	COMMAND PID USER FD TYPE DEVICE SIZE/OFF NODE NAME
//	netbird 12345 root 27u IPv4 0x... 0t0 TCP 192.168.1.10:54321->10.0.0.1:443 (ESTABLISHED)
//
// We skip the header row + filter on the host:port substring because
// lsof can match by interface name without us asking, and we want
// strict (host, port) matches only.
func parseLsofCount(out, host string, port int) int {
	needle := fmt.Sprintf("%s:%d", host, port)
	count := 0
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" || strings.HasPrefix(line, "COMMAND ") {
			continue
		}
		if strings.Contains(line, needle) {
			count++
		}
	}
	return count
}

// parseNetstatCount counts ESTABLISHED rows whose remote endpoint
// matches (host, port) in `netstat -an -p <proto>` output. Windows
// production runner. For UDP we don't filter on ESTABLISHED (UDP is
// connectionless; presence of any matching row is the signal).
//
// Sample output (one row per socket):
//
//	Active Connections
//	Proto  Local Address          Foreign Address        State
//	TCP    192.168.1.10:54321     8.8.8.8:443            ESTABLISHED
//	UDP    192.168.1.10:54321     *:*
//
// Tolerates IPv6 address bracket form: [::1]:443.
func parseNetstatCount(out, host string, port int, proto string) int {
	needle := fmt.Sprintf("%s:%d", host, port)
	bracketed := fmt.Sprintf("[%s]:%d", host, port)
	count := 0
	for _, line := range strings.Split(strings.TrimRight(out, "\r\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if proto == "tcp" && !strings.Contains(line, "ESTABLISHED") {
			continue
		}
		if strings.Contains(line, needle) || strings.Contains(line, bracketed) {
			count++
		}
	}
	return count
}
