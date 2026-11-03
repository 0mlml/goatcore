package decoder

import (
	"encoding/json"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestRenderDescriptorSet_HappyPath(t *testing.T) {
	fdsBytes, _ := buildTrackFDS(t)
	out, err := RenderDescriptorSet(fdsBytes)
	if err != nil {
		t.Fatalf("RenderDescriptorSet: %v", err)
	}
	if len(out.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(out.Files))
	}
	if out.Files[0].Package != "caracal.tracks.v1" {
		t.Errorf("package = %q want caracal.tracks.v1", out.Files[0].Package)
	}
	if out.Files[0].Syntax != "proto3" {
		t.Errorf("syntax = %q want proto3", out.Files[0].Syntax)
	}
	if len(out.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(out.Messages))
	}
	m := out.Messages[0]
	if m.FullName != "caracal.tracks.v1.Track" {
		t.Errorf("FullName = %q", m.FullName)
	}
	if len(m.Fields) != 4 {
		t.Fatalf("expected 4 fields, got %d", len(m.Fields))
	}
	wantFields := map[string]string{
		"id":    "string",
		"lat":   "double",
		"lon":   "double",
		"speed": "uint32",
	}
	for _, f := range m.Fields {
		want, ok := wantFields[f.Name]
		if !ok {
			t.Errorf("unexpected field %q", f.Name)
			continue
		}
		if f.Type != want {
			t.Errorf("field %s type = %q want %q", f.Name, f.Type, want)
		}
		if f.Cardinality != "singular" {
			t.Errorf("field %s cardinality = %q want singular (proto3 implicit)", f.Name, f.Cardinality)
		}
	}
}

func TestRenderDescriptorSet_Empty(t *testing.T) {
	if _, err := RenderDescriptorSet(nil); err == nil {
		t.Fatalf("expected error on empty input")
	}
	if _, err := RenderDescriptorSet([]byte{0xff, 0xff, 0xff}); err == nil {
		t.Fatalf("expected parse error on garbage")
	}
}

func TestRenderDescriptorSet_RepeatedField(t *testing.T) {
	syntax := "proto3"
	file := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("rep.proto"),
		Package: proto.String("rep"),
		Syntax:  &syntax,
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("Bag"),
			Field: []*descriptorpb.FieldDescriptorProto{{
				Name:   proto.String("items"),
				Number: proto.Int32(1),
				Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				Label:  descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
			}},
		}},
	}
	fds := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{file}}
	b, err := proto.Marshal(fds)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out, err := RenderDescriptorSet(b)
	if err != nil {
		t.Fatalf("RenderDescriptorSet: %v", err)
	}
	if len(out.Messages) != 1 || len(out.Messages[0].Fields) != 1 {
		t.Fatalf("unexpected shape: %+v", out.Messages)
	}
	got := out.Messages[0].Fields[0]
	if got.Cardinality != "repeated" {
		t.Errorf("cardinality = %q want repeated", got.Cardinality)
	}
	if got.Type != "string" {
		t.Errorf("type = %q want string", got.Type)
	}
}

func TestRenderDescriptorSet_Enum(t *testing.T) {
	syntax := "proto3"
	file := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("e.proto"),
		Package: proto.String("e"),
		Syntax:  &syntax,
		EnumType: []*descriptorpb.EnumDescriptorProto{{
			Name: proto.String("State"),
			Value: []*descriptorpb.EnumValueDescriptorProto{
				{Name: proto.String("UNSET"), Number: proto.Int32(0)},
				{Name: proto.String("OPEN"), Number: proto.Int32(1)},
				{Name: proto.String("CLOSED"), Number: proto.Int32(2)},
			},
		}},
	}
	fds := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{file}}
	b, err := proto.Marshal(fds)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out, err := RenderDescriptorSet(b)
	if err != nil {
		t.Fatalf("RenderDescriptorSet: %v", err)
	}
	if len(out.Enums) != 1 {
		t.Fatalf("expected 1 enum, got %d", len(out.Enums))
	}
	if len(out.Enums[0].Values) != 3 {
		t.Fatalf("expected 3 values, got %d", len(out.Enums[0].Values))
	}
}

func TestRenderDescriptorSet_JSONShape(t *testing.T) {
	fdsBytes, _ := buildTrackFDS(t)
	out, err := RenderDescriptorSet(fdsBytes)
	if err != nil {
		t.Fatalf("RenderDescriptorSet: %v", err)
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	// Round-trip sanity — must produce something the frontend can parse.
	var back map[string]any
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	if _, ok := back["files"]; !ok {
		t.Errorf("missing files key")
	}
	if _, ok := back["messages"]; !ok {
		t.Errorf("missing messages key")
	}
}
