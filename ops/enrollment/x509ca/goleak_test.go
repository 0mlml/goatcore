// goleak_test.go — Block 50E.11 production-readiness hardening.
//
// Catches goroutine leaks across the x509ca package (Block 22J
// internal CA — root cert ceremony + leaf signing for service TLS).
// Cert-sign tooling runs at issuance time; a leak in the test surface
// would protect us against latent leaks introduced as the CA grows
// to support per-service-bundle issuance.

package x509ca

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
