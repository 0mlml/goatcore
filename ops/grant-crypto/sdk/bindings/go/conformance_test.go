package grantcrypto_test

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	gc "github.com/usareur-af/goatcore/ops/grant-crypto/sdk/bindings/go"
)

// The shared cross-language decrypt-direction vectors live outside the SDK subtree
// (with the Go reference implementation). This binding is its own module, so rather
// than take a cross-module dependency on the conformance package we mirror its schema
// locally and read the committed JSON directly — keeping the SDK subtree self-contained
// (grant-crypto-sdk.md: "no path-deps outside the subtree"). The schema is a verbatim
// copy of ops/grant-crypto/conformance/conformance.go.
const vectorsDir = "../../../envelope/testdata/vectors"

type vectors struct {
	Suite     string              `json:"suite"`
	Note      string              `json:"note"`
	Keys      map[string]keyRef   `json:"keys"`
	Artifacts map[string]artifact `json:"artifacts"`
	Cases     []vectorCase        `json:"cases"`
}

type keyRef struct {
	PrivScalarHex string `json:"priv_scalar_hex,omitempty"`
	PubSpkiB64    string `json:"pub_spki_b64,omitempty"`
}

type artifact struct {
	Envelope json.RawMessage `json:"envelope"`
	Bundle   json.RawMessage `json:"bundle"`
	Keyexpr  string          `json:"keyexpr"`
}

type vectorCase struct {
	Name     string `json:"name"`
	Artifact string `json:"artifact"`
	Member   string `json:"member"`
	Verify   string `json:"verify,omitempty"`
	Keyexpr  string `json:"keyexpr,omitempty"`
	Expect   struct {
		Result       string `json:"result"`
		PlaintextB64 string `json:"plaintext_b64,omitempty"`
		Reason       string `json:"reason,omitempty"`
	} `json:"expect"`
}

func loadVectors(t *testing.T) *vectors {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(vectorsDir, "vectors.json"))
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	var v vectors
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("parse vectors: %v", err)
	}
	return &v
}

func memberScalar(t *testing.T, v *vectors, id string) []byte {
	t.Helper()
	raw, err := hex.DecodeString(v.Keys[id].PrivScalarHex)
	if err != nil {
		t.Fatalf("scalar decode %q: %v", id, err)
	}
	return raw
}

func signerVK(t *testing.T, v *vectors, id string) []byte {
	t.Helper()
	der, err := base64.StdEncoding.DecodeString(v.Keys[id].PubSpkiB64)
	if err != nil {
		t.Fatalf("spki decode %q: %v", id, err)
	}
	return der
}

// TestConformanceVectors is the merge gate: every committed vector must produce the
// same accept/reject decision (and, on accept, the exact plaintext) through the
// binding as through the Rust core and the Go reference. It runs the documented
// consumer order verify → admit(slot) → open, and additionally asserts each reject
// lands on a recognized sentinel error consistent with the vector's advisory reason —
// never a random failure, and never collapsing the capability signal (ErrUnsupportedSuite)
// or the signature reject (ErrBadSignature) into a generic decrypt error.
func TestConformanceVectors(t *testing.T) {
	v := loadVectors(t)
	if v.Suite != "v1-ecdh-p256" {
		t.Fatalf("vectors suite = %q, want v1-ecdh-p256", v.Suite)
	}
	if len(v.Cases) == 0 {
		t.Fatal("no cases in vectors")
	}

	for _, c := range v.Cases {
		t.Run(c.Name, func(t *testing.T) {
			accepted, pt, rejErr := runCase(t, v, c)
			switch c.Expect.Result {
			case "accept":
				if !accepted {
					t.Fatalf("expected accept, got reject: %v", rejErr)
				}
				want, err := base64.StdEncoding.DecodeString(c.Expect.PlaintextB64)
				if err != nil {
					t.Fatalf("bad expected plaintext: %v", err)
				}
				if string(pt) != string(want) {
					t.Fatalf("plaintext = %q, want %q", pt, want)
				}
			case "reject":
				if accepted {
					t.Fatalf("expected reject (%s), got accept", c.Expect.Reason)
				}
				if rejErr == nil {
					t.Fatal("reject produced a nil error")
				}
				assertReasonSentinel(t, c.Expect.Reason, rejErr)
			default:
				t.Fatalf("unknown expected result %q", c.Expect.Result)
			}
		})
	}
}

// runCase drives one vector through the public binding surface and returns
// (accepted, plaintext, rejectError). It mirrors the reference consumer order.
func runCase(t *testing.T, v *vectors, c vectorCase) (bool, []byte, error) {
	t.Helper()
	art, ok := v.Artifacts[c.Artifact]
	if !ok {
		t.Fatalf("unknown artifact %q", c.Artifact)
	}
	ke := c.Keyexpr
	if ke == "" {
		ke = art.Keyexpr
	}

	id, err := gc.NewIdentity(c.Member, memberScalar(t, v, c.Member))
	if err != nil {
		t.Fatalf("new identity %q: %v", c.Member, err)
	}
	defer id.Close()

	// 1. verify the bundle signature (if the case supplies a trust identity).
	if c.Verify != "" {
		if err := gc.VerifyBundle(art.Bundle, signerVK(t, v, c.Verify)); err != nil {
			return false, nil, err
		}
	}
	// 2 + 3. admit unwraps this member's slot (absent slot -> hadSlot false).
	ks, err := gc.NewKeyStore()
	if err != nil {
		t.Fatalf("new keystore: %v", err)
	}
	defer ks.Close()
	hadSlot, err := ks.Admit(id, art.Bundle)
	if err != nil {
		return false, nil, err
	}
	if !hadSlot {
		return false, nil, gc.ErrNoSlot
	}
	// 4. open the payload with the case key-expression.
	pt, err := ks.Open(ke, art.Envelope)
	if err != nil {
		return false, nil, err
	}
	return true, pt, nil
}

// assertReasonSentinel checks the reject error is one the vector's advisory reason
// permits for this binding. Reasons are advisory (the contract is the accept/reject
// decision), but the capability-vs-reject distinction is load-bearing, so we pin the
// unambiguous ones. "open-failed" admits both ErrDecrypt (AEAD failure: wrong
// key-expression, tampered ciphertext) and ErrNoSlot: in the keystore model a
// wrong-generation envelope finds no held DEK for its (rs, gen) and is rejected at
// lookup rather than at the AEAD — a benign difference from the reference, same
// accept/reject decision.
func assertReasonSentinel(t *testing.T, reason string, err error) {
	t.Helper()
	var allowed []error
	switch reason {
	case "no-slot":
		allowed = []error{gc.ErrNoSlot}
	case "bad-signature":
		allowed = []error{gc.ErrBadSignature}
	case "unsupported-suite":
		allowed = []error{gc.ErrUnsupportedSuite}
	case "open-failed":
		allowed = []error{gc.ErrDecrypt, gc.ErrNoSlot}
	default:
		return // unrecognized advisory reason: non-nil error already asserted
	}
	for _, a := range allowed {
		if errors.Is(err, a) {
			return
		}
	}
	t.Fatalf("reason %q: error %v is not one of the expected sentinels %v", reason, err, allowed)
}
