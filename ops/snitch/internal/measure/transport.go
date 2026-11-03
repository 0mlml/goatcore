// Package measure — transport quality: the signals that ping cannot see.
//
// WHY THIS EXISTS. two severe mesh defects were found by hand,
// over roughly ten hours, that snitch as built would have reported as healthy:
//
//   - F-300: the Jetson NIC's hardware UDP segmentation offload emitted
//     malformed packets. 20% of WireGuard traffic destroyed, 250x throughput
//     collapse on a gigabit LAN. Snitch would have seen NOTHING: ICMP is
//     never batched into a GSO super-packet, so ping never touches the broken
//     path. Latency, jitter and loss all read perfect throughout.
//   - F-301: ambient loss on the Starlink leg pinned cubic's congestion
//     window at 43-56 segments where 208-295 were needed, costing 5.6x.
//     Snitch would have seen NOTHING: cwnd is not a ping-visible quantity,
//     and the path's latency and loss were unremarkable at ping rates.
//
// The lesson is not "add more probes". It is that **every cheap diagnostic
// uses small, unbatched, low-rate packets**, and an entire class of failure
// only appears once you move data. Ping at 5 pps measured 0% loss on a path
// that UDP at 488 pps measured at 0.54%.
//
// So this file adds three things, in ascending order of cost:
//
//  1. TCP socket quality, read from the kernel for connections that ALREADY
//     EXIST. Zero packets generated. This is the highest value-per-byte
//     signal available and it is what would have caught F-301 in one cycle:
//     a cwnd pinned at 2 with a healthy send window is unmistakable.
//
//  2. REORDERING DISPLACEMENT, read from the kernel's own estimate in the
//     same `ss` output. Also zero packets. Displacement is the part that
//     matters and the part nobody records: Linux seeds the estimate at 3 and
//     raises it only on observed reordering, and >= 3 is TCP's duplicate-ACK
//     threshold — the difference between "the network jitters" and "TCP will
//     collapse on this path".
//
//     This began as a synthetic UDP echo burst. That version was written,
//     tested and reported as deployed while never running once, because it
//     needed an echo responder on the far end that was never built. Reading
//     what the kernel already measures needs no listening port, sends nothing,
//     and reports on the traffic actually crossing the link rather than on a
//     probe's own packets. The synthetic version is deleted, not parked.
//
//  3. Nothing else here. Sustained throughput stays on the daily cadence in
//     throughput.go — it is the expensive signal and it does not belong in a
//     60-second loop.
package measure

import (
	"context"
	"fmt"
	"math"
	"net"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/usareur-af/goatcore/ops/snitch/internal/types"
)

// ---------------------------------------------------------------- TCP state

// ss -tin emits one whitespace-separated `key:value` soup per connection. We
// parse defensively: kernels differ in which fields they emit, and a missing
// field must degrade the sample rather than fail it. Every field here earned
// its place by being load-bearing in the RCA.
var (
	reCwnd     = regexp.MustCompile(`\bcwnd:(\d+)`)
	reSsthresh = regexp.MustCompile(`\bssthresh:(\d+)`)
	reRTT      = regexp.MustCompile(`\brtt:([\d.]+)/([\d.]+)`)
	reMinRTT   = regexp.MustCompile(`\bminrtt:([\d.]+)`)
	reRetrans  = regexp.MustCompile(`\bretrans:\d+/(\d+)`)
	reBytesRt  = regexp.MustCompile(`\bbytes_retrans:(\d+)`)
	reBytesSnt = regexp.MustCompile(`\bbytes_sent:(\d+)`)
	reMSS      = regexp.MustCompile(`\bmss:(\d+)`)
	reNotsent  = regexp.MustCompile(`\bnotsent:(\d+)`)
	reCC       = regexp.MustCompile(`(?m)^\s+(\w+)\s+wscale:`)
	reReord    = regexp.MustCompile(`\breordering:(\d+)`)
)

// TCPQualityFor reads kernel TCP state for established connections to peerIP.
//
// GENERATES NO TRAFFIC. It reads what the kernel already knows about
// connections the mesh is already carrying, which makes it safe to run every
// cycle on every peer — the property that makes it worth having at all.
//
// Returns nil (not an error) when there is no established connection to that
// peer. That is the common case for an idle peer and is not a fault: a link
// carrying no TCP has no TCP quality to report, and inventing a zero-valued
// sample would poison the series.
func TCPQualityFor(ctx context.Context, peerIP string) (*types.TransportQuality, error) {
	if runtime.GOOS != "linux" {
		// macOS has no `ss` and its `netstat` exposes no cwnd. Rather than
		// emit a subtly different metric from a different tool, report
		// nothing — a gap that is visible as a gap beats a number that is
		// not comparable to the Linux one.
		return nil, nil
	}
	// Parse before exec. peerIP reaches here from the netbird peer list, so it
	// is not attacker-controlled today — but "today" is the whole problem with
	// trusting an upstream string, and `ss` takes a filter EXPRESSION, so a
	// value carrying spaces would be read as extra filter terms rather than an
	// address. Re-serializing the parsed IP means only a canonical literal is
	// ever passed, whatever the caller handed us.
	ip := net.ParseIP(peerIP)
	if ip == nil {
		return nil, fmt.Errorf("not an IP address: %q", peerIP)
	}
	// #nosec G204 -- ip.String() re-serializes a net.ParseIP result, so the
	// argument is a canonical IP literal by construction; the caller's
	// string never reaches the command line.
	out, err := exec.CommandContext(ctx, "ss", "-tin", "dst", ip.String()).Output()
	if err != nil {
		return nil, fmt.Errorf("ss: %w", err)
	}
	return parseSSOutput(string(out)), nil
}

// parseSSOutput is split out so it can be tested against captured real output
// without touching the network. Every fixture in transport_test.go is a real
// capture from the RCA.
func parseSSOutput(out string) *types.TransportQuality {
	// Aggregate across all connections to the peer, keeping the WORST cwnd.
	// Rationale: one collapsed flow beside nine healthy ones is exactly the
	// F-301 signature, and an average would hide it. Monitoring should
	// surface the bad case, not the typical one.
	var (
		q     types.TransportQuality
		found bool
	)
	q.MinCwnd = math.MaxInt32

	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "cwnd:") {
			continue
		}
		found = true
		if m := reCwnd.FindStringSubmatch(line); m != nil {
			if v, err := strconv.Atoi(m[1]); err == nil && v < q.MinCwnd {
				q.MinCwnd = v
			}
		}
		if m := reSsthresh.FindStringSubmatch(line); m != nil {
			if v, err := strconv.Atoi(m[1]); err == nil {
				if q.MinSsthresh == 0 || v < q.MinSsthresh {
					q.MinSsthresh = v
				}
			}
		}
		if m := reRTT.FindStringSubmatch(line); m != nil {
			if v, err := strconv.ParseFloat(m[1], 64); err == nil {
				q.RTTMs = v
			}
			if v, err := strconv.ParseFloat(m[2], 64); err == nil {
				q.RTTVarMs = v
			}
		}
		if m := reMinRTT.FindStringSubmatch(line); m != nil {
			if v, err := strconv.ParseFloat(m[1], 64); err == nil {
				q.MinRTTMs = v
			}
		}
		if m := reMSS.FindStringSubmatch(line); m != nil {
			if v, err := strconv.Atoi(m[1]); err == nil {
				q.MSS = v
			}
		}
		if m := reRetrans.FindStringSubmatch(line); m != nil {
			if v, err := strconv.Atoi(m[1]); err == nil {
				q.RetransSegs += v
			}
		}
		var sent, rt uint64
		if m := reBytesSnt.FindStringSubmatch(line); m != nil {
			if v, err := strconv.ParseUint(m[1], 10, 64); err == nil {
				sent = v
			}
		}
		if m := reBytesRt.FindStringSubmatch(line); m != nil {
			if v, err := strconv.ParseUint(m[1], 10, 64); err == nil {
				rt = v
			}
		}
		if sent > 0 {
			if pct := float64(rt) / float64(sent) * 100; pct > q.RetransPct {
				q.RetransPct = pct
			}
		}
		if m := reNotsent.FindStringSubmatch(line); m != nil {
			if v, err := strconv.ParseUint(m[1], 10, 64); err == nil {
				q.NotSentBytes += v
			}
		}
		if m := reCC.FindStringSubmatch(line); m != nil {
			q.CongestionControl = m[1]
		}
		// Reordering: keep the WORST estimate, for the same reason cwnd keeps
		// the minimum. One path reordering badly is the finding; averaging it
		// against well-behaved connections hides exactly that.
		if m := reReord.FindStringSubmatch(line); m != nil {
			if v, err := strconv.Atoi(m[1]); err == nil && v > q.Reordering {
				q.Reordering = v
			}
		}
		// The limiting flags are the interpretation, and they are why this
		// probe can distinguish "the app had nothing to send" from "TCP
		// refused to send". Without them a low cwnd is ambiguous.
		if strings.Contains(line, "app_limited") {
			q.AppLimited = true
		}
		if strings.Contains(line, "rwnd_limited") {
			q.RwndLimited = true
		}
		if strings.Contains(line, "sndbuf_limited") {
			q.SndbufLimited = true
		}
	}
	if !found {
		return nil
	}
	if q.MinCwnd == math.MaxInt32 {
		q.MinCwnd = 0
	}

	// The verdict. A cwnd this small on a link that is NOT app-limited and
	// has data queued is the F-301 signature, and it is worth stating in
	// words rather than leaving an operator to interpret a number.
	//
	// Threshold 10 is Linux's initial cwnd (initcwnd 10): a connection that
	// has been running and sits AT OR BELOW its own starting window has not
	// merely failed to grow, it has been beaten back down.
	q.CwndCollapsed = q.MinCwnd > 0 && q.MinCwnd <= 10 &&
		!q.AppLimited && q.NotSentBytes > 0

	// Linux seeds its reordering estimate at 3 and raises it only when it
	// OBSERVES worse, so 3 is "no evidence" and anything above it is measured
	// displacement past the duplicate-ACK threshold — the point where
	// reordering starts causing spurious fast retransmits and a loss-based
	// controller begins surrendering the path. That is F-301 in one field.
	q.ReorderTripsFastRetransmit = q.Reordering > 3

	return &q
}
