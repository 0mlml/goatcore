package spiffemtls

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- test PKI helpers -------------------------------------------------------

type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

func newCA(t *testing.T) *testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ca key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-spire-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("ca cert: %v", err)
	}
	cert, _ := x509.ParseCertificate(der)
	return &testCA{cert: cert, key: key, pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

// leaf issues an SVID-shaped leaf carrying spiffeID as its URI-SAN, signed by
// the CA. Returns cert+key PEM.
func (ca *testCA) leaf(t *testing.T, spiffeID string) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("leaf key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	if spiffeID != "" {
		u, err := url.Parse(spiffeID)
		if err != nil {
			t.Fatalf("parse spiffe id: %v", err)
		}
		tmpl.URIs = []*url.URL{u}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatalf("leaf cert: %v", err)
	}
	keyDER, _ := x509.MarshalPKCS8PrivateKey(key)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}

func writeFiles(t *testing.T, dir string, files map[string][]byte) map[string]string {
	t.Helper()
	paths := map[string]string{}
	for name, data := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		paths[name] = p
	}
	return paths
}

// --- unit tests for the SPIFFE-ID verifier ----------------------------------

func TestPeerSPIFFEIDVerifier(t *testing.T) {
	ca := newCA(t)
	bundleFile := filepath.Join(t.TempDir(), "bundle.pem")
	if err := os.WriteFile(bundleFile, ca.pem, 0o600); err != nil {
		t.Fatal(err)
	}
	const runnerID = "spiffe://zagros.zagros.example/onboard-runner"

	certPEM, _ := ca.leaf(t, runnerID)
	block, _ := pem.Decode(certPEM)
	leaf, _ := x509.ParseCertificate(block.Bytes)

	// Simulate the server side (stdlib supplies verifiedChains).
	verify := peerSPIFFEIDVerifier(bundleFile, []string{runnerID})
	if err := verify(nil, [][]*x509.Certificate{{leaf}}); err != nil {
		t.Fatalf("matching SPIFFE ID should pass: %v", err)
	}

	// Wrong ID rejected.
	verifyOther := peerSPIFFEIDVerifier(bundleFile, []string{"spiffe://zagros.zagros.example/someone-else"})
	if err := verifyOther(nil, [][]*x509.Certificate{{leaf}}); err == nil {
		t.Fatal("non-allowed SPIFFE ID must be rejected")
	}

	// Leaf with no URI-SAN rejected.
	noURIPEM, _ := ca.leaf(t, "")
	nblock, _ := pem.Decode(noURIPEM)
	noURILeaf, _ := x509.ParseCertificate(nblock.Bytes)
	if err := verify(nil, [][]*x509.Certificate{{noURILeaf}}); err == nil {
		t.Fatal("a leaf without a SPIFFE URI-SAN must be rejected")
	}

	// Client side (no verifiedChains): a leaf from an UNTRUSTED CA is rejected
	// even with the right SPIFFE ID, because it does not chain to the bundle.
	other := newCA(t)
	roguePEM, _ := other.leaf(t, runnerID)
	if err := verify([][]byte{decode(t, roguePEM)}, nil); err == nil {
		t.Fatal("a correctly-named leaf from an untrusted CA must be rejected")
	}
	// And a trusted-CA leaf with the right ID passes the client-side path.
	if err := verify([][]byte{decode(t, certPEM)}, nil); err != nil {
		t.Fatalf("trusted leaf, right ID, client-side path should pass: %v", err)
	}
}

func decode(t *testing.T, p []byte) []byte {
	t.Helper()
	b, _ := pem.Decode(p)
	if b == nil {
		t.Fatal("bad pem")
	}
	return b.Bytes
}

// --- end-to-end handshake: ServerConfig ⇄ ClientConfig ----------------------

func TestServerClientHandshake(t *testing.T) {
	ca := newCA(t)
	dir := t.TempDir()
	const (
		runnerID  = "spiffe://zagros.zagros.example/onboard-runner"
		consoleID = "spiffe://zagros.zagros.example/goat-id-console"
	)
	srvCert, srvKey := ca.leaf(t, consoleID)
	cliCert, cliKey := ca.leaf(t, runnerID)
	p := writeFiles(t, dir, map[string][]byte{
		"bundle.pem":   ca.pem,
		"srv-cert.pem": srvCert, "srv-key.pem": srvKey,
		"cli-cert.pem": cliCert, "cli-key.pem": cliKey,
	})

	srvTLS, err := ServerConfig(p["srv-cert.pem"], p["srv-key.pem"], p["bundle.pem"], []string{runnerID})
	if err != nil {
		t.Fatalf("ServerConfig: %v", err)
	}

	// A client with the runner SVID that expects the console SVID → success.
	cliTLS, err := ClientConfig(p["cli-cert.pem"], p["cli-key.pem"], p["bundle.pem"], []string{consoleID})
	if err != nil {
		t.Fatalf("ClientConfig: %v", err)
	}
	if err := handshakeOnce(t, srvTLS, cliTLS); err != nil {
		t.Fatalf("expected minted over handshake, got error: %v", err)
	}

	// A client that expects the WRONG server ID → handshake fails.
	badExpect, _ := ClientConfig(p["cli-cert.pem"], p["cli-key.pem"], p["bundle.pem"], []string{"spiffe://zagros.zagros.example/imposter"})
	if err := handshakeOnce(t, srvTLS, badExpect); err == nil {
		t.Fatal("client expecting the wrong server SPIFFE ID must fail the handshake")
	}

	// A client presenting a non-runner SVID → server rejects it.
	wrongCliCert, wrongCliKey := ca.leaf(t, "spiffe://zagros.zagros.example/not-the-runner")
	wp := writeFiles(t, t.TempDir(), map[string][]byte{"c.pem": wrongCliCert, "k.pem": wrongCliKey, "b.pem": ca.pem})
	wrongCliTLS, _ := ClientConfig(wp["c.pem"], wp["k.pem"], wp["b.pem"], []string{consoleID})
	if err := handshakeOnce(t, srvTLS, wrongCliTLS); err == nil {
		t.Fatal("server must reject a client whose SPIFFE ID is not the runner")
	}
}

func TestConfigFailClosed(t *testing.T) {
	if _, err := ServerConfig("x", "y", "z", nil); err == nil {
		t.Fatal("ServerConfig with an empty allow-list must error")
	}
	if _, err := ClientConfig("x", "y", "z", nil); err == nil {
		t.Fatal("ClientConfig with no expected server IDs must error")
	}
}

func handshakeOnce(t *testing.T, srvCfg, cliCfg *tls.Config) error {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	server := tls.Server(serverConn, srvCfg)
	client := tls.Client(clientConn, cliCfg)
	errCh := make(chan error, 1)
	go func() {
		defer serverConn.Close()
		if err := server.HandshakeContext(context.Background()); err != nil {
			errCh <- err
			return
		}
		_, err := server.Write([]byte("minted"))
		errCh <- err
	}()
	if err := client.HandshakeContext(context.Background()); err != nil {
		clientConn.Close()
		return err
	}
	buf := make([]byte, len("minted"))
	if _, err := io.ReadFull(client, buf); err != nil {
		clientConn.Close()
		return err
	}
	clientConn.Close()
	if err := <-errCh; err != nil {
		return err
	}
	if string(buf) != "minted" {
		return fmt.Errorf("expected minted, got %q", string(buf))
	}
	return nil
}

// TestServerConfigReloadsRotatedSVID pins the console side of SVID
// rotation: the spiffe-helper rotates the ~1-hour SVID files in place, and a
// listener that loads its keypair once at startup serves an expired cert one
// rotation after any restart. GetConfigForClient must build each connection's
// config from the CURRENT on-disk files.
func TestServerConfigReloadsRotatedSVID(t *testing.T) {
	ca := newCA(t)
	dir := t.TempDir()
	bundleFile := filepath.Join(dir, "bundle.pem")
	if err := os.WriteFile(bundleFile, ca.pem, 0o600); err != nil {
		t.Fatal(err)
	}
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	write := func(id string) *x509.Certificate {
		certPEM, keyPEM := ca.leaf(t, id)
		if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
			t.Fatal(err)
		}
		b, _ := pem.Decode(certPEM)
		c, _ := x509.ParseCertificate(b.Bytes)
		return c
	}
	first := write("spiffe://zagros.zagros.example/goat-id-console")
	cfg, err := ServerConfig(certFile, keyFile, bundleFile, []string{"spiffe://x/y"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GetConfigForClient == nil {
		t.Fatal("server must rebuild its config per connection, not cache the startup cert")
	}
	c1, err := cfg.GetConfigForClient(nil)
	if err != nil {
		t.Fatal(err)
	}
	// Rotate in place, exactly like the helper does.
	second := write("spiffe://zagros.zagros.example/goat-id-console")
	c2, err := cfg.GetConfigForClient(nil)
	if err != nil {
		t.Fatal(err)
	}
	s1, _ := x509.ParseCertificate(c1.Certificates[0].Certificate[0])
	s2, _ := x509.ParseCertificate(c2.Certificates[0].Certificate[0])
	if s1.SerialNumber.Cmp(first.SerialNumber) != 0 || s2.SerialNumber.Cmp(second.SerialNumber) != 0 {
		t.Fatalf("per-connection config must present the current on-disk SVID (got %v then %v, want %v then %v)",
			s1.SerialNumber, s2.SerialNumber, first.SerialNumber, second.SerialNumber)
	}
}
