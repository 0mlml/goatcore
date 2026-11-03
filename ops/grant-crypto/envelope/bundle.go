package envelope

import (
	"crypto/ecdh"
	"encoding/json"
	"fmt"
	"sort"
)

// KeyBundle is the payload published on keys/<release-set>/<generation>: the
// current DEK generation for a release-set, wrapped once per authorized member
// (keyed by a stable member id — in production the SVID SPIFFE ID / cert KID).
// A joining member finds its own entry and unwraps; non-members see only
// ciphertext they cannot open. E0 carries a single hand-cut member; E1
// (the key-distribution service) fills Members from the resolved audience.
type KeyBundle struct {
	Suite      Suite                 `json:"suite"`
	ReleaseSet string                `json:"rs"`
	Generation Generation            `json:"gen"`
	Members    map[string]WrappedDEK `json:"members"`
	// Signer + Sig authenticate the whole bundle (decision §7): the key-service
	// signs, a member verifies before unwrapping. omitempty so an unsigned bundle
	// (the E0 proof without a key-service identity) still serializes cleanly.
	Signer string `json:"signer,omitempty"`
	Sig    []byte `json:"sig,omitempty"`
	// SignerSVID carries the signer's SVID (PEM) so a member can validate it against
	// the SPIRE root and obtain the verifying key without a side channel — the
	// self-describing bundle (decision §7 rev 2). It is deliberately NOT part of the
	// signing transcript (see bundleTranscript), so its presence never changes the
	// signature or the cross-language test vectors.
	SignerSVID string `json:"signer_svid,omitempty"`
}

// BundleTopic is the Zenoh key expression a bundle is published on. Keeping it a
// single helper means publisher and consumer can never drift on the path shape.
func BundleTopic(releaseSet string, gen Generation) string {
	return fmt.Sprintf("keys/%s/%d", releaseSet, gen)
}

// NewBundle builds a bundle by wrapping dek to each recipient. recipients is
// keyed by member id; a nil/failed key aborts the whole bundle (fail-closed —
// a partial bundle would silently drop a member).
func NewBundle(releaseSet string, gen Generation, dek DEK, recipients map[string]RecipientKey) (KeyBundle, error) {
	b := KeyBundle{Suite: SuiteV1ECDHP256, ReleaseSet: releaseSet, Generation: gen, Members: make(map[string]WrappedDEK, len(recipients))}
	// Wrap in sorted id order so the operation is deterministic given fixed
	// randomness inputs and easy to reason about in tests/logs.
	ids := make([]string, 0, len(recipients))
	for id := range recipients {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		w, err := WrapDEK(dek, recipients[id].Pub)
		if err != nil {
			return KeyBundle{}, fmt.Errorf("bundle: wrapping for %q: %w", id, err)
		}
		b.Members[id] = w
	}
	return b, nil
}

// RecipientKey pairs a member id with its EC public key. Named so callers read
// as "who gets the key", not "an ecdh pointer".
type RecipientKey struct {
	Pub *ecdh.PublicKey
}

// Marshal serializes the bundle for transport.
func (b KeyBundle) Marshal() ([]byte, error) { return json.Marshal(b) }

// ParseBundle deserializes a bundle payload.
func ParseBundle(data []byte) (KeyBundle, error) {
	var b KeyBundle
	if err := json.Unmarshal(data, &b); err != nil {
		return KeyBundle{}, fmt.Errorf("bundle: parse: %w", err)
	}
	return b, nil
}
