// Package canonical provides deterministic JSON serialization for NtpState
// used in Ed25519 attestation signatures (Block 12D).
package canonical

import (
	"encoding/json"
	"fmt"
)

// Marshal returns sorted-key JSON of v suitable for signing.
// For structs with json tags, encoding/json already produces consistent output;
// this wrapper exists so callers don't need to know that and the contract is explicit.
func Marshal(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("canonical marshal: %w", err)
	}
	return b, nil
}
