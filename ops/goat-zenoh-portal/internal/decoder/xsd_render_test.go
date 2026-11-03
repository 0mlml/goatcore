package decoder

import (
	"encoding/json"
	"testing"
)

// A CoT-shaped XSD with a root element that references a named
// complexType, a repeated child, an attribute, and an enumeration
// simpleType — exercises every RenderXSD mapping branch.
const renderXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:tak:cot">
  <xs:element name="event" type="EventType"/>
  <xs:complexType name="EventType">
    <xs:sequence>
      <xs:element name="point" type="PointType"/>
      <xs:element name="detail" type="xs:string" minOccurs="0" maxOccurs="unbounded"/>
    </xs:sequence>
    <xs:attribute name="uid" type="xs:string" use="required"/>
    <xs:attribute name="how" type="HowEnum"/>
  </xs:complexType>
  <xs:complexType name="PointType">
    <xs:attribute name="lat" type="xs:double" use="required"/>
    <xs:attribute name="lon" type="xs:double" use="required"/>
  </xs:complexType>
  <xs:simpleType name="HowEnum">
    <xs:restriction base="xs:string">
      <xs:enumeration value="m-g"/>
      <xs:enumeration value="h-e"/>
    </xs:restriction>
  </xs:simpleType>
</xs:schema>`

func findMsg(rs *RenderedSchema, name string) *RenderedMessage {
	for i := range rs.Messages {
		if rs.Messages[i].Name == name {
			return &rs.Messages[i]
		}
	}
	return nil
}

func TestRenderXSD_Structure(t *testing.T) {
	rs, err := RenderXSD([]byte(renderXSD))
	if err != nil {
		t.Fatalf("RenderXSD: %v", err)
	}
	if len(rs.Files) != 1 || rs.Files[0].Syntax != "xsd" || rs.Files[0].Package != "urn:tak:cot" {
		t.Errorf("file meta wrong: %+v", rs.Files)
	}

	// The root element resolves its referenced complexType's fields.
	event := findMsg(rs, "event")
	if event == nil {
		t.Fatalf("no 'event' message; got %v", msgNames(rs))
	}
	fields := map[string]RenderedField{}
	for _, f := range event.Fields {
		fields[f.Name] = f
	}
	if _, ok := fields["point"]; !ok {
		t.Errorf("event missing 'point' field: %v", event.Fields)
	}
	if f := fields["detail"]; f.Cardinality != "repeated" {
		t.Errorf("detail cardinality = %q, want repeated", f.Cardinality)
	}
	if f := fields["@uid"]; f.Cardinality != "singular" || f.Type != "string" {
		t.Errorf("@uid = %+v, want singular string", f)
	}
	if f := fields["@how"]; f.Type != "ref" || f.TypeRef != "HowEnum" {
		t.Errorf("@how = %+v, want ref->HowEnum", f)
	}
	if f := fields["point"]; f.Type != "ref" || f.TypeRef != "PointType" {
		t.Errorf("point = %+v, want ref->PointType", f)
	}

	// The referenced complexType is browsable as its own message.
	if findMsg(rs, "PointType") == nil {
		t.Errorf("PointType message missing; got %v", msgNames(rs))
	}

	// The enumeration simpleType becomes an enum.
	if len(rs.Enums) != 1 || rs.Enums[0].Name != "HowEnum" || len(rs.Enums[0].Values) != 2 {
		t.Errorf("enums wrong: %+v", rs.Enums)
	}
}

func TestRenderXSD_InlineComplexType(t *testing.T) {
	xsd := `<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
	  <xs:element name="msg"><xs:complexType>
	    <xs:sequence><xs:element name="body" type="xs:string"/></xs:sequence>
	  </xs:complexType></xs:element>
	</xs:schema>`
	rs, err := RenderXSD([]byte(xsd))
	if err != nil {
		t.Fatalf("RenderXSD: %v", err)
	}
	m := findMsg(rs, "msg")
	if m == nil || len(m.Fields) != 1 || m.Fields[0].Name != "body" {
		t.Errorf("inline complexType not rendered: %v", msgNames(rs))
	}
}

func TestRenderXSD_BadInputs(t *testing.T) {
	if _, err := RenderXSD([]byte(`<html><body/></html>`)); err == nil {
		t.Errorf("expected error when root is not <schema>")
	}
	if _, err := RenderXSD([]byte(`<xs:schema><oops`)); err == nil {
		t.Errorf("expected error for malformed xml")
	}
}

func TestRenderXSD_JSONShape(t *testing.T) {
	rs, err := RenderXSD([]byte(renderXSD))
	if err != nil {
		t.Fatalf("RenderXSD: %v", err)
	}
	// Must serialize into the same shape schema.js consumes.
	b, err := json.Marshal(rs)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back struct {
		Files    []map[string]any `json:"files"`
		Messages []map[string]any `json:"messages"`
		Enums    []map[string]any `json:"enums"`
	}
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back.Messages) == 0 || back.Messages[0]["fields"] == nil {
		t.Errorf("rendered JSON missing messages/fields: %s", b)
	}
}

func msgNames(rs *RenderedSchema) []string {
	out := make([]string, len(rs.Messages))
	for i, m := range rs.Messages {
		out[i] = m.Name
	}
	return out
}
