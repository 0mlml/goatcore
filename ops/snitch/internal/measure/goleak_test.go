// goleak_test.go — Block 50E.5 production-readiness hardening.
//
// Catches goroutine leaks across the snitch measure package's tests
// (cache / dialer-log / mtu / netbird / policy / probe / relay /
// throughput). The measure functions are called both from the snitch
// daemon (long-running) and from the snitch probe binary; a leaked
// goroutine in one would silently bloat probe processes in the field.

package measure

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
