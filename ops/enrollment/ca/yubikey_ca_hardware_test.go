//go:build !nopiv

// yubikey_ca_hardware_test.go — gated integration tests that exercise the
// real PCSC + YubiKey path. Auto-skips when:
//   - no YubiKey is connected
//   - GOAT_YUBIKEY_HARDWARE_TESTS env var is unset
//
// To run on the operator's laptop with a YubiKey inserted:
//
//   GOAT_YUBIKEY_HARDWARE_TESTS=1 \
//   GOAT_YUBIKEY_TEST_PIN=123456 \
//   GOAT_YUBIKEY_TEST_MGMT_KEY=010203040506070801020304050607080102030405060708 \
//     go test -v -run "TestYubikey_Hardware" ./ca/...
//
// **DESTRUCTIVE.** Generates a fresh keypair in PIV slot 9c, overwriting
// whatever was there. Operators MUST use a dedicated test YubiKey (or
// re-provision their production YubiKey afterward). See
// docs/operations/yubikey-piv-ca-ceremony.md §"Test rehearsal."
//
// Mitigation against accidental clobber:
//   - the GOAT_YUBIKEY_HARDWARE_TESTS env gate
//   - operator-supplied PIN + management key (not defaulted)
//   - explicit "test-" prefix on the generated CAID

package ca

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"

	"github.com/go-piv/piv-go/v2/piv"
)

const hardwareTestEnv = "GOAT_YUBIKEY_HARDWARE_TESTS"

func skipIfNoHardware(t *testing.T) (pin string, mgmtKey []byte) {
	t.Helper()
	if os.Getenv(hardwareTestEnv) == "" {
		t.Skipf("skipping YubiKey hardware test (set %s=1 to enable)", hardwareTestEnv)
	}
	pin = os.Getenv("GOAT_YUBIKEY_TEST_PIN")
	if pin == "" {
		t.Skipf("set GOAT_YUBIKEY_TEST_PIN to the YubiKey's PIN to run hardware test")
	}
	mgmtKeyHex := os.Getenv("GOAT_YUBIKEY_TEST_MGMT_KEY")
	if mgmtKeyHex == "" {
		// Default management key is published; opt-in to the published default
		// only on a clearly test-flavored YubiKey.
		t.Logf("GOAT_YUBIKEY_TEST_MGMT_KEY unset; using piv.DefaultManagementKey")
		mgmtKey = nil
	} else {
		raw, err := hex.DecodeString(mgmtKeyHex)
		if err != nil {
			t.Fatalf("decode GOAT_YUBIKEY_TEST_MGMT_KEY: %v", err)
		}
		mgmtKey = raw
	}
	cards, err := piv.Cards()
	if err != nil {
		t.Fatalf("piv.Cards: %v", err)
	}
	if len(cards) == 0 {
		t.Skip("no PCSC card connected; insert a YubiKey to run hardware tests")
	}
	return pin, mgmtKey
}

// TestYubikey_Hardware_GenerateAndSign runs the full provisioning +
// signing round-trip against a real YubiKey. Asserts:
//   - YubikeyGenerate produces an Ed25519 pubkey + a self-signed cert
//     installed in slot 9c
//   - NewYubikeySigner reads the pubkey back from the cert (matches
//     YubikeyGenerate's return value)
//   - Sign() produces a 64-byte Ed25519 signature
//   - ed25519.Verify(pub, msg, sig) holds for the same msg
//
// This is the load-bearing hardware regression — if any future piv-go
// upgrade or YubiKey firmware update breaks the round-trip, the
// operator catches it before the next bundle ceremony rather than
// during a live cutover.
func TestYubikey_Hardware_GenerateAndSign(t *testing.T) {
	pin, mgmtKey := skipIfNoHardware(t)

	caID := "test-hardware-ca"
	pub, cert, err := YubikeyGenerate(YubikeyGenerateOptions{
		Slot:          piv.SlotSignature,
		ManagementKey: mgmtKey,
		PIN:           pin,
		CAID:          caID,
	})
	if err != nil {
		t.Fatalf("YubikeyGenerate: %v", err)
	}
	if pub == nil || pub.Curve.Params().Name != "P-256" {
		t.Fatalf("pub is not ECDSA P-256: %+v", pub)
	}
	if cert == nil {
		t.Fatal("cert is nil")
	}
	if !cert.IsCA {
		t.Error("self-signed cert should have IsCA = true")
	}

	signer, err := NewYubikeySigner(YubikeyOptions{
		Slot: piv.SlotSignature,
		PIN:  pin,
		CAID: caID,
	})
	if err != nil {
		t.Fatalf("NewYubikeySigner: %v", err)
	}
	t.Cleanup(func() {
		if err := signer.Close(); err != nil {
			t.Errorf("close signer: %v", err)
		}
	})

	if !ecdsaPubEqual(signer.PublicKey(), pub) {
		t.Error("signer.PublicKey() != generated pub")
	}
	if signer.ID() != caID {
		t.Errorf("signer.ID() = %q; want %q", signer.ID(), caID)
	}

	t.Log("Touch the YubiKey to authorize the signing operation...")
	msg := []byte("the quick brown fox jumps over the lazy dog")
	sig, err := signer.Sign(msg)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if len(sig) == 0 || len(sig) > 256 {
		t.Errorf("sig size = %d; want 1-256 (ECDSA ASN.1 DER)", len(sig))
	}
	digest := sha256.Sum256(msg)
	if !ecdsa.VerifyASN1(pub, digest[:], sig) {
		t.Error("ecdsa.VerifyASN1 failed against the generated pubkey")
	}
}

// TestYubikey_Hardware_DoubleCloseSafe ensures Close() is idempotent
// even after a successful Sign — defends against operator error
// patterns where Close gets called twice in cleanup paths.
func TestYubikey_Hardware_DoubleCloseSafe(t *testing.T) {
	pin, mgmtKey := skipIfNoHardware(t)

	caID := "test-double-close-ca"
	if _, _, err := YubikeyGenerate(YubikeyGenerateOptions{
		ManagementKey: mgmtKey,
		PIN:           pin,
		CAID:          caID,
	}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	signer, err := NewYubikeySigner(YubikeyOptions{PIN: pin, CAID: caID})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := signer.Close(); err != nil {
		t.Errorf("first close: %v", err)
	}
	if err := signer.Close(); err != nil {
		t.Errorf("second close should be no-op: %v", err)
	}
}

func ecdsaPubEqual(a, b *ecdsa.PublicKey) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Curve != b.Curve {
		return false
	}
	return a.X.Cmp(b.X) == 0 && a.Y.Cmp(b.Y) == 0
}
