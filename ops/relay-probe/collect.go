package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// run executes a command with a short timeout and returns trimmed stdout.
// Errors collapse to "" — collectors degrade to a safe default, never panic
// (the same forgiving posture as the bash `2>/dev/null || echo default`).
func run(timeout time.Duration, name string, args ...string) string {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	_ = timeout // exec.Command has no built-in deadline; callers keep args cheap
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func serviceActive(unit string) string {
	s := run(3*time.Second, "systemctl", "is-active", unit)
	if s == "" {
		return "unknown"
	}
	return s
}

// dockerLogs returns `docker logs --since <since> <container>` (stdout+stderr
// merged, like the bash `2>&1`).
func dockerLogs(container, since string) (string, error) {
	cmd := exec.Command("docker", "logs", "--since", since, container)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

var peerIDRe = regexp.MustCompile(`PeerId\("[^"]+"\)`)

// peersRecent counts DISTINCT PeerIds that authenticated in the window — an
// approximate recent-activity signal, not a live count (F-235 / relay stats
// will give the accurate version). Matches the widened bash behaviour.
func peersRecent(container string, windowMin int) int {
	logs, _ := dockerLogs(container, strconv.Itoa(windowMin)+"m")
	seen := map[string]struct{}{}
	for _, m := range peerIDRe.FindAllString(logs, -1) {
		seen[m] = struct{}{}
	}
	return len(seen)
}

func healsLastHour() int {
	out := run(3*time.Second, "journalctl", "-t", "rathole-watchdog", "--since", "-1h")
	return strings.Count(out, "threshold reached")
}

// countListen greps `ss -lun` for a :port and returns the match count.
func countUDPListen(port string) int {
	out := run(3*time.Second, "ss", "-lun")
	n := 0
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, ":"+port) {
			n++
		}
	}
	return n
}

func relayRunning(container string) int {
	out := run(3*time.Second, "docker", "ps", "--filter", "name="+container, "--filter", "status=running", "-q")
	if strings.TrimSpace(out) != "" {
		return 1
	}
	return 0
}

// dockerStatPct returns the CPU or Mem percent (as the historical string, sans
// '%'); "?" when unavailable.
func dockerStatPct(container, field string) string {
	out := run(3*time.Second, "docker", "stats", "--no-stream", "--format", "{{."+field+"}}", container)
	out = strings.TrimRight(strings.TrimSpace(out), "% ")
	if out == "" {
		return "?"
	}
	return out
}

func diskPctRoot() int {
	out := run(3*time.Second, "df", "--output=pcent", "/")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		return 0
	}
	return atoiSafe(strings.TrimFunc(lines[len(lines)-1], func(r rune) bool { return r < '0' || r > '9' }))
}

func defaultIface() string {
	out := run(3*time.Second, "ip", "route", "show", "default")
	f := strings.Fields(out)
	for i := 0; i < len(f)-1; i++ {
		if f[i] == "dev" {
			return f[i+1]
		}
	}
	return ""
}

// ifaceCounters reads rx_bytes, rx_packets, tx_bytes, tx_packets from
// /proc/net/dev for the iface (the same four fields the bash awk pulled).
func ifaceCounters(iface string) (rxb, rxp, txb, txp int64, ok bool) {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return 0, 0, 0, 0, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, iface+":") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, iface+":"))
		if len(fields) < 10 {
			return 0, 0, 0, 0, false
		}
		// receive: bytes packets ... (fields[0], fields[1]); transmit bytes packets = fields[8], fields[9]
		rxb = atoi64(fields[0])
		rxp = atoi64(fields[1])
		txb = atoi64(fields[8])
		txp = atoi64(fields[9])
		return rxb, rxp, txb, txp, true
	}
	return 0, 0, 0, 0, false
}

// throughput rate-diffs the iface counters vs the previous sample cached in
// stateDir/<name>.prev. Returns nil rates on the first sample (→ JSON null).
func throughput(iface, stateDir, name string) (rxBps, txBps, rxPps, txPps *int64) {
	now := time.Now().Unix()
	rxb, rxp, txb, txp, ok := ifaceCounters(iface)
	prevPath := filepath.Join(stateDir, name+".prev")
	if ok {
		if prev, err := os.ReadFile(prevPath); err == nil {
			f := strings.Fields(string(prev))
			if len(f) == 5 {
				pts, prxb, prxp, ptxb, ptxp := atoi64(f[0]), atoi64(f[1]), atoi64(f[2]), atoi64(f[3]), atoi64(f[4])
				dt := now - pts
				if dt > 0 && rxb >= prxb {
					rxBps = ptr((rxb - prxb) / dt)
					txBps = ptr((txb - ptxb) / dt)
					rxPps = ptr((rxp - prxp) / dt)
					txPps = ptr((txp - ptxp) / dt)
				}
			}
		}
		_ = os.WriteFile(prevPath, []byte(strings.Join([]string{
			strconv.FormatInt(now, 10), strconv.FormatInt(rxb, 10), strconv.FormatInt(rxp, 10),
			strconv.FormatInt(txb, 10), strconv.FormatInt(txp, 10),
		}, " ")), 0o644)
	}
	return
}

// maxContainerLogMB returns the largest docker container json-log size in MB.
func maxContainerLogMB() int {
	matches, _ := filepath.Glob("/var/lib/docker/containers/*/*.log")
	var max int64
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil && fi.Size() > max {
			max = fi.Size()
		}
	}
	return int(max / (1024 * 1024))
}

// authRejectExpiredRe/authRejectWSTimeoutRe/authRejectEOFRe/authOffenderIPRe
// are the Go ports of metrics-publish.sh's F-269 grep patterns
// (ops/relay/metrics-publish.sh), kept byte-identical so a relay's rejection
// log produces the same counts regardless of which binary is running.
var (
	authRejectExpiredRe   = regexp.MustCompile(`validator rejected.*expired auth token`)
	authRejectWSTimeoutRe = regexp.MustCompile(`WS upgrade timed out`)
	authRejectEOFRe       = regexp.MustCompile(`hand-off failed.*eof`)
	authOffenderIPRe      = regexp.MustCompile(`for ([0-9]+(?:\.[0-9]+){3})`)

	deliveryWriterDeathRe = regexp.MustCompile(`send_datagram failed.*closing connection|writer exiting`)
	deliveryEvictionsRe   = regexp.MustCompile(`routing: evicted`)
	deliveryTooLargeRe    = regexp.MustCompile(`datagram too large`)
)

// countMatchingLines counts LINES matching re, matching bash `grep -cE`
// (which counts matching lines, not total occurrences).
func countMatchingLines(text string, re *regexp.Regexp) int {
	n := 0
	for _, line := range strings.Split(text, "\n") {
		if re.MatchString(line) {
			n++
		}
	}
	return n
}

// authRejectBlockFromLogs builds the F-269 auth-reject block from an
// already-fetched docker-logs window (al), matching metrics-publish.sh's
// AL-derived AREXP/ARWST/AREOF/ARTOP. Takes the log text rather than fetching
// it itself so the caller can share ONE docker-logs call with deliveryBlock
// (the same window), exactly like metrics-publish.sh's single `AL=$(docker
// logs ...)` capture.
func authRejectBlockFromLogs(al string, windowMin int) AuthReject {
	return AuthReject{
		ExpiredToken:     countMatchingLines(al, authRejectExpiredRe),
		WSUpgradeTimeout: countMatchingLines(al, authRejectWSTimeoutRe),
		WSHandoffEOF:     countMatchingLines(al, authRejectEOFRe),
		WindowMin:        windowMin,
		TopOffenders:     topAuthOffenders(al),
	}
}

// topAuthOffenders extracts the source IP from each expired-token rejection
// line and returns the top 3 by count, matching the bash
// `sort | uniq -c | sort -rn | head -3`.
func topAuthOffenders(al string) []AuthOffender {
	counts := map[string]int{}
	for _, line := range strings.Split(al, "\n") {
		if !authRejectExpiredRe.MatchString(line) {
			continue
		}
		m := authOffenderIPRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		counts[m[1]]++
	}
	if len(counts) == 0 {
		return nil
	}
	offenders := make([]AuthOffender, 0, len(counts))
	for ip, c := range counts {
		offenders = append(offenders, AuthOffender{IP: ip, Count: c})
	}
	sort.Slice(offenders, func(i, j int) bool {
		if offenders[i].Count != offenders[j].Count {
			return offenders[i].Count > offenders[j].Count
		}
		return offenders[i].IP < offenders[j].IP // deterministic tie-break
	})
	if len(offenders) > 3 {
		offenders = offenders[:3]
	}
	return offenders
}

// deliveryBlock builds the F-275 delivery block. The log-derived fields
// (writer-death / evictions / datagram-too-large-recent) come from the SAME
// docker-logs window the auth-reject block used (al), matching
// metrics-publish.sh's single AL capture. drops/conns come from the relay's
// white-box stats endpoint (ADR 1055 §2) if one was fetched; null when unknown.
func deliveryBlock(al string, stats *relayStatsDoc, windowMin int) Delivery {
	d := Delivery{
		WriterDeath:      countMatchingLines(al, deliveryWriterDeathRe),
		Evictions:        countMatchingLines(al, deliveryEvictionsRe),
		DatagramTooLarge: countMatchingLines(al, deliveryTooLargeRe),
		WindowMin:        windowMin,
	}
	if stats == nil {
		return d
	}
	if stats.Drops != nil {
		d.Drops = &DropCounters{
			WriterFull:       stats.Drops.WriterFull,
			WriterClosed:     stats.Drops.WriterClosed,
			PeerNotFound:     stats.Drops.PeerNotFound,
			DatagramTooLarge: stats.Drops.DatagramTooLarge,
			Connections:      stats.Connections,
		}
	}
	if stats.Peers != nil {
		d.ConnsQUIC, d.ConnsWS = new(int), new(int)
	}
	for _, p := range stats.Peers {
		switch p.Transport {
		case "quic":
			(*d.ConnsQUIC)++
		case "ws":
			(*d.ConnsWS)++
		}
	}
	return d
}

func ptr(v int64) *int64 { return &v }
func atoi64(s string) int64 {
	v, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return v
}
func atoiSafe(s string) int {
	v, _ := strconv.Atoi(strings.TrimSpace(s))
	return v
}

// A failed read may include diagnostics or partial logs. Neither proves zero.
func setDeliveryMeasurements(deep *Deep, logs string, logErr error, stats *relayStatsDoc, windowMin int) {
	deep.AuthReject, deep.Delivery = nil, nil
	if logErr != nil {
		return
	}
	ar := authRejectBlockFromLogs(logs, windowMin)
	dv := deliveryBlock(logs, stats, windowMin)
	deep.AuthReject, deep.Delivery = &ar, &dv
}
