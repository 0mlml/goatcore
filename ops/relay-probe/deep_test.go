package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Golden fixture for the F-269/F-275 log window. The expected counts below
// were derived by hand-applying metrics-publish.sh's grep patterns
// (ops/relay/metrics-publish.sh) to this fixture line-by-line — the same
// patterns authRejectExpiredRe/authRejectWSTimeoutRe/authRejectEOFRe/
// deliveryWriterDeathRe/deliveryEvictionsRe/deliveryTooLargeRe port into Go.
// This is a hand-derived golden, not a live run of the bash script (no
// `docker logs`/container available in this sandbox); see HANDOFF_467.md.
const authDeliveryLogFixture = `2026-10-29T00:00:01Z goat-relay: validator rejected connection: expired auth token for 203.0.113.5 (reused)
2026-10-29T00:00:02Z goat-relay: validator rejected connection: expired auth token for 203.0.113.5 (reused)
2026-10-29T00:00:03Z goat-relay: validator rejected connection: expired auth token for 203.0.113.9 (reused)
2026-10-29T00:00:04Z goat-relay: WS upgrade timed out waiting for client
2026-10-29T00:00:05Z goat-relay: hand-off failed: eof reading frame
2026-10-29T00:00:06Z goat-relay: send_datagram failed, closing connection
2026-10-29T00:00:07Z goat-relay: routing: evicted peer abc123
2026-10-29T00:00:08Z goat-relay: datagram too large for quic frame
2026-10-29T00:00:09Z goat-relay: normal benign log line, no match`

func TestAuthRejectBlockFromLogs_MatchesBashFieldNamesAndCounts(t *testing.T) {
	got := authRejectBlockFromLogs(authDeliveryLogFixture, 5)
	// Three lines (1,2,3) match "validator rejected.*expired auth token"
	// (grep -cE counts matching LINES, not occurrences): 203.0.113.5 appears
	// twice, 203.0.113.9 once.
	want := AuthReject{
		ExpiredToken:     3,
		WSUpgradeTimeout: 1,
		WSHandoffEOF:     1,
		WindowMin:        5,
		TopOffenders: []AuthOffender{
			{IP: "203.0.113.5", Count: 2},
			{IP: "203.0.113.9", Count: 1},
		},
	}

	if got.ExpiredToken != want.ExpiredToken {
		t.Errorf("ExpiredToken = %d, want %d", got.ExpiredToken, want.ExpiredToken)
	}
	if got.WSUpgradeTimeout != want.WSUpgradeTimeout {
		t.Errorf("WSUpgradeTimeout = %d, want %d", got.WSUpgradeTimeout, want.WSUpgradeTimeout)
	}
	if got.WSHandoffEOF != want.WSHandoffEOF {
		t.Errorf("WSHandoffEOF = %d, want %d", got.WSHandoffEOF, want.WSHandoffEOF)
	}
	if got.WindowMin != want.WindowMin {
		t.Errorf("WindowMin = %d, want %d", got.WindowMin, want.WindowMin)
	}
	if !reflect.DeepEqual(got.TopOffenders, want.TopOffenders) {
		t.Errorf("TopOffenders = %+v, want %+v", got.TopOffenders, want.TopOffenders)
	}
}

func TestDeliveryBlock_LogDerivedFieldsMatchBashPatterns(t *testing.T) {
	got := deliveryBlock(authDeliveryLogFixture, nil, 5)
	if got.WriterDeath != 1 {
		t.Errorf("WriterDeath = %d, want 1", got.WriterDeath)
	}
	if got.Evictions != 1 {
		t.Errorf("Evictions = %d, want 1", got.Evictions)
	}
	if got.DatagramTooLarge != 1 {
		t.Errorf("DatagramTooLarge = %d, want 1", got.DatagramTooLarge)
	}
	if got.WindowMin != 5 {
		t.Errorf("WindowMin = %d, want 5", got.WindowMin)
	}
	// No stats endpoint fetched (nil doc): drops/conns stay at the honest
	// "no data" default. The dashboard already accepts null connection counts.
	if got.Drops != nil {
		t.Errorf("Drops = %+v, want nil (no stats doc)", got.Drops)
	}
	if got.ConnsQUIC != nil || got.ConnsWS != nil {
		t.Errorf("ConnsQUIC/ConnsWS = %v/%v, want null/null", got.ConnsQUIC, got.ConnsWS)
	}
}

func TestDeliveryBlock_DropsAndConnsFromStatsDoc(t *testing.T) {
	doc := &relayStatsDoc{
		Connections: 7,
		Peers: []struct {
			PeerID    string `json:"peer_id"`
			FwdBytes  int64  `json:"fwd_bytes"`
			RcvBytes  int64  `json:"rcv_bytes"`
			Transport string `json:"transport"`
		}{
			{PeerID: "a", Transport: "quic"},
			{PeerID: "b", Transport: "ws"},
			{PeerID: "c", Transport: "quic"},
		},
	}
	doc.Drops = &struct {
		WriterFull       int64 `json:"writer_full"`
		WriterClosed     int64 `json:"writer_closed"`
		PeerNotFound     int64 `json:"peer_not_found"`
		DatagramTooLarge int64 `json:"datagram_too_large"`
	}{WriterFull: 5, WriterClosed: 2, PeerNotFound: 169000, DatagramTooLarge: 42}

	got := deliveryBlock(authDeliveryLogFixture, doc, 5)
	if got.ConnsQUIC == nil || got.ConnsWS == nil || *got.ConnsQUIC != 2 || *got.ConnsWS != 1 {
		t.Errorf("ConnsQUIC/ConnsWS = %v/%v, want 2/1", got.ConnsQUIC, got.ConnsWS)
	}
	if got.Drops == nil {
		t.Fatal("Drops = nil, want populated DropCounters")
	}
	want := DropCounters{WriterFull: 5, WriterClosed: 2, PeerNotFound: 169000, DatagramTooLarge: 42, Connections: 7}
	if *got.Drops != want {
		t.Errorf("Drops = %+v, want %+v", *got.Drops, want)
	}
}

// TestDeep_AuthRejectAndDeliveryAlwaysPresentInJSON is the shape-compatibility
// assertion the issue (#467) asks for: the published payload must carry
// `auth_reject` and `delivery` with the SAME field names metrics-publish.sh
// emits (ops/relay/metrics-publish.sh) and relay-health-to-prom.py /
// relay-health.html read (ops/relay/health/). Zeros must be emitted, not
// omitted (relay-health-to-prom.py's F-269 comment: "Zeros ARE emitted").
func TestDeep_AuthRejectAndDeliveryAlwaysPresentInJSON(t *testing.T) {
	d := Deep{
		AuthReject: &AuthReject{WindowMin: 5},
		Delivery:   &Delivery{WindowMin: 5},
	}
	body, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	ar, ok := m["auth_reject"]
	if !ok {
		t.Fatal(`"auth_reject" key missing from marshaled Deep`)
	}
	var arParsed map[string]json.RawMessage
	if err := json.Unmarshal(ar, &arParsed); err != nil {
		t.Fatalf("auth_reject unmarshal: %v", err)
	}
	for _, key := range []string{"expired_token", "ws_upgrade_timeout", "ws_handoff_eof", "window_min", "top_offenders"} {
		if _, ok := arParsed[key]; !ok {
			t.Errorf("auth_reject missing key %q (bash field name)", key)
		}
	}

	dv, ok := m["delivery"]
	if !ok {
		t.Fatal(`"delivery" key missing from marshaled Deep`)
	}
	var dvParsed map[string]json.RawMessage
	if err := json.Unmarshal(dv, &dvParsed); err != nil {
		t.Fatalf("delivery unmarshal: %v", err)
	}
	for _, key := range []string{"drops", "conns_quic", "conns_ws", "writer_death", "evictions", "datagram_too_large", "window_min"} {
		if _, ok := dvParsed[key]; !ok {
			t.Errorf("delivery missing key %q (bash field name)", key)
		}
	}
	// drops is explicitly null (not omitted) when there is no stats doc —
	// matches metrics-publish.sh's drops=null fallback.
	if string(dvParsed["drops"]) != "null" {
		t.Errorf(`delivery.drops = %s, want "null" (no stats doc)`, dvParsed["drops"])
	}
}

func TestDeliveryMeasurementsDistinguishMissingFromZero(t *testing.T) {
	for _, failed := range []bool{false, true} {
		var readErr error
		if failed {
			readErr = errors.New("docker unavailable")
		}
		var d Deep
		setDeliveryMeasurements(&d, "", readErr, nil, 5)
		body, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		var wire map[string]any
		if err := json.Unmarshal(body, &wire); err != nil {
			t.Fatal(err)
		}
		if failed {
			if wire["auth_reject"] != nil || wire["delivery"] != nil {
				t.Fatalf("failed read claimed a measurement: %s", body)
			}
		} else {
			ar := wire["auth_reject"].(map[string]any)
			dv := wire["delivery"].(map[string]any)
			if ar["expired_token"] != float64(0) || dv["writer_death"] != float64(0) || dv["conns_quic"] != nil {
				t.Fatalf("wrong measured/unknown distinction: %s", body)
			}
		}
	}
}

func TestDockerLogsPreservesFailure(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\necho daemon-unavailable >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	logs, err := dockerLogs("fixture", "5m")
	if err == nil {
		t.Fatal("failed docker command reported success")
	}
	var d Deep
	setDeliveryMeasurements(&d, logs, err, nil, 5)
	if d.AuthReject != nil || d.Delivery != nil {
		t.Fatal("docker failure published measured zeros")
	}
}
