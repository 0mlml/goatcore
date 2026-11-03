// Catches goroutine leaks across the wg-cp0 cmd's tests so the
// orchestrator entrypoint — which every operator interaction routes
// through — does not silently accumulate leaked workers as 22R.2
// piles mutating subcommands on top.

package main

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
