// Package spiffemtls builds file-based SPIFFE mTLS tls.Configs for the
// mint-capable onboarding leg (onboard-runner → goat-id-console).
//
// The SVID material (cert, key, trust bundle) is written to a shared volume by
// a spiffe-helper sidecar fetching from the SPIRE agent Workload API — the same
// pattern the fabric-health meter uses (ADR 0412, SPIRE Phase A). This
// package therefore takes plain PEM file paths and does standard crypto/tls
// mTLS, with the peer authenticated by its SPIFFE ID (the cert's URI-SAN)
// rather than by DNS name — so no go-spiffe runtime dependency is pulled in.
//
// It hardens the one privileged internal leg (runner mints identities): without
// it the console authenticates the runner by "membership on the mgmt docker
// bridge + knowledge of a static bearer". With this, transport auth becomes the runner's
// cryptographic SVID. The bearer is kept for per-handle attribution;
// this is additive and default-off (see the console config gate).
package spiffemtls

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/url"
	"os"
	"strings"
)

// ServerConfig builds the tls.Config for the console's mint-capable mTLS
// listener. It presents the console's own SVID (certFile/keyFile) and requires
// a client SVID whose SPIFFE ID (URI-SAN) is one of allowedClientIDs, chained
// to the SPIRE trust bundle (bundleFile). Fail-closed: an empty allow-list, an
// unreadable file, or a peer whose SPIFFE ID is not allowed is rejected.
//
// The SVID keypair and bundle are re-read from disk PER CONNECTION
// (GetConfigForClient): SPIRE SVIDs live ~1 hour and the spiffe-helper
// rotates the files in place, so a load-once server serves an expired cert
// one rotation after any restart. Both directions matter: a client must not
// present a stale cert, and a server must not serve one when a redeploy races
// the helper's rotation. Startup still fail-fasts on unreadable/empty files.
func ServerConfig(certFile, keyFile, bundleFile string, allowedClientIDs []string) (*tls.Config, error) {
	if len(allowedClientIDs) == 0 {
		return nil, fmt.Errorf("spiffemtls: no allowed client SPIFFE IDs — refusing to serve an mTLS listener that would admit any client")
	}
	build := func() (*tls.Config, error) {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("spiffemtls: load server SVID keypair: %w", err)
		}
		pool, err := loadBundle(bundleFile)
		if err != nil {
			return nil, err
		}
		return &tls.Config{
			Certificates: []tls.Certificate{cert},
			ClientCAs:    pool,
			ClientAuth:   tls.RequireAndVerifyClientCert,
			MinVersion:   tls.VersionTLS13,
			// The stdlib chain verification (against ClientCAs) already ran by the
			// time this fires, so verifiedChains is populated; we add the SPIFFE-ID
			// authorization check on top of it.
			VerifyPeerCertificate: peerSPIFFEIDVerifier(bundleFile, allowedClientIDs), //nolint:gosec // custom SPIFFE-ID verifier builds on top of standard chain verification
		}, nil
	}
	base, err := build() // fail-fast at boot on bad files
	if err != nil {
		return nil, err
	}
	base.GetConfigForClient = func(*tls.ClientHelloInfo) (*tls.Config, error) {
		return build() // fresh keypair + bundle for every connection
	}
	return base, nil
}

// ClientConfig builds the tls.Config for the runner's call to the console. It
// presents the runner's SVID and verifies the server SVID chains to the SPIRE
// bundle AND carries one of expectedServerIDs as its SPIFFE ID. InsecureSkipVerify
// disables ONLY the stdlib DNS-name check (a SPIFFE leaf has no matching DNS
// name); full chain + SPIFFE-ID verification is done in VerifyPeerCertificate.
func ClientConfig(certFile, keyFile, bundleFile string, expectedServerIDs []string) (*tls.Config, error) {
	if len(expectedServerIDs) == 0 {
		return nil, fmt.Errorf("spiffemtls: no expected server SPIFFE IDs — refusing to dial without a server identity to verify")
	}
	// Fail-fast at boot on bad files; the handshake paths below re-read.
	if _, err := tls.LoadX509KeyPair(certFile, keyFile); err != nil {
		return nil, fmt.Errorf("spiffemtls: load client SVID keypair: %w", err)
	}
	if _, err := loadBundle(bundleFile); err != nil {
		return nil, err
	}
	return &tls.Config{
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			cert, err := tls.LoadX509KeyPair(certFile, keyFile)
			if err != nil {
				return nil, fmt.Errorf("spiffemtls: reload client SVID keypair: %w", err)
			}
			return &cert, nil
		},
		InsecureSkipVerify:    true, //nolint:gosec // DNS check only; replaced by the verifier below
		MinVersion:            tls.VersionTLS13,
		VerifyPeerCertificate: peerSPIFFEIDVerifier(bundleFile, expectedServerIDs), //nolint:gosec // custom SPIFFE-ID verifier enforces SPIFFE ID and bundle chain
	}, nil
}

// peerSPIFFEIDVerifier returns a tls.Config.VerifyPeerCertificate callback that
// verifies the peer's leaf chains to pool and that its SPIFFE ID (URI-SAN) is in
// allowed. It works for both the server side (verifiedChains pre-populated by
// the stdlib) and the client side (verifiedChains nil under InsecureSkipVerify —
// it builds + verifies the chain itself).
func peerSPIFFEIDVerifier(bundleFile string, allowed []string) func([][]byte, [][]*x509.Certificate) error {
	allowSet := make(map[string]bool, len(allowed))
	for _, id := range allowed {
		allowSet[strings.TrimSpace(id)] = true
	}
	return func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
		// Fresh pool per handshake so an in-place bundle rotation is honored.
		pool, err := loadBundle(bundleFile)
		if err != nil {
			return err
		}
		leaf, err := leafFrom(rawCerts, verifiedChains, pool)
		if err != nil {
			return err
		}
		for _, u := range leaf.URIs {
			if allowSet[u.String()] {
				return nil
			}
		}
		return fmt.Errorf("spiffemtls: peer SPIFFE ID %v not in allow-list %v", spiffeURIs(leaf.URIs), allowed)
	}
}

// leafFrom returns the verified leaf certificate. If the stdlib already built a
// verified chain (server side), it uses chains[0][0]. Otherwise (client side,
// InsecureSkipVerify) it parses rawCerts[0], verifies it against pool, and
// returns it — so a peer whose cert does not chain to the SPIRE bundle is
// rejected even though the stdlib DNS check was skipped.
func leafFrom(rawCerts [][]byte, verifiedChains [][]*x509.Certificate, pool *x509.CertPool) (*x509.Certificate, error) {
	if len(verifiedChains) > 0 && len(verifiedChains[0]) > 0 {
		return verifiedChains[0][0], nil
	}
	if len(rawCerts) == 0 {
		return nil, fmt.Errorf("spiffemtls: peer presented no certificate")
	}
	leaf, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return nil, fmt.Errorf("spiffemtls: parse peer leaf: %w", err)
	}
	inter := x509.NewCertPool()
	for _, der := range rawCerts[1:] {
		if c, perr := x509.ParseCertificate(der); perr == nil {
			inter.AddCert(c)
		}
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: pool, Intermediates: inter}); err != nil {
		return nil, fmt.Errorf("spiffemtls: peer leaf does not chain to the SPIRE bundle: %w", err)
	}
	return leaf, nil
}

func loadBundle(bundleFile string) (*x509.CertPool, error) {
	pem, err := os.ReadFile(bundleFile)
	if err != nil {
		return nil, fmt.Errorf("spiffemtls: read trust bundle %q: %w", bundleFile, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("spiffemtls: trust bundle %q contained no usable certificates", bundleFile)
	}
	return pool, nil
}

func spiffeURIs(uris []*url.URL) []string {
	out := make([]string, 0, len(uris))
	for _, u := range uris {
		out = append(out, u.String())
	}
	return out
}
