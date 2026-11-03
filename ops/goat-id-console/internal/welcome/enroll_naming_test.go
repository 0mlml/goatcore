package welcome

import (
	"strings"
	"testing"
)

// F-338 / peer-name-grammar: a re-enrollment must CLEAR a stale rosenpass flag
// and must give the peer an attributable name. Both were silently absent.

func TestEnrollCommandExplicitlyDisablesRosenpass(t *testing.T) {
	cmd := EnrollCommand("https://netbird.example.net", "KEY-123", false, "zagros")
	if !strings.Contains(cmd, "--enable-rosenpass=false") {
		t.Fatalf("rosenpass-off command must clear the flag EXPLICITLY; omission does not.\ngot: %s", cmd)
	}
	if !strings.Contains(cmd, "--rosenpass-permissive=false") {
		t.Fatalf("permissive flag also persists and must be cleared.\ngot: %s", cmd)
	}
}

func TestEnrollCommandStillEnablesWhenAsked(t *testing.T) {
	cmd := EnrollCommand("https://netbird.example.net", "KEY-123", true, "zagros")
	if !strings.Contains(cmd, "--enable-rosenpass --rosenpass-permissive") {
		t.Fatalf("rosenpass-on command lost its flags.\ngot: %s", cmd)
	}
	if strings.Contains(cmd, "=false") {
		t.Fatalf("rosenpass-on command must not also disable it.\ngot: %s", cmd)
	}
}

func TestEnrollCommandNamedGivesAnAttributableName(t *testing.T) {
	cmd := EnrollCommandNamed("https://netbird.example.net", "KEY-123", false,
		"peer-jane-doe-zagros-2026-08-15", "zagros")
	if !strings.Contains(cmd, `--hostname "peer-jane-doe-zagros-2026-08-15-$(hostname -s)"`) {
		t.Fatalf("expected a grammar-shaped --hostname with local expansion.\ngot: %s", cmd)
	}
}

// A goatnet that has not adopted named enrollment must not suddenly emit an
// empty --hostname, which NetBird would reject.
func TestEnrollCommandNamedOmitsHostnameWhenBlank(t *testing.T) {
	cmd := EnrollCommandNamed("https://netbird.example.net", "KEY-123", false, "", "zagros")
	if strings.Contains(cmd, "--hostname") {
		t.Fatalf("blank prefix must emit no --hostname at all.\ngot: %s", cmd)
	}
}

// PR #1336 review: the switch to (or creation of) the goatnet's profile must
// happen in the same `up` that sets the management URL (switchOrCreateProfile),
// so sudo cannot split it from the up, and a blank or multi-word goatnet must
// fall back to a placeholder rather than splice nothing, or spaces, into the
// command unquoted.
func TestEnrollCommandNamedCarriesAProfileFlag(t *testing.T) {
	cmd := EnrollCommandNamed("https://netbird.example.net", "KEY-123", false, "", "zagros")
	if !strings.Contains(cmd, "netbird up --profile zagros --management-url") {
		t.Fatalf("expected --profile zagros right after `netbird up`.\ngot: %s", cmd)
	}
}

func TestEnrollCommandNamedBlankGoatnetFallsBackToPlaceholder(t *testing.T) {
	for _, g := range []string{"", "  ", "the data fabric"} {
		cmd := EnrollCommandNamed("https://netbird.example.net", "KEY-123", false, "", g)
		if !strings.Contains(cmd, "--profile <goatnet> ") {
			t.Errorf("goatnet %q must fall back to the <goatnet> placeholder.\ngot: %s", g, cmd)
		}
	}
}
