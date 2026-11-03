// goleak_test.go — Block 50E.11 production-readiness hardening.
//
// Catches goroutine leaks in the age-CA test surface (offline CA
// keypair generation + age multi-recipient encryption helpers). The
// CA tooling runs at issuance time on the operator's host; a leak
// would not be operator-visible, but the discipline matters when
// future tests add concurrency.

package ca

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
