// Package conformance defines the cross-language test-vector format for the
// grant-crypto wire format (see ../WIRE-FORMAT.md). The Go reference generates
// these vectors (cmd/vectorgen) and validates against them (envelope's
// vectors_test); other-language implementations — the Rust production core — must
// validate against the same committed vectors, so independent implementations
// cannot silently drift on accept/reject decisions.
//
// Vectors are decrypt-direction: fixed key material + fixed produced artifacts
// (a sealed envelope and a signed bundle, generated once and committed) plus a
// list of cases, each of which a conformant consumer must accept (recovering the
// stated plaintext) or reject. The reference consumer order the cases assume is:
//
//	1. if a verify identity is given, VerifyBundle — reject on bad signature;
//	2. find the member's slot — reject if absent (not in the audience);
//	3. UnwrapDEK with the member key — reject on failure;
//	4. Open the envelope with the case key-expression — reject on failure.
//
// Reasons are advisory (an implementation may map them to its own errors); the
// binding contract is the accept/reject decision and, on accept, the plaintext.
package conformance

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Vectors is the top-level vectors.json document.
type Vectors struct {
	Suite     string              `json:"suite"`
	Note      string              `json:"note"`
	Keys      map[string]KeyRef   `json:"keys"`      // id → PEM file refs (relative to the vectors dir)
	Artifacts map[string]Artifact `json:"artifacts"` // name → a produced envelope+bundle pair
	Cases     []Case              `json:"cases"`
}

// KeyRef is a test identity's key material, embedded so the vectors are
// self-contained (no separate key files). These are throwaway keys generated for
// the vectors only. PrivScalarHex is the P-256 private scalar (hex) used to
// unwrap; PubSpkiB64 is the SPKI DER (base64) of the public key used to verify a
// bundle signature. An identity carries whichever it is used for.
type KeyRef struct {
	PrivScalarHex string `json:"priv_scalar_hex,omitempty"`
	PubSpkiB64    string `json:"pub_spki_b64,omitempty"`
}

// Artifact is a produced (sealed) envelope and (signed) bundle, plus the
// key-expression the envelope was sealed under. Kept as raw JSON so it is the
// exact reference bytes each implementation must parse.
type Artifact struct {
	Envelope json.RawMessage `json:"envelope"`
	Bundle   json.RawMessage `json:"bundle"`
	Keyexpr  string          `json:"keyexpr"`
}

// Case is one accept/reject expectation.
type Case struct {
	Name     string `json:"name"`
	Artifact string `json:"artifact"`          // which Artifacts entry
	Member   string `json:"member"`            // which Keys id unwraps
	Verify   string `json:"verify,omitempty"`  // which Keys id's cert verifies the bundle sig (empty = skip verify)
	Keyexpr  string `json:"keyexpr,omitempty"` // override the artifact key-expression (empty = use the artifact's)
	Expect   Expect `json:"expect"`
}

// Expect is the required outcome.
type Expect struct {
	Result       string `json:"result"`                 // "accept" | "reject"
	PlaintextB64 string `json:"plaintext_b64,omitempty"` // std base64; required when result == "accept"
	Reason       string `json:"reason,omitempty"`        // advisory
}

// Load reads vectors.json from dir.
func Load(dir string) (*Vectors, error) {
	b, err := os.ReadFile(filepath.Join(dir, "vectors.json"))
	if err != nil {
		return nil, err
	}
	var v Vectors
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	return &v, nil
}
