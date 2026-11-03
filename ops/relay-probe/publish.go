package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type publishConfig struct {
	relayName   string
	ingestURL   string // primary (site mgmt host), reached via the relay's own rathole :443 hairpin
	ingestHost  string // Host header / TLS SNI for the primary
	token       string
	ingestURL2  string // optional DIRECT second target (the cloud box)
	token2      string
	container   string
	histKeep    int
	stateDir    string
	peersWindow int
	statsURL    string // relay white-box stats endpoint (ADR 1055); "" = skip
}

func publishConfigFromEnv() (publishConfig, error) {
	c := publishConfig{
		relayName:   os.Getenv("RELAY_NAME"),
		ingestURL:   os.Getenv("RELAY_METRICS_INGEST_URL"),
		token:       os.Getenv("RELAY_METRICS_BEARER_TOKEN"),
		ingestHost:  os.Getenv("RELAY_METRICS_INGEST_HOST"),
		ingestURL2:  os.Getenv("RELAY_METRICS_INGEST_URL2"),
		token2:      os.Getenv("RELAY_METRICS_BEARER_TOKEN2"),
		container:   envOr("GOAT_RELAY_CONTAINER", "goat-relay"),
		histKeep:    envInt("HIST_KEEP", 40),
		stateDir:    envOr("STATE_DIR", "/var/lib/relay-metrics"),
		peersWindow: envInt("PEERS_WINDOW_MIN", 30),
		statsURL:    os.Getenv("RELAY_STATS_URL"), // e.g. http://127.0.0.1:9098/; unset = skip
	}
	if c.relayName == "" || c.ingestURL == "" || c.token == "" {
		return c, fmt.Errorf("RELAY_NAME, RELAY_METRICS_INGEST_URL and RELAY_METRICS_BEARER_TOKEN are required")
	}
	if c.ingestHost == "" {
		c.ingestHost = hostFromURL(c.ingestURL)
	}
	if c.token2 == "" {
		c.token2 = c.token
	}
	return c, nil
}

func runPublish() int {
	c, err := publishConfigFromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, "relay-probe publish:", err)
		return 2
	}
	_ = os.MkdirAll(c.stateDir, 0o755)

	iface := defaultIface()
	rxBps, txBps, rxPps, txPps := throughput(iface, c.stateDir, c.relayName)
	peers := peersRecent(c.container, c.peersWindow)

	deep := Deep{
		Rathole:        serviceActive("rathole-server.service"),
		Watchdog:       serviceActive("rathole-watchdog.timer"),
		Heals1h:        healsLastHour(),
		CoturnListen:   countUDPListen("3478"),
		RelayUp:        relayRunning(c.container),
		DataplaneListn: countUDPListen("33073"),
		PeersRecent:    peers,
		PeersWindowMin: c.peersWindow,
		CPUPct:         dockerStatPct(c.container, "CPUPerc"),
		MemPct:         dockerStatPct(c.container, "MemPerc"),
		DiskPct:        diskPctRoot(),
		MaxLogMB:       maxContainerLogMB(),
		RxBps:          rxBps, TxBps: txBps, RxPps: rxPps, TxPps: txPps,
	}
	deep.History = updateHistory(c, deep, peers)

	// Fetch the relay's white-box stats endpoint (ADR 1055 §2) ONCE, same as
	// metrics-publish.sh — it feeds both the per-peer distribution below and
	// the delivery block's drops/conns split. Optional + best-effort: a relay
	// without --stats-addr (c.statsURL == "") just omits everything it feeds;
	// a fetch failure drops to the same "no data" shape, never fails the
	// publish.
	var statsDoc *relayStatsDoc
	if c.statsURL != "" {
		statsDoc = fetchRelayStats(c.statsURL)
	}
	// Per-peer distribution, rate-diffed like the NIC counters.
	if statsDoc != nil {
		deep.Peers = peerStats(statsDoc, c.stateDir, c.relayName)
	}

	// F-269 auth-reject + F-275 delivery (#467): ported from metrics-publish.sh
	// so the Go publisher emits the fields RelayPeerAuthRejected and the
	// delivery card depend on. Both share one 5-minute docker-logs window,
	// like the bash.
	const authDeliveryWindowMin = 5
	al, logErr := dockerLogs(c.container, strconv.Itoa(authDeliveryWindowMin)+"m")
	setDeliveryMeasurements(&deep, al, logErr, statsDoc, authDeliveryWindowMin)

	body, _ := json.Marshal(PublishEnvelope{PublishedAt: time.Now().Unix(), Deep: deep})

	// Primary: the site ingest, forced through THIS relay's rathole :443 hairpin
	// (dial 127.0.0.1:443, present Host/SNI = ingestHost). Deterministic; no
	// dependence on public DNS round-robin — mirrors the bash `--resolve`.
	primaryCode := post(hairpinClient(c.ingestHost), c.ingestURL+"/"+c.relayName, c.token, body)
	fmt.Fprintf(os.Stderr, "relay-metrics: %s -> %s http=%d rx=%s tx=%s peers=%d\n",
		c.relayName, c.ingestURL, primaryCode, showRate(rxBps), showRate(txBps), peers)

	// Optional second target: a DIRECT (non-hairpin) ingest — the cloud box on
	// its public :443 — so both dashboards get throughput parity. Best-effort:
	// never gates the exit code.
	if c.ingestURL2 != "" {
		code2 := post(directClient(), c.ingestURL2+"/"+c.relayName, c.token2, body)
		fmt.Fprintf(os.Stderr, "relay-metrics: %s -> %s http=%d (direct, best-effort)\n",
			c.relayName, c.ingestURL2, code2)
	}

	if primaryCode == 200 || primaryCode == 204 {
		return 0
	}
	return 1
}

// updateHistory appends the current rate sample to stateDir/history/<name>.tsv,
// trims to histKeep, and returns the ring as [t, rx, tx, rxpps, txpps, peers].
func updateHistory(c publishConfig, d Deep, peers int) [][6]int64 {
	hdir := filepath.Join(c.stateDir, "history")
	_ = os.MkdirAll(hdir, 0o755)
	hfile := filepath.Join(hdir, c.relayName+".tsv")

	var rows []string
	if b, err := os.ReadFile(hfile); err == nil {
		rows = strings.Split(strings.TrimSpace(string(b)), "\n")
	}
	if d.RxBps != nil { // only append grounded samples
		rows = append(rows, strings.Join([]string{
			strconv.FormatInt(time.Now().Unix(), 10),
			strconv.FormatInt(*d.RxBps, 10), strconv.FormatInt(deref(d.TxBps), 10),
			strconv.FormatInt(deref(d.RxPps), 10), strconv.FormatInt(deref(d.TxPps), 10),
			strconv.Itoa(peers),
		}, " "))
	}
	if len(rows) > c.histKeep {
		rows = rows[len(rows)-c.histKeep:]
	}
	_ = os.WriteFile(hfile, []byte(strings.Join(rows, "\n")), 0o644)

	out := make([][6]int64, 0, len(rows))
	for _, r := range rows {
		f := strings.Fields(r)
		if len(f) != 6 {
			continue
		}
		out = append(out, [6]int64{atoi64(f[0]), atoi64(f[1]), atoi64(f[2]), atoi64(f[3]), atoi64(f[4]), atoi64(f[5])})
	}
	return out
}

// relayStatsDoc is the relay's white-box stats JSON (ADR 1055 §2). Peers[]
// carries the per-peer cumulative counters (for peerStats) plus Transport
// (added for #467's conns_quic/conns_ws split); Drops/Connections feed the
// F-275 delivery block. Drops is a pointer so a relay whose stats JSON omits
// "drops" (or the endpoint is unreachable) distinguishes from a relay that
// reported all-zero drops.
type relayStatsDoc struct {
	Drops *struct {
		WriterFull       int64 `json:"writer_full"`
		WriterClosed     int64 `json:"writer_closed"`
		PeerNotFound     int64 `json:"peer_not_found"`
		DatagramTooLarge int64 `json:"datagram_too_large"`
	} `json:"drops"`
	Connections int64 `json:"connections"`
	Peers       []struct {
		PeerID    string `json:"peer_id"`
		FwdBytes  int64  `json:"fwd_bytes"`
		RcvBytes  int64  `json:"rcv_bytes"`
		Transport string `json:"transport"`
	} `json:"peers"`
}

// fetchRelayStats fetches and decodes the relay's white-box stats endpoint
// once. Returns nil on any failure (unreachable, bad JSON) — the endpoint is
// optional and a fetch failure must never fail the publish.
func fetchRelayStats(statsURL string) *relayStatsDoc {
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Get(statsURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "relay-metrics: stats fetch %s failed: %v\n", statsURL, err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var doc relayStatsDoc
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		fmt.Fprintf(os.Stderr, "relay-metrics: stats decode failed: %v\n", err)
		return nil
	}
	return &doc
}

// peerStats rate-diffs each peer's cumulative fwd/rcv bytes (from an
// already-fetched stats doc) against the prior sample (state file, same
// pattern as throughput()). FwdBps/RcvBps stay nil (JSON null) on the first
// sample or across a counter reset (reconnect), never a fabricated 0.
func peerStats(doc *relayStatsDoc, stateDir, relayName string) []PeerStat {
	now := time.Now().Unix()
	// prev: peer_id -> {fwdBytes, rcvBytes, ts}
	prev := map[string][3]int64{}
	prevPath := filepath.Join(stateDir, relayName+".peers")
	if b, err := os.ReadFile(prevPath); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if f := strings.Fields(line); len(f) == 4 {
				prev[f[0]] = [3]int64{atoi64(f[1]), atoi64(f[2]), atoi64(f[3])}
			}
		}
	}
	out := make([]PeerStat, 0, len(doc.Peers))
	lines := make([]string, 0, len(doc.Peers))
	for _, p := range doc.Peers {
		ps := PeerStat{PeerID: p.PeerID, FwdBytes: p.FwdBytes, RcvBytes: p.RcvBytes}
		if pv, ok := prev[p.PeerID]; ok {
			dt := now - pv[2]
			// Monotonic guard: a reconnect resets the relay's per-peer counters,
			// so a lower current value means "new connection" — skip the rate.
			if dt > 0 && p.FwdBytes >= pv[0] && p.RcvBytes >= pv[1] {
				ps.FwdBps = ptr((p.FwdBytes - pv[0]) / dt)
				ps.RcvBps = ptr((p.RcvBytes - pv[1]) / dt)
			}
		}
		out = append(out, ps)
		lines = append(lines, fmt.Sprintf("%s %d %d %d", p.PeerID, p.FwdBytes, p.RcvBytes, now))
	}
	_ = os.WriteFile(prevPath, []byte(strings.Join(lines, "\n")), 0o644)
	return out
}

// hairpinClient forces every request onto 127.0.0.1:443 while presenting the
// given Host/SNI, the Go equivalent of curl --resolve host:443:127.0.0.1. The
// rathole tunnel carries the TLS session to the site mgmt host, which presents the
// publicly trusted certificate for the enrollment name, so the client verifies
// it against the system roots like any other client of that name.
func hairpinClient(sni string) *http.Client {
	return hairpinClientVia("127.0.0.1:443", sni)
}

func hairpinClientVia(addr, sni string) *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{Timeout: 6 * time.Second}).DialContext(ctx, "tcp", addr)
			},
			TLSClientConfig: &tls.Config{ServerName: sni, MinVersion: tls.VersionTLS12},
		},
	}
}

// directClient posts to the second ingest over public DNS, so it verifies the
// server certificate against the system roots. The request carries a bearer
// token (the primary token when no second one is set), and this path leaves
// the relay over the open internet.
func directClient() *http.Client {
	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}},
	}
}

func post(client *http.Client, url, token string, body []byte) int {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func envInt(k string, d int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return d
}
func hostFromURL(u string) string {
	s := strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	return s
}
func deref(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
func showRate(p *int64) string {
	if p == nil {
		return "null"
	}
	return strconv.FormatInt(*p, 10)
}
