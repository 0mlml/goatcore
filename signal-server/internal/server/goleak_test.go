// goleak_test.go — Block 50A.2 production-readiness hardening.
//
// goleak.VerifyTestMain catches goroutine leaks at the package boundary.
// A leaked goroutine that survives test completion almost always indicates
// a missing context cancellation, an unhandled channel close, or a
// connection / stream that was never reaped — bugs that pass tests but
// accumulate in production.
//
// Scope: package `server` unit tests (registry, federation logic, server
// behavior). The external `server_test` integration tests (file
// federation_integration_test.go) start real gRPC servers and aren't
// covered here yet — gRPC server lifecycle during test teardown leaks
// known background goroutines and would need explicit IgnoreTopFunction
// suppressions to be quiet. That's a follow-up under Block 50A.2.

package server

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
