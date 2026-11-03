// Package decoder turns wire-format payload bytes into human-readable
// JSON using a registered schema.
//
// Block 85 PR-C scope: protobuf only. Given a FileDescriptorSet (.fds)
// bytes + a message name + a serialized payload, returns the message
// fields as JSON. Other formats (json / cbor / msgpack / text / raw)
// already have decoders elsewhere (web/static/decoders.js); only
// protobuf needed schema-fetch + descriptor-driven decode, which is
// what this package adds.
package decoder

import (
	"errors"
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

// DecodeProtobuf parses fdsBytes as a serialized FileDescriptorSet,
// locates the message type matching msgName, and decodes payload
// against that descriptor. Returns indented JSON.
//
// msgName may be either fully-qualified ("foo.bar.Track") or the bare
// type name ("Track"); the descriptor walk handles both.
//
// Errors:
//   - "decoder: parse descriptor set: ..."   — fdsBytes is not a valid
//     FileDescriptorSet.
//   - "decoder: message %q not found ..."    — no matching message in
//     the descriptor set.
//   - "decoder: unmarshal payload: ..."      — payload doesn't parse
//     against the descriptor (wire format mismatch).
func DecodeProtobuf(fdsBytes []byte, msgName string, payload []byte) ([]byte, error) {
	if len(fdsBytes) == 0 {
		return nil, errors.New("decoder: empty descriptor set")
	}
	if msgName == "" {
		return nil, errors.New("decoder: empty message name")
	}

	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(fdsBytes, &fds); err != nil {
		return nil, fmt.Errorf("decoder: parse descriptor set: %w", err)
	}

	files, err := protodesc.NewFiles(&fds)
	if err != nil {
		return nil, fmt.Errorf("decoder: build files: %w", err)
	}

	md, err := findMessage(files, msgName)
	if err != nil {
		return nil, err
	}

	msg := dynamicpb.NewMessage(md)
	if err := proto.Unmarshal(payload, msg); err != nil {
		return nil, fmt.Errorf("decoder: unmarshal payload: %w", err)
	}

	out, err := protojson.MarshalOptions{
		Multiline:       true,
		Indent:          "  ",
		EmitUnpopulated: false,
		UseProtoNames:   true,
	}.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("decoder: marshal json: %w", err)
	}
	return out, nil
}

// findMessage looks up a message descriptor by either fully-qualified
// name or bare type name. Bare names match if exactly one matches;
// ambiguous bare names return an error rather than guessing.
//
// TOP-LEVEL ONLY. A message nested inside another is refused even when the
// name resolves, because the rest of the system already refuses it:
//
//	docs/design/portal-schema-publishing.md   "not a top-level entry in the
//	                                          descriptor ... nested messages
//	                                          don't match by convention"
//	crates/stethoscope-checks schema_body.rs  refuses, and tells the vendor to
//	                                          "fix the source so it's a
//	                                          top-level (not nested) message"
//	decoder/format_validate.go                refuses at upload
//
// That doc requires the write side and the read side to "agree byte-for-byte",
// and this function was the one place that did not — it walked nested messages
// and resolved a nested fully-qualified name, so a topic that could never pass
// upload could still decode here. Removed rather than left as a
// harmless-looking permissive edge: the value of a shared convention is that
// every component applies it, and a decoder quietly accepting more than the
// gate lets through is how "the two sides agree" stops being true without
// anyone editing the sentence that says so.
//
// Both lookups still run, so the error can say WHICH nested message was found
// and where — the same remediation stethoscope gives, rather than a bare
// "not found" that sends someone hunting a typo.
func findMessage(files *protoregistry.Files, name string) (protoreflect.MessageDescriptor, error) {
	// Try fully-qualified first. This resolves nested names too
	// ("Parent.Nested" is a valid full name), so the top-level check below is
	// what enforces the convention, not the absence of a recursive walk.
	if d, err := files.FindDescriptorByName(protoreflect.FullName(name)); err == nil {
		md, ok := d.(protoreflect.MessageDescriptor)
		if !ok {
			return nil, fmt.Errorf("decoder: %q is not a message descriptor", name)
		}
		if err := refuseNested(md, name); err != nil {
			return nil, err
		}
		return md, nil
	}

	// Bare-name walk. Collect every matching descriptor; require exactly one.
	// Nested messages are still collected so a nested-only match produces the
	// actionable error below instead of "not found".
	var matches []protoreflect.MessageDescriptor
	files.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		walkMessages(fd.Messages(), name, &matches)
		return true
	})
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("decoder: message %q not found in descriptor set", name)
	case 1:
		if err := refuseNested(matches[0], name); err != nil {
			return nil, err
		}
		return matches[0], nil
	default:
		fqs := make([]string, 0, len(matches))
		for _, m := range matches {
			fqs = append(fqs, string(m.FullName()))
		}
		return nil, fmt.Errorf("decoder: message %q is ambiguous; matches: %v — use fully-qualified name", name, fqs)
	}
}

// refuseNested returns an error when md is declared inside another message.
// A top-level message's parent is the file; a nested one's parent is a message.
func refuseNested(md protoreflect.MessageDescriptor, declared string) error {
	parent, nested := md.Parent().(protoreflect.MessageDescriptor)
	if !nested {
		return nil
	}
	return fmt.Errorf(
		"decoder: message %q resolves to %q, which is NESTED inside %q — "+
			"topic formats must name a top-level message. Hoist it to top level in the "+
			".proto and re-upload the descriptor set, or declare the parent instead",
		declared, md.FullName(), parent.FullName(),
	)
}

func walkMessages(msgs protoreflect.MessageDescriptors, target string, out *[]protoreflect.MessageDescriptor) {
	for i := 0; i < msgs.Len(); i++ {
		m := msgs.Get(i)
		if string(m.Name()) == target {
			*out = append(*out, m)
		}
		// Recurse into nested messages — see refuseNested. They are collected
		// so the error names them, then rejected.
		walkMessages(m.Messages(), target, out)
	}
}
