package welcome

import (
	"strings"
	"testing"
)

// The phone block (ADR 1175): a person with a handset key gets the store app,
// the management server, and the key labeled as the handset key.
func TestRender_phoneBlockUsesTheHandsetKey(t *testing.T) {
	out := Render(zagrosCatalog(),
		Recipient{Name: "New", Handle: "new", Tier: "browse"},
		Artifacts{SetupKey: "K", HandsetKey: "HK"})
	for _, m := range []string{
		"Phone or tablet (iPhone, iPad, Android)",
		"App Store or Google Play",
		"\"Change Server\"",
		"https://netbird.zagros.example",
		"Your handset key",
		"HK",
		"Phones run one VPN at a time",
		"Private DNS",
		"peer-new-<site>-<date>-<device name>",
		"it cannot run the goat",
		"command, publish to the fabric, or receive files",
	} {
		if !strings.Contains(out, m) {
			t.Errorf("phone block missing %q\n---\n%s", m, out)
		}
	}
	if strings.Contains(out, "also works on a phone") {
		t.Errorf("phone block fell back to the device key although a handset key exists\n---\n%s", out)
	}
}

// Without a handset key (a mint that predates it), the device key is offered
// for the phone with the warning that it carries the laptop's access.
func TestRender_phoneBlockFallsBackToTheDeviceKeyWithAWarning(t *testing.T) {
	out := Render(zagrosCatalog(),
		Recipient{Name: "New", Handle: "new", Tier: "platform-admin"},
		Artifacts{SetupKey: "K"})
	for _, m := range []string{
		"Phone or tablet (iPhone, iPad, Android)",
		"Your mesh enrollment key (also works on a phone)",
		"holds the same access as your laptop",
		"ask your operator for a handset key instead",
	} {
		if !strings.Contains(out, m) {
			t.Errorf("phone fallback missing %q\n---\n%s", m, out)
		}
	}
}

// A manifest that declared no handsets (netbird.handset_limit: 0) and no
// handset key: no phone block, no phone note.
func TestRender_phoneBlockAbsentWhenNoHandsetDeclaredAndNoKey(t *testing.T) {
	out := Render(zagrosCatalog(),
		Recipient{Name: "New", Handle: "new", Tier: "browse", NoHandset: true},
		Artifacts{SetupKey: "K"})
	for _, m := range []string{"Phone or tablet", "Phones run one VPN at a time", "Change Server"} {
		if strings.Contains(out, m) {
			t.Errorf("phone block present although handset_limit is 0 and no handset key: %q\n---\n%s", m, out)
		}
	}
	// A handset key that exists anyway is shown even when the manifest said 0.
	out = Render(zagrosCatalog(),
		Recipient{Name: "New", Handle: "new", Tier: "browse", NoHandset: true},
		Artifacts{SetupKey: "K", HandsetKey: "HK"})
	if !strings.Contains(out, "Your handset key") {
		t.Errorf("a minted handset key must be handed over even when the manifest declared 0\n---\n%s", out)
	}
}

// The note letters referenced in step 1's prose (A, B, C) must not shift when
// the phone note is added after them.
func TestRender_phoneNoteDoesNotShiftEarlierNoteLetters(t *testing.T) {
	out := Render(zagrosCatalog(),
		Recipient{Name: "New", Handle: "new", Tier: "browse"},
		Artifacts{SetupKey: "K", HandsetKey: "HK"})
	for _, m := range []string{
		"A. Do not create a netbird.io account",
		"B. Windows: install the Windows client, not one inside WSL",
		"E. Phones run one VPN at a time",
	} {
		if !strings.Contains(out, m) {
			t.Errorf("note lettering: missing %q\n---\n%s", m, out)
		}
	}
}
