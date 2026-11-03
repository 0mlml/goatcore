//go:build nopiv

// prod_nopiv.go — stub for the `nopiv` build.
//
// Built with `-tags nopiv` to drop the piv-go (CGO + PC/SC) dependency,
// yielding a CGO-free, fully-static binary that cross-compiles anywhere
// and runs in a distroless container (e.g. bind-mounted into
// goat-zenoh-portal, which only ever mints in dev/age mode). `--mode prod`
// is unavailable in this build and returns a clear, actionable error.

package main

import (
	"errors"

	"github.com/usareur-af/goatcore/ops/enrollment/ca"
)

func openYubikeySigner(_, _, _, _ string) (ca.Signer, error) {
	return nil, errors.New(
		"prod/YubiKey mode unavailable: this bundle-create was built with -tags nopiv " +
			"(CGO-free, dev/age mode only). Use a default (piv-enabled) build for --mode prod.")
}
