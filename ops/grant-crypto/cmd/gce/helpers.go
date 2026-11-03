package main

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// newFlagSet builds a subcommand flag set with a usage line, erroring (not
// exiting) on parse failure so main can render it uniformly.
func newFlagSet(usage string) *flag.FlagSet {
	fs := flag.NewFlagSet(usage, flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: gce %s\n", usage)
		fs.PrintDefaults()
	}
	return fs
}

// memberFlag collects repeated --member id=path pairs into a map.
type memberFlag map[string]string

func (m *memberFlag) String() string { return fmt.Sprintf("%v", map[string]string(*m)) }

func (m *memberFlag) Set(v string) error {
	id, path, ok := strings.Cut(v, "=")
	if !ok || id == "" || path == "" {
		return fmt.Errorf("--member must be id=path/to/cert.pem, got %q", v)
	}
	if *m == nil {
		*m = memberFlag{}
	}
	if _, dup := (*m)[id]; dup {
		return fmt.Errorf("--member %q specified twice", id)
	}
	(*m)[id] = path
	return nil
}

// readInput returns the contents of path, or stdin when path is empty.
func readInput(path string) ([]byte, error) {
	if path == "" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

// writeOutput writes b to path, or stdout when path is empty.
func writeOutput(path string, b []byte) error {
	if path == "" {
		_, err := os.Stdout.Write(b)
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func writePEM(path, blockType string, der []byte) (err error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, pemMode(blockType))
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, f.Close())
	}()
	return pem.Encode(f, &pem.Block{Type: blockType, Bytes: der})
}

// pemMode locks a private key file to 0600; certs stay world-readable.
func pemMode(blockType string) os.FileMode {
	if strings.Contains(blockType, "PRIVATE") {
		return 0o600
	}
	return 0o644
}

func loadCert(path string) (*x509.Certificate, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	blk, _ := pem.Decode(b)
	if blk == nil {
		return nil, fmt.Errorf("%s: no PEM block", path)
	}
	return x509.ParseCertificate(blk.Bytes)
}

// loadECDHKey loads a PKCS8 EC private key PEM and converts it to the ECDH form
// UnwrapDEK needs. A non-EC key is rejected — the wrap is EC-only by design.
func loadECDHKey(path string) (*ecdh.PrivateKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	blk, _ := pem.Decode(b)
	if blk == nil {
		return nil, fmt.Errorf("%s: no PEM block", path)
	}
	key, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
	if err != nil {
		return nil, err
	}
	ec, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("private key is not EC/P-256")
	}
	return ec.ECDH()
}

// loadECDSAKey loads a PKCS8 EC private key PEM as the ECDSA key used to sign
// bundles (the key-service identity). A non-EC key is rejected.
func loadECDSAKey(path string) (*ecdsa.PrivateKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	blk, _ := pem.Decode(b)
	if blk == nil {
		return nil, fmt.Errorf("%s: no PEM block", path)
	}
	key, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
	if err != nil {
		return nil, err
	}
	ec, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("private key is not EC/P-256")
	}
	return ec, nil
}
