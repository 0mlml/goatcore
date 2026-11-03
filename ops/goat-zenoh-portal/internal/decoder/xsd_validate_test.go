package decoder

import (
	"errors"
	"strings"
	"testing"
)

// A minimal but realistic XSD with two top-level element declarations and
// one nested one, using the conventional xs: namespace prefix.
const cotXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
  <xs:element name="event">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="point"/>
        <xs:element name="detail"/>
      </xs:sequence>
      <xs:attribute name="uid" type="xs:string"/>
    </xs:complexType>
  </xs:element>
  <xs:element name="events"/>
</xs:schema>`

func TestParseXMLFormat(t *testing.T) {
	cases := []struct {
		in       string
		wantKind string
		wantName string
	}{
		{"xml:event", "xml", "event"},
		{"xml:", "xml", ""},
		{"xml", "xml", ""},
		{"protobuf:Foo", "", ""},
		{"json", "", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		k, n := ParseXMLFormat(c.in)
		if k != c.wantKind || n != c.wantName {
			t.Errorf("ParseXMLFormat(%q) = (%q,%q), want (%q,%q)", c.in, k, n, c.wantKind, c.wantName)
		}
	}
}

func TestTopLevelElementNames(t *testing.T) {
	names, err := TopLevelElementNames([]byte(cotXSD))
	if err != nil {
		t.Fatalf("TopLevelElementNames: %v", err)
	}
	// "event" and "events" are top-level; "point"/"detail" are nested and
	// must NOT appear.
	if len(names) != 2 || names[0] != "event" || names[1] != "events" {
		t.Errorf("got %v, want [event events]", names)
	}
}

// Namespace-prefix-agnostic: xsd: prefix and default namespace both parse.
func TestTopLevelElementNames_PrefixVariants(t *testing.T) {
	variants := []string{
		`<xsd:schema xmlns:xsd="http://www.w3.org/2001/XMLSchema"><xsd:element name="msg"/></xsd:schema>`,
		`<schema xmlns="http://www.w3.org/2001/XMLSchema"><element name="msg"/></schema>`,
	}
	for _, v := range variants {
		names, err := TopLevelElementNames([]byte(v))
		if err != nil {
			t.Fatalf("TopLevelElementNames(%q): %v", v, err)
		}
		if len(names) != 1 || names[0] != "msg" {
			t.Errorf("variant %q: got %v, want [msg]", v, names)
		}
	}
}

func TestTopLevelElementNames_BadInputs(t *testing.T) {
	if _, err := TopLevelElementNames(nil); err == nil {
		t.Errorf("expected error for nil bytes")
	}
	if _, err := TopLevelElementNames([]byte(`<event>not-well-formed`)); err == nil {
		t.Errorf("expected error for non-well-formed XML")
	}
	if _, err := TopLevelElementNames([]byte(`<html><body/></html>`)); err == nil {
		t.Errorf("expected error when root is not <schema>")
	}
}

func TestValidateFormatAgainstXSD_Pass(t *testing.T) {
	if err := ValidateFormatAgainstXSD("xml:event", []byte(cotXSD)); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestValidateFormatAgainstXSD_NameMismatch(t *testing.T) {
	err := ValidateFormatAgainstXSD("xml:track", []byte(cotXSD))
	if err == nil {
		t.Fatalf("expected mismatch error, got nil")
	}
	var xme *FormatXSDMismatchError
	if !errors.As(err, &xme) {
		t.Fatalf("expected *FormatXSDMismatchError, got %T", err)
	}
	if xme.DeclaredName != "track" {
		t.Errorf("declared = %q, want track", xme.DeclaredName)
	}
	if !strings.Contains(err.Error(), "event") {
		t.Errorf("error should mention found names, got: %s", err.Error())
	}
	if got := xme.Suggestion(); got != "xml:event" {
		t.Errorf("Suggestion() = %q, want xml:event", got)
	}
}

func TestValidateFormatAgainstXSD_NonXMLSkipped(t *testing.T) {
	// bare xml + non-xml formats carry no root element to match → skipped.
	for _, c := range []string{"xml", "json", "raw", "protobuf:Foo", ""} {
		if err := ValidateFormatAgainstXSD(c, []byte("not even xml")); err != nil {
			t.Errorf("ValidateFormatAgainstXSD(%q) = %v, want nil (skipped)", c, err)
		}
	}
}

func TestValidateXSDWellFormed(t *testing.T) {
	if err := ValidateXSDWellFormed([]byte(cotXSD)); err != nil {
		t.Errorf("well-formed xsd rejected: %v", err)
	}
	if err := ValidateXSDWellFormed([]byte(`<schema><oops`)); err == nil {
		t.Errorf("expected error for malformed xsd")
	}
}
