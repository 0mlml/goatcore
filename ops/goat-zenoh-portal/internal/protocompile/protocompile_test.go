package protocompile

import (
	"context"
	"errors"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

const validProto = `syntax = "proto3";
package vendor.tracks.v1;
import "google/protobuf/timestamp.proto";

message Den {
  string id = 1;
  google.protobuf.Timestamp seen_at = 2;
  double confidence = 3;
}
`

const syntaxErrorProto = `syntax = "proto3";
package vendor.tracks.v1;

mesage Den {
  string id = 1;
}
`

const unsupportedImportProto = `syntax = "proto3";
package vendor.tracks.v1;
import "vendor/other.proto";

message Den {
  string id = 1;
}
`

// TestCompileSingleProto_Valid — happy path. A single .proto importing
// google.protobuf.Timestamp compiles cleanly and the emitted
// FileDescriptorSet round-trips through proto.Unmarshal AND carries
// google/protobuf/timestamp.proto as a sibling entry (proves the
// --include_imports equivalence the decoder depends on).
func TestCompileSingleProto_Valid(t *testing.T) {
	out, err := CompileSingleProto(context.Background(), "den.proto", []byte(validProto))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(out) == 0 {
		t.Fatalf("compile returned empty bytes")
	}

	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(out, &fds); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	var (
		foundUser bool
		foundWKT  bool
	)
	for _, f := range fds.File {
		switch f.GetName() {
		case "den.proto":
			foundUser = true
			if f.GetPackage() != "vendor.tracks.v1" {
				t.Errorf("user file package = %q, want vendor.tracks.v1", f.GetPackage())
			}
			if len(f.MessageType) != 1 || f.MessageType[0].GetName() != "Den" {
				t.Errorf("user file message-type shape unexpected: %+v", f.MessageType)
			}
		case "google/protobuf/timestamp.proto":
			foundWKT = true
		}
	}
	if !foundUser {
		t.Errorf("FileDescriptorSet missing user file den.proto; got %d files", len(fds.File))
	}
	if !foundWKT {
		t.Errorf("FileDescriptorSet missing google/protobuf/timestamp.proto — --include_imports equivalence broken")
	}
}

// TestCompileSingleProto_SyntaxError — `mesage` typo. Compiler must
// return a CompileError with line+col populated so the HTTP handler can
// render a protoc-style diagnostic for the vendor.
func TestCompileSingleProto_SyntaxError(t *testing.T) {
	_, err := CompileSingleProto(context.Background(), "broken.proto", []byte(syntaxErrorProto))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ce *CompileError
	if !errors.As(err, &ce) {
		t.Fatalf("error is not *CompileError: %T %v", err, err)
	}
	if ce.Line <= 0 {
		t.Errorf("CompileError.Line not populated: %+v", ce)
	}
	if ce.Col <= 0 {
		t.Errorf("CompileError.Col not populated: %+v", ce)
	}
	if !strings.Contains(ce.File, "broken.proto") {
		t.Errorf("CompileError.File = %q, want substring broken.proto", ce.File)
	}
	if ce.Message == "" {
		t.Errorf("CompileError.Message is empty")
	}
	// Stringification should carry the position so the HTTP body is
	// directly useful to the vendor.
	if got := ce.Error(); !strings.Contains(got, "broken.proto:") {
		t.Errorf("CompileError.Error() = %q, want substring broken.proto:", got)
	}
}

// TestCompileSingleProto_UnsupportedImport — importing a path the WKT
// resolver doesn't know about. Vendor sees a clean "missing import"
// diagnostic and can decide to either inline the import or use the
// .fds path with their own assembled descriptor set.
func TestCompileSingleProto_UnsupportedImport(t *testing.T) {
	_, err := CompileSingleProto(context.Background(), "imp.proto", []byte(unsupportedImportProto))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ce *CompileError
	if !errors.As(err, &ce) {
		t.Fatalf("error is not *CompileError: %T %v", err, err)
	}
	if !strings.Contains(ce.Message+ce.File, "vendor/other.proto") {
		// protocompile renders missing-import as "import not found" or
		// "could not find import" depending on version; the test asserts
		// the missing path is in the rendered diagnostic, which is the
		// vendor-actionable part.
		t.Errorf("error doesn't mention missing import: file=%q msg=%q",
			ce.File, ce.Message)
	}
}

const multiWKTProto = `syntax = "proto3";
package vendor.tracks.v1;
import "google/protobuf/timestamp.proto";
import "google/protobuf/duration.proto";
import "google/protobuf/empty.proto";

message Multi {
  string id = 1;
  google.protobuf.Timestamp seen_at = 2;
  google.protobuf.Duration ttl = 3;
  google.protobuf.Empty marker = 4;
}
`

// TestCompileSingleProto_MultipleWKTs — a single .proto importing THREE
// well-known-type files (Timestamp + Duration + Empty). Confirms the
// import-walker emits each unique WKT exactly once and doesn't drop any.
// The walker correctness here is load-bearing: the decoder's
// protodesc.NewFiles requires every transitively-imported file present,
// so a missed WKT silently breaks the render endpoint downstream.
func TestCompileSingleProto_MultipleWKTs(t *testing.T) {
	out, err := CompileSingleProto(context.Background(), "multi.proto", []byte(multiWKTProto))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(out, &fds); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := map[string]bool{
		"multi.proto":                     false,
		"google/protobuf/timestamp.proto": false,
		"google/protobuf/duration.proto":  false,
		"google/protobuf/empty.proto":     false,
	}
	counts := map[string]int{}
	for _, f := range fds.File {
		name := f.GetName()
		counts[name]++
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("FileDescriptorSet missing %s — walker dropped a transitive import", name)
		}
		if counts[name] > 1 {
			t.Errorf("FileDescriptorSet has %d copies of %s — walker dedup broken", counts[name], name)
		}
	}
}

// TestCompileSingleProto_EmptyBody — early-return path; no protocompile
// invocation. Should be cheap.
func TestCompileSingleProto_EmptyBody(t *testing.T) {
	_, err := CompileSingleProto(context.Background(), "x.proto", nil)
	if err == nil {
		t.Fatal("expected error on empty body")
	}
	var ce *CompileError
	if !errors.As(err, &ce) {
		t.Fatalf("error is not *CompileError: %T %v", err, err)
	}
	if ce.Message == "" {
		t.Errorf("empty-body error has empty Message: %+v", ce)
	}
}
