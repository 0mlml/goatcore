package api

import (
	"testing"
	"time"
)

func TestParseAuditSinceDefaultsToOneHour(t *testing.T) {
	got, err := parseAuditSince("")
	if err != nil {
		t.Fatal(err)
	}
	want := time.Now().UTC().Add(-1 * time.Hour)
	if d := got.Sub(want); d < -2*time.Second || d > 2*time.Second {
		t.Errorf("default since = %v, want ~%v", got, want)
	}
}

func TestParseAuditSinceDuration(t *testing.T) {
	got, err := parseAuditSince("90m")
	if err != nil {
		t.Fatal(err)
	}
	want := time.Now().UTC().Add(-90 * time.Minute)
	if d := got.Sub(want); d < -2*time.Second || d > 2*time.Second {
		t.Errorf("90m since = %v, want ~%v", got, want)
	}
}

func TestParseAuditSinceClampsToWindow(t *testing.T) {
	// A duration past the max window is clamped to the floor, not honored.
	got, err := parseAuditSince("1000h")
	if err != nil {
		t.Fatal(err)
	}
	floor := time.Now().UTC().Add(-auditPresignMaxWindow)
	if d := got.Sub(floor); d < -2*time.Second || d > 2*time.Second {
		t.Errorf("over-window since = %v, want clamped to ~%v", got, floor)
	}
}

func TestParseAuditSinceRFC3339(t *testing.T) {
	// A recent absolute timestamp is honored verbatim.
	ts := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Second)
	got, err := parseAuditSince(ts.Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(ts) {
		t.Errorf("rfc3339 since = %v, want %v", got, ts)
	}
}

func TestParseAuditSinceFutureClampsToNow(t *testing.T) {
	got, err := parseAuditSince(time.Now().UTC().Add(48 * time.Hour).Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	if got.After(time.Now().UTC().Add(time.Second)) {
		t.Errorf("future since not clamped: %v", got)
	}
}

func TestParseAuditSinceInvalid(t *testing.T) {
	if _, err := parseAuditSince("not-a-time"); err == nil {
		t.Error("expected error for garbage since")
	}
}
