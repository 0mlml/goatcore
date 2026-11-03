// goleak_test.go — Block 50A.2 production-readiness hardening.
//
// Catches goroutine leaks across the mgmt-host-bootstrap CLI's pure-function
// tests. The CLI itself is short-lived (one-shot DB seed) so leaks are less
// catastrophic than for long-running daemons, but the discipline matters for
// future test additions and protects against regressions if the bootstrap
// pattern ever spawns workers (parallel inserts, retry workers, etc.).
//
// Block 27C / 50B.4 OTel coverage: any test that installs an SDK
// TracerProvider (see installRecorder in otel_test.go) defers tp.Shutdown
// in t.Cleanup. If the BatchSpanProcessor ever leaks a goroutine on
// shutdown, VerifyTestMain catches it here.

package main

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
