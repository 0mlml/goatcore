package grantcrypto_test

// A runnable, self-verifying tour of the whole grant-crypto Go surface: build an
// Identity, acquire a producer StreamKey and Seal, then Verify → Admit → Open as the
// consumer, and finally Rotate the leaf. It runs under `go test` (the `// Output:`
// block is checked), so it can never rot away from the API.
//
// The signed KeyBundle comes from the committed cross-language conformance vectors
// (the same material the Go reference produces and every binding validates against) —
// the example does not re-implement the key service, matching the binding tests'
// fixture pattern. Its `member` identity has a slot in the `base` bundle, so it acts
// as both producer and consumer here.

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	gc "github.com/usareur-af/goatcore/ops/grant-crypto/sdk/bindings/go"
)

// mustVectors reads the committed vectors without a *testing.T so the runnable Example
// can use them. It mirrors loadVectors (conformance_test.go); the vectors type and
// vectorsDir const are shared across the test package.
func mustVectors() *vectors {
	b, err := os.ReadFile(filepath.Join(vectorsDir, "vectors.json"))
	if err != nil {
		panic(err)
	}
	var v vectors
	if err := json.Unmarshal(b, &v); err != nil {
		panic(err)
	}
	return &v
}

// mustFreshScalar returns a valid 32-byte P-256 scalar to rotate into (a stand-in for
// the member's next enrollment credential).
func mustFreshScalar() []byte {
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	return k.Bytes()
}

// Example walks the full lifecycle: identity → acquire → seal → admit → open → rotate.
func Example() {
	v := mustVectors()

	// Enrollment material for our member, plus the key service's verifying key. In
	// production these come off the fabric and the member's credential store; here they
	// are the committed vectors' throwaway keys.
	memberScalar, err := hex.DecodeString(v.Keys["member"].PrivScalarHex)
	if err != nil {
		panic(err)
	}
	keyServiceVK, err := base64.StdEncoding.DecodeString(v.Keys["keyservice"].PubSpkiB64)
	if err != nil {
		panic(err)
	}
	bundle := []byte(v.Artifacts["base"].Bundle) // one signed KeyBundle (wire JSON)
	bundles, err := json.Marshal([]json.RawMessage{v.Artifacts["base"].Bundle})
	if err != nil {
		panic(err)
	}
	const (
		releaseSet = "coalition-air"
		keyexpr    = "data/coalition-air/cop"
	)

	id, err := gc.NewIdentity("member", memberScalar)
	if err != nil {
		panic(err)
	}
	defer id.Close()

	// --- Producer: acquire a seal-capable key from our own bundle slot, then seal. ---
	// seal takes no recipients — the key service already resolved the audience; a
	// producer only seals under a key it could itself unwrap.
	sk, err := gc.AcquireStreamKey(bundles, releaseSet, id)
	if err != nil {
		panic(err)
	}
	defer sk.Close()
	// Tighten the per-key seal budget so Remaining reads legibly below. The budget is
	// process-cumulative (a re-acquire cannot reset it) and the cap is monotone, so we
	// tighten to leave exactly two seals from here rather than to an absolute value.
	const defaultSealCap = 1 << 24 // grant-crypto's frozen default budget
	rem, err := sk.Remaining()
	if err != nil {
		panic(err)
	}
	if err := sk.SetCap(defaultSealCap - rem + 2); err != nil {
		panic(err)
	}
	envelope, err := sk.Seal(keyexpr, []byte("brigade COP snapshot"))
	if err != nil {
		panic(err)
	}
	remaining, err := sk.Remaining() // one of the two seals used → 1 left
	if err != nil {
		panic(err)
	}

	// --- Consumer: verify the bundle, admit our slot, open the message. ---
	// Consumers MUST verify before admitting; a bad signature stops here.
	if err := gc.VerifyBundle(bundle, keyServiceVK); err != nil {
		panic(err)
	}
	ks, err := gc.NewKeyStore()
	if err != nil {
		panic(err)
	}
	defer ks.Close()
	if _, err := ks.Admit(id, bundle); err != nil {
		panic(err)
	}
	plaintext, err := ks.Open(keyexpr, envelope)
	if err != nil {
		panic(err)
	}

	// --- Rotate: destroy-by-default forward secrecy (re-wraps nothing). ---
	// The receipt is a structured, self-dating fact; we read two stable counts from it.
	receiptJSON, err := id.Rotate(mustFreshScalar(), false, bundles)
	if err != nil {
		panic(err)
	}
	var receipt struct {
		Policy        string `json:"policy"`
		NowUnreadable int    `json:"now_unreadable_generations"`
	}
	if err := json.Unmarshal(receiptJSON, &receipt); err != nil {
		panic(err)
	}

	fmt.Printf("opened: %s\n", plaintext)
	fmt.Printf("seals remaining: %d\n", remaining)
	fmt.Printf("rotation: %s, %d generation now unreadable\n", receipt.Policy, receipt.NowUnreadable)

	// Output:
	// opened: brigade COP snapshot
	// seals remaining: 1
	// rotation: destroy, 1 generation now unreadable
}
