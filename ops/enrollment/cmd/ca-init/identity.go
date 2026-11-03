package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// publicKeysRel is where validators and the name-snapshot signer's CI guard
// expect a CA public key, relative to the repo root.
const publicKeysRel = "ops/enrollment/public-keys"

// rootSubjectCN returns the root cert's CommonName. An explicit --root-cn
// wins. Without one the CN is derived from the CA id, so no two goatnets'
// roots share a subject and a leaf's issuer names its goatnet (#393). The
// legacy "goat Root CA" default is left to roots already minted.
func rootSubjectCN(explicit, caID string) (cn string, derived bool) {
	if explicit != "" {
		return explicit, false
	}
	return caID + " Root CA", true
}

// publicKeyPath is the file the CA public key is written to, or empty when
// no --public-key-dir was given. There is deliberately no implicit default:
// validators trust every key in public-keys/, and test fixtures run ca-init
// from inside the repo, so writing there must be the operator's explicit ask.
func publicKeyPath(dir, caID string) string {
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, caID+".pem")
}

// refuseExisting fails when path already holds a key. A CA id names exactly
// one key; overwriting it would silently untrust every bundle it signed.
// Called before key generation so a refusal costs nothing.
func refuseExisting(path string) error {
	_, err := os.Stat(path)
	if err == nil {
		return fmt.Errorf("%s already exists: a CA id names one key, pick a new --id", path)
	}
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// writePublicKey creates path with the PEM, refusing to replace a file that
// appeared since refuseExisting ran.
func writePublicKey(path string, pemBytes []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644) //nolint:gosec // public key is public material.
	if err != nil {
		return err
	}
	if _, err := f.Write(pemBytes); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// persistPublicKey writes the PEM to path, or says loudly that it did not.
func persistPublicKey(path string, pemBytes []byte) {
	if path == "" {
		fmt.Fprintf(os.Stderr, "ca-init: WARNING public key NOT persisted (no --public-key-dir). For a goatnet CA pass --public-key-dir %s\n", publicKeysRel)
		return
	}
	if err := writePublicKey(path, pemBytes); err != nil {
		fatal("write public key: %v", err)
	}
	fmt.Fprintf(os.Stderr, "ca-init: wrote public key %s\n", path)
}

// warnDerivedCN names a derived root CN so the operator sees the subject
// the root will carry before it is distributed.
func warnDerivedCN(cn string) {
	fmt.Fprintf(os.Stderr, "ca-init: no --root-cn given; root subject CN derived from the CA id: %q\n", cn)
}

// pubKeyStep is the first next-step line: commit the written file, or save
// the printed PEM by hand when nothing was written.
func pubKeyStep(pubPath, caID string) string {
	if pubPath != "" {
		return "Review " + pubPath + " (written above)."
	}
	return "Save the PEM above to " + publicKeysRel + "/" + caID + ".pem"
}
