package decoder

import (
	"errors"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

func buildFDS(t *testing.T, messages ...string) []byte {
	t.Helper()
	syntax := "proto3"
	msgs := make([]*descriptorpb.DescriptorProto, len(messages))
	for i, n := range messages {
		name := n
		msgs[i] = &descriptorpb.DescriptorProto{Name: &name}
	}
	pkg := "test"
	fname := "test.proto"
	fds := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{{
			Name:        &fname,
			Package:     &pkg,
			Syntax:      &syntax,
			MessageType: msgs,
		}},
	}
	b, err := proto.Marshal(fds)
	if err != nil {
		t.Fatalf("marshal fds: %v", err)
	}
	return b
}

func TestParseProtobufFormat(t *testing.T) {
	cases := []struct {
		in       string
		wantKind string
		wantName string
	}{
		{"protobuf:DenObservation", "protobuf", "DenObservation"},
		{"protobuf:", "protobuf", ""},
		{"json", "", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		k, n := ParseProtobufFormat(c.in)
		if k != c.wantKind || n != c.wantName {
			t.Errorf("ParseProtobufFormat(%q) = (%q,%q), want (%q,%q)", c.in, k, n, c.wantKind, c.wantName)
		}
	}
}

func TestTopLevelMessageNames(t *testing.T) {
	fds := buildFDS(t, "Alpha", "Beta")
	names, err := TopLevelMessageNames(fds)
	if err != nil {
		t.Fatalf("TopLevelMessageNames: %v", err)
	}
	if len(names) != 2 || names[0] != "Alpha" || names[1] != "Beta" {
		t.Errorf("got %v, want [Alpha Beta]", names)
	}
}

func TestTopLevelMessageNames_EmptyInputs(t *testing.T) {
	if _, err := TopLevelMessageNames(nil); err == nil {
		t.Errorf("expected error for nil bytes")
	}
	if _, err := TopLevelMessageNames([]byte{0xff, 0xff}); err == nil {
		t.Errorf("expected decode error for garbage bytes")
	}
}

func TestValidateFormatAgainstFDS_Pass(t *testing.T) {
	fds := buildFDS(t, "DenObservation", "Status")
	if err := ValidateFormatAgainstFDS("protobuf:DenObservation", fds); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestValidateFormatAgainstFDS_NameMismatch(t *testing.T) {
	fds := buildFDS(t, "Status", "SoftwareVersion")
	err := ValidateFormatAgainstFDS("protobuf:MyMessage", fds)
	if err == nil {
		t.Fatalf("expected mismatch error, got nil")
	}
	var fme *FormatFDSMismatchError
	if !errors.As(err, &fme) {
		t.Fatalf("expected *FormatFDSMismatchError, got %T", err)
	}
	if fme.DeclaredName != "MyMessage" {
		t.Errorf("declared = %q, want MyMessage", fme.DeclaredName)
	}
	if !strings.Contains(err.Error(), "Status") {
		t.Errorf("error should mention found names, got: %s", err.Error())
	}
	if got := fme.Suggestion(); got != "protobuf:Status" {
		t.Errorf("Suggestion() = %q, want protobuf:Status", got)
	}
}

func TestValidateFormatAgainstFDS_NonProtobufSkipped(t *testing.T) {
	// json + bare protobuf + empty are all accepted at write-time.
	cases := []string{"json", "raw", "cbor", "protobuf:", ""}
	for _, c := range cases {
		if err := ValidateFormatAgainstFDS(c, []byte{0xff}); err != nil {
			t.Errorf("ValidateFormatAgainstFDS(%q) = %v, want nil (skipped)", c, err)
		}
	}
}

func TestValidateFormatAgainstFDS_NestedNotMatched(t *testing.T) {
	// Top-level only — a nested message of the same name is NOT a match,
	// mirroring the read-side schema-body check's contract.
	syntax := "proto3"
	pkg := "test"
	fname := "test.proto"
	outer := "Outer"
	inner := "Inner"
	fds := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{{
			Name:    &fname,
			Package: &pkg,
			Syntax:  &syntax,
			MessageType: []*descriptorpb.DescriptorProto{{
				Name:       &outer,
				NestedType: []*descriptorpb.DescriptorProto{{Name: &inner}},
			}},
		}},
	}
	body, _ := proto.Marshal(fds)
	if err := ValidateFormatAgainstFDS("protobuf:Inner", body); err == nil {
		t.Errorf("expected mismatch — nested types should not match")
	}
}
