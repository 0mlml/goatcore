// goleak_test.go — Block 50A.2 production-readiness hardening.
//
// Catches goroutine leaks at the package boundary. ntp-admit-sidecar's
// production main() spawns a signal-handler goroutine and the http.Server
// runs its own; tests today are pure logic but goleak guards against
// regressions when integration tests are added.

package main

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
