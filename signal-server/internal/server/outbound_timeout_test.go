// F-277 — the outbound pump must not park forever on a receiver that
// stops draining.
//
// grpc-go's stream.Send blocks in transport flow-control until the peer's
// window frees. Before the fix, a client that stopped calling Recv parked
// ConnectStream's pump indefinitely: the peer stayed registered, its 16-deep
// queue filled, and every sender was told "outbound full" while the registry
// still reported the peer present — a permanent blackhole with no self-heal
// (upstream netbird bounds the send and cancels the peer; the rewrite
// dropped that).
//
// These tests pin the restored behavior at the seam where it lives —
// sendWithTimeout — rather than trying to force real TCP flow-control
// exhaustion, which would need megabytes of traffic and be timing-flaky.
package server

import (
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	signalpb "github.com/usareur-af/goatcore/signal-server/gen/signalexchange"
)

// blockedStream stands in for a client that has stopped reading: Send never
// returns, exactly as grpc-go behaves once the flow-control window is full.
type blockedStream struct {
	signalpb.SignalExchange_ConnectStreamServer
	released chan struct{}
}

func (b *blockedStream) Send(*signalpb.EncryptedMessage) error {
	<-b.released // never closed during the test
	return nil
}

// promptStream returns immediately, like a healthy receiver.
type promptStream struct {
	signalpb.SignalExchange_ConnectStreamServer
	sent int
}

func (p *promptStream) Send(*signalpb.EncryptedMessage) error {
	p.sent++
	return nil
}

func TestSendWithTimeout_AbandonsAStalledReceiver(t *testing.T) {
	// Keep the test fast: the constant is 10s in production, so drive the
	// same code path with a stream that never returns and assert we give up
	// with DeadlineExceeded rather than blocking forever. A goroutine that
	// outlives the deadline is the failure this guards.
	if testing.Short() {
		t.Skip("skipping 10s timeout test in -short mode")
	}
	stream := &blockedStream{released: make(chan struct{})}
	// Release the stalled Send when the test ends, mirroring production: the
	// DeadlineExceeded return tears the stream down, which unblocks the
	// abandoned goroutine. Without this the goroutine outlives the test and
	// goleak (correctly) fails the package.
	t.Cleanup(func() {
		close(stream.released)
		// Give the released goroutine a moment to exit before goleak looks.
		time.Sleep(50 * time.Millisecond)
	})

	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- sendWithTimeout(stream, &signalpb.EncryptedMessage{}) }()

	select {
	case err := <-done:
		elapsed := time.Since(start)
		if status.Code(err) != codes.DeadlineExceeded {
			t.Fatalf("want DeadlineExceeded so ConnectStream returns and the peer "+
				"reconnects, got %v (%v)", status.Code(err), err)
		}
		if elapsed < outboundSendTimeout {
			t.Fatalf("gave up after %v, before the %v budget — a slow but healthy "+
				"receiver would be dropped", elapsed, outboundSendTimeout)
		}
	case <-time.After(outboundSendTimeout + 5*time.Second):
		t.Fatal("sendWithTimeout never returned — the pre-fix blackhole: the pump " +
			"parks forever and the peer is never evicted")
	}
}

func TestSendWithTimeout_HealthyReceiverIsUnaffected(t *testing.T) {
	stream := &promptStream{}
	for i := 0; i < 3; i++ {
		if err := sendWithTimeout(stream, &signalpb.EncryptedMessage{}); err != nil {
			t.Fatalf("healthy send %d failed: %v", i, err)
		}
	}
	if stream.sent != 3 {
		t.Fatalf("want 3 delivered, got %d", stream.sent)
	}
}

func TestSendWithTimeout_PropagatesSendError(t *testing.T) {
	// A real Send error (stream torn down) must surface unchanged, not be
	// masked as a timeout — ConnectStream relies on it to unregister.
	want := status.Error(codes.Unavailable, "transport closing")
	stream := &erroringStream{err: want}
	got := sendWithTimeout(stream, &signalpb.EncryptedMessage{})
	if status.Code(got) != codes.Unavailable {
		t.Fatalf("want the underlying Unavailable error preserved, got %v", got)
	}
}

type erroringStream struct {
	signalpb.SignalExchange_ConnectStreamServer
	err error
}

func (e *erroringStream) Send(*signalpb.EncryptedMessage) error { return e.err }
