package probe

import (
	"context"
	"crypto/tls"
	"fmt"
	"math"
	"net"
	"time"
)

// TLSProbe connects to the domain on port 443 and inspects certificate expiry.
type TLSProbe struct{}

func init() {
	Register(&TLSProbe{})
}

func (p *TLSProbe) Category() Category { return CatTLS }

func (p *TLSProbe) Run(ctx context.Context, cfg *Config) []Result {
	addr := net.JoinHostPort(cfg.Domain, "443")

	dialer := tls.Dialer{
		Config: &tls.Config{
			ServerName: cfg.Domain,
		},
		NetDialer: &net.Dialer{
			Timeout: 10 * time.Second,
		},
	}

	start := time.Now()
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	elapsed := time.Since(start)

	if err != nil {
		return []Result{{
			Category:  CatTLS,
			Name:      "cert-expiry",
			Status:    Fail,
			Detail:    fmt.Sprintf("TLS connect to %s: %v", addr, err),
			Timestamp: time.Now().UTC(),
		}}
	}
	defer conn.Close()

	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		return []Result{{
			Category:  CatTLS,
			Name:      "cert-expiry",
			Status:    Fail,
			Detail:    "connection is not TLS",
			Timestamp: time.Now().UTC(),
		}}
	}

	state := tlsConn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return []Result{{
			Category:  CatTLS,
			Name:      "cert-expiry",
			Status:    Fail,
			Detail:    "no peer certificates presented",
			Timestamp: time.Now().UTC(),
		}}
	}

	cert := state.PeerCertificates[0]
	daysLeft := math.Floor(time.Until(cert.NotAfter).Hours() / 24)

	var status Status
	switch {
	case daysLeft < 0:
		status = Fail
	case daysLeft <= 14:
		status = Warn
	default:
		status = Pass
	}

	return []Result{{
		Category:  CatTLS,
		Name:      "cert-expiry",
		Status:    status,
		Detail:    fmt.Sprintf("%s expires %s (%.0f days)", cert.Subject.CommonName, cert.NotAfter.Format("2006-01-02"), daysLeft),
		LatencyMs: float64(elapsed.Milliseconds()),
		Timestamp: time.Now().UTC(),
		Metadata: map[string]any{
			"subject":    cert.Subject.CommonName,
			"issuer":     cert.Issuer.CommonName,
			"not_after":  cert.NotAfter.Format(time.RFC3339),
			"not_before": cert.NotBefore.Format(time.RFC3339),
			"days_left":  daysLeft,
		},
	}}
}
