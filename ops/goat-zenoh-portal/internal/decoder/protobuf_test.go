package decoder

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

// buildTrackFDS constructs a FileDescriptorSet containing one message:
//
//	message Track {
//	  string id    = 1;
//	  double lat   = 2;
//	  double lon   = 3;
//	  uint32 speed = 4;
//	}
//
// Returns the serialized FileDescriptorSet + the descriptor so tests
// can build matching payloads.
func buildTrackFDS(t *testing.T) ([]byte, protoreflect.MessageDescriptor) {
	t.Helper()
	syntax := "proto3"
	file := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("track.proto"),
		Package: proto.String("caracal.tracks.v1"),
		Syntax:  &syntax,
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("Track"),
			Field: []*descriptorpb.FieldDescriptorProto{
				{
					Name:   proto.String("id"),
					Number: proto.Int32(1),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				},
				{
					Name:   proto.String("lat"),
					Number: proto.Int32(2),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_DOUBLE.Enum(),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				},
				{
					Name:   proto.String("lon"),
					Number: proto.Int32(3),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_DOUBLE.Enum(),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				},
				{
					Name:   proto.String("speed"),
					Number: proto.Int32(4),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_UINT32.Enum(),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				},
			},
		}},
	}
	fds := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{file}}
	fdsBytes, err := proto.Marshal(fds)
	if err != nil {
		t.Fatalf("marshal fds: %v", err)
	}

	files, err := protodesc.NewFiles(fds)
	if err != nil {
		t.Fatalf("build files: %v", err)
	}
	d, err := files.FindDescriptorByName("caracal.tracks.v1.Track")
	if err != nil {
		t.Fatalf("find Track: %v", err)
	}
	return fdsBytes, d.(protoreflect.MessageDescriptor)
}

// buildPayload constructs a serialized Track message from a map.
// Caller passes id / lat / lon / speed.
func buildPayload(t *testing.T, md protoreflect.MessageDescriptor, id string, lat, lon float64, speed uint32) []byte {
	t.Helper()
	msg := dynamicpb.NewMessage(md)
	fields := md.Fields()
	msg.Set(fields.ByName("id"), protoreflect.ValueOfString(id))
	msg.Set(fields.ByName("lat"), protoreflect.ValueOfFloat64(lat))
	msg.Set(fields.ByName("lon"), protoreflect.ValueOfFloat64(lon))
	msg.Set(fields.ByName("speed"), protoreflect.ValueOfUint32(speed))
	out, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return out
}

func TestDecodeProtobuf_HappyPath(t *testing.T) {
	fdsBytes, md := buildTrackFDS(t)
	payload := buildPayload(t, md, "track-42", 38.8951, -77.0364, 420) // oss-allow: invented test value

	// Try both fully-qualified and bare name.
	for _, name := range []string{"caracal.tracks.v1.Track", "Track"} {
		t.Run(name, func(t *testing.T) {
			out, err := DecodeProtobuf(fdsBytes, name, payload)
			if err != nil {
				t.Fatalf("DecodeProtobuf: %v", err)
			}
			s := string(out)
			for _, want := range []string{`"id":`, `"track-42"`, `"lat":`, `38.8951`, `"lon":`, `-77.0364`, `"speed":`, `420`} {
				if !strings.Contains(s, want) {
					t.Errorf("decoded JSON missing %q: %s", want, s)
				}
			}
		})
	}
}

func TestDecodeProtobuf_EmptyInputs(t *testing.T) {
	t.Run("empty-fds", func(t *testing.T) {
		_, err := DecodeProtobuf(nil, "Track", []byte("x"))
		if err == nil {
			t.Fatalf("expected error on empty fds")
		}
	})
	t.Run("empty-name", func(t *testing.T) {
		fdsBytes, _ := buildTrackFDS(t)
		_, err := DecodeProtobuf(fdsBytes, "", []byte{})
		if err == nil {
			t.Fatalf("expected error on empty msg name")
		}
	})
}

func TestDecodeProtobuf_UnknownMessage(t *testing.T) {
	fdsBytes, _ := buildTrackFDS(t)
	_, err := DecodeProtobuf(fdsBytes, "NotARealMessage", []byte{})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got: %v", err)
	}
}

func TestDecodeProtobuf_AmbiguousBareName(t *testing.T) {
	// Two files, each with a top-level message called "Track" in different packages.
	mk := func(pkg string) *descriptorpb.FileDescriptorProto {
		syntax := "proto3"
		return &descriptorpb.FileDescriptorProto{
			Name:    proto.String(pkg + ".proto"),
			Package: proto.String(pkg),
			Syntax:  &syntax,
			MessageType: []*descriptorpb.DescriptorProto{{
				Name: proto.String("Track"),
				Field: []*descriptorpb.FieldDescriptorProto{{
					Name:   proto.String("id"),
					Number: proto.Int32(1),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				}},
			}},
		}
	}
	fds := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{mk("a"), mk("b")}}
	fdsBytes, err := proto.Marshal(fds)
	if err != nil {
		t.Fatalf("marshal fds: %v", err)
	}
	_, err = DecodeProtobuf(fdsBytes, "Track", []byte{})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous error, got: %v", err)
	}
	// Fully-qualified should still work.
	if _, err := DecodeProtobuf(fdsBytes, "a.Track", []byte{}); err != nil {
		t.Errorf("fully-qualified lookup should succeed: %v", err)
	}
}

func TestDecodeProtobuf_MalformedPayload(t *testing.T) {
	fdsBytes, _ := buildTrackFDS(t)
	// Random bytes that aren't a valid protobuf wire encoding for Track.
	bad := []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	_, err := DecodeProtobuf(fdsBytes, "Track", bad)
	if err == nil {
		t.Fatalf("expected unmarshal error, got nil")
	}
	if !strings.Contains(err.Error(), "unmarshal payload") {
		t.Errorf("error message should mention payload: %v", err)
	}
}

func TestDecodeProtobuf_NestedMessage(t *testing.T) {
	// outer.Inner — nested messages are REFUSED.
	//
	// This test asserted the opposite previously . It was written in #177
	// to cover the nested walk; the write-side validator that
	// refuses nested arrived ten days later in #599, and
	// portal-schema-publishing.md requires the two sides to "agree
	// byte-for-byte". So this never recorded a decision to allow nested — it
	// predated the rule, and the decoder was the side left behind. Flipped
	// rather than deleted, so the gap stays covered from this direction too.
	syntax := "proto3"
	file := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("nest.proto"),
		Package: proto.String("nest"),
		Syntax:  &syntax,
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("Outer"),
			NestedType: []*descriptorpb.DescriptorProto{{
				Name: proto.String("Inner"),
				Field: []*descriptorpb.FieldDescriptorProto{{
					Name:   proto.String("v"),
					Number: proto.Int32(1),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				}},
			}},
		}},
	}
	fds := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{file}}
	fdsBytes, err := proto.Marshal(fds)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	files, _ := protodesc.NewFiles(fds)
	d, _ := files.FindDescriptorByName("nest.Outer.Inner")
	md := d.(protoreflect.MessageDescriptor)
	msg := dynamicpb.NewMessage(md)
	msg.Set(md.Fields().ByName("v"), protoreflect.ValueOfString("hello"))
	payload, _ := proto.Marshal(msg)

	_, err = DecodeProtobuf(fdsBytes, "Inner", payload)
	if err == nil {
		t.Fatal("nested message must be refused — the upload gate refuses it, so a topic naming it cannot exist")
	}
	if !strings.Contains(err.Error(), "NESTED") {
		t.Errorf("error should name the real problem, not read as a typo: %v", err)
	}
}
