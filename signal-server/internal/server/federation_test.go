package server_test

import (
	"context"
	"testing"

	signalpb "github.com/usareur-af/goatcore/signal-server/gen/signalexchange"
)

// TestForwardEmptyRemoteKey exercises the defensive check in the Forward
// handler. An empty remoteKey is malformed — we return delivered=false
// without touching the registry.
func TestForwardEmptyRemoteKey(t *testing.T) {
	s, _ := newTestServer(t)

	resp, err := s.Forward(context.Background(), &signalpb.EncryptedMessage{
		Key:       "sender",
		RemoteKey: "",
		Body:      []byte("p"),
	})
	if err != nil {
		t.Fatalf("Forward returned err: %v", err)
	}
	if resp.GetDelivered() {
		t.Errorf("Forward with empty remoteKey reported delivered=true, want false")
	}
}

// TestForwardUnknownPeer confirms Forward returns delivered=false without
// error when the target peer isn't registered locally — that's the signal
// the calling server uses to try the next peer. At the Send RPC edge, when
// no peer accepts, the server returns OK with empty payload (silent drop)
// to match upstream's contract — see F-105 + TestStandaloneSendSilentDropOnUnknownPeer.
func TestForwardUnknownPeer(t *testing.T) {
	s, _ := newTestServer(t)

	resp, err := s.Forward(context.Background(), &signalpb.EncryptedMessage{
		Key:       "sender",
		RemoteKey: "nobody-registered-anywhere",
		Body:      []byte("p"),
	})
	if err != nil {
		t.Fatalf("Forward returned err: %v", err)
	}
	if resp.GetDelivered() {
		t.Errorf("Forward to unknown peer reported delivered=true, want false")
	}
}

// TestStandaloneSendSilentDropOnUnknownPeer confirms that a server
// configured with no federation peers (federationPeers=nil in newTestServer)
// returns OK with empty payload on local miss, matching upstream netbird
// signal-server's silent-drop contract for the unary Send RPC. See F-105
// for the regression history (this test originally asserted NotFound, which
// broke vanilla netbird daemon's IsHealthy() liveness probe). Regression
// guard: also makes sure s.federation==nil short-circuits the Broadcast path
// without panicking.
func TestStandaloneSendSilentDropOnUnknownPeer(t *testing.T) {
	s, _ := newTestServer(t)

	resp, err := s.Send(context.Background(), &signalpb.EncryptedMessage{
		Key:       "sender",
		RemoteKey: "nobody",
		Body:      []byte("p"),
	})
	if err != nil {
		t.Fatalf("Send returned err=%v; want nil per upstream silent-drop contract", err)
	}
	if resp == nil {
		t.Fatal("Send returned nil response; want empty EncryptedMessage")
	}
}
