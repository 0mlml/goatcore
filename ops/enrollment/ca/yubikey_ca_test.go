//go:build !nopiv

// yubikey_ca_test.go — covers the hardware-free portions of the
// YubiKey-PIV signer (option validation + card selection logic). The
// hardware-dependent path (Open + GenerateKey + Sign) is exercised by
// yubikey_ca_hardware_test.go which auto-skips when no card is present.
//
// Tests in this file run on every CI machine. The hardware tests run
// only on machines with a YubiKey 5.7+ inserted; that's the operator's
// laptop during ceremony rehearsal.

package ca

import (
	"errors"
	"testing"
)

func withFakeCards(t *testing.T, cards []string, listErr error) {
	t.Helper()
	old := pivCards
	pivCards = func() ([]string, error) {
		if listErr != nil {
			return nil, listErr
		}
		return cards, nil
	}
	t.Cleanup(func() { pivCards = old })
}

func TestSelectCard_EmptyListReturnsErrNoYubikey(t *testing.T) {
	withFakeCards(t, nil, nil)
	_, err := selectCard("")
	if !errors.Is(err, ErrNoYubikey) {
		t.Errorf("got %v; want ErrNoYubikey", err)
	}
}

func TestSelectCard_EmptySelectorReturnsFirst(t *testing.T) {
	withFakeCards(t, []string{"Yubico YubiKey OTP+FIDO+CCID 01", "Yubico YubiKey OTP+FIDO+CCID 02"}, nil)
	got, err := selectCard("")
	if err != nil {
		t.Fatalf("selectCard: %v", err)
	}
	if got != "Yubico YubiKey OTP+FIDO+CCID 01" {
		t.Errorf("got %q; want first card", got)
	}
}

func TestSelectCard_SubstringMatchCaseInsensitive(t *testing.T) {
	withFakeCards(t, []string{"Yubico YubiKey OTP+FIDO+CCID 01", "Yubico YubiKey FIPS 02"}, nil)
	got, err := selectCard("fips")
	if err != nil {
		t.Fatalf("selectCard: %v", err)
	}
	if got != "Yubico YubiKey FIPS 02" {
		t.Errorf("got %q; want the FIPS card", got)
	}
}

func TestSelectCard_NoMatchReturnsErrNoYubikey(t *testing.T) {
	withFakeCards(t, []string{"Yubico YubiKey OTP 01"}, nil)
	_, err := selectCard("nonsense")
	if !errors.Is(err, ErrNoYubikey) {
		t.Errorf("got %v; want ErrNoYubikey", err)
	}
}

func TestSelectCard_PropagatesListErr(t *testing.T) {
	withFakeCards(t, nil, errors.New("pcsc daemon refused"))
	_, err := selectCard("")
	if err == nil {
		t.Fatal("expected list error to propagate")
	}
	if errors.Is(err, ErrNoYubikey) {
		t.Errorf("list-err should NOT be ErrNoYubikey: %v", err)
	}
}

func TestNewYubikeySigner_RequiresCAID(t *testing.T) {
	_, err := NewYubikeySigner(YubikeyOptions{PIN: "123456"})
	if err == nil {
		t.Fatal("expected error for missing CAID")
	}
}

func TestNewYubikeySigner_RequiresPIN(t *testing.T) {
	_, err := NewYubikeySigner(YubikeyOptions{CAID: "x"})
	if err == nil {
		t.Fatal("expected error for missing PIN")
	}
}

func TestNewYubikeySigner_NoCardSurfacesErrNoYubikey(t *testing.T) {
	withFakeCards(t, nil, nil)
	_, err := NewYubikeySigner(YubikeyOptions{CAID: "x", PIN: "123456"})
	if !errors.Is(err, ErrNoYubikey) {
		t.Errorf("got %v; want ErrNoYubikey", err)
	}
}

func TestYubikeyGenerate_RequiresCAID(t *testing.T) {
	_, _, err := YubikeyGenerate(YubikeyGenerateOptions{PIN: "123456"})
	if err == nil {
		t.Fatal("expected error for missing CAID")
	}
}

func TestYubikeyGenerate_RequiresPIN(t *testing.T) {
	_, _, err := YubikeyGenerate(YubikeyGenerateOptions{CAID: "x"})
	if err == nil {
		t.Fatal("expected error for missing PIN")
	}
}

func TestYubikeyGenerate_NoCardSurfacesErrNoYubikey(t *testing.T) {
	withFakeCards(t, nil, nil)
	_, _, err := YubikeyGenerate(YubikeyGenerateOptions{CAID: "x", PIN: "123456"})
	if !errors.Is(err, ErrNoYubikey) {
		t.Errorf("got %v; want ErrNoYubikey", err)
	}
}

// TestErrYubikeyNotImplemented_StillExported guards backward compat:
// callers that pinned to the historical sentinel should keep compiling
// against the post-Block-22K-implemented backend.
func TestErrYubikeyNotImplemented_StillExported(t *testing.T) {
	if ErrYubikeyNotImplemented == nil {
		t.Error("ErrYubikeyNotImplemented removed (was historical sentinel; keep exported for backward compat)")
	}
}