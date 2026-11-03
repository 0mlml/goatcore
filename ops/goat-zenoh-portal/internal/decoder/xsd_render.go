// XSD schema-rendering walker — the XML/XSD counterpart of render.go.
// Given an XML Schema Definition (.xsd), produces the SAME RenderedSchema
// shape the protobuf renderer emits, so the schema-viewer frontend renders
// an XSD data dictionary with no format-specific code path:
//
//   top-level <xs:element> + named <xs:complexType>  → RenderedMessage
//   child <xs:element>                                → RenderedField
//   <xs:attribute>                                    → RenderedField ("@name")
//   named <xs:simpleType> with <xs:enumeration>       → RenderedEnum
//
// Cardinality is mapped into the protobuf vocabulary the frontend already
// understands ("singular"/"optional"/"repeated") so no rendering branch is
// needed. Read-only: no payload bytes, no validation of instance documents.
//
// Scope: the common XSD subset — element/attribute declarations, complexType
// sequence/all/choice, simpleType restriction enumerations, type references,
// minOccurs/maxOccurs, and xs:annotation/xs:documentation. Advanced
// constructs (substitution groups, complexContent extension chains across
// imports, redefine) are rendered best-effort, not resolved.

package decoder

import (
	"encoding/xml"
	"sort"
	"strings"
)

// --- Parse model (namespace-prefix-agnostic: tags match by local name) ---

type xsdSchemaDoc struct {
	XMLName         xml.Name         `xml:"schema"`
	TargetNamespace string           `xml:"targetNamespace,attr"`
	Elements        []xsdElementDecl `xml:"element"`
	ComplexTypes    []xsdComplexType `xml:"complexType"`
	SimpleTypes     []xsdSimpleType  `xml:"simpleType"`
}

type xsdElementDecl struct {
	Name        string          `xml:"name,attr"`
	Ref         string          `xml:"ref,attr"`
	Type        string          `xml:"type,attr"`
	MinOccurs   string          `xml:"minOccurs,attr"`
	MaxOccurs   string          `xml:"maxOccurs,attr"`
	ComplexType *xsdComplexType `xml:"complexType"`
	SimpleType  *xsdSimpleType  `xml:"simpleType"`
	Annotation  *xsdAnnotation  `xml:"annotation"`
}

type xsdComplexType struct {
	Name       string         `xml:"name,attr"`
	Sequence   *xsdModelGroup `xml:"sequence"`
	All        *xsdModelGroup `xml:"all"`
	Choice     *xsdModelGroup `xml:"choice"`
	Attributes []xsdAttribute `xml:"attribute"`
	Annotation *xsdAnnotation `xml:"annotation"`
}

// xsdModelGroup is a sequence/all/choice. Nested groups are flattened
// one level (the common shape); deeper nesting degrades gracefully.
type xsdModelGroup struct {
	Elements  []xsdElementDecl `xml:"element"`
	Sequences []xsdModelGroup  `xml:"sequence"`
	Choices   []xsdModelGroup  `xml:"choice"`
	Alls      []xsdModelGroup  `xml:"all"`
}

type xsdAttribute struct {
	Name       string         `xml:"name,attr"`
	Type       string         `xml:"type,attr"`
	Use        string         `xml:"use,attr"`
	Annotation *xsdAnnotation `xml:"annotation"`
}

type xsdSimpleType struct {
	Name        string          `xml:"name,attr"`
	Restriction *xsdRestriction `xml:"restriction"`
}

type xsdRestriction struct {
	Base         string           `xml:"base,attr"`
	Enumerations []xsdEnumeration `xml:"enumeration"`
}

type xsdEnumeration struct {
	Value string `xml:"value,attr"`
}

type xsdAnnotation struct {
	Documentation string `xml:"documentation"`
}

// RenderXSD parses xsdBytes as an XML Schema and emits the same
// RenderedSchema structure the protobuf path produces, so the frontend
// renders both identically.
func RenderXSD(xsdBytes []byte) (*RenderedSchema, error) {
	var doc xsdSchemaDoc
	if err := xml.Unmarshal(xsdBytes, &doc); err != nil {
		return nil, err
	}
	if doc.XMLName.Local != "schema" {
		return nil, &xsdRenderError{doc.XMLName.Local}
	}

	// Index named complexTypes so element declarations that reference a
	// type by name resolve to that type's fields.
	byName := make(map[string]*xsdComplexType, len(doc.ComplexTypes))
	for i := range doc.ComplexTypes {
		if doc.ComplexTypes[i].Name != "" {
			byName[doc.ComplexTypes[i].Name] = &doc.ComplexTypes[i]
		}
	}

	file := "schema.xsd"
	pkg := doc.TargetNamespace
	out := &RenderedSchema{
		Files: []RenderedFile{{Name: file, Package: pkg, Syntax: "xsd"}},
	}

	// Top-level element declarations → a message per root element, with
	// fields resolved from an inline or referenced complexType.
	for _, el := range doc.Elements {
		if el.Name == "" {
			continue
		}
		ct := el.ComplexType
		if ct == nil {
			if named := byName[localName(el.Type)]; named != nil {
				ct = named
			}
		}
		out.Messages = append(out.Messages, messageFromComplexType(el.Name, file, pkg, ct))
	}

	// Named complexTypes → a message each (browsable referenced types).
	for i := range doc.ComplexTypes {
		ct := &doc.ComplexTypes[i]
		if ct.Name == "" {
			continue
		}
		out.Messages = append(out.Messages, messageFromComplexType(ct.Name, file, pkg, ct))
	}

	// Named simpleTypes with an enumeration → an enum each.
	for _, st := range doc.SimpleTypes {
		if st.Name == "" || st.Restriction == nil || len(st.Restriction.Enumerations) == 0 {
			continue
		}
		e := RenderedEnum{FullName: st.Name, Name: st.Name, File: file, Package: pkg}
		for i, en := range st.Restriction.Enumerations {
			e.Values = append(e.Values, RenderedEnumValue{Name: en.Value, Number: int32(i)})
		}
		out.Enums = append(out.Enums, e)
	}

	sort.SliceStable(out.Messages, func(i, j int) bool { return out.Messages[i].FullName < out.Messages[j].FullName })
	sort.SliceStable(out.Enums, func(i, j int) bool { return out.Enums[i].FullName < out.Enums[j].FullName })
	return out, nil
}

// messageFromComplexType renders one complexType (or a bare element with
// no type, which becomes an empty message) as a RenderedMessage named
// `name`. Child elements become fields; attributes become "@name" fields.
func messageFromComplexType(name, file, pkg string, ct *xsdComplexType) RenderedMessage {
	m := RenderedMessage{FullName: name, Name: name, File: file, Package: pkg}
	if ct == nil {
		return m
	}
	for _, el := range collectGroupElements(ct) {
		if el.Name == "" && el.Ref == "" {
			continue
		}
		m.Fields = append(m.Fields, fieldFromElement(el))
	}
	for _, at := range ct.Attributes {
		if at.Name == "" {
			continue
		}
		card := "optional"
		if at.Use == "required" {
			card = "singular"
		}
		m.Fields = append(m.Fields, fieldFromType("@"+at.Name, at.Type, card))
	}
	return m
}

// collectGroupElements flattens the sequence/all/choice model groups of a
// complexType (one level of nesting) into a single element list.
func collectGroupElements(ct *xsdComplexType) []xsdElementDecl {
	var els []xsdElementDecl
	for _, g := range []*xsdModelGroup{ct.Sequence, ct.All, ct.Choice} {
		if g != nil {
			els = append(els, flattenGroup(*g)...)
		}
	}
	return els
}

func flattenGroup(g xsdModelGroup) []xsdElementDecl {
	els := append([]xsdElementDecl{}, g.Elements...)
	for _, sub := range g.Sequences {
		els = append(els, flattenGroup(sub)...)
	}
	for _, sub := range g.Choices {
		els = append(els, flattenGroup(sub)...)
	}
	for _, sub := range g.Alls {
		els = append(els, flattenGroup(sub)...)
	}
	return els
}

func fieldFromElement(el xsdElementDecl) RenderedField {
	name := el.Name
	typ := el.Type
	if name == "" && el.Ref != "" {
		// ref="ns:Foo" — display the referenced element, best-effort.
		name = localName(el.Ref)
		typ = el.Ref
	}
	if el.ComplexType != nil {
		typ = name // inline anonymous type; reference by the field name
	}
	f := fieldFromType(name, typ, cardinalityOf(el.MinOccurs, el.MaxOccurs))
	return f
}

// fieldFromType builds a RenderedField, mapping XSD builtins to a scalar
// Type and any other (named) type to a TypeRef, mirroring how the
// protobuf renderer distinguishes scalars from message/enum references.
func fieldFromType(name, xsdType, cardinality string) RenderedField {
	f := RenderedField{Name: name, JSONName: name, Cardinality: cardinality}
	local := localName(xsdType)
	if local == "" {
		f.Type = "anyType"
		return f
	}
	if xsdBuiltins[local] {
		f.Type = local
		return f
	}
	// A reference to a named complexType or simpleType.
	f.Type = "ref"
	f.TypeRef = local
	return f
}

// cardinalityOf maps (minOccurs, maxOccurs) into the protobuf-flavored
// vocabulary the frontend already renders.
func cardinalityOf(min, max string) string {
	if max == "unbounded" || (max != "" && max != "0" && max != "1") {
		return "repeated"
	}
	if min == "0" {
		return "optional"
	}
	return "singular"
}

// localName strips a namespace prefix from a QName ("xs:string" → "string").
func localName(qname string) string {
	if i := strings.LastIndex(qname, ":"); i >= 0 {
		return qname[i+1:]
	}
	return qname
}

// xsdBuiltins is the set of XML Schema built-in datatype local names we
// render as scalars rather than type references.
var xsdBuiltins = map[string]bool{
	"string": true, "boolean": true, "decimal": true, "float": true, "double": true,
	"duration": true, "dateTime": true, "time": true, "date": true,
	"gYearMonth": true, "gYear": true, "gMonthDay": true, "gDay": true, "gMonth": true,
	"hexBinary": true, "base64Binary": true, "anyURI": true, "QName": true, "NOTATION": true,
	"normalizedString": true, "token": true, "language": true, "NMTOKEN": true, "NMTOKENS": true,
	"Name": true, "NCName": true, "ID": true, "IDREF": true, "IDREFS": true,
	"ENTITY": true, "ENTITIES": true,
	"integer": true, "nonPositiveInteger": true, "negativeInteger": true, "long": true,
	"int": true, "short": true, "byte": true, "nonNegativeInteger": true, "unsignedLong": true,
	"unsignedInt": true, "unsignedShort": true, "unsignedByte": true, "positiveInteger": true,
	"anyType": true, "anySimpleType": true,
}

// xsdRenderError is returned when the document root is not a <schema>.
type xsdRenderError struct{ root string }

func (e *xsdRenderError) Error() string {
	return "decoder: xsd root is <" + e.root + ">, expected <schema>"
}
