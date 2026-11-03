package measure

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/usareur-af/goatcore/ops/snitch/internal/types"
)

// DetectNetwork auto-detects the local network environment.
func DetectNetwork(ctx context.Context) types.NetworkFingerprint {
	fp := types.NetworkFingerprint{}

	iface, ifType := detectInterface(ctx)
	fp.Interface = iface
	fp.InterfaceType = ifType

	if ifType == "wifi" {
		fp.SSID = detectSSID(ctx, iface)
	}

	fp.GatewayIP = detectGateway(ctx)
	if fp.GatewayIP != "" {
		if result, err := Ping(ctx, fp.GatewayIP, 3); err == nil {
			fp.GatewayRTT = result.AvgMs
		}
	}

	pub, asn, org := detectPublicIP(ctx)
	fp.PublicIP = pub
	fp.ASN = asn
	fp.ASNOrg = org

	// Gateway hardware characterization
	fp.Gateway = DetectGateway(ctx, fp.GatewayIP)

	// Local subnet
	fp.Subnet = DetectSubnet(ctx, iface)

	return fp
}

// detectInterface finds the default route interface and classifies it.
func detectInterface(ctx context.Context) (name, ifType string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "route", "-n", "get", "default")
	case "windows":
		// PowerShell. `Get-NetRoute -DestinationPrefix 0.0.0.0/0` lists
		// default-route interface index + next-hop. Format-Table strips
		// the column borders; we parse with regex below.
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command",
			`Get-NetRoute -DestinationPrefix 0.0.0.0/0 -ErrorAction SilentlyContinue | Sort-Object RouteMetric | Select-Object -First 1 | Format-List InterfaceAlias,NextHop,InterfaceIndex`)
	default:
		cmd = exec.CommandContext(ctx, "ip", "route", "show", "default")
	}
	out, err := cmd.Output()
	if err != nil {
		return "unknown", "unknown"
	}

	name = parseDefaultRouteIface(runtime.GOOS, string(out))
	if name == "" {
		return "unknown", "unknown"
	}
	// classifyInterface keeps a context-free signature so the test
	// harness can call it directly with a literal name. On Darwin it
	// dispatches to isWiFiInterface, which builds its own short-budget
	// ctx internally. The contextcheck false-positive is suppressed.
	return name, classifyInterface(name) //nolint:contextcheck // see comment
}

// classifyInterface guesses the interface type from its name.
//
// Windows InterfaceAlias is a free-form label (e.g. "Wi-Fi", "Ethernet 2",
// "Cellular") rather than a kernel device name. We match on the labels
// Windows ships by default; non-default labels fall through to "unknown"
// and operators can override via the SSID-presence signal further down.
func classifyInterface(name string) string {
	lower := strings.ToLower(name)
	if runtime.GOOS == "windows" {
		switch {
		case strings.Contains(lower, "wi-fi") || strings.Contains(lower, "wifi") || strings.Contains(lower, "wireless"):
			return "wifi"
		case strings.Contains(lower, "cellular") || strings.Contains(lower, "mobile broadband"):
			return "cellular"
		case strings.Contains(lower, "wireguard") || strings.Contains(lower, "vpn") || strings.Contains(lower, "tap"):
			return "tunnel"
		case strings.Contains(lower, "ethernet") || strings.Contains(lower, "local area connection"):
			return "ethernet"
		}
		return "unknown"
	}
	switch {
	case strings.HasPrefix(lower, "en") || strings.HasPrefix(lower, "eth"):
		// Could be ethernet or WiFi on macOS (en0 is typically WiFi).
		// Check if it's a WiFi adapter by looking for wireless info.
		if isWiFiInterface(name) {
			return "wifi"
		}
		return "ethernet"
	case strings.HasPrefix(lower, "wl"):
		return "wifi"
	case strings.HasPrefix(lower, "ww") || strings.HasPrefix(lower, "rmnet") || strings.HasPrefix(lower, "pdp"):
		return "cellular"
	case strings.HasPrefix(lower, "utun") || strings.HasPrefix(lower, "wg"):
		return "tunnel"
	default:
		return "unknown"
	}
}

func isWiFiInterface(name string) bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	// Self-contained 2 s timeout: this helper is called from
	// classifyInterface which doesn't take ctx (preserving its signature
	// keeps the package's test surface stable). networksetup output is
	// small + bounded; a hard timeout prevents stuck-tool hangs.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "networksetup", "-listallhardwareports")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	// Look for "Hardware Port: Wi-Fi" followed by "Device: <name>"
	lines := strings.Split(string(out), "\n")
	for i, line := range lines {
		if strings.Contains(line, "Wi-Fi") && i+1 < len(lines) {
			if strings.Contains(lines[i+1], name) {
				return true
			}
		}
	}
	return false
}

// detectSSID returns the current WiFi SSID for the given interface.
func detectSSID(ctx context.Context, iface string) string {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// gosec G204 false-positive on the per-OS branches below: `iface` is
	// a kernel device name (en0 / wl0 / "Wi-Fi") sourced from the local
	// route-table parse upstream, never from a network or operator surface.
	// Each ping/arp/networksetup invocation passes it as a separate exec
	// argument (no shell), so any embedded metacharacter would degrade to
	// a benign tool error rather than escape the argv boundary.
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		// macOS 15+: networksetup is the stable surface; airport binary
		// was removed in macOS 14.4.
		cmd = exec.CommandContext(ctx, "/usr/sbin/networksetup", "-getairportnetwork", iface) //nolint:gosec // see comment above
	case "windows":
		// netsh wlan show interfaces emits "SSID : <name>" line. The
		// `iface` arg is ignored — Windows reports the active SSID
		// across all WLAN adapters (typically one).
		cmd = exec.CommandContext(ctx, "netsh", "wlan", "show", "interfaces")
	default:
		cmd = exec.CommandContext(ctx, "iwgetid", "-r", iface) //nolint:gosec // iface is local-OS-sourced; see comment above
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return parseSSID(runtime.GOOS, string(out))
}

func detectGateway(ctx context.Context) string {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "route", "-n", "get", "default")
	case "windows":
		// PowerShell. Same Get-NetRoute query as detectInterface — we
		// extract NextHop instead of InterfaceAlias.
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command",
			`Get-NetRoute -DestinationPrefix 0.0.0.0/0 -ErrorAction SilentlyContinue | Sort-Object RouteMetric | Select-Object -First 1 | Format-List NextHop`)
	default:
		cmd = exec.CommandContext(ctx, "ip", "route", "show", "default")
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	return parseDefaultGatewayIP(runtime.GOOS, string(out))
}

// detectPublicIP queries an external service for public IP and ASN info.
// Uses ip-api.com which returns JSON with IP, ASN, and org in one call.
func detectPublicIP(ctx context.Context) (ip, asn, org string) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://ip-api.com/json/?fields=query,as,org", nil)
	if err != nil {
		return "", "", ""
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext,
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", ""
	}
	defer resp.Body.Close()

	var result struct {
		Query string `json:"query"`
		AS    string `json:"as"` // "AS14593 SpaceX Services, Inc."
		Org   string `json:"org"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", ""
	}

	ip = result.Query
	org = result.Org

	// Parse ASN from "AS14593 SpaceX Services, Inc." → "AS14593"
	if parts := strings.Fields(result.AS); len(parts) > 0 {
		asn = parts[0]
	}
	if org == "" && len(strings.Fields(result.AS)) > 1 {
		org = strings.Join(strings.Fields(result.AS)[1:], " ")
	}

	return ip, asn, org
}

// StatusFunc returns netbird peer status. The default calls `netbird status --json`.
type StatusFunc func(ctx context.Context) (*types.NetbirdStatusJSON, error)

// PingFunc measures latency to an IP. The default runs ICMP ping.
type PingFunc func(ctx context.Context, ip string, count int) (*PingResult, error)

// NetworkFunc detects local network environment. The default probes system interfaces.
type NetworkFunc func(ctx context.Context) types.NetworkFingerprint

// ProbeOption configures RunProbe dependencies for testing.
type ProbeOption func(*probeConfig)

type probeConfig struct {
	statusFn  StatusFunc
	pingFn    PingFunc
	networkFn NetworkFunc
	// New (Block 61 — F-042 closed loop). All optional; if unset the
	// corresponding sub-probe is skipped, leaving zero values in the
	// report. This keeps RunProbe backward-compatible with callers
	// (e.g. `snitch run`) that only want the legacy surface.
	relayProbeFn   func(ctx context.Context, relayURL string) types.RelayHealth
	mtuProbeFn     func(ctx context.Context, target, iface string) types.MTUInfo
	dialerCounters func(ctx context.Context, windowSeconds int) (types.RaceDialerCounters, bool)
	// CycleBudget caps the total wall-clock for the whole probe cycle.
	// Zero or negative = no soft cap (legacy behavior). When set,
	// sub-steps that haven't started before the budget elapses are
	// skipped with a counter incremented in the report.
	cycleBudget time.Duration
	// dialerWindowSeconds tells the race-dialer counter how wide a
	// `journalctl --since` window to scan. Default 300s (5 min) — wide
	// enough to catch reconnect storms without re-counting on every
	// 60s probe cycle.
	dialerWindowSeconds int
}

// WithStatus overrides netbird status retrieval (e.g. fixture data for testing).
func WithStatus(fn StatusFunc) ProbeOption {
	return func(c *probeConfig) { c.statusFn = fn }
}

// WithPing overrides the ping function (e.g. stub latency for testing).
func WithPing(fn PingFunc) ProbeOption {
	return func(c *probeConfig) { c.pingFn = fn }
}

// WithNetwork overrides network fingerprinting (e.g. fixed values for testing).
func WithNetwork(fn NetworkFunc) ProbeOption {
	return func(c *probeConfig) { c.networkFn = fn }
}

// WithRelayProbe enables per-relay transport-assertion + reachability +
// TLS expiry probing. Pass nil to disable; tests pass
// a stub that doesn't touch the OS.
func WithRelayProbe(fn func(ctx context.Context, relayURL string) types.RelayHealth) ProbeOption {
	return func(c *probeConfig) { c.relayProbeFn = fn }
}

// WithMTUProbe enables one-shot path-MTU detection.
func WithMTUProbe(fn func(ctx context.Context, target, iface string) types.MTUInfo) ProbeOption {
	return func(c *probeConfig) { c.mtuProbeFn = fn }
}

// WithDialerCounters enables race-dialer outcome counting from the
// netbird daemon log over the given window. The bool return signals
// whether the read succeeded (false = log unreadable; counters dropped
// from the report).
func WithDialerCounters(fn func(ctx context.Context, windowSeconds int) (types.RaceDialerCounters, bool)) ProbeOption {
	return func(c *probeConfig) { c.dialerCounters = fn }
}

// WithCycleBudget enables the per-cycle soft budget. When set, sub-steps
// whose start time would exceed the budget are skipped (counter in the
// report's CycleBudget). 5*time.Second is the recommended default.
func WithCycleBudget(d time.Duration) ProbeOption {
	return func(c *probeConfig) { c.cycleBudget = d }
}

// WithDialerWindowSeconds overrides the race-dialer counter window. The
// default (300s / 5min) is do-no-harm-friendly: long enough to catch
// reconnect storms, short enough that journalctl reads stay cheap.
func WithDialerWindowSeconds(s int) ProbeOption {
	return func(c *probeConfig) { c.dialerWindowSeconds = s }
}

// RunProbe executes a full measurement cycle: netbird status + ping all peers + fingerprint.
//
// Block 61 (snitch v2). Now also collects per-relay transport assertion,
// MTU info, and race-dialer outcome counters when those probe options are
// wired in. Sub-steps respect a soft cycle budget (WithCycleBudget); a
// step that would start past the budget is skipped + counted, NOT canceled
// mid-flight (we want partial reports, not corrupted ones).
func RunProbe(ctx context.Context, opts ...ProbeOption) (*types.ProbeReport, error) {
	cfg := probeConfig{
		statusFn:  NetbirdStatus,
		pingFn:    Ping,
		networkFn: DetectNetwork,
	}
	for _, o := range opts {
		o(&cfg)
	}

	cycleStart := time.Now()
	skipped := 0
	overBudget := func() bool {
		if cfg.cycleBudget <= 0 {
			return false
		}
		return time.Since(cycleStart) > cfg.cycleBudget
	}

	// F-098: NetbirdStatus failure is NOT fatal. Hosts without a running
	// netbird daemon (CLI present but no socket — see F-099) are still
	// useful as fleet-health probe targets; we just leave the netbird-
	// related fields empty and continue. Pre-F-098 the indefinite CLI
	// retry loop hung the probe forever; post-F-098 the fast-fail returns
	// quickly but RunProbe used to bail with `return nil, err` which
	// dropped the entire cycle. Now we log + continue with zero peers.
	var status *types.NetbirdStatusJSON
	statusRaw, statusErr := cfg.statusFn(ctx)
	if statusErr != nil {
		// Empty status — downstream LocalPeerFromStatus + PeersFromStatus
		// handle nil-or-empty input by returning empty values. The probe
		// report carries explicit zero netbird fields that the collector
		// can recognize as "host has no netbird daemon".
		status = &types.NetbirdStatusJSON{}
	} else {
		status = statusRaw
	}

	name, ip := LocalPeerFromStatus(status)
	peers := PeersFromStatus(status)
	network := cfg.networkFn(ctx)

	report := &types.ProbeReport{
		PeerName:  name,
		PeerIP:    ip,
		Timestamp: time.Now().UTC(),
		Network:   network,
		Netbird: types.NetbirdHealth{
			ManagementConnected: status.Management.Connected,
			ManagementURL:       status.Management.URL,
			SignalConnected:     status.Signal.Connected,
			SignalURL:           status.Signal.URL,
			RelaysTotal:         status.Relays.Total,
			RelaysAvailable:     status.Relays.Available,
		},
	}

	// THE BOUNDED SUB-STEPS RUN FIRST, AND THE ORDER IS THE POINT.
	//
	// These three used to sit after the peer sweep, where they were gated on a
	// budget the sweep had already spent: on a large mesh the sweep alone takes
	// most of a 60s interval, so all three opted out on EVERY cycle and the
	// fleet collected no relay probe, no dialer counters and no MTU at all.
	// Raising the budget cannot fix that,
	// because the sweep, not the budget, is what consumes the interval.
	//
	// Running them first inverts which work the budget protects. Each is bounded
	// and small; the peer sweep is the open-ended part that grows with the
	// estate, so it is the right thing to truncate under pressure. The sweep
	// itself remains ungated below, exactly as before.
	// Per-relay probe. Only runs if the option is
	// wired AND we haven't blown the budget. Each relay URL gets one
	// RelayHealth row; we read URLs from the netbird-status's relay
	// list rather than configuration so this auto-tracks fleet-wide
	// changes.
	if cfg.relayProbeFn != nil {
		if overBudget() {
			skipped++
		} else {
			for _, rurl := range relayURLsFromStatus(status) {
				report.Relays = append(report.Relays, cfg.relayProbeFn(ctx, rurl))
				if overBudget() {
					skipped++
					break
				}
			}
		}
	}

	// Race-dialer outcome counters. The window
	// covers the last 5 minutes — wide enough to catch reconnect-storm
	// patterns without re-counting the same dial outcomes on every
	// 60s cycle. Caller can override via cfg.dialerWindowSeconds.
	if cfg.dialerCounters != nil {
		if overBudget() {
			skipped++
		} else {
			windowSeconds := cfg.dialerWindowSeconds
			if windowSeconds <= 0 {
				windowSeconds = 300 // 5 min default
			}
			if counters, ok := cfg.dialerCounters(ctx, windowSeconds); ok {
				report.RaceDialer = &counters
			}
		}
	}

	// MTU probe — interface-change-cached. Caller must pass a
	// closure (typically from MTUProberCachingByInterface) that
	// internally caches and only re-probes on Interface change.
	// MTU correlates with interface flips, not with the 60s loop —
	// per-cycle DF=on ping to the gateway adds cost on cellular /
	// Satlink uplinks that we don't get useful signal for.
	if cfg.mtuProbeFn != nil {
		if overBudget() {
			skipped++
		} else {
			target := network.GatewayIP
			if target != "" {
				mtu := cfg.mtuProbeFn(ctx, target, network.Interface)
				report.MTU = &mtu
			}
		}
	}

	// Ping each connected peer. Innermost loop — runs unconditionally
	// (legacy behavior + this is the most operationally important data).
	for _, p := range peers {
		m := types.PeerMeasurement{
			PeerName:  p.Name,
			PeerIP:    p.IP,
			Connected: p.Connected,
			Direct:    p.Direct,
		}

		if p.Connected {
			if result, pingErr := cfg.pingFn(ctx, p.IP, defaultPingCount); pingErr == nil {
				m.LatencyMs = result.AvgMs
				m.JitterMs = result.JitterMs
				m.LossPct = result.LossPct
				m.P50Ms = result.P50Ms
				m.P95Ms = result.P95Ms
				m.Samples = result.Samples
			}

			// Transport quality. Reads kernel TCP state for connections the
			// mesh is ALREADY carrying — generates no packets, so it is safe
			// in the unconditional inner loop alongside ping.
			//
			// This is the cheapest high-value signal snitch has. F-301 sat
			// undetected for the life of the fleet because latency, jitter
			// and loss all read normal while cwnd was pinned at 2. An error
			// here is non-fatal: a missing sample degrades the cycle, it does
			// not fail it.
			if tq, tqErr := TCPQualityFor(ctx, p.IP); tqErr == nil && tq != nil {
				m.Transport = tq
			}
		}

		report.Peers = append(report.Peers, m)
	}

	// CycleBudget always reported when set, so dashboards can graph it.
	if cfg.cycleBudget > 0 {
		actual := time.Since(cycleStart)
		report.CycleBudget = &types.CycleBudgetInfo{
			BudgetMs:   int(cfg.cycleBudget.Milliseconds()),
			ActualMs:   int(actual.Milliseconds()),
			Skipped:    skipped,
			OverBudget: actual > cfg.cycleBudget,
		}
	}

	return report, nil
}

// relayURLsFromStatus extracts the configured relay URL list from the
// netbird status JSON. Today the upstream type only exposes a count
// (Total/Available); the URL list is in the verbose `--detail` form
// which we don't currently parse. Until that lands, callers can pass
// an env-var override (SNITCH_RELAY_URLS, comma-separated) which this
// helper checks. Empty list = no relay probes this cycle.
//
// Pure-function-friendly via the env-var read; tests can set it.
func relayURLsFromStatus(_ *types.NetbirdStatusJSON) []string {
	raw := envRelayURLs()
	if raw == "" {
		return nil
	}
	out := []string{}
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// envRelayURLs is a thin wrapper around os.Getenv to keep
// relayURLsFromStatus pure-testable (caller can inject via env).
//
// We don't parse netbird's `--detail` output yet. When that lands,
// flip this to read from the status struct; remove the env-var path.
func envRelayURLs() string {
	return strings.TrimSpace(getenv("SNITCH_RELAY_URLS"))
}

// getenv is hookable for tests that want to inject env values.
var getenv = os.Getenv
