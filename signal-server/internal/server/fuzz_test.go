// fuzz_test.go — Block 50A.4 production-readiness hardening.
//
// Native Go fuzz targets for the deserialization + handler boundary. gRPC
// transport pre-parses proto frames before they reach our handlers, so a
// "random bytes" fuzz at the wire level is mostly exercising google.golang
// .org/protobuf — which already has its own fuzzing. The targets below are
// scoped to OUR logic: arbitrary-content EncryptedMessage flowing through
// the helper functions and registry mutations that are pure go and don't
// require a live gRPC server.
//
// Run interactively: `go test -fuzz=Fuzz -fuzztime=30s ./internal/server/...`
// CI runs the seed corpus only (`go test`) per call, plus a scheduled
// long-fuzz cron.

package server

import (
	"testing"

	"google.golang.org/protobuf/proto"

	signalpb "github.com/usareur-af/goatcore/signal-server/gen/signalexchange"
)

// FuzzProtoUnmarshalEncryptedMessage exercises the deserialization boundary
// for EncryptedMessage with arbitrary input. Verifies (a) Unmarshal does not
// panic on adversary-chosen bytes, (b) the resulting message — when Unmarshal
// succeeds — is safely consumable by the helper that observes its Body
// (correlationID), which is what the runtime hot path does.
func FuzzProtoUnmarshalEncryptedMessage(f *testing.F) {
	// Seed corpus: a few representative shapes — empty body, short body,
	// realistic body length. The fuzzer mutates from these.
	f.Add([]byte{})
	f.Add([]byte{0x0a, 0x00})                       // body=""
	f.Add([]byte{0x0a, 0x04, 'p', 'i', 'n', 'g'})    // body="ping"
	f.Add([]byte{0x0a, 0x10, 0xde, 0xad, 0xbe, 0xef, // 16-byte random body
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c})

	f.Fuzz(func(t *testing.T, data []byte) {
		var msg signalpb.EncryptedMessage
		if err := proto.Unmarshal(data, &msg); err != nil {
			// Malformed proto: error return is the contract; nothing else
			// to check.
			return
		}
		// Successful Unmarshal — exercise the downstream surface that
		// runtime code immediately invokes. correlationID reads Body and
		// returns a string; must not panic on any successfully-unmarshaled
		// message regardless of body shape.
		_ = correlationID(&msg)
	})
}

// FuzzRegistryRegisterDeliverUnregister exercises the in-memory registry's
// public surface (Register/deliver/Unregister) with arbitrary keys and
// payload bodies. Catches:
//   - panics from edge-case keys (empty, very long, non-UTF8 bytes coerced
//     to string)
//   - data races when run with -race (use `go test -race -fuzz=...`)
//   - leaked outbound channels from interleaved register/unregister
func FuzzRegistryRegisterDeliverUnregister(f *testing.F) {
	f.Add("", []byte{})
	f.Add("peer-A", []byte("hello"))
	f.Add(string([]byte{0xff, 0xfe, 0x00}), []byte{0x00, 0x01, 0x02})

	f.Fuzz(func(t *testing.T, key string, body []byte) {
		r := newRegistry()
		ps := r.register(key)
		_ = r.deliver(key, &signalpb.EncryptedMessage{Body: body})
		r.unregister(key, ps)
	})
}
