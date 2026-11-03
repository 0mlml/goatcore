package api_test

// The roster payload's notes[] mixes two unlike things: failure notes ("a layer
// did not answer, this picture is incomplete") and standing doctrinal notes
// emitted on every healthy response. Consumers were string-matching the prose
// for "unavailable" / "unreachable" / "error:" to tell them apart, which breaks
// the first time a message is reworded. note_details carries a severity and a
// stable kind on each entry instead; notes[] is unchanged and index-aligned
// with it.

import (
	"net/http"
	"path/filepath"
	"testing"
)

func TestRosterNoteDetails_AlignedWithNotesAndGraded(t *testing.T) {
	dir := t.TempDir()
	ts, _ := newSlotPoolServer(t, filepath.Join(dir, "config.json5"))
	c := &http.Client{}

	resp := doJSON(t, c, "GET", ts.URL+"/api/access/roster", "dev-operator-key", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("roster: status %d body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	body := mustReadJSON(t, resp)

	notes, _ := body["notes"].([]any)
	details, _ := body["note_details"].([]any)
	if len(notes) == 0 {
		t.Fatal("roster carries no notes at all")
	}
	if len(details) != len(notes) {
		t.Fatalf("note_details has %d entries, notes has %d — the two must stay index-aligned",
			len(details), len(notes))
	}

	var errors, infos int
	for i, d := range details {
		row, _ := d.(map[string]any)
		text, _ := row["text"].(string)
		kind, _ := row["kind"].(string)
		severity, _ := row["severity"].(string)

		if want, _ := notes[i].(string); text != want {
			t.Errorf("note_details[%d].text = %q, want the verbatim notes[%d] %q", i, text, i, want)
		}
		if kind == "" {
			t.Errorf("note_details[%d] has no kind slug: %#v", i, row)
		}
		switch severity {
		case "error":
			errors++
		case "warn":
		case "info":
			infos++
		default:
			t.Errorf("note_details[%d].severity = %q, want error|warn|info", i, severity)
		}
	}

	// This test server has no Kratos, no roster file, and no NetBird mgmt, so
	// three layers are genuinely unavailable — the notes that say so must grade
	// as errors, and the standing write/read-model notes must not.
	if errors == 0 {
		t.Error("no note graded error, though the identity/declared/mesh layers are all unconfigured here")
	}
	if infos == 0 {
		t.Error("no note graded info, though the write-model and read-model notes are emitted on every response")
	}
}
