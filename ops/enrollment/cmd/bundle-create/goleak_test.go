// goleak_test.go — Block 50A discipline applied to bundle-create.
//
// The CLI is a one-shot operator command (issue + sign + write +
// exit) — but the package-level tests exercise full round-trips
// including age-decryption, so latent goroutine leaks would
// silently bloat the issuance ceremony's resident-set size at
// scale (e.g. fleet-wide reissue runs). Verify clean shutdown.

package main

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
