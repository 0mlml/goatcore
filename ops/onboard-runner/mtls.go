package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// mtlsClient builds an *http.Client that presents the runner's SPIFFE SVID
// (certFile/keyFile, written by a spiffe-helper sidecar from the SPIRE agent —
// ADR 0412, SPIRE Phase A) and verifies the console's server SVID chains to the
// SPIRE bundle AND carries one of expectedServerIDs as its SPIFFE ID (URI-SAN).
//
// This makes the mint-capable onboarding leg (runner → goat-id-console)
// cryptographically mutually authenticated rather than "same docker bridge + a
// static bearer" (docs/design/runner-console-mtls.md). The bearer is still sent
// for per-handle attribution — this is the transport layer only.
//
// InsecureSkipVerify disables ONLY the stdlib DNS-name check (a SPIFFE leaf has
// no matching DNS name); full chain + SPIFFE-ID verification runs in
// VerifyPeerCertificate. Fail-closed: an empty expected-ID set, an unreadable
// SVID/bundle file, or a server whose SPIFFE ID is not expected is refused.
//
// The SVID keypair and trust bundle are re-read from disk AT EVERY HANDSHAKE
// (GetClientCertificate + a per-call pool in the verifier), never cached in
// the process: SPIRE SVIDs live ~1 hour and the spiffe-helper sidecar rotates
// the files in place, so a load-once client goes stale silently.
// A runner that keeps its startup cert fails every approved-queue poll with
// "remote error: tls: expired certificate" until it is restarted.
// Handshakes happen only on new connections (the transport keeps conns alive),
// so the extra file reads are negligible.
func mtlsClient(certFile, keyFile, bundleFile string, expectedServerIDs []string, timeout time.Duration) (*http.Client, error) {
	if len(expectedServerIDs) == 0 {
		return nil, fmt.Errorf("mtls: no expected server SPIFFE IDs — refusing to dial without a server identity to verify")
	}
	// Load once at startup so misconfiguration (missing files, empty bundle)
	// still fails fast at boot, not at first poll.
	if _, err := tls.LoadX509KeyPair(certFile, keyFile); err != nil {
		return nil, fmt.Errorf("mtls: load runner SVID keypair: %w", err)
	}
	if _, err := loadBundlePool(bundleFile); err != nil {
		return nil, err
	}
	tlsCfg := &tls.Config{
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			cert, err := tls.LoadX509KeyPair(certFile, keyFile)
			if err != nil {
				return nil, fmt.Errorf("mtls: reload runner SVID keypair: %w", err)
			}
			return &cert, nil
		},
		InsecureSkipVerify:    true, // DNS check only; replaced by the verifier below
		MinVersion:            tls.VersionTLS13,
		VerifyPeerCertificate: serverSPIFFEIDVerifier(bundleFile, expectedServerIDs),
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{TLSClientConfig: tlsCfg},
	}, nil
}

// loadBundlePool reads the SPIRE trust bundle into a fresh pool. Called per
// handshake so an in-place bundle rotation is picked up without a restart.
func loadBundlePool(bundleFile string) (*x509.CertPool, error) {
	pem, err := os.ReadFile(bundleFile)
	if err != nil {
		return nil, fmt.Errorf("mtls: read trust bundle %q: %w", bundleFile, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("mtls: trust bundle %q contained no usable certificates", bundleFile)
	}
	return pool, nil
}

// serverSPIFFEIDVerifier verifies the console's leaf chains to the SPIRE
// bundle (re-read from disk per handshake — see mtlsClient) and carries an
// allowed SPIFFE ID. Because InsecureSkipVerify is set, verifiedChains is nil,
// so this builds + verifies the chain itself against the SPIRE bundle before
// checking the SPIFFE ID — a server whose cert does not chain to SPIRE is
// rejected even though the DNS check was skipped.
func serverSPIFFEIDVerifier(bundleFile string, allowed []string) func([][]byte, [][]*x509.Certificate) error {
	allowSet := make(map[string]bool, len(allowed))
	for _, id := range allowed {
		allowSet[strings.TrimSpace(id)] = true
	}
	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		pool, err := loadBundlePool(bundleFile)
		if err != nil {
			return err
		}
		if len(rawCerts) == 0 {
			return fmt.Errorf("mtls: server presented no certificate")
		}
		leaf, err := x509.ParseCertificate(rawCerts[0])
		if err != nil {
			return fmt.Errorf("mtls: parse server leaf: %w", err)
		}
		inter := x509.NewCertPool()
		for _, der := range rawCerts[1:] {
			if c, perr := x509.ParseCertificate(der); perr == nil {
				inter.AddCert(c)
			}
		}
		if _, err := leaf.Verify(x509.VerifyOptions{Roots: pool, Intermediates: inter}); err != nil {
			return fmt.Errorf("mtls: server leaf does not chain to the SPIRE bundle: %w", err)
		}
		for _, u := range leaf.URIs {
			if allowSet[u.String()] {
				return nil
			}
		}
		return fmt.Errorf("mtls: server SPIFFE ID not in expected set %v", allowed)
	}
}
