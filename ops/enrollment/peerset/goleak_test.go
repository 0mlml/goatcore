// goleak_test.go — Block 50E.11 parity. Catches goroutine leaks
// across the peerset package's tests so the accessor library — which
// every wg-cp0 ceremony tool will route through — never silently
// accumulates leaked workers as the surface grows in 22R.2.

package peerset

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
