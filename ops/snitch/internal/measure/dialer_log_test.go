package measure

import (
	"strings"
	"testing"
)

const sampleLog = `
2026-05-04T06:13:14.184Z INFO [relay: rels://r:33073] shared/relay/client/dialer/race_dialer.go:91: successfully dialed via: quic
2026-05-04T06:14:16.119Z INFO [relay: rels://r:33073] shared/relay/client/dialer/race_dialer.go:91: successfully dialed via: quic
2026-05-04T06:25:15.453Z INFO [relay: rels://r:33073] shared/relay/client/dialer/race_dialer.go:91: successfully dialed via: WS
2026-05-04T06:30:07.144Z WARN shared/signal/client/grpc.go:178: disconnected from the Signal service
2026-05-04T06:42:19.138Z ERRO shared/relay/client/dialer/quic/quic.go:66: failed to dial via quic: CRYPTO_ERROR 0x178
2026-05-04T06:43:00.519Z INFO race_dialer.go:91: successfully dialed via: quic
2026-05-04T06:43:01.000Z ERRO race_dialer.go:79: failed to dial via WS: connection refused
2026-05-04T06:43:02.000Z INFO race_dialer.go:91: successfully dialed via: WS
2026-05-04T06:43:03.000Z INFO some unrelated log line
`

func TestCountDialerOutcomes(t *testing.T) {
	out := CountDialerOutcomes(strings.NewReader(sampleLog), 600)
	if out.WindowSeconds != 600 {
		t.Errorf("WindowSeconds = %d; want 600", out.WindowSeconds)
	}
	if out.QUICSuccess != 3 {
		t.Errorf("QUICSuccess = %d; want 3", out.QUICSuccess)
	}
	if out.WSSuccess != 2 {
		t.Errorf("WSSuccess = %d; want 2", out.WSSuccess)
	}
	if out.QUICFailure != 1 {
		t.Errorf("QUICFailure = %d; want 1", out.QUICFailure)
	}
	if out.WSFailure != 1 {
		t.Errorf("WSFailure = %d; want 1", out.WSFailure)
	}
}

func TestCountDialerOutcomes_Empty(t *testing.T) {
	out := CountDialerOutcomes(strings.NewReader(""), 60)
	if out.QUICSuccess+out.WSSuccess+out.QUICFailure+out.WSFailure != 0 {
		t.Errorf("empty log should produce zero counters; got %+v", out)
	}
}

func TestCountDialerOutcomes_LongLines(t *testing.T) {
	// One very long line shouldn't blow up the scanner; ensure our
	// buffer setup tolerates 64KB+ lines.
	long := strings.Repeat("x", 70_000) + " successfully dialed via: quic"
	out := CountDialerOutcomes(strings.NewReader(long), 60)
	if out.QUICSuccess != 1 {
		t.Errorf("very-long-line dial entry not counted; got %+v", out)
	}
}

func TestTransportLabel(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"quic", "quic"},
		{"QUIC", "quic"},
		{"WS", "ws"},
		{"ws:", "ws"},
		{"WebSocket", "ws"},
		{"http3", "http3"}, // unknown — passes through
	}
	for _, tt := range tests {
		if got := transportLabel(tt.input); got != tt.want {
			t.Errorf("transportLabel(%q) = %q; want %q", tt.input, got, tt.want)
		}
	}
}
