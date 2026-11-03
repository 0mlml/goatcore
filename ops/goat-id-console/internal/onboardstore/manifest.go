package onboardstore

import (
	"errors"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardlint"
)

// ErrInvalidManifest distinguishes a record that needs admin repair from a
// transient store failure. Never include manifest values in this public error.
var ErrInvalidManifest = errors.New("the stored provisioning manifest is incomplete or invalid; restore and retry are refused because a re-mint needs a complete manifest, and no supported repair path exists yet")

// ValidateProvisioningManifest applies the same gate as initial approval.
// Historical index rows (notably {}) are not provisioning specifications.
func ValidateProvisioningManifest(manifest string) error {
	if !onboardlint.Lint([]byte(manifest), "json").OK {
		return ErrInvalidManifest
	}
	return nil
}
