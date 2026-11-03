// goleak_test.go — Block 50A discipline applied to wg-cp0-bundle-apply.
//
// One-shot CLI per Block 22K, but the package-level tests exercise full
// bundle parse + verify + render cycles; a goroutine leak here would
// silently bloat the device-side first-boot ceremony.

package main

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
