package server_test

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	signalpb "github.com/usareur-af/goatcore/signal-server/gen/signalexchange"
)

// A metric can exist as a DEFINITION and never as a MEASUREMENT:
// created, registered, assigned to the metrics struct, and never observed once.
// An unobserved histogram is worse than an absent one — it exports every bucket
// at zero, so a dashboard reads "we measured, and there is nothing there".
//
// These tests assert the OBSERVATION, not the registration. A test that only
// checked the metric was registered would have passed against the broken code,
// which is precisely how this shipped.

// bucketCount returns the cumulative count of the message-size histogram bucket
// whose upper bound is le, read from the gathered protobuf rather than from
// rendered text — the text encoder needs a format argument and panics without
// one, and the numbers are what matter here, not their formatting.
func bucketCount(t *testing.T, reg *prometheus.Registry, le float64) (uint64, bool) {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != "signal_server_message_bytes" {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, b := range m.GetHistogram().GetBucket() {
				if b.GetUpperBound() == le {
					return b.GetCumulativeCount(), true
				}
			}
		}
	}
	return 0, false
}

func TestRelayRecordsMessageSize(t *testing.T) {
	s, reg := newTestServer(t)

	// A message to a peer nobody has registered. The delivery MISSES, which is
	// the point: the size must be recorded on every exit path, not only the
	// happy one. Most Rosenpass offers can go to peers that are
	// offline, so a recording that only fires on successful delivery would have
	// missed the traffic that mattered.
	_, err := s.Send(context.Background(), &signalpb.EncryptedMessage{
		Key:       "sender-key",
		RemoteKey: "nobody-is-registered-under-this",
		Body:      make([]byte, 300),
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got := testutil.CollectAndCount(reg, "signal_server_message_bytes"); got == 0 {
		t.Fatal("signal_server_message_bytes has no series after a relay — " +
			"the histogram is registered but never observed, which is the exact " +
			"defect this test exists for")
	}

	// ~300 bytes of payload plus framing: comfortably inside 512, nowhere near
	// the 524288 edge where a Rosenpass key lands.
	c, ok := bucketCount(t, reg, 512)
	if !ok {
		t.Fatal("no 512-byte bucket on signal_server_message_bytes")
	}
	if c == 0 {
		t.Errorf("expected an observation at or below the 512-byte bucket, got %d", c)
	}
}

func TestMessageSizeBucketsSeparateTheRosenpassKey(t *testing.T) {
	s, reg := newTestServer(t)

	// A Rosenpass v1 static public key is Classic McEliece 460896 — 524,160
	// bytes — and netbird puts the whole key in every signal OFFER. The bucket
	// edges have to put that in a DIFFERENT bucket from an ordinary ICE offer,
	// otherwise the bimodality is invisible and only a mean survives, which
	// describes no real message.
	_, err := s.Send(context.Background(), &signalpb.EncryptedMessage{
		Key:       "sender-key",
		RemoteKey: "nobody-is-registered-under-this",
		Body:      make([]byte, 524160),
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	small, ok := bucketCount(t, reg, 512)
	if !ok {
		t.Fatal("no 512-byte bucket on signal_server_message_bytes")
	}
	big, ok := bucketCount(t, reg, 1048576)
	if !ok {
		t.Fatal("no 1 MB bucket on signal_server_message_bytes")
	}
	if small != 0 {
		t.Errorf("a 524,160-byte message landed at or below 512 bytes (count %d) — "+
			"the buckets do not separate a key-bearing offer from an ICE offer", small)
	}
	if big == 0 {
		t.Error("a 524,160-byte message was not counted in the 1 MB bucket")
	}
}
