package decoder

import (
	"strings"
	"testing"
)

func TestRenderXSDCodeExamples(t *testing.T) {
	ex := RenderXSDCodeExamples(XSDExampleParams{
		TopicKey:    "tak/cop/cot/v1",
		RootElement: "event",
		SampleField: "@uid",
	})
	if len(ex) != 4 {
		t.Fatalf("want 4 language examples, got %d", len(ex))
	}
	wantLangs := map[string]bool{"rust": true, "go": true, "python": true, "typescript": true}
	for _, e := range ex {
		if !wantLangs[e.Language] {
			t.Errorf("unexpected language %q", e.Language)
		}
		if e.Source != "autogen" {
			t.Errorf("%s source = %q, want autogen", e.Language, e.Source)
		}
		if !strings.Contains(e.Code, "tak/cop/cot/v1") {
			t.Errorf("%s snippet missing topic key", e.Language)
		}
		if !strings.Contains(e.Code, "event") {
			t.Errorf("%s snippet missing root element", e.Language)
		}
	}
}

func TestRenderXSDCodeExamples_Insufficient(t *testing.T) {
	if ex := RenderXSDCodeExamples(XSDExampleParams{TopicKey: "k"}); ex != nil {
		t.Errorf("want nil when RootElement empty, got %d", len(ex))
	}
	if ex := RenderXSDCodeExamples(XSDExampleParams{RootElement: "event"}); ex != nil {
		t.Errorf("want nil when TopicKey empty, got %d", len(ex))
	}
}
