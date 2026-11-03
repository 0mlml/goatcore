package main

import (
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/usareur-af/goatcore/ops/enrollment/bundle"
)

// buildGoatCLIExtras constructs the GoatCLIExtras nested struct from the
// --goat-cli-* flags. Returns the zero value when no flags are set (so the
// CBOR omitempty drops the field cleanly + v0.2-and-earlier bundles
// round-trip byte-identically through new parser code). When any flag is
// set, ALL six are required — the bundle is operator-useless without the
// full endpoint + token + cert + key + ca-roots set, so partial population
// is a fail-loud bundle-create error rather than a silent half-install
// surprise at `goat profile init` time. ADR 1019.
//
// Cert/key/CA-roots are loaded from disk; portal-token is also a path
// (the cleartext lives in the per-operator portal-issued tarball; passing
// it as a value would leak it into shell history + process listings).
// Endpoint flags carry the values directly (URLs / Zenoh locators are not
// secret).
func buildGoatCLIExtras(args runArgs) (bundle.GoatCLIExtras, error) {
	anySet := args.GoatCLIPortalEndpoint != "" ||
		args.GoatCLIPortalTokenPath != "" ||
		args.GoatCLIRouterEndpoint != "" ||
		args.GoatCLIMTLSCertPath != "" ||
		args.GoatCLIMTLSKeyPath != "" ||
		args.GoatCLICARootsPath != ""
	if !anySet {
		return bundle.GoatCLIExtras{}, nil
	}

	required := []struct {
		name string
		val  string
	}{
		{"--goat-cli-portal-endpoint", args.GoatCLIPortalEndpoint},
		{"--goat-cli-portal-token", args.GoatCLIPortalTokenPath},
		{"--goat-cli-router-endpoint", args.GoatCLIRouterEndpoint},
		{"--goat-cli-mtls-cert", args.GoatCLIMTLSCertPath},
		{"--goat-cli-mtls-key", args.GoatCLIMTLSKeyPath},
		{"--goat-cli-ca-roots", args.GoatCLICARootsPath},
	}
	for _, r := range required {
		if r.val == "" {
			return bundle.GoatCLIExtras{}, fmt.Errorf("%s is required when any other --goat-cli-* flag is set", r.name)
		}
	}

	if !strings.HasPrefix(args.GoatCLIPortalEndpoint, "https://") {
		return bundle.GoatCLIExtras{}, fmt.Errorf("--goat-cli-portal-endpoint must be https; got %q", args.GoatCLIPortalEndpoint)
	}
	// OPTIONAL: onboarding-engine endpoint (goat-id-console). Validated only when
	// set; a goatnet without the onboard route omits it and the CLI portal-falls-back.
	if args.GoatCLIOnboardEndpoint != "" && !strings.HasPrefix(args.GoatCLIOnboardEndpoint, "https://") {
		return bundle.GoatCLIExtras{}, fmt.Errorf("--goat-cli-onboard-endpoint must be https; got %q", args.GoatCLIOnboardEndpoint)
	}
	if err := validateZenohLocator(args.GoatCLIRouterEndpoint); err != nil {
		return bundle.GoatCLIExtras{}, fmt.Errorf("--goat-cli-router-endpoint: %w", err)
	}

	token, err := loadOpaqueTokenFile(args.GoatCLIPortalTokenPath)
	if err != nil {
		return bundle.GoatCLIExtras{}, fmt.Errorf("--goat-cli-portal-token: %w", err)
	}
	mtlsCert, err := loadPEMWithBlock(args.GoatCLIMTLSCertPath, "CERTIFICATE")
	if err != nil {
		return bundle.GoatCLIExtras{}, fmt.Errorf("--goat-cli-mtls-cert: %w", err)
	}
	mtlsKey, err := loadPEMWithPrivateKey(args.GoatCLIMTLSKeyPath)
	if err != nil {
		return bundle.GoatCLIExtras{}, fmt.Errorf("--goat-cli-mtls-key: %w", err)
	}
	caRoots, err := loadPEMWithBlock(args.GoatCLICARootsPath, "CERTIFICATE")
	if err != nil {
		return bundle.GoatCLIExtras{}, fmt.Errorf("--goat-cli-ca-roots: %w", err)
	}

	return bundle.GoatCLIExtras{
		PortalEndpoint:  args.GoatCLIPortalEndpoint,
		PortalToken:     token,
		RouterEndpoint:  args.GoatCLIRouterEndpoint,
		OnboardEndpoint: args.GoatCLIOnboardEndpoint, // optional; "" is dropped by omitempty
		MTLSCert:        mtlsCert,
		MTLSKey:         mtlsKey,
		CARoots:         caRoots,
	}, nil
}

// validateZenohLocator checks that a router endpoint has one of the
// Zenoh-supported transport-protocol prefixes (tcp/, tls/, quic/, udp/,
// ws/). This is a fail-loud smoke check; the Zenoh session config does
// the strict parse. tls/ is the production form for fabric routers.
func validateZenohLocator(s string) error {
	for _, prefix := range []string{"tls/", "tcp/", "quic/", "udp/", "ws/"} {
		if strings.HasPrefix(s, prefix) {
			return nil
		}
	}
	return fmt.Errorf("must start with a Zenoh transport prefix (tls/, tcp/, quic/, udp/, ws/); got %q", s)
}

// loadOpaqueTokenFile reads a single-token file (one line of opaque
// characters) and returns the bytes with surrounding whitespace
// stripped. Empty file is rejected.
func loadOpaqueTokenFile(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, errors.New("token file is empty")
	}
	return []byte(trimmed), nil
}

// loadPEMWithBlock reads a PEM file and asserts that at least one
// block of the named type is present. Returns the raw PEM bytes
// unchanged so the goat-cli verifier sees exactly what the operator
// supplied (canonical block formatting + comments preserved). The
// device-side parser does the strict ASN.1 walk; this is a smoke
// check that catches obvious paste-error / empty-file mistakes
// before the bundle is signed.
func loadPEMWithBlock(path, wantType string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	rest := raw
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type == wantType {
			return raw, nil
		}
	}
	return nil, fmt.Errorf("no %s block found in %s", wantType, path)
}

// loadPEMWithPrivateKey accepts any of the three common PEM key block
// types (PRIVATE KEY = PKCS#8, EC PRIVATE KEY = SEC1, RSA PRIVATE KEY
// = PKCS#1). The goat-cli verifier handles all three via rustls; this
// helper just asserts at least one is present.
func loadPEMWithPrivateKey(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	rest := raw
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		switch block.Type {
		case "PRIVATE KEY", "EC PRIVATE KEY", "RSA PRIVATE KEY":
			return raw, nil
		}
	}
	return nil, fmt.Errorf("no PRIVATE KEY block found in %s (need PKCS#8, SEC1, or PKCS#1)", path)
}
