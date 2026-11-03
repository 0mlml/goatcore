// XML sample decoding — the XML/XSD counterpart of protobuf.go's
// DecodeProtobuf. Given the topic's uploaded .xsd, the declared root
// element, and a raw XML payload off the fabric, produce a JSON rendering
// of the document so the inspector's decoded-sample view shows structured
// fields instead of a hex dump.
//
// XML is self-describing text, so "decode" here is a structural XML→JSON
// projection (elements → keys, attributes → "@name", text → "#text",
// repeated siblings → arrays) plus a root-element check against the
// topic's xml:<Root> format. It is NOT full XSD instance validation — a
// complete validating parser is out of scope; the xsdBytes are used to
// confirm the declared root, and the well-formedness parse is the gate
// that fails malformed payloads. This mirrors DecodeProtobuf's contract:
// bytes + schema-reference in, JSON out, error on mismatch.

package decoder

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

// DecodeXML parses payload as an XML document and returns a JSON encoding
// of it. When rootElement is non-empty, the document's root local name
// must match, else an error is returned (the wrong-message analog of the
// protobuf path). xsdBytes is accepted for signature parity and future
// type-aware coercion; today it is used only to keep the call shape
// identical to DecodeProtobuf.
func DecodeXML(xsdBytes []byte, rootElement string, payload []byte) ([]byte, error) {
	if len(payload) == 0 {
		return nil, errors.New("decoder: empty xml payload")
	}
	dec := xml.NewDecoder(bytes.NewReader(payload))

	// Advance to the first start element (the document root).
	var root xml.StartElement
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil, errors.New("decoder: xml has no root element")
		}
		if err != nil {
			return nil, fmt.Errorf("decoder: parse xml: %w", err)
		}
		if se, ok := tok.(xml.StartElement); ok {
			root = se
			break
		}
	}
	if rootElement != "" && root.Name.Local != rootElement {
		return nil, fmt.Errorf("decoder: xml root is <%s> but topic declares xml:%s", root.Name.Local, rootElement)
	}

	val, err := decodeElement(dec, root)
	if err != nil {
		return nil, fmt.Errorf("decoder: parse xml: %w", err)
	}
	wrapped := map[string]any{root.Name.Local: val}
	return json.Marshal(wrapped)
}

// decodeElement consumes tokens until the matching end element and returns
// the element's value: a bare string for text-only leaves, or an object
// carrying "@attr" attributes, "#text" character data, and child elements
// (repeated child names collapse into arrays).
func decodeElement(dec *xml.Decoder, start xml.StartElement) (any, error) {
	obj := map[string]any{}
	for _, a := range start.Attr {
		if a.Name.Local == "xmlns" || a.Name.Space == "xmlns" {
			continue // drop namespace declarations from the projection
		}
		obj["@"+a.Name.Local] = a.Value
	}
	var text strings.Builder

	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			child, err := decodeElement(dec, t)
			if err != nil {
				return nil, err
			}
			addChild(obj, t.Name.Local, child)
		case xml.CharData:
			text.Write([]byte(t))
		case xml.EndElement:
			trimmed := strings.TrimSpace(text.String())
			// Text-only leaf with no attributes/children → bare string.
			if len(obj) == 0 {
				return trimmed, nil
			}
			if trimmed != "" {
				obj["#text"] = trimmed
			}
			return obj, nil
		}
	}
}

// addChild inserts a child under key, promoting to a slice when the key
// repeats so repeated XML elements render as a JSON array.
func addChild(obj map[string]any, key string, val any) {
	existing, ok := obj[key]
	if !ok {
		obj[key] = val
		return
	}
	if arr, isArr := existing.([]any); isArr {
		obj[key] = append(arr, val)
		return
	}
	obj[key] = []any{existing, val}
}
