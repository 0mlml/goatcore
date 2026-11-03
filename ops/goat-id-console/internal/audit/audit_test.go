package audit

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
)

func TestRingBufferOldestFirstDropped(t *testing.T) {
	e := NewStderrEmitter(3)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		ev := Event{Action: "test", Result: "ok"}
		ev.Actor.Email = string(rune('a' + i))
		e.Emit(ctx, ev)
	}
	got := e.Recent(10)
	if len(got) != 3 {
		t.Fatalf("ring size = %d, want 3", len(got))
	}
	// Newest first: actor emails should be e, d, c (most recent three).
	wantEmails := []string{"e", "d", "c"}
	for i, ev := range got {
		if ev.Actor.Email != wantEmails[i] {
			t.Errorf("idx %d email = %q, want %q", i, ev.Actor.Email, wantEmails[i])
		}
	}
}

func TestRingNotYetFull(t *testing.T) {
	e := NewStderrEmitter(10)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		e.Emit(ctx, Event{Action: "test", Actor: Actor{Email: string(rune('a' + i))}})
	}
	got := e.Recent(0)
	if len(got) != 3 {
		t.Fatalf("recent len = %d, want 3", len(got))
	}
	if got[0].Actor.Email != "c" || got[1].Actor.Email != "b" || got[2].Actor.Email != "a" {
		t.Errorf("not newest-first: %v", []string{got[0].Actor.Email, got[1].Actor.Email, got[2].Actor.Email})
	}
}

func TestRecentLimit(t *testing.T) {
	e := NewStderrEmitter(100)
	ctx := context.Background()
	for i := 0; i < 50; i++ {
		e.Emit(ctx, Event{Action: "test"})
	}
	if got := e.Recent(10); len(got) != 10 {
		t.Errorf("limit=10 returned %d", len(got))
	}
	if got := e.Recent(200); len(got) != 50 {
		t.Errorf("limit=200 with 50 stored returned %d", len(got))
	}
}

// TestFileEmitterPreloadsHistory: a restart must not empty the page.
func TestFileEmitterPreloadsHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	first, err := NewFileEmitter(5, path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 7; i++ {
		first.Emit(context.Background(), Event{Action: "a" + strconv.Itoa(i), Result: "ok"})
	}
	second, err := NewFileEmitter(5, path)
	if err != nil {
		t.Fatal(err)
	}
	got := second.Recent(0)
	if len(got) != 5 {
		t.Fatalf("after restart Recent() has %d events, want 5 (the file holds 7, ring cap 5)", len(got))
	}
	if got[0].Action != "a6" || got[4].Action != "a2" {
		t.Errorf("newest-first order broken: %s .. %s", got[0].Action, got[4].Action)
	}
	if !second.Durable() || (NewStderrEmitter(1)).Durable() {
		t.Errorf("Durable() must be true for the file sink and false for stderr")
	}
}

func TestFileEmitterReportsWriteFailureAsNotDurable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	e, err := NewFileEmitter(5, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.f.Close(); err != nil {
		t.Fatal(err)
	}

	e.Emit(context.Background(), Event{Action: "lost", Result: "ok"})
	if e.Durable() {
		t.Fatal("Durable() remained true after the audit file rejected a write")
	}
}
