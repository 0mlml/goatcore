package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testLeaf(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, spiffeID string) *x509.Certificate {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	if spiffeID != "" {
		u, _ := url.Parse(spiffeID)
		tmpl.URIs = []*url.URL{u}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("leaf: %v", err)
	}
	c, _ := x509.ParseCertificate(der)
	return c
}

func TestServerSPIFFEIDVerifier(t *testing.T) {
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "spire-ca"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, KeyUsage: x509.KeyUsageCertSign, BasicConstraintsValid: true,
	}
	caDER, _ := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	ca, _ := x509.ParseCertificate(caDER)
	bundleFile := filepath.Join(t.TempDir(), "bundle.pem")
	if err := os.WriteFile(bundleFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o600); err != nil {
		t.Fatal(err)
	}

	const consoleID = "spiffe://zagros.zagros.example/goat-id-console"
	verify := serverSPIFFEIDVerifier(bundleFile, []string{consoleID})

	// Right ID, trusted CA → accepted (client-side path: build raw chain).
	good := testLeaf(t, ca, caKey, consoleID)
	if err := verify([][]byte{good.Raw}, nil); err != nil {
		t.Fatalf("trusted console SVID should verify: %v", err)
	}
	// Wrong ID → rejected.
	wrong := testLeaf(t, ca, caKey, "spiffe://zagros.zagros.example/imposter")
	if err := verify([][]byte{wrong.Raw}, nil); err == nil {
		t.Fatal("a server with the wrong SPIFFE ID must be rejected")
	}
	// Right ID but untrusted CA → rejected (does not chain to the SPIRE bundle).
	otherKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	otherDER, _ := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &otherKey.PublicKey, otherKey)
	otherCA, _ := x509.ParseCertificate(otherDER)
	rogue := testLeaf(t, otherCA, otherKey, consoleID)
	if err := verify([][]byte{rogue.Raw}, nil); err == nil {
		t.Fatal("a correctly-named server from an untrusted CA must be rejected")
	}
}

func TestMTLSClientFailClosed(t *testing.T) {
	if _, err := mtlsClient("c", "k", "b", nil, time.Second); err == nil {
		t.Fatal("mtlsClient with no expected server IDs must error")
	}
}

// writeKeyPair writes a self-signed leaf + key as PEM files with the given
// serial, returning (certFile, keyFile) — reused across the rotation test.
func writeKeyPair(t *testing.T, dir string, serial int64) (string, string) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		NotBefore:    time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalECPrivateKey(key)
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile
}

// TestMTLSClientReloadsRotatedSVID pins the rule: the
// spiffe-helper rotates the SVID files in place (~hourly), and a client that
// loads the keypair once at startup presents an expired cert forever after.
// The client must pick up an in-place rotation at the next handshake.
func TestMTLSClientReloadsRotatedSVID(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := writeKeyPair(t, dir, 1)
	bundleFile := filepath.Join(dir, "bundle.pem")
	bundlePEM, _ := os.ReadFile(certFile) // any parseable cert works as a bundle here
	if err := os.WriteFile(bundleFile, bundlePEM, 0o600); err != nil {
		t.Fatal(err)
	}
	cli, err := mtlsClient(certFile, keyFile, bundleFile, []string{"spiffe://x/y"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	get := cli.Transport.(*http.Transport).TLSClientConfig.GetClientCertificate
	if get == nil {
		t.Fatal("client must fetch its certificate per handshake, not cache it at startup")
	}
	c1, err := get(nil)
	if err != nil {
		t.Fatal(err)
	}
	// Rotate the files in place, exactly like the spiffe-helper does.
	writeKeyPair(t, dir, 2)
	c2, err := get(nil)
	if err != nil {
		t.Fatal(err)
	}
	s1, _ := x509.ParseCertificate(c1.Certificate[0])
	s2, _ := x509.ParseCertificate(c2.Certificate[0])
	if s1.SerialNumber.Cmp(big.NewInt(1)) != 0 || s2.SerialNumber.Cmp(big.NewInt(2)) != 0 {
		t.Fatalf("handshake must present the CURRENT on-disk SVID: got serials %v then %v, want 1 then 2", s1.SerialNumber, s2.SerialNumber)
	}
}
