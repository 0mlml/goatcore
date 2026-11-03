// Reconstructs .proto source text from a parsed FileDescriptorSet.
// Used by the schema viewer's "Source" tab — operators want to see the
// declaration in the familiar shape, not the raw descriptor binary.
//
// Comments are recovered from source_code_info when protoc was invoked
// with --include_source_info (the build-fds.sh script in
// ops/zenoh-trial-peers/synth-vendor-schemas/ does this). Without that
// flag, the output is structurally accurate but un-commented.

package decoder

import (
	"fmt"
	"sort"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// WriteProtoText emits one .proto file's worth of text for each
// FileDescriptor in the registered descriptor set. Returns a map keyed
// by file path so the UI can present multi-file sets as separate
// tabs or accordion panels.
func WriteProtoText(fdsBytes []byte) (map[string]string, error) {
	files, err := loadFilesFromBytes(fdsBytes)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string)
	files.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		out[fd.Path()] = writeFile(fd)
		return true
	})
	return out, nil
}

// writeFile emits a single .proto file as text. The shape mirrors what
// `protoc` would have accepted as input — readable in any editor that
// knows the .proto grammar.
func writeFile(fd protoreflect.FileDescriptor) string {
	var b strings.Builder
	// Header.
	switch fd.Syntax() {
	case protoreflect.Proto2:
		b.WriteString("syntax = \"proto2\";\n\n")
	case protoreflect.Proto3:
		b.WriteString("syntax = \"proto3\";\n\n")
	}
	if pkg := fd.Package(); pkg != "" {
		fmt.Fprintf(&b, "package %s;\n\n", pkg)
	}
	// Top-level enums + messages, in declaration order.
	enums := fd.Enums()
	for i := 0; i < enums.Len(); i++ {
		writeEnum(&b, enums.Get(i), 0)
		b.WriteString("\n")
	}
	msgs := fd.Messages()
	for i := 0; i < msgs.Len(); i++ {
		writeMessage(&b, msgs.Get(i), 0)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func writeMessage(b *strings.Builder, md protoreflect.MessageDescriptor, indent int) {
	if md.IsMapEntry() {
		return // surfaced via the parent field's map<K,V>; don't double-render.
	}
	writeComment(b, md, indent)
	fmt.Fprintf(b, "%smessage %s {\n", pad(indent), md.Name())

	// Reserved ranges + names.
	rr := md.ReservedRanges()
	if rr.Len() > 0 {
		var parts []string
		for i := 0; i < rr.Len(); i++ {
			r := rr.Get(i)
			if r[0]+1 == r[1] {
				parts = append(parts, fmt.Sprintf("%d", r[0]))
			} else {
				parts = append(parts, fmt.Sprintf("%d to %d", r[0], r[1]-1))
			}
		}
		fmt.Fprintf(b, "%sreserved %s;\n", pad(indent+1), strings.Join(parts, ", "))
	}
	rn := md.ReservedNames()
	if rn.Len() > 0 {
		var parts []string
		for i := 0; i < rn.Len(); i++ {
			parts = append(parts, fmt.Sprintf("%q", rn.Get(i)))
		}
		fmt.Fprintf(b, "%sreserved %s;\n", pad(indent+1), strings.Join(parts, ", "))
	}

	// Nested enums + messages first so they're available to field decls.
	en := md.Enums()
	for i := 0; i < en.Len(); i++ {
		writeEnum(b, en.Get(i), indent+1)
	}
	nm := md.Messages()
	for i := 0; i < nm.Len(); i++ {
		child := nm.Get(i)
		if child.IsMapEntry() {
			continue
		}
		writeMessage(b, child, indent+1)
	}

	// Group fields by oneof so oneof blocks render correctly.
	fields := md.Fields()
	oneofs := md.Oneofs()
	emittedInOneof := map[int]bool{}
	for j := 0; j < oneofs.Len(); j++ {
		o := oneofs.Get(j)
		if o.IsSynthetic() {
			continue
		}
		fmt.Fprintf(b, "%soneof %s {\n", pad(indent+1), o.Name())
		of := o.Fields()
		for k := 0; k < of.Len(); k++ {
			f := of.Get(k)
			writeField(b, f, indent+2)
			emittedInOneof[int(f.Number())] = true
		}
		fmt.Fprintf(b, "%s}\n", pad(indent+1))
	}
	// Top-level fields (not in a real oneof).
	for j := 0; j < fields.Len(); j++ {
		f := fields.Get(j)
		if emittedInOneof[int(f.Number())] {
			continue
		}
		writeField(b, f, indent+1)
	}

	fmt.Fprintf(b, "%s}\n", pad(indent))
}

func writeField(b *strings.Builder, f protoreflect.FieldDescriptor, indent int) {
	writeComment(b, f, indent)
	prefix := ""
	if f.IsMap() {
		// map<K,V> doesn't take a label.
		k := protoText(f.MapKey())
		v := protoText(f.MapValue())
		fmt.Fprintf(b, "%smap<%s, %s> %s = %d;\n", pad(indent), k, v, f.Name(), f.Number())
		return
	}
	if f.IsList() {
		prefix = "repeated "
	} else if f.HasOptionalKeyword() {
		prefix = "optional "
	}
	fmt.Fprintf(b, "%s%s%s %s = %d;\n", pad(indent), prefix, protoText(f), f.Name(), f.Number())
}

func writeEnum(b *strings.Builder, ed protoreflect.EnumDescriptor, indent int) {
	writeComment(b, ed, indent)
	fmt.Fprintf(b, "%senum %s {\n", pad(indent), ed.Name())
	vs := ed.Values()
	for j := 0; j < vs.Len(); j++ {
		v := vs.Get(j)
		writeComment(b, v, indent+1)
		fmt.Fprintf(b, "%s%s = %d;\n", pad(indent+1), v.Name(), v.Number())
	}
	fmt.Fprintf(b, "%s}\n", pad(indent))
}

// writeComment emits leading-detached + leading comments above the
// declaration, if source_code_info is present. No-op when missing.
func writeComment(b *strings.Builder, d protoreflect.Descriptor, indent int) {
	loc := d.ParentFile().SourceLocations().ByDescriptor(d)
	for _, c := range loc.LeadingDetachedComments {
		writeCommentBlock(b, c, indent)
		b.WriteString("\n")
	}
	if loc.LeadingComments != "" {
		writeCommentBlock(b, loc.LeadingComments, indent)
	}
}

func writeCommentBlock(b *strings.Builder, comment string, indent int) {
	for _, line := range strings.Split(strings.TrimRight(comment, "\n"), "\n") {
		// Comments in source_code_info don't include the // marker — protoc
		// strips them. Re-add the marker; preserve user spacing after it.
		if line == "" {
			fmt.Fprintf(b, "%s//\n", pad(indent))
		} else {
			fmt.Fprintf(b, "%s//%s\n", pad(indent), line)
		}
	}
}

// protoText returns the .proto-syntax type for a scalar / message /
// enum field. Mirrors WriteProtoText in protoc itself.
func protoText(f protoreflect.FieldDescriptor) string {
	switch f.Kind() {
	case protoreflect.BoolKind:
		return "bool"
	case protoreflect.Int32Kind:
		return "int32"
	case protoreflect.Sint32Kind:
		return "sint32"
	case protoreflect.Sfixed32Kind:
		return "sfixed32"
	case protoreflect.Uint32Kind:
		return "uint32"
	case protoreflect.Fixed32Kind:
		return "fixed32"
	case protoreflect.Int64Kind:
		return "int64"
	case protoreflect.Sint64Kind:
		return "sint64"
	case protoreflect.Sfixed64Kind:
		return "sfixed64"
	case protoreflect.Uint64Kind:
		return "uint64"
	case protoreflect.Fixed64Kind:
		return "fixed64"
	case protoreflect.FloatKind:
		return "float"
	case protoreflect.DoubleKind:
		return "double"
	case protoreflect.StringKind:
		return "string"
	case protoreflect.BytesKind:
		return "bytes"
	case protoreflect.EnumKind:
		return string(f.Enum().FullName())
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return string(f.Message().FullName())
	default:
		return "unknown"
	}
}

func pad(n int) string {
	return strings.Repeat("  ", n)
}

// SortedFileNames returns the file paths in a stable, sorted order so
// the UI presents them deterministically.
func SortedFileNames(files map[string]string) []string {
	out := make([]string, 0, len(files))
	for k := range files {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
