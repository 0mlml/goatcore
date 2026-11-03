package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"net"
	"net/http"
	"strconv"
	"time"
)

// probe_net.go — the raw blackbox network probes the `probe` subcommand runs
// against a relay's PUBLIC IP. Direct ports of the inline python3 in
// relay-health-probe.sh (tcp_ok / stun_ok / quic_ok / tcp_rtt / the :443 curl).
// Stdlib only; no crypto handshake — these are liveness signals, not sessions.

// tcpOK reports whether a TCP connect to host:port succeeds within timeout.
// Port of the bash `tcp_ok` (exec 3<>/dev/tcp/…). timeout ~3s like the bash.
func tcpOK(host string, port int, timeout time.Duration) bool {
	c, err := net.DialTimeout("tcp", joinHostPort(host, port), timeout)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// tcpRTTms times a TCP connect and returns the connect-RTT in ms, or -1 on
// refuse/timeout. Port of the bash `tcp_rtt` — used for the :33073 WS/TCP RTT.
func tcpRTTms(host string, port int, timeout time.Duration) int {
	t0 := time.Now()
	c, err := net.DialTimeout("tcp", joinHostPort(host, port), timeout)
	if err != nil {
		return -1
	}
	ms := int(time.Since(t0).Milliseconds())
	_ = c.Close()
	return ms
}

// stunOK sends a real STUN binding request (RFC 5389) over UDP and reports
// whether a binding SUCCESS response comes back. Port of the bash `stun_ok`:
//   header = type(0x0001 binding-request) len(0) magic(0x2112A442) + 12B txid
//   success = >=20 bytes back with message type 0x0101 (binding response).
func stunOK(host string, port int, timeout time.Duration) bool {
	conn, err := net.DialTimeout("udp", joinHostPort(host, port), timeout)
	if err != nil {
		return false
	}
	defer conn.Close()

	var msg [20]byte
	binary.BigEndian.PutUint16(msg[0:2], 0x0001) // binding request
	binary.BigEndian.PutUint16(msg[2:4], 0)      // message length
	binary.BigEndian.PutUint32(msg[4:8], 0x2112A442)
	_, _ = rand.Read(msg[8:20]) // 96-bit transaction id

	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write(msg[:]); err != nil {
		return false
	}
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		return false
	}
	return n >= 20 && binary.BigEndian.Uint16(buf[0:2]) == 0x0101
}

// quicVN forces a QUIC Version Negotiation and returns (ok, rtt_ms). Port of the
// bash `quic_ok`. RFC 9000 §6/§17.2.1: we send a long-header Initial-shaped
// packet whose 4-byte VERSION is a RESERVED value (0x1a2a3a4a matches the
// 0x?a?a?a?a "force VN" pattern of §15), which a compliant server MUST answer
// with a Version Negotiation packet. The datagram is padded to 1200 bytes so it
// clears the server's anti-amplification minimum (a smaller one may be dropped).
// No TLS/crypto is needed, so this is a dependency-free genuine QUIC-liveness
// signal — NOT a bare UDP send (which can't tell "open" from "filtered").
//
// A VN packet is identified by: long-header form bit (0x80) set AND the 4-byte
// version field == 0 (RFC 9000 §17.2.1). No reply -> (false, -1), rendered
// amber/info (can't distinguish "UDP filtered" from "VN disabled").
func quicVN(host string, port int, timeout time.Duration) (bool, int) {
	conn, err := net.DialTimeout("udp", joinHostPort(host, port), timeout)
	if err != nil {
		return false, -1
	}
	defer conn.Close()

	// Long-header byte: header-form (0x80) + fixed bit (0x40). The low bits are
	// don't-care for a VN probe (the server keys off the reserved version).
	pkt := make([]byte, 0, 1200)
	pkt = append(pkt, 0xC0)                     // long header, fixed bit
	pkt = append(pkt, 0x1a, 0x2a, 0x3a, 0x4a)   // reserved version -> forces VN
	dcid := make([]byte, 8)
	scid := make([]byte, 8)
	_, _ = rand.Read(dcid)
	_, _ = rand.Read(scid)
	pkt = append(pkt, byte(len(dcid)))
	pkt = append(pkt, dcid...)
	pkt = append(pkt, byte(len(scid)))
	pkt = append(pkt, scid...)
	// Pad the datagram to the 1200-byte anti-amplification minimum.
	for len(pkt) < 1200 {
		pkt = append(pkt, 0x00)
	}

	_ = conn.SetDeadline(time.Now().Add(timeout))
	t0 := time.Now()
	if _, err := conn.Write(pkt); err != nil {
		return false, -1
	}
	buf := make([]byte, 2048)
	n, err := conn.Read(buf)
	if err != nil {
		return false, -1
	}
	rtt := int(time.Since(t0).Milliseconds())
	// Version Negotiation: long-header form bit set + 4-byte version == 0.
	ok := n >= 7 && buf[0]&0x80 != 0 &&
		buf[1] == 0 && buf[2] == 0 && buf[3] == 0 && buf[4] == 0
	if !ok {
		return false, -1
	}
	return true, rtt
}

// enrollResult is one :443 enrollment TLS attempt's outcome.
type enrollResult struct {
	ok    int    // 1 if the TLS tunnel answered a real HTTP status, else 0
	code  string // HTTP status text ("200", "404" …); "" when unreached
	tlsS  string // seconds to complete the TLS app-connect, as a string
	latMs int    // TCP-connect RTT in ms for this try, or -1
}

// enrollTLS runs ONE :443 enrollment probe against a relay, forcing the SNI
// host to resolve to the relay's IP (the Go equivalent of curl --resolve host:
// 443:ip -k). It mirrors the bash's curl -w '%{http_code}|%{time_appconnect}|
// %{time_connect}': ok when a real HTTP status comes back (not connection-
// refused/timeout), plus the TLS app-connect time and the TCP-connect latency.
//
// InsecureSkipVerify: the relay serves the goatnet's own enrollment cert for
// netbird.<goatnet>-backbone.net; we're checking tunnel liveness, not trust.
func enrollTLS(ip, sniHost string, timeout time.Duration) enrollResult {
	res := enrollResult{latMs: -1}

	// Force every dial onto the relay's IP:443 (like curl --resolve), timing the
	// raw TCP connect for the latency field. The app-connect (TLS) time is timed
	// around client.Do below.
	var connectMs int64 = -1
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			t0 := time.Now()
			c, err := (&net.Dialer{Timeout: timeout}).DialContext(ctx, "tcp", joinHostPort(ip, 443))
			if err == nil {
				connectMs = time.Since(t0).Milliseconds()
			}
			return c, err
		},
		TLSClientConfig: &tls.Config{
			ServerName:         sniHost,
			InsecureSkipVerify: true, //nolint:gosec // liveness probe, not a trust check
		},
		DisableKeepAlives: true,
	}
	client := &http.Client{Timeout: timeout, Transport: transport}

	t0 := time.Now()
	req, err := http.NewRequest(http.MethodGet, "https://"+sniHost+"/", nil)
	if err != nil {
		return res
	}
	resp, err := client.Do(req)
	if err != nil {
		// Reached the box at TCP level but the tunnel didn't answer -> not ok,
		// but still surface the connect latency (mirrors curl's time_connect).
		if connectMs >= 0 {
			res.latMs = int(connectMs)
		}
		return res
	}
	defer resp.Body.Close()
	// A real HTTP status back = the enrollment tunnel is alive (any code counts,
	// like the bash test `c != "000"`).
	res.ok = 1
	res.code = strconv.Itoa(resp.StatusCode)
	res.tlsS = strconv.FormatFloat(time.Since(t0).Seconds(), 'f', 6, 64)
	if connectMs >= 0 {
		res.latMs = int(connectMs)
	}
	return res
}

// joinHostPort formats host:port, bracketing IPv6 literals correctly.
func joinHostPort(host string, port int) string {
	return net.JoinHostPort(host, strconv.Itoa(port))
}
