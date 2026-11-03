// Schema-rendering walker. Given a serialized FileDescriptorSet (.fds),
// produces a structured JSON tree describing every message + enum the
// set defines. Consumed by the portal's schema-viewer page to render a
// Swagger-style human-readable view of a vendor's data standard.
//
// The walker is intentionally read-only: no payload bytes, no decoding.
// Pair it with DecodeProtobuf when the caller wants both the standard
// and a live sample.

package decoder

import (
	"errors"
	"fmt"
	"sort"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

// RenderedSchema is the top-level shape returned by RenderDescriptorSet.
type RenderedSchema struct {
	Files    []RenderedFile    `json:"files"`
	Messages []RenderedMessage `json:"messages"`
	Enums    []RenderedEnum    `json:"enums"`
}

// RenderedFile names every .proto file in the descriptor set + the
// package it declares + the syntax level (proto2 / proto3 / editions).
type RenderedFile struct {
	Name    string `json:"name"`
	Package string `json:"package"`
	Syntax  string `json:"syntax"`
}

// RenderedMessage describes one message type. Nested messages and
// nested enums are emitted at the top level of RenderedSchema and
// referenced by full-name from the parent's Fields list — flatter
// rendering is easier to consume on the frontend than recursive trees.
type RenderedMessage struct {
	FullName    string           `json:"full_name"`
	Name        string           `json:"name"`
	File        string           `json:"file"`
	Package     string           `json:"package"`
	Fields      []RenderedField  `json:"fields"`
	Oneofs      []RenderedOneof  `json:"oneofs,omitempty"`
	Nested      []string         `json:"nested,omitempty"`
	NestedEnums []string         `json:"nested_enums,omitempty"`
	Reserved    []RenderedReserv `json:"reserved,omitempty"`
}

// RenderedField is one field on a message. Cardinality is "singular"
// (proto3 default), "optional" (explicit presence), "repeated", or
// "map" (where MapKey + MapValue carry the entry's key/value types).
type RenderedField struct {
	Name        string           `json:"name"`
	JSONName    string           `json:"json_name"`
	Number      int32            `json:"number"`
	Cardinality string           `json:"cardinality"`
	Type        string           `json:"type"`
	TypeRef     string           `json:"type_ref,omitempty"`
	Oneof       string           `json:"oneof,omitempty"`
	MapKey      *RenderedMapHalf `json:"map_key,omitempty"`
	MapValue    *RenderedMapHalf `json:"map_value,omitempty"`
}

// RenderedMapHalf names the key or value type of a map<K,V> field.
type RenderedMapHalf struct {
	Type    string `json:"type"`
	TypeRef string `json:"type_ref,omitempty"`
}

// RenderedOneof groups field names that share a oneof.
type RenderedOneof struct {
	Name   string   `json:"name"`
	Fields []string `json:"fields"`
}

// RenderedReserv represents a reserved field number range or name.
type RenderedReserv struct {
	Kind  string `json:"kind"`
	Start int32  `json:"start,omitempty"`
	End   int32  `json:"end,omitempty"`
	Name  string `json:"name,omitempty"`
}

// RenderedEnum is one enum type with its values.
type RenderedEnum struct {
	FullName string              `json:"full_name"`
	Name     string              `json:"name"`
	File     string              `json:"file"`
	Package  string              `json:"package"`
	Values   []RenderedEnumValue `json:"values"`
}

// RenderedEnumValue is one entry on an enum.
type RenderedEnumValue struct {
	Name   string `json:"name"`
	Number int32  `json:"number"`
}

// loadFilesFromBytes is shared between the structural renderer and the
// .proto-text writer; both parse the descriptor set the same way.
func loadFilesFromBytes(fdsBytes []byte) (*protoregistry.Files, error) {
	if len(fdsBytes) == 0 {
		return nil, errors.New("decoder: empty descriptor set")
	}
	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(fdsBytes, &fds); err != nil {
		return nil, fmt.Errorf("decoder: parse descriptor set: %w", err)
	}
	files, err := protodesc.NewFiles(&fds)
	if err != nil {
		return nil, fmt.Errorf("decoder: build files: %w", err)
	}
	return files, nil
}

// RenderDescriptorSet parses fdsBytes as a serialized FileDescriptorSet
// and emits a flattened structural view suitable for direct JSON
// serialization to a browser.
func RenderDescriptorSet(fdsBytes []byte) (*RenderedSchema, error) {
	files, err := loadFilesFromBytes(fdsBytes)
	if err != nil {
		return nil, err
	}

	out := &RenderedSchema{}
	files.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		out.Files = append(out.Files, RenderedFile{
			Name:    fd.Path(),
			Package: string(fd.Package()),
			Syntax:  syntaxString(fd.Syntax()),
		})
		collectMessages(fd, fd.Messages(), out)
		collectEnums(fd, fd.Enums(), out)
		return true
	})

	// Stable output order — file emission order is descriptor-set order,
	// but messages + enums sort by FullName for deterministic UI.
	sort.SliceStable(out.Messages, func(i, j int) bool {
		return out.Messages[i].FullName < out.Messages[j].FullName
	})
	sort.SliceStable(out.Enums, func(i, j int) bool {
		return out.Enums[i].FullName < out.Enums[j].FullName
	})
	return out, nil
}

func collectMessages(fd protoreflect.FileDescriptor, msgs protoreflect.MessageDescriptors, out *RenderedSchema) {
	for i := 0; i < msgs.Len(); i++ {
		md := msgs.Get(i)
		if md.IsMapEntry() {
			// Map entries are synthetic; skip — their key/value types are
			// surfaced via the parent's RenderedField.MapKey/MapValue.
			continue
		}
		rm := RenderedMessage{
			FullName: string(md.FullName()),
			Name:     string(md.Name()),
			File:     fd.Path(),
			Package:  string(fd.Package()),
		}
		// Oneofs.
		oo := md.Oneofs()
		for j := 0; j < oo.Len(); j++ {
			o := oo.Get(j)
			if o.IsSynthetic() {
				// proto3 optional generates a synthetic oneof per field;
				// skip — surfaces via field cardinality instead.
				continue
			}
			ro := RenderedOneof{Name: string(o.Name())}
			of := o.Fields()
			for k := 0; k < of.Len(); k++ {
				ro.Fields = append(ro.Fields, string(of.Get(k).Name()))
			}
			rm.Oneofs = append(rm.Oneofs, ro)
		}
		// Fields.
		fds := md.Fields()
		for j := 0; j < fds.Len(); j++ {
			f := fds.Get(j)
			rm.Fields = append(rm.Fields, renderField(f))
		}
		// Nested message + enum names (caller resolves via FullName).
		nm := md.Messages()
		for j := 0; j < nm.Len(); j++ {
			child := nm.Get(j)
			if child.IsMapEntry() {
				continue
			}
			rm.Nested = append(rm.Nested, string(child.FullName()))
		}
		ne := md.Enums()
		for j := 0; j < ne.Len(); j++ {
			rm.NestedEnums = append(rm.NestedEnums, string(ne.Get(j).FullName()))
		}
		// Reserved ranges + names.
		rr := md.ReservedRanges()
		for j := 0; j < rr.Len(); j++ {
			r := rr.Get(j)
			rm.Reserved = append(rm.Reserved, RenderedReserv{
				Kind:  "range",
				Start: int32(r[0]),
				End:   int32(r[1]),
			})
		}
		rn := md.ReservedNames()
		for j := 0; j < rn.Len(); j++ {
			rm.Reserved = append(rm.Reserved, RenderedReserv{
				Kind: "name",
				Name: string(rn.Get(j)),
			})
		}
		out.Messages = append(out.Messages, rm)

		// Recurse into nested messages and enums.
		collectMessages(fd, md.Messages(), out)
		collectEnums(fd, md.Enums(), out)
	}
}

func collectEnums(fd protoreflect.FileDescriptor, enums protoreflect.EnumDescriptors, out *RenderedSchema) {
	for i := 0; i < enums.Len(); i++ {
		ed := enums.Get(i)
		re := RenderedEnum{
			FullName: string(ed.FullName()),
			Name:     string(ed.Name()),
			File:     fd.Path(),
			Package:  string(fd.Package()),
		}
		vs := ed.Values()
		for j := 0; j < vs.Len(); j++ {
			v := vs.Get(j)
			re.Values = append(re.Values, RenderedEnumValue{
				Name:   string(v.Name()),
				Number: int32(v.Number()),
			})
		}
		out.Enums = append(out.Enums, re)
	}
}

func renderField(f protoreflect.FieldDescriptor) RenderedField {
	rf := RenderedField{
		Name:     string(f.Name()),
		JSONName: f.JSONName(),
		Number:   int32(f.Number()),
		Type:     scalarTypeName(f),
	}
	switch {
	case f.IsMap():
		rf.Cardinality = "map"
		k := f.MapKey()
		v := f.MapValue()
		rf.MapKey = &RenderedMapHalf{Type: scalarTypeName(k)}
		rf.MapValue = &RenderedMapHalf{Type: scalarTypeName(v), TypeRef: typeRef(v)}
		rf.Type = "map"
		rf.TypeRef = ""
	case f.IsList():
		rf.Cardinality = "repeated"
		rf.TypeRef = typeRef(f)
	case f.HasPresence():
		// proto3 optional, proto2 optional, or oneof.
		rf.Cardinality = "optional"
		rf.TypeRef = typeRef(f)
	default:
		rf.Cardinality = "singular"
		rf.TypeRef = typeRef(f)
	}
	if oneof := f.ContainingOneof(); oneof != nil && !oneof.IsSynthetic() {
		rf.Oneof = string(oneof.Name())
	}
	return rf
}

func scalarTypeName(f protoreflect.FieldDescriptor) string {
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
		return "enum"
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return "message"
	default:
		return "unknown"
	}
}

func typeRef(f protoreflect.FieldDescriptor) string {
	switch f.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return string(f.Message().FullName())
	case protoreflect.EnumKind:
		return string(f.Enum().FullName())
	default:
		return ""
	}
}

func syntaxString(s protoreflect.Syntax) string {
	switch s {
	case protoreflect.Proto2:
		return "proto2"
	case protoreflect.Proto3:
		return "proto3"
	case protoreflect.Editions:
		return "editions"
	default:
		return ""
	}
}
