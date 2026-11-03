package canonical

import (
	"bytes"
	"encoding/json"
	"testing"
)

// TestMarshalDeterministic asserts that two structurally-equal inputs produce
// byte-identical output. Determinism is the load-bearing property — the
// signed payload must be reproducible on the verifier.
func TestMarshalDeterministic(t *testing.T) {
	type inner struct {
		B int    `json:"b"`
		A string `json:"a"`
	}
	type outer struct {
		Z inner `json:"z"`
		Y int   `json:"y"`
	}

	a := outer{Z: inner{A: "hello", B: 7}, Y: 42}
	b := outer{Z: inner{A: "hello", B: 7}, Y: 42}

	ba, err := Marshal(a)
	if err != nil {
		t.Fatalf("Marshal(a): %v", err)
	}
	bb, err := Marshal(b)
	if err != nil {
		t.Fatalf("Marshal(b): %v", err)
	}
	if !bytes.Equal(ba, bb) {
		t.Fatalf("Marshal not deterministic:\n  a=%s\n  b=%s", ba, bb)
	}
}

// TestMarshalParseable asserts the bytes round-trip back through encoding/json.
// Belt-and-suspenders: any future change to Marshal that writes invalid JSON
// fails this test before it reaches the signer.
func TestMarshalParseable(t *testing.T) {
	type payload struct {
		Host string `json:"host"`
		N    int    `json:"n"`
	}
	want := payload{Host: "vega-a", N: 9}

	b, err := Marshal(want)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got payload
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal of canonical bytes: %v\n  bytes=%s", err, b)
	}
	if got != want {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, want)
	}
}

// TestMarshalNil documents what nil produces (the literal JSON "null") so a
// future regression that silently swaps to "" or "{}" would fail loudly.
func TestMarshalNil(t *testing.T) {
	b, err := Marshal(nil)
	if err != nil {
		t.Fatalf("Marshal(nil): %v", err)
	}
	if string(b) != "null" {
		t.Errorf("Marshal(nil) = %q, want %q", b, "null")
	}
}
