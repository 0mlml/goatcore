package decoder

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// Write-time validation and read-time decode must resolve a message name the
// SAME way. They did not: the validator compared against bare top-level names
// while findMessage tries fully-qualified first and walks nested messages. A
// format string the decoder would happily use was therefore rejected at upload
// with an error blaming the operator's descriptor.
//
// Hit for real registering an ISA topic for the site-one radar
// feed: `protobuf:DataModel.Wildcard` was correct and was refused.

// fdsWith builds a descriptor set with one package, one top-level message that
// itself contains a nested message.
func fdsWith(t *testing.T, pkg, msg, nested string) []byte {
	t.Helper()
	fds := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{{
			Name:    proto.String("parity_test.proto"),
			Package: proto.String(pkg),
			Syntax:  proto.String("proto3"),
			MessageType: []*descriptorpb.DescriptorProto{{
				Name: proto.String(msg),
				Field: []*descriptorpb.FieldDescriptorProto{{
					Name:   proto.String("v"),
					Number: proto.Int32(1),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				}},
				NestedType: []*descriptorpb.DescriptorProto{{Name: proto.String(nested)}},
			}},
		}},
	}
	b, err := proto.Marshal(fds)
	if err != nil {
		t.Fatalf("marshal fds: %v", err)
	}
	return b
}

func TestValidateFormat_AcceptsFullyQualifiedName(t *testing.T) {
	// THE REGRESSION. Previously 422'd; the decoder resolves it fine.
	fds := fdsWith(t, "DataModel", "Wildcard", "Inner")
	if err := ValidateFormatAgainstFDS("protobuf:DataModel.Wildcard", fds); err != nil {
		t.Fatalf("fully-qualified name rejected at upload but the decoder accepts it: %v", err)
	}
}

func TestValidateFormat_StillAcceptsBareName(t *testing.T) {
	fds := fdsWith(t, "DataModel", "Wildcard", "Inner")
	if err := ValidateFormatAgainstFDS("protobuf:Wildcard", fds); err != nil {
		t.Fatalf("bare name must keep working: %v", err)
	}
}

func TestValidateFormat_StillRejectsNestedMessage(t *testing.T) {
	// Top-level only is DELIBERATE, not an oversight: stethoscope's read-side
	// check refuses a nested message and tells the vendor to promote it. If
	// upload accepted it, the topic would pass write-time and then be flagged
	// at read-time — the disagreement this change exists to remove.
	fds := fdsWith(t, "DataModel", "Wildcard", "Inner")
	if err := ValidateFormatAgainstFDS("protobuf:Inner", fds); err == nil {
		t.Fatal("nested message must still be refused; stethoscope refuses it too")
	}
}

func TestValidateFormat_RejectsWrongPackage(t *testing.T) {
	// A fully-qualified name with the WRONG package must not pass just because
	// the bare half matches.
	fds := fdsWith(t, "DataModel", "Wildcard", "Inner")
	if err := ValidateFormatAgainstFDS("protobuf:SomeOther.Wildcard", fds); err == nil {
		t.Fatal("fully-qualified name with a wrong package must be refused")
	}
}

func TestValidateFormat_UnpackagedFDSAcceptsBareName(t *testing.T) {
	// A .proto with no `package` yields a bare top-level name; the FQN form is
	// the same string, and neither path may double-count or reject it.
	fds := fdsWith(t, "", "PowerState", "Inner")
	if err := ValidateFormatAgainstFDS("protobuf:PowerState", fds); err != nil {
		t.Fatalf("unpackaged descriptor must accept the bare name: %v", err)
	}
}

func TestValidateFormat_StillRejectsAbsentMessage(t *testing.T) {
	// The gate must not become a rubber stamp — this is what caught the two
	// invented names (IsaTrack, AsterixCat062) on the site-one topics.
	fds := fdsWith(t, "DataModel", "Wildcard", "Inner")
	err := ValidateFormatAgainstFDS("protobuf:IsaTrack", fds)
	if err == nil {
		t.Fatal("a message that exists nowhere must still be refused")
	}
	var mismatch *FormatFDSMismatchError
	if !asMismatch(err, &mismatch) {
		t.Fatalf("want FormatFDSMismatchError so the API renders a 422, got %T", err)
	}
	if !strings.Contains(mismatch.Error(), "Wildcard") {
		t.Errorf("error should preview what IS present: %s", mismatch.Error())
	}
	if strings.Contains(mismatch.Error(), "top-level") {
		t.Errorf("error must not claim a top-level-only rule it no longer applies: %s", mismatch.Error())
	}
}

func TestValidateFormat_NonProtobufAndBareProtobufUntouched(t *testing.T) {
	fds := fdsWith(t, "DataModel", "Wildcard", "Inner")
	for _, format := range []string{"json", "raw", "xml:event", "protobuf:"} {
		if err := ValidateFormatAgainstFDS(format, fds); err != nil {
			t.Errorf("format %q must be accepted unchanged: %v", format, err)
		}
	}
}

func TestValidateFormat_UnparseableFDSBlamesTheDescriptor(t *testing.T) {
	// Not a mismatch — a corrupt upload must not be reported as "your format
	// string names a message that isn't there".
	err := ValidateFormatAgainstFDS("protobuf:Whatever", []byte("not a descriptor set"))
	if err == nil {
		t.Fatal("a corrupt descriptor set must be an error")
	}
	var mismatch *FormatFDSMismatchError
	if asMismatch(err, &mismatch) {
		t.Errorf("corrupt fds reported as a format mismatch, which blames the wrong thing: %v", err)
	}
}

// asMismatch is errors.As without importing errors into the test's happy path.
func asMismatch(err error, target **FormatFDSMismatchError) bool {
	if m, ok := err.(*FormatFDSMismatchError); ok {
		*target = m
		return true
	}
	return false
}
