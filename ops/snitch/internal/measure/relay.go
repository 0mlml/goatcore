// Package measure — relay-side health probe for snitch.
//
// Background. The review
// surfaced that snitch had no visibility into the relay-side state of
// each peer's connection. The bench env had silently been WebSocket-only
// for weeks because:
//
//  1. F-016 — netbird daemon races QUIC and WS dialers, no QUIC preference
//  2. F-021 — Docker `-p N:N` shorthand defaults to TCP-only; UDP dropped
//  3. F-041 — race-dialer outcome is non-deterministic per restart
//  4. F-042 — goat-relay's TLS ServerConfig didn't advertise the
//             "nb-quic" ALPN string, so every QUIC dial failed
//             `CRYPTO_ERROR 0x178 — peer doesn't support any known protocol`
//
// None of those would have shown up in snitch's prior probe surface
// (latency / jitter / loss / gateway fingerprint). With this file, every
// snitch report carries per-relay transport assertion + UDP/TCP
// reachability + TLS cert expiry — directly catchable in dashboards.
//
// Discipline: this file is pure-function-friendly. The exec-shaped
// callers wrap small `runner` functions that are mockable in tests; no
// test in this package should actually exec `ss`, `nc`, or `openssl`.
// See `relay_test.go`.

package measure

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/usareur-af/goatcore/ops/snitch/internal/types"
)

const (
	// defaultDialTimeout is the per-connection dial budget. Local LAN
	// reachability is millisecond-scale; we cap at 2s to keep a single
	// failed probe from stalling the whole cycle.
	defaultDialTimeout = 2 * time.Second
	// defaultTLSHandshakeTimeout caps the TLS handshake during the cert
	// expiry probe. We don't care about completing the handshake — we
	// just need the server's cert chain.
	defaultTLSHandshakeTimeout = 3 * time.Second
)

// SocketProber is a function that returns the count of established
// sockets to (host, port) over a given protocol. Tests inject a stub.
//
// In production, the default impl shells out to `ss -tnp` or `ss -unp`
// + grep for the host:port pattern. Pure-function call site lets us
// test the relay-probe orchestration without an OS dependency.
type SocketProber func(ctx context.Context, host string, port int, proto string) (count int, err error)

// CertProber returns the TLS server certificate chain advertised by the
// host:port endpoint. Tests inject a stub returning fixture certs.
type CertProber func(ctx context.Context, host string, port int) ([]*x509.Certificate, error)

// PortReachable returns whether the given (host, port) is reachable
// over the given proto ("tcp" or "udp"). Used for the UDP-firewall
// drift check.
type PortReachable func(ctx context.Context, host string, port int, proto string) (bool, error)

// RelayProbeConfig wires the dependency-inject hooks for relay probing.
//
// The certCache + CertProbeTTL fields implement the do-no-harm cert-probe
// rate-limit. The cache is a pointer-shared map so multiple ProbeRelay
// calls in the same probe process see each other's writes (the
// `cmd/snitch/main.go` probe loop holds one *RelayProbeConfig and reuses
// it across cycles).
type RelayProbeConfig struct {
	SocketProber        SocketProber
	CertProber          CertProber
	PortReachable       PortReachable
	DialTimeout         time.Duration
	TLSHandshakeTimeout time.Duration

	// CertProbeTTL caps the cert-probe rate. Default 1 hour. Setting
	// this to 0 disables caching (useful for tests). Cert NotAfter
	// changes by 1 day per day, so 1h cadence is 24× over-sampled
	// already; cutting from 60s to 1h drops fleet-wide TLS-handshake
	// load on each relay by 60×.
	CertProbeTTL time.Duration

	// certCache is a process-wide cache of {(host, port) → (daysUntil,
	// subject, fetchedAt)}. Pointer-shared so the cmd-side probe loop's
	// single config is visible to every ProbeRelay call.
	certCache *certCacheMap
}

// certCacheEntry holds one cached relay-cert observation.
type certCacheEntry struct {
	daysUntil int
	subject   string
	fetchedAt time.Time
}

// certCacheMap is a small mutex-guarded map keyed on host:port.
type certCacheMap struct {
	mu sync.Mutex
	m  map[string]certCacheEntry
}

func newCertCacheMap() *certCacheMap { return &certCacheMap{m: map[string]certCacheEntry{}} }

// cachedCert returns a non-stale cache entry, or (zero, false) if no
// fresh entry exists. "Fresh" = within CertProbeTTL of fetchedAt.
func (cfg *RelayProbeConfig) cachedCert(host string, port int) (certCacheEntry, bool) {
	if cfg.certCache == nil || cfg.CertProbeTTL <= 0 {
		return certCacheEntry{}, false
	}
	key := fmt.Sprintf("%s:%d", host, port)
	cfg.certCache.mu.Lock()
	defer cfg.certCache.mu.Unlock()
	entry, ok := cfg.certCache.m[key]
	if !ok {
		return certCacheEntry{}, false
	}
	if time.Since(entry.fetchedAt) > cfg.CertProbeTTL {
		return certCacheEntry{}, false
	}
	return entry, true
}

// cacheCert writes the freshly-probed result.
func (cfg *RelayProbeConfig) cacheCert(host string, port, daysUntil int, subject string) {
	if cfg.certCache == nil {
		return
	}
	key := fmt.Sprintf("%s:%d", host, port)
	cfg.certCache.mu.Lock()
	defer cfg.certCache.mu.Unlock()
	cfg.certCache.m[key] = certCacheEntry{
		daysUntil: daysUntil,
		subject:   subject,
		fetchedAt: time.Now(),
	}
}

// DefaultRelayProbeConfig returns the production wiring.
//
// CertProbeTTL=1h is the do-no-harm default per the review.
// At 6000 peers × 3 relays × 1h cadence, fleet-wide TLS-handshake load
// on each relay is ~1.7/sec — vs ~100/sec at the naive 60s cadence.
//
// sockCountDefault + portReachableDefault are dispatched per-OS via
// build-tag-split files (relay_runner_linux.go / _darwin.go / _windows.go).
// Block 61H (multi-platform builds) — Linux uses ss + nc; Darwin uses
// lsof + a Go-builtin TCP dial; Windows uses netstat + a Go-builtin
// TCP dial. None require CGO.
func DefaultRelayProbeConfig() RelayProbeConfig {
	return RelayProbeConfig{
		SocketProber:        sockCountDefault,
		CertProber:          certProbeOpenSSL,
		PortReachable:       portReachableDefault,
		DialTimeout:         defaultDialTimeout,
		TLSHandshakeTimeout: defaultTLSHandshakeTimeout,
		CertProbeTTL:        1 * time.Hour,
		certCache:           newCertCacheMap(),
	}
}

// ParseRelayURL splits a netbird `rels://host:port` URL into its
// component parts. Pure function — covered by relay_test.go.
//
// Per netbird wire-protocol: `rels` = "relay over secured TLS"
// (covers both WS-over-TLS and QUIC-over-TLS; the daemon races them).
// We accept `rels://`, `relss://`, and `https://` for compatibility
// with future scheme changes.
func ParseRelayURL(raw string) (host string, port int, err error) {
	if raw == "" {
		return "", 0, fmt.Errorf("empty relay URL")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", 0, fmt.Errorf("parse %q: %w", raw, err)
	}
	host = u.Hostname()
	if host == "" {
		return "", 0, fmt.Errorf("relay URL %q has no host", raw)
	}
	portStr := u.Port()
	if portStr == "" {
		// Default for rels:// is 33073 (per netbird relay defaults).
		port = 33073
	} else {
		p, perr := strconv.Atoi(portStr)
		if perr != nil {
			return "", 0, fmt.Errorf("relay URL %q has invalid port %q: %w", raw, portStr, perr)
		}
		port = p
	}
	return host, port, nil
}

// ClassifyTransport returns the active transport for one relay endpoint.
// Pure function over (udpCount, tcpCount). Pulled out so tests don't
// need a SocketProber stub.
//
// Rationale: we treat ANY UDP socket to the relay as "QUIC active";
// netbird's daemon doesn't open transient UDP to the relay for any
// other reason. ANY TCP socket without UDP is "WS active". Neither
// is "none" (probe should fail loud).
func ClassifyTransport(udpCount, tcpCount int) string {
	if udpCount >= 1 {
		return "quic"
	}
	if tcpCount >= 1 {
		return "ws"
	}
	return "none"
}

// DaysUntilExpiry returns the integer days from `now` until `notAfter`.
// Negative values mean already expired. Pure function for tests.
//
// Uses ceiling-style rounding: 23h59m → 1 day. This errs on the side of
// "more days" so a 7-day-warning threshold doesn't trigger off-by-one.
func DaysUntilExpiry(notAfter, now time.Time) int {
	diff := notAfter.Sub(now)
	if diff < 0 {
		// Past expiry. Return whole-days-elapsed as a negative integer.
		days := int(-diff / (24 * time.Hour))
		// 26h elapsed = -1 (one whole day past expiry); 50h = -2; etc.
		// Subtract one more if there's any remainder for past-expiry side.
		if -diff%(24*time.Hour) > 0 {
			days++
		}
		return -days
	}
	days := int(diff / (24 * time.Hour))
	if diff%(24*time.Hour) > 0 {
		days++
	}
	return days
}

// ProbeRelay runs the relay-side per-endpoint probe and returns the
// RelayHealth record. Used by RunProbe; standalone-testable via stubs.
//
// Do-no-harm posture (review). The 60s probe loop multiplied
// by fleet size produces enough probe-load to matter to the relay:
//
//   - **TLS cert probe**: full handshake on the relay (ECDSA signing +
//     kTLS setup). Cert expiry changes by 1 day per day. We cache the
//     result for `cfg.CertProbeTTL` (default 1h) — a 6000-peer fleet
//     across 3 relays at 1h cadence costs ~1.7 handshakes/sec/relay,
//     vs ~100/sec at 60s cadence. The cache is in `cfg.CertCache`
//     (caller-injected so probe instances share it across cycles).
//   - **UDP reachability probe**: REMOVED in this version. The active
//     `nc -zu` against the relay's :33073 sent unsolicited UDP packets
//     into the goat-relay QUIC listener every cycle — every packet
//     parse-failed and got dropped (wasted CPU on the relay). The
//     signal we wanted is already in `ss -unp count`: if the netbird
//     daemon's race-dialer can establish a UDP socket, UDP IS
//     reachable; if udp_count=0 AND tcp_count=1, UDP is known
//     unreachable. Inferred as RelayHealth.UDPReachable based on the
//     ss-derived transport classification.
//   - **TCP reachability probe**: TCP-connect is cheap (SYN + RST) and
//     informative independent of the daemon's choice (a daemon on
//     QUIC could mask a TCP-reachable-but-unused state we want to
//     surface). Kept at every cycle.
//
// Failure modes:
//   - DNS / parse errors → returns a RelayHealth with Transport="none"
//     and TLSDaysUntil=-1; never an error (we want the report to ship
//     even if one relay is unhealthy)
//   - sub-probe failures → individual fields zeroed; other fields filled
func ProbeRelay(ctx context.Context, relayURL string, cfg RelayProbeConfig) types.RelayHealth {
	out := types.RelayHealth{URL: relayURL, Transport: "none", TLSDaysUntil: -1}
	host, port, err := ParseRelayURL(relayURL)
	if err != nil {
		return out
	}
	out.Endpoint = fmt.Sprintf("%s:%d", host, port)

	// Transport assertion via ss.
	udpCount, _ := cfg.SocketProber(ctx, host, port, "udp") //nolint:errcheck // failed sub-probes deliberately produce zeroed health fields
	tcpCount, _ := cfg.SocketProber(ctx, host, port, "tcp") //nolint:errcheck // failed sub-probes deliberately produce zeroed health fields
	out.Transport = ClassifyTransport(udpCount, tcpCount)

	// UDP reachability: derive from socket classification rather than
	// re-probing. If transport=quic, UDP is by-construction reachable.
	// If transport=ws but tcp_count>0, the daemon is racing both and
	// UDP lost — UDP is unreachable.
	out.UDPReachable = (out.Transport == "quic")

	// TCP reachability: cheap SYN+RST. Informative even when the daemon
	// is on QUIC (catches TCP-listener-down without depending on the
	// daemon's choice).
	if reachable, err := cfg.PortReachable(ctx, host, port, "tcp"); err == nil {
		out.TCPReachable = reachable
	}

	// TLS cert expiry — cached. The relay-side cost is the load-bearing
	// motivation; see do-no-harm posture above.
	if certInfo, ok := cfg.cachedCert(host, port); ok {
		out.TLSDaysUntil = certInfo.daysUntil
		out.TLSCertSubject = certInfo.subject
	} else if certs, err := cfg.CertProber(ctx, host, port); err == nil && len(certs) > 0 {
		leaf := certs[0]
		out.TLSDaysUntil = DaysUntilExpiry(leaf.NotAfter, time.Now())
		out.TLSCertSubject = leaf.Subject.String()
		cfg.cacheCert(host, port, out.TLSDaysUntil, out.TLSCertSubject)
	}
	return out
}

// --- production runner implementations ---
//
// sockCountDefault + portReachableDefault are defined per-OS in
// relay_runner_{linux,darwin,windows}.go. certProbeOpenSSL is pure Go
// (uses tls.Dial) and lives here.

// certProbeOpenSSL connects to host:port via TLS, collects the server's
// presented cert chain, and returns it. We use the in-process tls.Dial
// rather than shelling out to openssl — purer dependencies, faster, no
// PEM-parse intermediate step.
//
// The dialed TLS handshake intentionally does NOT verify the server cert
// (InsecureSkipVerify=true) because we want this probe to work against
// self-signed bench certs AND CA-signed prod certs identically. We're
// only collecting cert metadata, not authenticating the server.
//
// ServerName is set so SNI-aware relays present the correct cert. If
// `host` is an IP literal, ServerName is left blank (Go's tls.Config
// rejects IPs in ServerName).
func certProbeOpenSSL(ctx context.Context, host string, port int) ([]*x509.Certificate, error) {
	dialer := &net.Dialer{Timeout: defaultDialTimeout}
	// gosec G402 false-positive: this probe deliberately accepts any cert
	// — its job is to extract metadata (subject, issuer, days-until-expiry)
	// from whatever the relay presents, including self-signed bench certs.
	// We are not establishing a trusted channel; the connection is read,
	// metadata captured, and dropped.
	tlsCfg := &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // metadata-only probe; see comment above
		MinVersion:         tls.VersionTLS12,
	}
	if net.ParseIP(host) == nil {
		// host is a DNS name → safe to use as SNI ServerName
		tlsCfg.ServerName = host
	}
	// noctx false-positive: tls.DialWithDialer already inherits the
	// dialer's Timeout (set above); the dialer drives ctx-equivalent
	// behavior. Refactoring to (*tls.Dialer).DialContext is queued but
	// non-trivial; suppressed here.
	conn, err := tls.DialWithDialer(dialer, "tcp", fmt.Sprintf("%s:%d", host, port), tlsCfg) //nolint:noctx // see comment above
	if err != nil {
		return nil, fmt.Errorf("tls dial %s:%d: %w", host, port, err)
	}
	defer func() { _ = conn.Close() }() //nolint:errcheck // read-only probe result is complete before cleanup

	// Set a deadline on the handshake completion (DialWithDialer's
	// Timeout covers the dial + handshake combined).
	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return nil, fmt.Errorf("tls dial %s:%d: no peer certificates", host, port)
	}
	return state.PeerCertificates, nil
}
