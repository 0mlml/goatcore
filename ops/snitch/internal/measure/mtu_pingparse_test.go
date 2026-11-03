// Cross-platform ping-output parser tests for the MTU probe.
//
// Block 61H. The pingReceivedRE regex needs to handle three different
// summary formats from the three target platforms. We can't actually
// exec ping in a unit test (it would touch the network), but we can
// verify the parser handles canned per-OS output. CGO_ENABLED=0 stays
// (regexp is pure-Go).

package measure

import (
	"strconv"
	"strings"
	"testing"
)

// extractReceivedCount runs the same parsing logic as
// pingPayloadDontFragment but returns the integer count rather than a
// boolean, so tests can verify off-by-one cases. Keep this in lockstep
// with the parser in mtu.go.
func extractReceivedCount(output string) int {
	for _, m := range pingReceivedRE.FindAllStringSubmatch(output, -1) {
		var raw string
		if m[1] != "" {
			raw = m[1]
		} else {
			raw = m[2]
		}
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			return n
		}
	}
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "received") {
			continue
		}
		for _, f := range strings.Fields(line) {
			if n, err := strconv.Atoi(strings.TrimRight(f, ",")); err == nil && n > 0 {
				return n
			}
		}
	}
	return 0
}

func TestPingReceivedParse_Linux(t *testing.T) {
	// iputils ping summary on Linux.
	out := `PING 192.168.1.1 (192.168.1.1) 1472(1500) bytes of data.
1480 bytes from 192.168.1.1: icmp_seq=1 ttl=64 time=2.879 ms

--- 192.168.1.1 ping statistics ---
1 packets transmitted, 1 received, 0% packet loss, time 0ms
rtt min/avg/max/mdev = 2.879/2.879/2.879/0.000 ms
`
	if got := extractReceivedCount(out); got != 1 {
		t.Errorf("Linux ping output: got %d, want 1", got)
	}
}

func TestPingReceivedParse_Darwin(t *testing.T) {
	// BSD/macOS ping summary.
	out := `PING 192.168.1.1 (192.168.1.1): 1472 data bytes
1480 bytes from 192.168.1.1: icmp_seq=0 ttl=64 time=2.879 ms

--- 192.168.1.1 ping statistics ---
1 packets transmitted, 1 packets received, 0.0% packet loss
round-trip min/avg/max/stddev = 2.879/2.879/2.879/0.000 ms
`
	if got := extractReceivedCount(out); got != 1 {
		t.Errorf("Darwin ping output: got %d, want 1", got)
	}
}

func TestPingReceivedParse_Windows(t *testing.T) {
	// Windows ping.exe summary.
	out := `Pinging 192.168.1.1 with 1472 bytes of data:
Reply from 192.168.1.1: bytes=1472 time=3ms TTL=64

Ping statistics for 192.168.1.1:
    Packets: Sent = 1, Received = 1, Lost = 0 (0% loss),
Approximate round trip times in milli-seconds:
    Minimum = 3ms, Maximum = 3ms, Average = 3ms
`
	if got := extractReceivedCount(out); got != 1 {
		t.Errorf("Windows ping output: got %d, want 1", got)
	}
}

func TestPingReceivedParse_DFFailure_Windows(t *testing.T) {
	// Windows DF=on failure surface (used by the explicit branch).
	out := `Pinging 8.8.8.8 with 1472 bytes of data:
Packet needs to be fragmented but DF set.
Packet needs to be fragmented but DF set.

Ping statistics for 8.8.8.8:
    Packets: Sent = 2, Received = 0, Lost = 2 (100% loss),
`
	if got := extractReceivedCount(out); got != 0 {
		t.Errorf("Windows DF=on failure: got %d, want 0", got)
	}
}

func TestPingReceivedParse_ZeroDoesNotFalseSuccess(t *testing.T) {
	out := `--- 192.168.1.1 ping statistics ---
1 packets transmitted, 0 received, 100% packet loss, time 0ms
`
	if got := extractReceivedCount(out); got != 0 {
		t.Errorf("zero-received line: got %d, want 0", got)
	}
}

func TestPingReceivedParse_HighCount(t *testing.T) {
	out := `--- 192.168.1.1 ping statistics ---
100 packets transmitted, 99 received, 1% packet loss
`
	if got := extractReceivedCount(out); got != 99 {
		t.Errorf("high-count line: got %d, want 99", got)
	}
}
