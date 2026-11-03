package store

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/usareur-af/goatcore/ops/smoketest/internal/probe"
)

func TestAppendPreservesRunHistory(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "results.jsonl"))
	for _, id := range []string{"first", "second"} {
		if err := s.Append(&probe.RunResult{RunID: id}); err != nil {
			t.Fatal(err)
		}
	}
	results, err := s.List(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].RunID != "first" || results[1].RunID != "second" {
		t.Fatalf("unexpected persisted history: %+v", results)
	}
}

func TestAppendReportsOpenFailure(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "missing", "results.jsonl"))
	if err := s.Append(&probe.RunResult{RunID: "first"}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected missing-directory error, got %v", err)
	}
}

func TestAppendRejectsInvalidResultWithoutChangingHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "results.jsonl")
	s := New(path)
	if err := s.Append(&probe.RunResult{RunID: "first"}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Append(&probe.RunResult{DurationS: math.NaN()}); err == nil {
		t.Fatal("expected JSON encoding failure")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("invalid result changed existing history")
	}
}
