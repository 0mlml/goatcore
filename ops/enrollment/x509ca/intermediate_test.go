package x509ca

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"path/filepath"
	"testing"

	"filippo.io/age"

	"github.com/usareur-af/goatcore/ops/enrollment/ca"
)

// TestIntermediate_ChainValidates exercises the F-090 closure path:
// generate an Ed25519 root, sign an ECDSA-P256 intermediate from it,
// issue a leaf chained through the intermediate, then verify the
// leaf chain validates against an OS-style "trust the intermediate"
// posture (which is what macOS keychain trust will do for operators).
func TestIntermediate_ChainValidates(t *testing.T) {
	tmp := t.TempDir()
	caKeyPath := filepath.Join(tmp, "ca.key.age")

	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate age identity: %v", err)
	}
	if _, err := ca.GenerateAndWrite(caKeyPath, []age.Recipient{identity.Recipient()}); err != nil {
		t.Fatalf("ca.GenerateAndWrite: %v", err)
	}
	signer, err := ca.NewAgeSigner("test-ca", caKeyPath, []age.Identity{identity})
	if err != nil {
		t.Fatalf("ca.NewAgeSigner: %v", err)
	}
	t.Cleanup(func() {
		if err := signer.Close(); err != nil {
			t.Errorf("close signer: %v", err)
		}
	})

	// Root + intermediate
	_, rootCert, err := GenerateRootCert(signer, RootCertOptions{
		Organization: "goat",
		CommonName:   "goat Root CA — Test",
	})
	if err != nil {
		t.Fatalf("GenerateRootCert: %v", err)
	}
	intCertPEM, intKeyPEM, err := GenerateIntermediate(signer, rootCert, IntermediateCertOptions{
		CommonName: "goat Intermediate CA — Test",
	})
	if err != nil {
		t.Fatalf("GenerateIntermediate: %v", err)
	}

	// Parse intermediate cert + key from PEM
	intCertBlock, _ := pem.Decode(intCertPEM)
	if intCertBlock == nil {
		t.Fatal("intermediate cert PEM did not decode")
	}
	intCert, err := x509.ParseCertificate(intCertBlock.Bytes)
	if err != nil {
		t.Fatalf("parse intermediate cert: %v", err)
	}
	if !intCert.IsCA {
		t.Fatal("intermediate cert IsCA=false (must be true for chain signing)")
	}
	intKeyBlock, _ := pem.Decode(intKeyPEM)
	if intKeyBlock == nil {
		t.Fatal("intermediate key PEM did not decode")
	}
	intKeyParsed, err := x509.ParsePKCS8PrivateKey(intKeyBlock.Bytes)
	if err != nil {
		t.Fatalf("parse intermediate PKCS8 key: %v", err)
	}
	intKey, ok := intKeyParsed.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatalf("intermediate key is not *ecdsa.PrivateKey: %T", intKeyParsed)
	}

	// Sanity: intermediate signed by root
	if err := intCert.CheckSignatureFrom(rootCert); err != nil {
		t.Fatalf("intermediate not validly signed by root: %v", err)
	}

	// Issue a leaf chained through the intermediate
	leaf, err := IssueCertViaIntermediate(&LeafTemplate{
		CommonName: "test.example.invalid",
		OrgUnit:    "test-site",
		SANs:       []string{"test.example.invalid"},
		Validity:   30 * 24 * 3600 * 1e9, // 30 days in nanoseconds (time.Duration)
		EKU:        []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		KeyType:    LeafKeyECDSAP256,
	}, intKey, intCert)
	if err != nil {
		t.Fatalf("IssueCertViaIntermediate: %v", err)
	}
	leafBlock, _ := pem.Decode(leaf.CertPEM)
	if leafBlock == nil {
		t.Fatal("leaf PEM did not decode")
	}
	leafCert, err := x509.ParseCertificate(leafBlock.Bytes)
	if err != nil {
		t.Fatalf("parse leaf cert: %v", err)
	}

	// Validate leaf against a trust pool containing ONLY the
	// intermediate (mirroring macOS keychain trust state for operators
	// who installed the intermediate per F-090 closure path B). The
	// chain validates without needing the root in the trust pool
	// because cert path validation stops at the first trusted cert.
	intermediatePool := x509.NewCertPool()
	intermediatePool.AddCert(intCert)
	if _, err := leafCert.Verify(x509.VerifyOptions{
		Roots:       intermediatePool,
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		CurrentTime: leafCert.NotBefore.Add(60 * 1e9), // a minute after NotBefore
		DNSName:     "test.example.invalid",
	}); err != nil {
		t.Fatalf("leaf chain did not validate against intermediate-only trust pool: %v", err)
	}

	// NOTE: full chain validation from root (with intermediate as a chain
	// link) is intentionally NOT asserted here. The existing dev-CA root
	// is generated with MaxPathLen=0 / MaxPathLenZero=true (root.go's
	// ADR-0407 single-tier default), which forbids intermediates in the
	// chain. The F-090 closure path B trust posture is "operators trust
	// the INTERMEDIATE in their macOS keychain" — chain validation stops
	// at the trusted intermediate and never reaches the root, so the
	// MaxPathLen constraint on the root is irrelevant for operator-facing
	// browser TLS. If/when intra-mesh service-to-service callers need to
	// chain-validate from the root through the intermediate, root.go
	// must change to MaxPathLen=1 + the dev root must be regenerated.
	// That's an operator-fired ceremony; queued in active-work.yaml as
	// `pki-intermediate-tier-root-pathlen-regen`.
}
