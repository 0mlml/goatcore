// goleak_test.go — Block 50A.2 production-readiness hardening.
//
// Catches goroutine leaks at the package boundary. ntp-health-agent's
// production main() spawns four goroutines (poll-loop, unix-server,
// http-server, plus a shutdown-watcher inside serveHTTP); tests today are
// pure logic but goleak guards against regressions when integration tests
// land.

package main

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
