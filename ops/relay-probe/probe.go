package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// probe.go — the `probe` subcommand: the blackbox fleet prober, a faithful port
// of relay-health-probe.sh. It probes each relay's PUBLIC IP directly
// (so it works precisely when the relays are broken and the mesh is down),
// broken out by port/transport, and writes relay-health.json for
// relay-health.html.
//
// PORTED (this Phase 2): the FAST path + the --ingest-dir fold —
//   :443   enrollment TLS ok-ratio over N tries + min TCP-connect latency
//   :2333  rathole control-channel TCP reach
//   :3478  coturn STUN binding request (real STUN, UDP)
//   :33073 data plane, ws_tcp (TCP-connect) + quic_udp (QUIC Version-Negotiation)
//   per-relay :443 latency
//   relays.conf parse incl. cap_mbit=/cap_pps=/cap_peers=/cap_src= + provenance
//   --ingest-dir: fold each relay's self-published deep block (ADR 1058), fresh-capped
//
// DEFERRED (follow-ons, intentionally not in this pass — see README phase plan):
//   --deep       the SSM operator path (deep_probe over aws ssm). The self-publish
//                (--ingest-dir) path already covers throughput credential-free;
//                the SSM leg needs the aws CLI + a poll loop and is a later increment.
//   --alert-sink/--site/--disk-*/--maxlog-warn-mb/--emit-only  the Alertmanager
//                push. Default-OFF in the bash too; the
//                prober stays pure-render without it. Follow-on.

// JSON wire structs. The json tags ARE the contract relay-health.html reads —
// keep them byte-compatible (verified against the HTML render()).

type probeDoc struct {
	Generated      int64       `json:"generated"` // NOTE: `generated`, not generated_at (HTML reads data.generated)
	Deep           bool        `json:"deep"`      // was this a deep pass? (false on fast+ingest)
	Vantage        string      `json:"vantage"`
	TunnelEndpoint bool        `json:"tunnel_endpoint"`
	Relays         []relayView `json:"relays"`
}

type relayView struct {
	Name       string          `json:"name"`
	IP         string          `json:"ip"`
	Region     string          `json:"region"`
	Goatnet    string          `json:"goatnet"`
	Enroll     enrollView      `json:"enroll"`
	LatMs      int             `json:"lat_ms"`
	RatholeCtl bool            `json:"rathole_ctl"`
	Stun       bool            `json:"stun"`
	Dataplane  dataplaneView   `json:"dataplane"`
	Cap        capView         `json:"cap"`
	// Deep is passed through verbatim: on the ingest path it's the relay's own
	// self-published deep object (whatever shape it published — the dashboard
	// reads it), and `null` when there's no fresh deep block. Kept as RawMessage
	// so the fold never lossily re-marshals a relay's block through our struct.
	Deep json.RawMessage `json:"deep"`
}

type enrollView struct {
	OK    int    `json:"ok"`
	Tries int    `json:"tries"`
	Code  string `json:"code"`
	TLSS  string `json:"tls_s"`
}

type dataplaneView struct {
	WsTCP     bool `json:"ws_tcp"`
	WsRTTMs   int  `json:"ws_rtt_ms"`
	QuicUDP   bool `json:"quic_udp"`
	QuicRTTMs int  `json:"quic_rtt_ms"`
}

// capView mirrors the bash's cap object. All fields are *T so an UNSET capacity
// marshals to JSON null (the dashboard renders the utilization gauge grey,
// "capacity not declared" — never a fabricated %).
type capView struct {
	Mbit  *int64  `json:"mbit"`
	Bps   *int64  `json:"bps"`
	Pps   *int64  `json:"pps"`
	Peers *int64  `json:"peers"`
	Src   *string `json:"src"`
}

// relayConf is one parsed relays.conf line.
type relayConf struct {
	name    string
	ip      string
	region  string
	inst    string // instance_id; only used by the deferred --deep SSM path
	goatnet string
	cap     capView
}

type probeConfig struct {
	conf           string
	out            string
	vantage        string
	tunnelEndpoint bool
	ingestDir      string
	ingestMaxAge   int64 // seconds; ingest files older than this drop to null
	tlsTries       int
}

func probeConfigFromArgs(args []string) (probeConfig, error) {
	here := selfDir()
	c := probeConfig{
		conf:         filepath.Join(here, "relays.conf"),
		out:          filepath.Join(here, "relay-health.json"),
		vantage:      envOr("GOAT_RELAY_HEALTH_VANTAGE", shortHostname()),
		ingestMaxAge: int64(envInt("INGEST_MAX_AGE", 3600)),
		tlsTries:     3,
	}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--conf":
			i++
			c.conf = argVal(args, i)
		case "--out":
			i++
			c.out = argVal(args, i)
		case "--vantage":
			i++
			c.vantage = argVal(args, i)
		case "--tunnel-endpoint":
			c.tunnelEndpoint = true
		case "--ingest-dir":
			i++
			c.ingestDir = argVal(args, i)
		case "-h", "--help":
			return c, errHelp
		default:
			return c, fmt.Errorf("unknown arg: %s", args[i])
		}
	}
	return c, nil
}

var errHelp = fmt.Errorf("help")

func runProbe(args []string) int {
	c, err := probeConfigFromArgs(args)
	if err == errHelp {
		probeUsage()
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "relay-probe probe:", err)
		probeUsage()
		return 2
	}

	relays, err := parseRelaysConf(c.conf)
	if err != nil {
		fmt.Fprintln(os.Stderr, "relay-probe probe:", err)
		return 2
	}
	fmt.Fprintf(os.Stderr, "probing %d relays (deep=0)...\n", len(relays))

	doc := probeDoc{
		Generated:      time.Now().Unix(),
		Deep:           false, // fast + ingest path; --deep is a deferred follow-on
		Vantage:        c.vantage,
		TunnelEndpoint: c.tunnelEndpoint,
		Relays:         make([]relayView, 0, len(relays)),
	}

	for _, rc := range relays {
		rv := probeOne(rc, c)
		doc.Relays = append(doc.Relays, rv)
		fmt.Fprintf(os.Stderr,
			"  %s %s  :443=%d/%d lat=%dms  :2333=%t  stun=%t  :33073 ws/tcp=%t(%dms) quic/udp=%t(%dms)\n",
			rc.name, rc.ip, rv.Enroll.OK, rv.Enroll.Tries, rv.LatMs, rv.RatholeCtl, rv.Stun,
			rv.Dataplane.WsTCP, rv.Dataplane.WsRTTMs, rv.Dataplane.QuicUDP, rv.Dataplane.QuicRTTMs)
	}

	body, err := json.Marshal(doc)
	if err != nil {
		fmt.Fprintln(os.Stderr, "relay-probe probe: marshal:", err)
		return 1
	}
	body = append(body, '\n')
	if err := os.WriteFile(c.out, body, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "relay-probe probe: write:", err)
		return 1
	}
	fmt.Fprintln(os.Stderr, "wrote", c.out)
	return 0
}

// probeOne runs every fast probe against one relay + folds its ingest deep
// block, assembling the relay's JSON view. Mirrors the bash while-loop body.
func probeOne(rc relayConf, c probeConfig) relayView {
	rv := relayView{
		Name:    rc.name,
		IP:      rc.ip,
		Region:  rc.region,
		Goatnet: rc.goatnet,
		Cap:     rc.cap,
		LatMs:   -1,
		Deep:    json.RawMessage("null"),
	}

	// :443 enrollment TLS — N tries; ok-ratio + MIN TCP-connect latency (the
	// F-213 per-connection flap is visible via ok/tries, not a binary up/down).
	rv.Enroll.Tries = c.tlsTries
	sni := "netbird." + rc.goatnet + "-backbone.net"
	for t := 0; t < c.tlsTries; t++ {
		r := enrollTLS(rc.ip, sni, 5*time.Second)
		if r.ok == 1 {
			rv.Enroll.OK++
			rv.Enroll.Code = r.code
			rv.Enroll.TLSS = r.tlsS
		}
		if r.latMs >= 0 && (rv.LatMs < 0 || r.latMs < rv.LatMs) {
			rv.LatMs = r.latMs // keep the minimum, like the bash
		}
	}

	// :2333 rathole control channel (TCP), :3478 STUN (real UDP binding request).
	rv.RatholeCtl = tcpOK(rc.ip, 2333, 3*time.Second)
	rv.Stun = stunOK(rc.ip, 3478, 2*time.Second)

	// :33073 data plane, broken out by transport. WS/TCP + its connect RTT;
	// QUIC/UDP via a real Version-Negotiation handshake + its round-trip RTT.
	// The two fail INDEPENDENTLY (UDP/QUIC is the leg most likely silently filtered).
	rv.Dataplane.WsTCP = tcpOK(rc.ip, 33073, 3*time.Second)
	rv.Dataplane.WsRTTMs = tcpRTTms(rc.ip, 33073, 3*time.Second)
	quicOK, quicRTT := quicVN(rc.ip, 33073, 2*time.Second)
	rv.Dataplane.QuicUDP = quicOK
	rv.Dataplane.QuicRTTMs = quicRTT

	// deep block: fast+ingest path — fold the relay's self-published deep block
	// (ADR 1058) if fresh. (--deep SSM is a deferred follow-on.)
	if c.ingestDir != "" {
		rv.Deep = ingestDeep(c.ingestDir, rc.name, c.ingestMaxAge)
	}
	return rv
}

// ingestDeep reads <dir>/<name>.json (written by the Thor ingest from a relay's
// own POST) and returns its `deep` object verbatim, or JSON `null` if the file
// is missing / has no deep object / is past the freshness cap. Port of the bash
// ingest_deep(): freshness is judged on the ingest-stamped `received_at` (never
// the relay's own clock), so a stale self-publish drops to "operator-deep only"
// rather than reading as fresh.
func ingestDeep(dir, name string, maxAge int64) json.RawMessage {
	null := json.RawMessage("null")
	f := filepath.Join(dir, name+".json")
	b, err := os.ReadFile(f)
	if err != nil {
		return null
	}
	// Only decode the two fields we gate on; keep `deep` raw for passthrough.
	var env struct {
		ReceivedAt int64           `json:"received_at"`
		Deep       json.RawMessage `json:"deep"`
	}
	if err := json.Unmarshal(b, &env); err != nil {
		return null
	}
	if len(env.Deep) == 0 || string(env.Deep) == "null" || !isJSONObject(env.Deep) {
		return null
	}
	if env.ReceivedAt > 0 && (time.Now().Unix()-env.ReceivedAt) > maxAge {
		return null // truly stale -> operator-deep-only
	}
	return env.Deep
}

// parseRelaysConf parses the relay fleet file: whitespace-separated
//   name ip region instance_id goatnet [cap_mbit=N cap_pps=N cap_peers=N cap_src=X]
// Comments (#) and blank lines are ignored (matches the bash grep -vE).
func parseRelaysConf(path string) ([]relayConf, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("relays.conf: %w", err)
	}
	var out []relayConf
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 5 {
			continue // need at least name ip region inst goatnet
		}
		rc := relayConf{name: f[0], ip: f[1], region: f[2], inst: f[3], goatnet: f[4]}
		rc.cap = parseCap(f[5:])
		out = append(out, rc)
	}
	return out, nil
}

// parseCap reads the optional trailing cap_* tokens. Numbers are validated
// digit-only (bad value -> nil -> JSON null, never invented); cap_src is a
// short provenance label. cap_mbit is derived to bytes/s for the gauge.
func parseCap(tokens []string) capView {
	var cv capView
	for _, tok := range tokens {
		switch {
		case strings.HasPrefix(tok, "cap_mbit="):
			cv.Mbit = digitsOrNil(tok[len("cap_mbit="):])
		case strings.HasPrefix(tok, "cap_pps="):
			cv.Pps = digitsOrNil(tok[len("cap_pps="):])
		case strings.HasPrefix(tok, "cap_peers="):
			cv.Peers = digitsOrNil(tok[len("cap_peers="):])
		case strings.HasPrefix(tok, "cap_src="):
			if v := tok[len("cap_src="):]; isCapSrc(v) {
				cv.Src = &v
			}
		}
	}
	if cv.Mbit != nil {
		bps := *cv.Mbit * 1000000 / 8
		cv.Bps = &bps
	}
	return cv
}

// digitsOrNil returns a pointer to the parsed value iff s is all digits (and
// non-empty), else nil — the Go analog of the bash `case … *[!0-9]* -> null`.
func digitsOrNil(s string) *int64 {
	if s == "" {
		return nil
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return nil
		}
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil
	}
	return &v
}

// isCapSrc allows the same [a-zA-Z0-9_-] provenance alphabet as the bash.
func isCapSrc(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-'
		if !ok {
			return false
		}
	}
	return true
}

// isJSONObject reports whether raw is a JSON object (starts with '{'), the Go
// analog of the bash `isinstance(deep, dict)` guard.
func isJSONObject(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return len(s) > 0 && s[0] == '{'
}

func selfDir() string {
	exe, err := os.Executable()
	if err != nil {
		if wd, e := os.Getwd(); e == nil {
			return wd
		}
		return "."
	}
	return filepath.Dir(exe)
}

func shortHostname() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "local"
	}
	if i := strings.IndexByte(h, '.'); i > 0 {
		h = h[:i] // hostname -s
	}
	return h
}

func argVal(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}
	return ""
}

func probeUsage() {
	fmt.Fprint(os.Stderr, `relay-probe probe — blackbox-probe the relay fleet -> relay-health.json

usage:
  relay-probe probe [--conf relays.conf] [--out relay-health.json]
                    [--vantage NAME] [--tunnel-endpoint] [--ingest-dir DIR]

flags:
  --conf FILE         relay fleet file (default: <exe-dir>/relays.conf)
  --out FILE          output JSON path (default: <exe-dir>/relay-health.json)
  --vantage NAME      vantage label (default: $GOAT_RELAY_HEALTH_VANTAGE or hostname -s)
  --tunnel-endpoint   this vantage is the rathole tunnel endpoint; :443/STUN/QUIC
                      hairpin back through the relay's own tunnel so the dashboard
                      renders them informational, basing health on :2333 + :33073 WS/TCP
  --ingest-dir DIR    fold each relay's self-published deep block from DIR/<name>.json
                      (ADR 1058), honoring INGEST_MAX_AGE (default 3600s)

deferred (use the bash relay-health-probe.sh for these): --deep (SSM), --alert-sink/--emit-only
`)
}
