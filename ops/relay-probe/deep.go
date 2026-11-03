package main

// Deep is the relay's self-measured host block. This struct is the canonical
// definition of the deep-metrics wire shape. `metrics-publish.sh` (the bash
// rollback copy) uses the same field names; the Go publisher also preserves
// null for unavailable measurements,
// and `relay-health-probe.sh`'s `deep_probe()` (the separate operator-run
// --deep SSM path) shares the base fields but is independently maintained —
// it does not yet carry the `delivery` block (a known, separate gap; see
// #467's follow-on note). `auth_reject` and `delivery` must exist in this
// struct as well as in the bash copies, because the Go self-publish binary
// (relay_metrics_publisher=binary) emits only what is here. The JSON
// tags ARE the wire contract the relay-health dashboard reads
// (ops/relay/health/relay-health.html); keep them byte-compatible.
//
// Honesty by construction: rate fields are *int64 so an un-computable rate
// (first sample, no prior) marshals to JSON `null`, never a fabricated 0.
// cpu_pct/mem_pct are strings ("?" when docker stats is unavailable), matching
// the historical shape.
type Deep struct {
	Rathole        string `json:"rathole"`
	Watchdog       string `json:"watchdog"`
	Heals1h        int    `json:"heals_1h"`
	CoturnListen   int    `json:"coturn_listen"`
	RelayUp        int    `json:"relay_up"`
	DataplaneListn int    `json:"dataplane_listen"`
	PeersRecent    int    `json:"peers_recent"`
	PeersWindowMin int    `json:"peers_window_min"`
	CPUPct         string `json:"cpu_pct"`
	MemPct         string `json:"mem_pct"`
	DiskPct        int    `json:"disk_pct"`
	MaxLogMB       int    `json:"max_container_log_mb"`
	RxBps          *int64 `json:"rx_bps"`
	TxBps          *int64 `json:"tx_bps"`
	RxPps          *int64 `json:"rx_pps"`
	TxPps          *int64 `json:"tx_pps"`
	// History rows are [t, rx_bps, tx_bps, rx_pps, tx_pps, peers] (the shape the
	// dashboard sparkline expects). Kept as int64 tuples, newest last.
	History [][6]int64 `json:"history"`
	// Per-peer relayed-traffic distribution (ADR 1055 §2b), pulled from the
	// relay's white-box stats endpoint and rate-diffed by the publisher. Absent
	// (omitempty) when the relay has no --stats-addr endpoint, so the old shape
	// is byte-identical for relays without it. The dashboard renders this as the
	// per-peer distribution + hog flag; nothing else the black-box probe reads
	// can produce it.
	Peers []PeerStat `json:"peers,omitempty"`
	// Unavailable log reads leave both blocks null, which consumers render
	// as unmeasured. A successful empty log window carries measured zeros.
	AuthReject *AuthReject `json:"auth_reject"`
	Delivery   *Delivery   `json:"delivery"`
}

// AuthReject mirrors metrics-publish.sh's `auth_reject` object and
// deep_probe()'s equivalent (relay-health-probe.sh) field-for-field.
type AuthReject struct {
	ExpiredToken     int            `json:"expired_token"`
	WSUpgradeTimeout int            `json:"ws_upgrade_timeout"`
	WSHandoffEOF     int            `json:"ws_handoff_eof"`
	WindowMin        int            `json:"window_min"`
	TopOffenders     []AuthOffender `json:"top_offenders"`
}

// AuthOffender is one source IP's expired-auth-token rejection count in the
// window — the top 3 by count, matching the bash `sort -rn | head -3`.
type AuthOffender struct {
	IP    string `json:"ip"`
	Count int    `json:"count"`
}

// Delivery mirrors metrics-publish.sh's `delivery` object field-for-field
//.
type Delivery struct {
	Drops            *DropCounters `json:"drops"`
	ConnsQUIC        *int          `json:"conns_quic"`
	ConnsWS          *int          `json:"conns_ws"`
	WriterDeath      int           `json:"writer_death"`
	Evictions        int           `json:"evictions"`
	DatagramTooLarge int           `json:"datagram_too_large"`
	WindowMin        int           `json:"window_min"`
}

// DropCounters mirrors the relay stats endpoint's lifetime `drops` object
// verbatim. Connections is carried for shape fidelity with the bash sibling
// even though no consumer reads it today.
type DropCounters struct {
	WriterFull       int64 `json:"writer_full"`
	WriterClosed     int64 `json:"writer_closed"`
	PeerNotFound     int64 `json:"peer_not_found"`
	DatagramTooLarge int64 `json:"datagram_too_large"`
	Connections      int64 `json:"connections"`
}

// PeerStat is one peer's relayed-traffic row from the relay stats endpoint
// (ADR 1055 §2b). `FwdBytes`/`RcvBytes` are cumulative (lifetime of the
// connection — the distribution shape is directly visible in them); `FwdBps`/
// `RcvBps` are the per-tick rate the publisher computed by diffing against the
// prior sample, and marshal to null on the first sample (honesty — no
// fabricated 0), exactly like the NIC rx_bps/tx_bps. PeerID is the relay's
// opaque hex peer-id (never an alias — ADR 1039; the dashboard resolves a
// display name if it can, else shows the truncated id).
type PeerStat struct {
	PeerID   string `json:"peer_id"`
	FwdBytes int64  `json:"fwd_bytes"`
	RcvBytes int64  `json:"rcv_bytes"`
	FwdBps   *int64 `json:"fwd_bps"`
	RcvBps   *int64 `json:"rcv_bps"`
}

// PublishEnvelope wraps Deep for the self-publish POST (ADR 1058): the relay
// stamps published_at; the ingest stamps its own received_at (never trusts the
// relay's clock). The `probe` (--deep SSM) path emits the bare Deep instead.
type PublishEnvelope struct {
	PublishedAt int64 `json:"published_at"`
	Deep        Deep  `json:"deep"`
}
