// goleak_test.go — Block 50E.11 production-readiness hardening.
//
// Catches goroutine leaks across the bundle-extract CLI tests
// (device-side reader for the offline-enrollment CBOR bundle).
// The CLI runs as a one-shot at peer first-boot; a leak there is
// negligible operationally but the discipline matches the rest of
// the enrollment module.

package main

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
