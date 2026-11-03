package measure

import (
	"context"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"testing"
	"time"
)

func TestParseRelayURL(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantHost  string
		wantPort  int
		wantError bool
	}{
		{"rels-with-port", "rels://relay.example:33073", "relay.example", 33073, false},
		{"rels-default-port", "rels://relay.example", "relay.example", 33073, false},
		{"https-with-port", "https://relay.example:443", "relay.example", 443, false},
		{"ipv4-with-port", "rels://10.0.0.1:33073", "10.0.0.1", 33073, false},
		{"empty", "", "", 0, true},
		{"no-host", "rels://:33073", "", 0, true},
		{"bad-port", "rels://relay.example:notaport", "", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, port, err := ParseRelayURL(tt.input)
			if tt.wantError {
				if err == nil {
					t.Fatalf("expected error, got nil; host=%q port=%d", host, port)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if host != tt.wantHost || port != tt.wantPort {
				t.Errorf("got host=%q port=%d; want host=%q port=%d", host, port, tt.wantHost, tt.wantPort)
			}
		})
	}
}

func TestClassifyTransport(t *testing.T) {
	tests := []struct {
		name          string
		udp, tcp      int
		wantTransport string
	}{
		{"only-udp", 1, 0, "quic"},
		{"only-tcp", 0, 1, "ws"},
		{"both-udp-wins", 1, 1, "quic"}, // UDP-presence is dispositive
		{"none", 0, 0, "none"},
		{"multi-udp", 3, 0, "quic"},
		{"negative-defensive", -1, 0, "none"}, // never happens but defensive
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyTransport(tt.udp, tt.tcp); got != tt.wantTransport {
				t.Errorf("ClassifyTransport(udp=%d, tcp=%d) = %q; want %q",
					tt.udp, tt.tcp, got, tt.wantTransport)
			}
		})
	}
}

func TestDaysUntilExpiry(t *testing.T) {
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		notAfter time.Time
		want     int
	}{
		{"exactly-30-days", now.Add(30 * 24 * time.Hour), 30},
		{"23h59m-rounds-up", now.Add(23*time.Hour + 59*time.Minute), 1},
		{"30d-and-1h-rounds-up", now.Add(30*24*time.Hour + 1*time.Hour), 31},
		{"7-day-warning-edge", now.Add(7 * 24 * time.Hour), 7},
		{"already-expired-1h", now.Add(-1 * time.Hour), -1},
		{"already-expired-2d", now.Add(-2 * 24 * time.Hour), -2},
		{"expired-26h", now.Add(-26 * time.Hour), -2}, // 26h past = entered second day
		{"now", now, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DaysUntilExpiry(tt.notAfter, now); got != tt.want {
				t.Errorf("DaysUntilExpiry(%s) = %d; want %d", tt.notAfter, got, tt.want)
			}
		})
	}
}

// stubProbe lets tests inject deterministic transport / reachability /
// cert results without touching the OS.
type stubProbe struct {
	udpCount     int
	tcpCount     int
	udpReachable bool
	tcpReachable bool
	certs        []*x509.Certificate
	probeErr     error
	reachableErr error
	certErr      error
}

func (s *stubProbe) sock(_ context.Context, _ string, _ int, proto string) (int, error) {
	if s.probeErr != nil {
		return 0, s.probeErr
	}
	if proto == "udp" {
		return s.udpCount, nil
	}
	return s.tcpCount, nil
}

func (s *stubProbe) reach(_ context.Context, _ string, _ int, proto string) (bool, error) {
	if s.reachableErr != nil {
		return false, s.reachableErr
	}
	if proto == "udp" {
		return s.udpReachable, nil
	}
	return s.tcpReachable, nil
}

func (s *stubProbe) cert(_ context.Context, _ string, _ int) ([]*x509.Certificate, error) {
	if s.certErr != nil {
		return nil, s.certErr
	}
	return s.certs, nil
}

func TestProbeRelay(t *testing.T) {
	now := time.Now()
	leaf := &x509.Certificate{
		Subject:      pkix.Name{CommonName: "relay.example"},
		NotAfter:     now.Add(45 * 24 * time.Hour),
		SerialNumber: big.NewInt(1),
	}

	t.Run("happy-path-quic", func(t *testing.T) {
		stub := &stubProbe{
			udpCount: 1, tcpCount: 0,
			udpReachable: true, tcpReachable: true,
			certs: []*x509.Certificate{leaf},
		}
		cfg := RelayProbeConfig{
			SocketProber:  stub.sock,
			CertProber:    stub.cert,
			PortReachable: stub.reach,
		}
		out := ProbeRelay(context.Background(), "rels://relay.example:33073", cfg)
		if out.Transport != "quic" {
			t.Errorf("Transport = %q; want quic", out.Transport)
		}
		if !out.UDPReachable || !out.TCPReachable {
			t.Errorf("UDPReachable=%v TCPReachable=%v; want both true", out.UDPReachable, out.TCPReachable)
		}
		if out.TLSDaysUntil < 44 || out.TLSDaysUntil > 46 {
			t.Errorf("TLSDaysUntil = %d; want ~45", out.TLSDaysUntil)
		}
		if out.TLSCertSubject == "" {
			t.Errorf("TLSCertSubject should be non-empty when cert is provided")
		}
	})

	t.Run("ws-only", func(t *testing.T) {
		stub := &stubProbe{
			udpCount: 0, tcpCount: 1,
			udpReachable: false, tcpReachable: true,
			certs: []*x509.Certificate{leaf},
		}
		cfg := RelayProbeConfig{
			SocketProber:  stub.sock,
			CertProber:    stub.cert,
			PortReachable: stub.reach,
		}
		out := ProbeRelay(context.Background(), "rels://relay.example:33073", cfg)
		if out.Transport != "ws" {
			t.Errorf("Transport = %q; want ws", out.Transport)
		}
		if out.UDPReachable {
			t.Errorf("UDPReachable should be false (firewall scenario)")
		}
	})

	t.Run("everything-broken", func(t *testing.T) {
		stub := &stubProbe{
			udpCount: 0, tcpCount: 0,
			udpReachable: false, tcpReachable: false,
			certErr: errors.New("dial failed"),
		}
		cfg := RelayProbeConfig{
			SocketProber:  stub.sock,
			CertProber:    stub.cert,
			PortReachable: stub.reach,
		}
		out := ProbeRelay(context.Background(), "rels://relay.example:33073", cfg)
		if out.Transport != "none" {
			t.Errorf("Transport = %q; want none", out.Transport)
		}
		if out.TLSDaysUntil != -1 {
			t.Errorf("TLSDaysUntil = %d; want -1 sentinel", out.TLSDaysUntil)
		}
	})

	t.Run("invalid-url-yields-none", func(t *testing.T) {
		stub := &stubProbe{}
		cfg := RelayProbeConfig{
			SocketProber:  stub.sock,
			CertProber:    stub.cert,
			PortReachable: stub.reach,
		}
		out := ProbeRelay(context.Background(), "", cfg)
		if out.Transport != "none" || out.TLSDaysUntil != -1 {
			t.Errorf("invalid URL should produce none/-1; got transport=%q tlsdays=%d",
				out.Transport, out.TLSDaysUntil)
		}
	})
}

// TestProbeRelay_CertCache asserts the do-no-harm posture: with
// CertProbeTTL=1h, a second ProbeRelay call within the TTL must NOT
// invoke the cert prober. This is the load-bearing fratricide guard
// for the relay's TLS-handshake CPU at fleet scale.
func TestProbeRelay_CertCache(t *testing.T) {
	now := time.Now()
	leaf := &x509.Certificate{
		Subject:      pkix.Name{CommonName: "relay.example"},
		NotAfter:     now.Add(45 * 24 * time.Hour),
		SerialNumber: big.NewInt(1),
	}

	certProbeCalls := 0
	countingCertProber := func(_ context.Context, _ string, _ int) ([]*x509.Certificate, error) {
		certProbeCalls++
		return []*x509.Certificate{leaf}, nil
	}
	stub := &stubProbe{
		udpCount: 1, tcpCount: 0,
		udpReachable: true, tcpReachable: true,
	}
	cfg := RelayProbeConfig{
		SocketProber:  stub.sock,
		CertProber:    countingCertProber,
		PortReachable: stub.reach,
		CertProbeTTL:  1 * time.Hour,
		certCache:     newCertCacheMap(),
	}

	// First call: cache miss → prober runs.
	out1 := ProbeRelay(context.Background(), "rels://relay.example:33073", cfg)
	if certProbeCalls != 1 {
		t.Fatalf("first call: certProbeCalls = %d; want 1", certProbeCalls)
	}
	if out1.TLSDaysUntil < 44 || out1.TLSDaysUntil > 46 {
		t.Errorf("first call TLSDaysUntil = %d; want ~45", out1.TLSDaysUntil)
	}

	// Second call within TTL: cache hit → prober must NOT run.
	out2 := ProbeRelay(context.Background(), "rels://relay.example:33073", cfg)
	if certProbeCalls != 1 {
		t.Errorf("cache miss on second call within TTL: certProbeCalls = %d; want still 1", certProbeCalls)
	}
	if out2.TLSDaysUntil != out1.TLSDaysUntil {
		t.Errorf("cached TLSDaysUntil mismatch: %d vs %d", out2.TLSDaysUntil, out1.TLSDaysUntil)
	}
	if out2.TLSCertSubject != out1.TLSCertSubject {
		t.Errorf("cached subject mismatch: %q vs %q", out2.TLSCertSubject, out1.TLSCertSubject)
	}
}

// TestProbeRelay_CertCacheDisabled asserts the test-friendly path:
// CertProbeTTL=0 disables caching (every call runs the prober).
func TestProbeRelay_CertCacheDisabled(t *testing.T) {
	leaf := &x509.Certificate{
		Subject:      pkix.Name{CommonName: "relay.example"},
		NotAfter:     time.Now().Add(45 * 24 * time.Hour),
		SerialNumber: big.NewInt(1),
	}
	calls := 0
	cfg := RelayProbeConfig{
		SocketProber: (&stubProbe{udpCount: 1}).sock,
		CertProber: func(_ context.Context, _ string, _ int) ([]*x509.Certificate, error) {
			calls++
			return []*x509.Certificate{leaf}, nil
		},
		PortReachable: (&stubProbe{tcpReachable: true}).reach,
		// CertProbeTTL: 0 → cache disabled
	}
	ProbeRelay(context.Background(), "rels://relay.example:33073", cfg)
	ProbeRelay(context.Background(), "rels://relay.example:33073", cfg)
	if calls != 2 {
		t.Errorf("with TTL=0, expected 2 prober calls; got %d", calls)
	}
}

// TestProbeRelay_UDPReachableFromTransport asserts the do-no-harm
// derivation: UDPReachable is derived from socket-count classification,
// NOT from an active `nc -zu` probe. This eliminates the unsolicited
// UDP packets we used to send to the relay's QUIC listener every
// cycle.
func TestProbeRelay_UDPReachableFromTransport(t *testing.T) {
	tests := []struct {
		name             string
		udpCount         int
		tcpCount         int
		wantUDPReachable bool
	}{
		{"quic-active", 1, 0, true},
		{"ws-only-implies-udp-broken", 0, 1, false},
		{"both-active-quic-wins", 1, 1, true},
		{"none", 0, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubProbe{udpCount: tt.udpCount, tcpCount: tt.tcpCount, tcpReachable: true}
			cfg := RelayProbeConfig{
				SocketProber:  stub.sock,
				CertProber:    stub.cert,
				PortReachable: stub.reach,
			}
			out := ProbeRelay(context.Background(), "rels://relay.example:33073", cfg)
			if out.UDPReachable != tt.wantUDPReachable {
				t.Errorf("UDPReachable = %v; want %v (transport=%q)",
					out.UDPReachable, tt.wantUDPReachable, out.Transport)
			}
		})
	}
}
