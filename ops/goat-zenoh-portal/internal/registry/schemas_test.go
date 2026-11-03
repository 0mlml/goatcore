package registry

import (
	"testing"
)

func TestValidSchemaVersion(t *testing.T) {
	cases := []struct {
		label string
		ok    bool
	}{
		{"v1", true},
		{"v2", true},
		{"v10", true},
		{"v99", true},
		{"version1", false},
		{"V1", false},
		{"v", false},
		{"v1.0", false},
		{"1v", false},
		{"", false},
	}
	for _, tc := range cases {
		err := ValidSchemaVersion(tc.label)
		if tc.ok && err != nil {
			t.Errorf("ValidSchemaVersion(%q) = %v, want nil", tc.label, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("ValidSchemaVersion(%q) = nil, want error", tc.label)
		}
	}
}

func TestValidSchemaFormat(t *testing.T) {
	for _, ok := range []string{"protobuf-fds", "protobuf-source", "xsd"} {
		if err := ValidSchemaFormat(ok); err != nil {
			t.Errorf("ValidSchemaFormat(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"json-schema", "asyncapi", "openapi", "yaml", "", "protobuf:Track", "xml"} {
		if err := ValidSchemaFormat(bad); err == nil {
			t.Errorf("ValidSchemaFormat(%q) = nil, want error", bad)
		}
	}
}

func TestValidCompatibility(t *testing.T) {
	for _, ok := range []string{"backward", "forward", "none"} {
		if err := ValidCompatibility(ok); err != nil {
			t.Errorf("ValidCompatibility(%q): %v", ok, err)
		}
	}
	for _, bad := range []string{"BACKWARD", "compatible", "", "any"} {
		if err := ValidCompatibility(bad); err == nil {
			t.Errorf("ValidCompatibility(%q) = nil, want error", bad)
		}
	}
}
