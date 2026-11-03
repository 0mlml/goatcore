// goleak_test.go — Block 50E.11 production-readiness hardening.
//
// Catches goroutine leaks across the bundle package's tests
// (CRL + bundle CBOR encode/decode/verify). Bundle issuance and
// validation happen on every offline-CA-signed peer enrollment;
// a leak would silently bloat both the issuance CLI and the
// validator sidecar over time.

package bundle

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
