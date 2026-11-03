// Package protocompile wraps github.com/bufbuild/protocompile so the
// portal's schema-upload endpoint can accept raw .proto source in
// addition to pre-compiled .fds bodies.
//
// CompileSingleProto takes the bytes of a single .proto file and
// returns the bytes that `protoc --descriptor_set_out=... --include_imports`
// would have written — a serialized google.protobuf.FileDescriptorSet
// containing the user file plus every transitively-imported
// well-known-type file. The decoder + render endpoints downstream
// consume these bytes via proto.Unmarshal into descriptorpb.FileDescriptorSet,
// so the on-the-wire shape post-compile is byte-equivalent to the
// existing .fds path.
//
// Scope of v1: single-file source with imports limited to the
// well-known types (google/protobuf/*). Multi-file vendor sources
// continue to use the .fds upload path; the trial pipeline's
// build-fds.sh remains the canonical recipe for that shape.
package protocompile

import (
	"context"
	"fmt"
	"path/filepath"

	bufproto "github.com/bufbuild/protocompile"
	"github.com/bufbuild/protocompile/reporter"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// CompileError carries a structured protoc-style diagnostic — file,
// line, column, message — so the HTTP handler can surface it in a 400
// response and the vendor sees the same shape they'd see from protoc
// locally.
type CompileError struct {
	File    string
	Line    int
	Col     int
	Message string
}

func (e *CompileError) Error() string {
	if e.File == "" || e.Line == 0 {
		return e.Message
	}
	return fmt.Sprintf("%s:%d:%d: %s", e.File, e.Line, e.Col, e.Message)
}

// CompileSingleProto compiles a single .proto file (as text bytes)
// into a serialized FileDescriptorSet matching the output of
// `protoc --descriptor_set_out=<out> --include_imports`.
//
// filename is used as the descriptor-set entry name and in error
// diagnostics; it is reduced to the base path component so the caller
// can pass a user-supplied multipart upload filename safely.
//
// Returns a *CompileError when the source is invalid; the handler can
// type-assert and surface .File/.Line/.Col directly. Other errors
// (marshal, internal) wrap the underlying cause.
func CompileSingleProto(ctx context.Context, filename string, source []byte) ([]byte, error) {
	if len(source) == 0 {
		return nil, &CompileError{Message: "empty .proto source"}
	}
	base := filepath.Base(filename)
	if base == "" || base == "." || base == "/" || base == `\` {
		base = "vendor.proto"
	}

	// Single-entry source map; protocompile's standard-imports wrapper
	// supplies the WKT files (google/protobuf/timestamp.proto, etc.)
	// transparently. Sources map is consumed verbatim per the package
	// docs — must not mutate after handing to the compiler.
	sources := map[string]string{base: string(source)}
	resolver := bufproto.WithStandardImports(&bufproto.SourceResolver{
		Accessor: bufproto.SourceAccessorFromMap(sources),
	})

	var firstErr *CompileError
	errFn := func(e reporter.ErrorWithPos) error {
		if firstErr == nil {
			pos := e.GetPosition()
			firstErr = &CompileError{
				File:    pos.Filename,
				Line:    pos.Line,
				Col:     pos.Col,
				Message: errMessage(e),
			}
		}
		// Returning non-nil aborts on first error — gives a clean
		// single-diagnostic surface for the HTTP 400 body.
		return e
	}

	compiler := bufproto.Compiler{
		Resolver:       resolver,
		SourceInfoMode: bufproto.SourceInfoStandard,
		Reporter:       reporter.NewReporter(errFn, nil),
	}

	files, err := compiler.Compile(ctx, base)
	if err != nil {
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, &CompileError{Message: err.Error()}
	}

	// Assemble a FileDescriptorSet matching --include_imports: walk the
	// compiled file's dependency tree and emit each unique file once.
	// protocompile's linker.Files is the slice of top-level compile
	// results; for each one we recursively descend into Imports() and
	// project each protoreflect.FileDescriptor through protodesc into a
	// descriptorpb.FileDescriptorProto, which is what
	// google.golang.org/protobuf wire-serializes.
	fds := &descriptorpb.FileDescriptorSet{}
	seen := map[string]bool{}
	var walk func(fd protoreflect.FileDescriptor)
	walk = func(fd protoreflect.FileDescriptor) {
		path := fd.Path()
		if seen[path] {
			return
		}
		seen[path] = true
		imps := fd.Imports()
		for i := 0; i < imps.Len(); i++ {
			walk(imps.Get(i).FileDescriptor)
		}
		fds.File = append(fds.File, protodesc.ToFileDescriptorProto(fd))
	}
	for _, f := range files {
		walk(f)
	}

	out, err := proto.Marshal(fds)
	if err != nil {
		return nil, fmt.Errorf("marshal descriptor set: %w", err)
	}
	// Round-trip sanity check — if protocompile ever emits bytes the
	// canonical google.golang.org/protobuf parser can't decode, surface
	// it as a clear internal error instead of letting the downstream
	// /api/schemas/render endpoint fail mysteriously.
	if uerr := proto.Unmarshal(out, &descriptorpb.FileDescriptorSet{}); uerr != nil {
		return nil, fmt.Errorf("compile produced unparseable descriptor set: %w", uerr)
	}
	return out, nil
}

// errMessage extracts the underlying message from a reporter.ErrorWithPos.
// e.Unwrap() returns the wrapped error; e.Error() prepends the position
// which we'd be double-rendering in CompileError.Error().
func errMessage(e reporter.ErrorWithPos) string {
	if u := e.Unwrap(); u != nil {
		return u.Error()
	}
	return e.Error()
}

// Ensure CompileError satisfies the error interface; callers
// type-assert via errors.As(err, &target).
var _ error = (*CompileError)(nil)
