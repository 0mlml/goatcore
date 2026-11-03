package main

import (
	"crypto/ecdsa"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// parseSPKIECDSAP256 parses a SubjectPublicKeyInfo DER blob and returns
// the contained ECDSA P-256 public key. ca-init writes pubkeys via
// x509.MarshalPKIXPublicKey; this is the inverse. Block 79 hard cutover
// from Ed25519 to ECDSA P-256.
func parseSPKIECDSAP256(der []byte) (*ecdsa.PublicKey, error) {
	pub, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, err
	}
	ec, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not an ECDSA public key (got %T)", pub)
	}
	if ec.Curve.Params().Name != "P-256" {
		return nil, errors.New("ECDSA public key is not P-256")
	}
	return ec, nil
}

// atomicWrite writes data to path with the given mode by writing to a
// sibling .tmp file and renaming. Avoids leaving a partial file if the
// process is killed mid-write — wg-quick reading a partial conf would
// fail unrecoverably. The .tmp file is created mode 0600 unconditionally
// so a writable-only-by-owner moment never exists, even before rename.
func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp.")
	if err != nil {
		return fmt.Errorf("create tmp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("chmod tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close tmp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
