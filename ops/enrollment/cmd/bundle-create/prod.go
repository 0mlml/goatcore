//go:build !nopiv

// prod.go — bundle-create's `--mode prod` (YubiKey/PIV) plumbing.
//
// PIV-slot string parsing + PIN resolution + the YubiKey signer
// construction (openYubikeySigner). This file pulls in piv-go, which
// needs CGO + PC/SC, so it is gated behind the default (`!nopiv`) build.
// A `nopiv` build (prod_nopiv.go) stubs openYubikeySigner with an error,
// yielding a CGO-free static binary for dev/age-mode-only use (e.g. the
// goat-zenoh-portal, which only ever mints in age mode).

package main

import (
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/usareur-af/goatcore/ops/enrollment/ca"
	"github.com/go-piv/piv-go/v2/piv"
	"golang.org/x/term"
)

// openYubikeySigner builds a YubiKey-backed CA signer for `--mode prod`.
// Lives here (not main.go) so the whole PIV/CGO surface drops out of a
// `nopiv` build in one unit.
func openYubikeySigner(caID, pivSlotName, cardSelector, pinFlag string) (ca.Signer, error) {
	slot, err := parsePIVSlot(pivSlotName)
	if err != nil {
		return nil, fmt.Errorf("--piv-slot: %w", err)
	}
	pin, err := resolvePIN(pinFlag)
	if err != nil {
		return nil, err
	}
	return ca.NewYubikeySigner(ca.YubikeyOptions{
		CardSelector: cardSelector,
		Slot:         slot,
		PIN:          pin,
		CAID:         caID,
	})
}

// parsePIVSlot accepts "9a" / "9c" / "9d" / "9e" (canonical PIV slot
// names) or hex equivalents and returns the corresponding piv.Slot.
// Slot 9c (Digital Signature) is the canonical choice for the offline
// CA per ADR 0406; default when name is empty.
func parsePIVSlot(name string) (piv.Slot, error) {
	switch strings.ToLower(strings.TrimPrefix(name, "0x")) {
	case "9a":
		return piv.SlotAuthentication, nil
	case "9c", "":
		return piv.SlotSignature, nil
	case "9d":
		return piv.SlotKeyManagement, nil
	case "9e":
		return piv.SlotCardAuthentication, nil
	}
	return piv.Slot{}, fmt.Errorf("unknown slot %q (valid: 9a, 9c, 9d, 9e)", name)
}

// resolvePIN picks the YubiKey PIV PIN from (in order) the explicit
// flag value, the GOAT_CA_PIN env var, or an interactive terminal
// prompt. Errors when none of these provide a value.
//
// The PIN is sensitive; we read from /dev/tty via term.ReadPassword
// (non-echoing) when prompting, and never log it.
func resolvePIN(flagVal string) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}
	if env := os.Getenv("GOAT_CA_PIN"); env != "" {
		return env, nil
	}
	if !term.IsTerminal(int(syscall.Stdin)) {
		return "", fmt.Errorf("PIN required: pass --pin, set GOAT_CA_PIN, or run on a terminal")
	}
	fmt.Fprint(os.Stderr, "Enter YubiKey PIV PIN: ")
	pin, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read PIN: %w", err)
	}
	return string(pin), nil
}
