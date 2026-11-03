package envelope_test

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/usareur-af/goatcore/ops/grant-crypto/conformance"
	"github.com/usareur-af/goatcore/ops/grant-crypto/envelope"
)

const vectorsDir = "testdata/vectors"

// TestConformanceVectors runs the committed cross-language vectors through the Go
// reference consumer, in the documented order (verify → slot → unwrap → open),
// and checks every accept/reject decision. The Rust production core must pass the
// same vectors — this is the drift guard between implementations.
func TestConformanceVectors(t *testing.T) {
	v, err := conformance.Load(vectorsDir)
	if err != nil {
		t.Fatalf("load vectors: %v", err)
	}
	if v.Suite != string(envelope.SuiteV1ECDHP256) {
		t.Fatalf("vectors suite = %q, want %q", v.Suite, envelope.SuiteV1ECDHP256)
	}
	if len(v.Cases) == 0 {
		t.Fatal("no cases in vectors")
	}
	for _, c := range v.Cases {
		t.Run(c.Name, func(t *testing.T) {
			accepted, pt := runCase(t, v, c)
			switch c.Expect.Result {
			case "accept":
				if !accepted {
					t.Fatalf("expected accept, got reject")
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
			default:
				t.Fatalf("unknown expected result %q", c.Expect.Result)
			}
		})
	}
}

// runCase is the reference consumer for one vector; returns (accepted, plaintext).
func runCase(t *testing.T, v *conformance.Vectors, c conformance.Case) (bool, []byte) {
	t.Helper()
	art, ok := v.Artifacts[c.Artifact]
	if !ok {
		t.Fatalf("unknown artifact %q", c.Artifact)
	}
	var env envelope.Envelope
	if err := json.Unmarshal(art.Envelope, &env); err != nil {
		t.Fatalf("parse envelope: %v", err)
	}
	bundle, err := envelope.ParseBundle(art.Bundle)
	if err != nil {
		t.Fatalf("parse bundle: %v", err)
	}
	ke := c.Keyexpr
	if ke == "" {
		ke = art.Keyexpr
	}

	// 1. verify the bundle signature (if the case supplies a trust identity).
	if c.Verify != "" {
		pub := signerPub(t, v, c.Verify)
		if envelope.VerifyBundle(bundle, pub) != nil {
			return false, nil
		}
	}
	// 2. the member's slot must exist.
	w, ok := bundle.Members[c.Member]
	if !ok {
		return false, nil
	}
	// 3. unwrap with the member key.
	dek, err := envelope.UnwrapDEK(w, memberKey(t, v, c.Member))
	if err != nil {
		return false, nil
	}
	// 4. open the payload.
	pt, err := envelope.Open(dek, env, []byte(ke))
	if err != nil {
		return false, nil
	}
	return true, pt
}

// signerPub reconstructs a verify public key from the embedded SPKI (base64).
func signerPub(t *testing.T, v *conformance.Vectors, id string) *ecdsa.PublicKey {
	t.Helper()
	der, err := base64.StdEncoding.DecodeString(v.Keys[id].PubSpkiB64)
	if err != nil {
		t.Fatalf("spki decode %q: %v", id, err)
	}
	pub, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		t.Fatalf("parse spki %q: %v", id, err)
	}
	ec, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		t.Fatalf("key %q is not ECDSA", id)
	}
	return ec
}

// memberKey reconstructs an ECDH private key from the embedded P-256 scalar (hex).
func memberKey(t *testing.T, v *conformance.Vectors, id string) *ecdh.PrivateKey {
	t.Helper()
	raw, err := hex.DecodeString(v.Keys[id].PrivScalarHex)
	if err != nil {
		t.Fatalf("scalar decode %q: %v", id, err)
	}
	priv, err := ecdh.P256().NewPrivateKey(raw)
	if err != nil {
		t.Fatalf("scalar to key %q: %v", id, err)
	}
	return priv
}
