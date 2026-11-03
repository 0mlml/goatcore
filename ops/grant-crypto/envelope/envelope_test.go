package envelope

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"testing"
	"time"
)

func mustDEK(t *testing.T) DEK {
	t.Helper()
	k, err := NewDEK()
	if err != nil {
		t.Fatalf("NewDEK: %v", err)
	}
	return k
}

func echKey(t *testing.T) *ecdh.PrivateKey {
	t.Helper()
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return k
}

// ---- payload envelope ----

func TestSealOpenRoundTrip(t *testing.T) {
	dek := mustDEK(t)
	pt := []byte("brigade COP snapshot, releasable to REL-EXAMPLE only")
	env, err := Seal(dek, "ZAGROS", 7, pt, []byte("data/ZAGROS/cop/v1"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	got, err := Open(dek, env, []byte("data/ZAGROS/cop/v1"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(got, pt) {
		t.Fatalf("round-trip mismatch: got %q want %q", got, pt)
	}
}

func TestOpenWrongDEKFails(t *testing.T) {
	env, err := Seal(mustDEK(t), "ZAGROS", 1, []byte("secret"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(mustDEK(t), env, nil); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("wrong DEK: want ErrDecrypt, got %v", err)
	}
}

func TestGenerationIsAuthenticated(t *testing.T) {
	dek := mustDEK(t)
	env, err := Seal(dek, "ZAGROS", 4, []byte("gen-4 payload"), nil)
	if err != nil {
		t.Fatal(err)
	}
	// A consumer holding the gen-4 DEK but presented a ciphertext relabelled
	// gen-5 (or a mismatched release-set) must fail — the generation tag is the
	// revocation lever, so it cannot be spoofable.
	relabelled := env
	relabelled.Generation = 5
	if _, err := Open(dek, relabelled, nil); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("relabelled generation: want ErrDecrypt, got %v", err)
	}
	wrongRS := env
	wrongRS.ReleaseSet = "REL-OTHER"
	if _, err := Open(dek, wrongRS, nil); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("relabelled release-set: want ErrDecrypt, got %v", err)
	}
}

func TestTamperCiphertextFails(t *testing.T) {
	dek := mustDEK(t)
	env, err := Seal(dek, "ZAGROS", 1, []byte("integrity"), nil)
	if err != nil {
		t.Fatal(err)
	}
	env.Ciphertext[0] ^= 0x01
	if _, err := Open(dek, env, nil); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("tampered ciphertext: want ErrDecrypt, got %v", err)
	}
}

func TestAdditionalAADMustMatch(t *testing.T) {
	dek := mustDEK(t)
	env, err := Seal(dek, "ZAGROS", 1, []byte("bound to key expr"), []byte("data/ZAGROS/a"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dek, env, []byte("data/ZAGROS/b")); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("mismatched additional AAD: want ErrDecrypt, got %v", err)
	}
}

func TestShortNonceRejected(t *testing.T) {
	dek := mustDEK(t)
	env, err := Seal(dek, "ZAGROS", 1, []byte("x"), nil)
	if err != nil {
		t.Fatal(err)
	}
	env.Nonce = env.Nonce[:6]
	if _, err := Open(dek, env, nil); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("short nonce: want ErrDecrypt, got %v", err)
	}
}

func TestNoncePerSealDiffers(t *testing.T) {
	dek := mustDEK(t)
	a, _ := Seal(dek, "ZAGROS", 1, []byte("same"), nil)
	b, _ := Seal(dek, "ZAGROS", 1, []byte("same"), nil)
	if bytes.Equal(a.Nonce, b.Nonce) {
		t.Fatal("two seals reused a nonce — GCM nonce reuse is catastrophic")
	}
	if bytes.Equal(a.Ciphertext, b.Ciphertext) {
		t.Fatal("identical plaintext produced identical ciphertext — nonce not applied")
	}
}

// ---- DEK wrapping ----

func TestWrapUnwrapRoundTrip(t *testing.T) {
	dek := mustDEK(t)
	recip := echKey(t)
	w, err := WrapDEK(dek, recip.PublicKey())
	if err != nil {
		t.Fatalf("WrapDEK: %v", err)
	}
	got, err := UnwrapDEK(w, recip)
	if err != nil {
		t.Fatalf("UnwrapDEK: %v", err)
	}
	if got != dek {
		t.Fatal("unwrapped DEK != original")
	}
}

func TestUnwrapWrongRecipientFails(t *testing.T) {
	w, err := WrapDEK(mustDEK(t), echKey(t).PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UnwrapDEK(w, echKey(t)); !errors.Is(err, ErrUnwrap) {
		t.Fatalf("wrong recipient: want ErrUnwrap, got %v", err)
	}
}

func TestUnwrapTamperedWrapFails(t *testing.T) {
	recip := echKey(t)
	w, err := WrapDEK(mustDEK(t), recip.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	w.Ciphertext[0] ^= 0x01
	if _, err := UnwrapDEK(w, recip); !errors.Is(err, ErrUnwrap) {
		t.Fatalf("tampered wrap: want ErrUnwrap, got %v", err)
	}
}

func TestUnwrapSwappedEphemeralFails(t *testing.T) {
	recip := echKey(t)
	w, err := WrapDEK(mustDEK(t), recip.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	// Swap in a different (valid) ephemeral point: the AAD binds it, so the
	// AEAD must reject even though the point parses.
	w.EphemeralPub = echKey(t).PublicKey().Bytes()
	if _, err := UnwrapDEK(w, recip); !errors.Is(err, ErrUnwrap) {
		t.Fatalf("swapped ephemeral: want ErrUnwrap, got %v", err)
	}
}

// ---- SVID cert extraction ----

func selfSignedECCert(t *testing.T) (*x509.Certificate, *ecdh.PrivateKey) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "spiffe://zagros/member"},
		NotBefore:    time.Unix(0, 0),
		NotAfter:     time.Unix(1<<31, 0),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	ep, err := priv.ECDH()
	if err != nil {
		t.Fatal(err)
	}
	return cert, ep
}

func TestRecipientFromECCert(t *testing.T) {
	cert, privECDH := selfSignedECCert(t)
	pub, err := RecipientFromCert(cert)
	if err != nil {
		t.Fatalf("RecipientFromCert: %v", err)
	}
	dek := mustDEK(t)
	w, err := WrapDEK(dek, pub)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnwrapDEK(w, privECDH)
	if err != nil {
		t.Fatalf("unwrap with cert's key: %v", err)
	}
	if got != dek {
		t.Fatal("cert-keyed wrap round-trip mismatch")
	}
}

func TestRecipientFromCertRejectsRSA(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Unix(0, 0), NotAfter: time.Unix(1<<31, 0)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	if _, err := RecipientFromCert(cert); err == nil {
		t.Fatal("RecipientFromCert accepted an RSA SVID; want rejection")
	}
}

// ---- bundle + the full E0 flow ----

func TestBundleMembersUnwrapOnlyTheirOwn(t *testing.T) {
	dek := mustDEK(t)
	members := map[string]*ecdh.PrivateKey{
		"spiffe://zagros/alpha":   echKey(t),
		"spiffe://zagros/bravo":   echKey(t),
		"spiffe://zagros/charlie": echKey(t),
	}
	recips := map[string]RecipientKey{}
	for id, k := range members {
		recips[id] = RecipientKey{Pub: k.PublicKey()}
	}
	bundle, err := NewBundle("ZAGROS", 3, dek, recips)
	if err != nil {
		t.Fatalf("NewBundle: %v", err)
	}
	if topic := BundleTopic(bundle.ReleaseSet, bundle.Generation); topic != "keys/ZAGROS/3" {
		t.Fatalf("BundleTopic = %q, want keys/ZAGROS/3", topic)
	}
	// serialize + parse (the wire round-trip)
	raw, err := bundle.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseBundle(raw)
	if err != nil {
		t.Fatal(err)
	}
	// each member unwraps its own entry
	for id, k := range members {
		w, ok := parsed.Members[id]
		if !ok {
			t.Fatalf("member %q missing from bundle", id)
		}
		got, err := UnwrapDEK(w, k)
		if err != nil {
			t.Fatalf("member %q unwrap: %v", id, err)
		}
		if got != dek {
			t.Fatalf("member %q got wrong DEK", id)
		}
	}
	// a non-member cannot unwrap any entry
	outsider := echKey(t)
	for id, w := range parsed.Members {
		if _, err := UnwrapDEK(w, outsider); !errors.Is(err, ErrUnwrap) {
			t.Fatalf("outsider unwrapped %q: want ErrUnwrap, got %v", id, err)
		}
	}
}

// TestE0EndToEnd is the M2.0 proof: generate a DEK, seal a payload to a
// release-set + generation, publish a single-member key bundle, have the member
// recover the DEK from the bundle and open the payload — while a non-member,
// holding the same public ciphertext + bundle, cannot.
func TestE0EndToEnd(t *testing.T) {
	const rs, gen = "ZAGROS", Generation(1)
	member := echKey(t)
	outsider := echKey(t)

	// key service: mint DEK, wrap to the one member, publish bundle + envelope.
	dek, err := NewDEK()
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := NewBundle(rs, gen, dek, map[string]RecipientKey{"member": {Pub: member.PublicKey()}})
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("REL-EXAMPLE imagery frame")
	env, err := Seal(dek, rs, gen, payload, []byte(BundleTopic(rs, gen)))
	if err != nil {
		t.Fatal(err)
	}

	// member: pull DEK from bundle, open the payload.
	mdek, err := UnwrapDEK(bundle.Members["member"], member)
	if err != nil {
		t.Fatalf("member unwrap: %v", err)
	}
	got, err := Open(mdek, env, []byte(BundleTopic(rs, gen)))
	if err != nil {
		t.Fatalf("member open: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("member decrypted wrong payload")
	}

	// outsider: sees the same envelope + bundle, recovers nothing.
	if _, err := UnwrapDEK(bundle.Members["member"], outsider); !errors.Is(err, ErrUnwrap) {
		t.Fatalf("outsider unwrap: want ErrUnwrap, got %v", err)
	}
	if _, err := Open(mustDEK(t), env, []byte(BundleTopic(rs, gen))); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("outsider open with guessed DEK: want ErrDecrypt, got %v", err)
	}
}
