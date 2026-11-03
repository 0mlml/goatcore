package decoder

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ParseXMLFormat splits a topic's `format` string into the wire kind
// ("xml") and the optional root-element name. It matches both the
// schema-bound shape "xml:<Root>" (kind="xml", name="<Root>") and bare
// "xml" (kind="xml", name=""). Returns kind="" for non-XML formats. This
// mirrors ParseProtobufFormat so callers branch on the same shape.
func ParseXMLFormat(format string) (kind, name string) {
	if rest, ok := strings.CutPrefix(format, "xml:"); ok {
		return "xml", rest
	}
	if format == "xml" {
		return "xml", ""
	}
	return "", ""
}

// TopLevelElementNames decodes xsdBytes as an XML Schema Definition and
// returns the `name` attribute of every top-level <xs:element> — i.e.
// every element declaration that is a direct child of the root <xs:schema>
// element. Nested element declarations (inside complexType/sequence/…) are
// NOT included, exactly as TopLevelMessageNames excludes nested protobuf
// messages, so the write-side validator matches the same contract a
// consumer sees when it looks up "which root element does this topic
// carry".
//
// Namespace-prefix-agnostic: XSD authors write xs:element, xsd:element, or
// a default-namespaced element; matching is by local name only. Returns an
// error if the bytes are not well-formed XML or the document root is not a
// schema element.
func TopLevelElementNames(xsdBytes []byte) ([]string, error) {
	if len(xsdBytes) == 0 {
		return nil, errors.New("decoder: empty xsd document")
	}
	dec := xml.NewDecoder(bytes.NewReader(xsdBytes))

	var (
		names   []string
		depth   int
		rootSet bool
	)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("decoder: parse xsd: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if !rootSet {
				// The document root must be the schema element.
				if t.Name.Local != "schema" {
					return nil, fmt.Errorf("decoder: xsd root is <%s>, expected <schema>", t.Name.Local)
				}
				rootSet = true
				depth++
				continue
			}
			// Depth 1 == direct child of <schema> == a top-level declaration.
			if depth == 1 && t.Name.Local == "element" {
				if n := attrValue(t.Attr, "name"); n != "" {
					names = append(names, n)
				}
			}
			depth++
		case xml.EndElement:
			depth--
		}
	}
	if !rootSet {
		return nil, errors.New("decoder: xsd has no root element")
	}
	return names, nil
}

// attrValue returns the value of the named attribute (by local name),
// or "" when absent.
func attrValue(attrs []xml.Attr, local string) string {
	for _, a := range attrs {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// FormatXSDMismatchError is returned by ValidateFormatAgainstXSD when the
// declared `xml:<Root>` is not a top-level element in the uploaded XSD.
// Carries the structured pieces the HTTP layer needs to render an
// actionable 422 — the mirror of FormatFDSMismatchError.
type FormatXSDMismatchError struct {
	DeclaredName string
	FoundNames   []string
}

func (e *FormatXSDMismatchError) Error() string {
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
	return fmt.Sprintf(
		"format declares 'xml:%s' but the uploaded .xsd declares no top-level element named %q. Found: %s%s",
		e.DeclaredName, e.DeclaredName, found, more,
	)
}

// Suggestion picks a likely-correct format declaration when the XSD has at
// least one top-level element. Empty string when the XSD declares none.
func (e *FormatXSDMismatchError) Suggestion() string {
	if len(e.FoundNames) == 0 {
		return ""
	}
	return "xml:" + e.FoundNames[0]
}

// ValidateFormatAgainstXSD is the XSD/XML counterpart of
// ValidateFormatAgainstFDS: when `format` is "xml:<Root>", the uploaded
// XSD must declare a top-level element named <Root>. Bare "xml" (no root)
// and non-XML formats are accepted without a schema check — the former is
// the opaque-XML path that carries no schema binding.
//
// Returns *FormatXSDMismatchError on mismatch, a plain error on XSD parse
// failure, or nil on success.
func ValidateFormatAgainstXSD(format string, xsdBytes []byte) error {
	kind, name := ParseXMLFormat(format)
	if kind != "xml" || name == "" {
		return nil
	}
	names, err := TopLevelElementNames(xsdBytes)
	if err != nil {
		return err
	}
	for _, n := range names {
		if n == name {
			return nil
		}
	}
	return &FormatXSDMismatchError{DeclaredName: name, FoundNames: names}
}

// ValidateXSDWellFormed returns nil iff xsdBytes parse as well-formed XML
// whose document root is a <schema> element. Uploaded xsd-format schema
// bodies are gated on this regardless of the topic's format, so a topic
// can never carry a schema body that isn't actually an XSD. (The protobuf
// path gets this for free — a non-FDS body fails proto.Unmarshal — so the
// upload handler calls this to give xsd uploads the same guarantee.)
func ValidateXSDWellFormed(xsdBytes []byte) error {
	_, err := TopLevelElementNames(xsdBytes)
	return err
}
