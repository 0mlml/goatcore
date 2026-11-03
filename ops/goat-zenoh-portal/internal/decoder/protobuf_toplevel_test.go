package decoder

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

// docs/design/portal-schema-publishing.md requires the write side and the read
// side to "agree byte-for-byte" on which message a topic format may name, and
// states the convention: top-level only, "nested messages don't match by
// convention". stethoscope's schema-body check enforces it and tells the vendor
// to hoist the message; format_validate.go enforces it at upload.
//
// findMessage did not. It walked nested messages and resolved nested
// fully-qualified names, so a topic that could never pass upload could still
// decode here. These pin the corrected behaviour in both directions.

func toplevelRegistry(t *testing.T, pkg, outer, inner string) *protoregistry.Files {
	t.Helper()
	fds := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{{
			Name:    proto.String("toplevel_test.proto"),
			Package: proto.String(pkg),
			Syntax:  proto.String("proto3"),
			MessageType: []*descriptorpb.DescriptorProto{{
				Name:       proto.String(outer),
				NestedType: []*descriptorpb.DescriptorProto{{Name: proto.String(inner)}},
			}},
		}},
	}
	files, err := protodesc.NewFiles(fds)
	if err != nil {
		t.Fatalf("build files: %v", err)
	}
	return files
}

func TestFindMessage_TopLevelStillResolves(t *testing.T) {
	files := toplevelRegistry(t, "DataModel", "Wildcard", "Inner")
	for _, name := range []string{"Wildcard", "DataModel.Wildcard"} {
		if _, err := findMessage(files, name); err != nil {
			t.Errorf("top-level %q must still resolve: %v", name, err)
		}
	}
}

func TestFindMessage_RefusesNestedByBareName(t *testing.T) {
	files := toplevelRegistry(t, "DataModel", "Wildcard", "Inner")
	_, err := findMessage(files, "Inner")
	if err == nil {
		t.Fatal("nested message must be refused; upload and stethoscope both refuse it")
	}
	if !strings.Contains(err.Error(), "NESTED") {
		t.Errorf("error should say it is nested, not just 'not found': %v", err)
	}
	if !strings.Contains(err.Error(), "DataModel.Wildcard") {
		t.Errorf("error should name the parent so the fix is obvious: %v", err)
	}
}

func TestFindMessage_RefusesNestedByFullyQualifiedName(t *testing.T) {
	// The FQN path resolves "Parent.Nested" perfectly well — dropping the
	// recursive walk alone would NOT have closed this.
	files := toplevelRegistry(t, "DataModel", "Wildcard", "Inner")
	if _, err := findMessage(files, "DataModel.Wildcard.Inner"); err == nil {
		t.Fatal("a nested message named fully-qualified must be refused too")
	}
}

func TestFindMessage_AbsentStillSaysNotFound(t *testing.T) {
	// A genuine typo must not be reported as a nesting problem.
	files := toplevelRegistry(t, "DataModel", "Wildcard", "Inner")
	_, err := findMessage(files, "IsaTrack")
	if err == nil {
		t.Fatal("absent message must error")
	}
	if strings.Contains(err.Error(), "NESTED") {
		t.Errorf("an absent message is not a nesting problem: %v", err)
	}
}
