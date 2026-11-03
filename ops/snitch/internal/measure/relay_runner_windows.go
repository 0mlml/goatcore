//go:build windows

package measure

import (
	"context"
	"net"
	"os/exec"
	"strconv"
	"time"
)

// sockCountDefault — Windows. Shells out to `netstat -an -p <proto>` and
// counts ESTABLISHED rows whose remote endpoint matches (host, port).
//
// Output format (one row per socket):
//
//	Proto  Local Address          Foreign Address        State
//	TCP    192.168.1.10:54321     8.8.8.8:443            ESTABLISHED
//	UDP    192.168.1.10:54321     *:*
//
// We don't have `Get-NetTCPConnection` everywhere (older Windows
// editions strip the cmdlet), so netstat is the lowest-common-denominator
// surface. PowerShell-Get-NetTCPConnection would be cleaner on Windows
// 10+ servers — call it out as a future improvement when the fleet
// drops Win-Server-2016 and earlier.
//
// Returns 0 on any error (probe is best-effort).
func sockCountDefault(ctx context.Context, host string, port int, proto string) (int, error) {
	protoFlag := "TCP"
	if proto == "udp" {
		protoFlag = "UDP"
	}
	cmd := exec.CommandContext(ctx, "netstat", "-an", "-p", protoFlag)
	out, err := cmd.Output()
	if err != nil {
		return 0, nil
	}
	return parseNetstatCount(string(out), host, port, proto), nil
}

// portReachableDefault — Windows. Pure-Go TCP dial (nc is not present on
// stock Windows; PowerShell `Test-NetConnection` is slow). UDP falls
// back to a 1-byte write — see Darwin variant for rationale.
func portReachableDefault(ctx context.Context, host string, port int, proto string) (bool, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(2 * time.Second)
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	if proto == "udp" {
		conn, err := net.DialTimeout("udp", addr, time.Until(deadline))
		if err != nil {
			return false, nil
		}
		defer conn.Close()
		_ = conn.SetWriteDeadline(deadline)
		if _, werr := conn.Write([]byte{0}); werr != nil {
			return false, nil
		}
		return true, nil
	}
	conn, err := net.DialTimeout("tcp", addr, time.Until(deadline))
	if err != nil {
		return false, nil
	}
	_ = conn.Close()
	return true, nil
}
