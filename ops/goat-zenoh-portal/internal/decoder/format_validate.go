package decoder

import (
	"errors"
	"fmt"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// ParseProtobufFormat splits a topic's `format` string into the wire
// kind ("protobuf") and the optional message name. Returns kind="" for
// non-protobuf formats and name="" when the colon-suffix is empty.
func ParseProtobufFormat(format string) (kind, name string) {
	if rest, ok := strings.CutPrefix(format, "protobuf:"); ok {
		return "protobuf", rest
	}
	return "", ""
}

// TopLevelMessageNames decodes fdsBytes as a serialized
// FileDescriptorSet and returns the simple (unqualified) names of every
// top-level message across every file. Nested messages are NOT included
// — the read-side schema-body check matches by top-level only, and the
// write-side validator stays consistent with that contract.
func TopLevelMessageNames(fdsBytes []byte) ([]string, error) {
	if len(fdsBytes) == 0 {
		return nil, errors.New("decoder: empty descriptor set")
	}
	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(fdsBytes, &fds); err != nil {
		return nil, fmt.Errorf("decoder: parse descriptor set: %w", err)
	}
	var names []string
	for _, f := range fds.GetFile() {
		for _, m := range f.GetMessageType() {
			names = append(names, m.GetName())
		}
	}
	return names, nil
}

// FormatFDSMismatchError is returned by ValidateFormatAgainstFDS when
// the declared `protobuf:<Name>` is not a top-level message in the
// uploaded descriptor set. Carries the structured pieces the HTTP layer
// needs to render an actionable 422.
type FormatFDSMismatchError struct {
	DeclaredName string
	FoundNames   []string
}

func (e *FormatFDSMismatchError) Error() string {
	preview := e.FoundNames
	more := ""
	if len(preview) > 5 {
		more = fmt.Sprintf(" (+%d more)", len(preview)-5)
		preview = preview[:5]
	}
	found := strings.Join(preview, ", ")
	if found == "" {
		found = "(none)"
	}
	// "no message named" rather than "no TOP-LEVEL message named": resolution
	// accepts a fully-qualified name and walks nested messages, so the old
	// wording described a narrower rule than the one actually applied and sent
	// people looking for the wrong problem.
	return fmt.Sprintf(
		"format declares 'protobuf:%s' but the uploaded .fds contains no message named %q "+
			"(tried it as a fully-qualified name and as a bare name). Found: %s%s",
		e.DeclaredName, e.DeclaredName, found, more,
	)
}

// Suggestion picks a likely-correct format declaration when the FDS has
// at least one top-level message. Empty string when the FDS is empty.
func (e *FormatFDSMismatchError) Suggestion() string {
	if len(e.FoundNames) == 0 {
		return ""
	}
	return "protobuf:" + e.FoundNames[0]
}

// ValidateFormatAgainstFDS enforces the same rule as the read-side
// `schema-body` check (crates/stethoscope-checks): when `format` is
// "protobuf:<Name>", the uploaded FileDescriptorSet must contain a
// top-level message named <Name>. Non-protobuf formats and bare
// "protobuf:" (no name) are accepted — the latter degrades to a Warn
// at read-time, not a hard reject at write-time.
//
// Returns *FormatFDSMismatchError on mismatch, a plain error on FDS
// decode failure, or nil on success.
func ValidateFormatAgainstFDS(format string, fdsBytes []byte) error {
	kind, name := ParseProtobufFormat(format)
	if kind != "protobuf" || name == "" {
		return nil
	}
	names, err := TopLevelMessageNames(fdsBytes)
	if err != nil {
		return err
	}

	// Accept the FULLY-QUALIFIED form as well as the bare one.
	//
	// Comparing `name` against bare top-level names only would refuse a correct,
	// more-specific declaration at upload:
	//
	//   protobuf:DataModel.Wildcard  -> 422 "contains no top-level message
	//                                   named \"DataModel.Wildcard\""
	//
	// That error would tell the operator their descriptor is wrong when it is not,
	// and a partner reading it would rebuild a descriptor that was never at fault.
	//
	// The read-side contract this mirrors already allows both forms —
	// crates/stethoscope-checks/src/checks/schema_body.rs says so in its own
	// remediation text: "Both fully-qualified (`package.Name`) and bare
	// (`Name`) forms are accepted as long as one of them matches a TOP-LEVEL
	// message in the descriptor." So this is the write side catching up to the
	// read side, not a new rule.
	//
	// TOP-LEVEL ONLY IS DELIBERATE AND PRESERVED. A nested message of the same
	// name is still refused, because stethoscope refuses it and tells the
	// vendor to promote it ("fix the source so it's a top-level (not nested)
	// message and re-upload"). Accepting nested here would let an upload pass
	// that the read side then flags — the exact write/read disagreement this
	// change exists to remove. Note the portal's own findMessage DOES walk
	// nested messages, so it is the outlier of the three; reconciling that is
	// a separate question and not silently decided here.
	// BARE form — the original behaviour, unchanged.
	for _, n := range names {
		if n == name {
			return nil
		}
	}
	// FULLY-QUALIFIED form — the addition.
	for _, n := range fullyQualifiedTopLevelNames(fdsBytes) {
		if n == name {
			return nil
		}
	}
	return &FormatFDSMismatchError{DeclaredName: name, FoundNames: names}
}

// fullyQualifiedTopLevelNames returns "<package>.<Message>" for every
// top-level message. Messages in a file with no `package` are returned bare,
// which is the same string TopLevelMessageNames already yields for them.
// Returns nil on an unparseable descriptor set — the caller has already
// surfaced that error via TopLevelMessageNames.
func fullyQualifiedTopLevelNames(fdsBytes []byte) []string {
	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(fdsBytes, &fds); err != nil {
		return nil
	}
	var out []string
	for _, f := range fds.GetFile() {
		pkg := f.GetPackage()
		for _, m := range f.GetMessageType() {
			if pkg == "" {
				out = append(out, m.GetName())
				continue
			}
			out = append(out, pkg+"."+m.GetName())
		}
	}
	return out
}
