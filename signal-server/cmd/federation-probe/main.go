// federation-probe is a live-network probe for signal-server federation.
//
// It dials two signal-server instances at --peer-a and --peer-b, registers a
// fake peer key on --peer-b via ConnectStream, then calls Send on --peer-a
// targeting that key. The round-trip proves that Peer A missed locally,
// Broadcast via its federation client to Peer B, and Peer B delivered the
// message to the ConnectStream receiver.
//
// Exits 0 on successful delivery, 1 otherwise.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	signalpb "github.com/usareur-af/goatcore/signal-server/gen/signalexchange"
)

func main() {
	peerA := flag.String("peer-a", "", "host:port of the 'sender' signal-server (must be configured with --federation-peers pointing at peer-b)")
	peerB := flag.String("peer-b", "", "host:port of the 'receiver' signal-server (the one the fake peer registers against)")
	key := flag.String("key", "probe-alpha", "WireGuard peer key to register and target")
	timeout := flag.Duration("timeout", 15*time.Second, "overall deadline")
	flag.Parse()

	if *peerA == "" || *peerB == "" {
		log.Fatal("--peer-a and --peer-b required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	// Register `key` on peer-b.
	connB, err := grpc.NewClient(*peerB, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("dial peer-b %s: %v", *peerB, err)
	}
	defer connB.Close()

	streamCtx := metadata.AppendToOutgoingContext(ctx, "x-wiretrustee-peer-id", *key)
	streamB, err := signalpb.NewSignalExchangeClient(connB).ConnectStream(streamCtx)
	if err != nil {
		log.Fatalf("ConnectStream(peer-b): %v", err)
	}
	hdr, err := streamB.Header()
	if err != nil {
		log.Fatalf("streamB.Header: %v", err)
	}
	if got := hdr.Get("x-wiretrustee-peer-registered"); len(got) == 0 || got[0] != "1" {
		log.Fatalf("registration header = %v, want [1]", got)
	}
	fmt.Printf("registered %q on peer-b (%s)\n", *key, *peerB)

	// Send via peer-a targeting `key` (which peer-a does NOT know locally).
	connA, err := grpc.NewClient(*peerA, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("dial peer-a %s: %v", *peerA, err)
	}
	defer connA.Close()

	payload := []byte(fmt.Sprintf("federation-probe-%d", time.Now().UnixNano()))
	start := time.Now()
	if _, err := signalpb.NewSignalExchangeClient(connA).Send(ctx, &signalpb.EncryptedMessage{
		Key:       "probe-sender",
		RemoteKey: *key,
		Body:      payload,
	}); err != nil {
		log.Fatalf("Send via peer-a: %v", err)
	}
	fmt.Printf("Send issued via peer-a (%s)\n", *peerA)

	// Expect peer-b's stream to receive the forwarded message.
	type recv struct {
		msg *signalpb.EncryptedMessage
		err error
	}
	recvCh := make(chan recv, 1)
	go func() {
		msg, err := streamB.Recv()
		recvCh <- recv{msg, err}
	}()

	select {
	case r := <-recvCh:
		if r.err != nil {
			log.Fatalf("streamB.Recv: %v", r.err)
		}
		elapsed := time.Since(start)
		if got := string(r.msg.GetBody()); got != string(payload) {
			log.Fatalf("body mismatch: got %q, want %q", got, string(payload))
		}
		fmt.Printf("delivered in %s; body=%q remoteKey=%q\n",
			elapsed.Truncate(time.Millisecond), r.msg.GetBody(), r.msg.GetRemoteKey())
		os.Exit(0)
	case <-ctx.Done():
		log.Fatalf("timeout waiting for forwarded message on peer-b stream (%s)", *timeout)
	}
}
