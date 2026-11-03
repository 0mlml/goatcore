package registry

// Wildcard topic registration + keyexpr-intersection resolution.
//
// validateTopic permits `*` as a COMPLETE mid-pattern segment (never the
// first segment, never `**`, never embedded); GetTopicByVendorKey resolves a
// concrete key to an intersecting wildcard pattern when no exact row exists,
// preferring the most specific pattern (fewest wildcards).

import (
	"context"
	"errors"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/pgtest"
)

func TestValidateTopicWildcardSegments(t *testing.T) {
	mk := func(pattern string) *Topic {
		return &Topic{VendorPrefix: "caracal", KeyPattern: pattern, Format: "json"}
	}
	ok := []string{
		"caracal/adsb/aircraft/*/v1",
		"caracal/tracks/*/state/v1",
		"caracal/system/*",
		"caracal/a/*/b/*/v1",
	}
	for _, p := range ok {
		if err := validateTopic(mk(p)); err != nil {
			t.Errorf("validateTopic(%q) = %v, want nil", p, err)
		}
	}
	bad := []string{
		"*/adsb/aircraft/v1",     // first segment must be the org prefix
		"caracal/adsb/**",        // multi-segment wildcard never registrable
		"caracal/**/v1",          // ditto, mid-pattern
		"caracal/adsb/air*/v1",   // embedded '*' in a segment
		"caracal/adsb/*craft/v1", // ditto
		"caracal/adsb/a?c/v1",    // other reserved chars still rejected
		"caracal/adsb/a#c/v1",    // ditto
	}
	for _, p := range bad {
		if err := validateTopic(mk(p)); err == nil {
			t.Errorf("validateTopic(%q) = nil, want error", p)
		}
	}
}

func TestPatternIntersects(t *testing.T) {
	cases := []struct {
		pattern, key string
		wild         int
		ok           bool
	}{
		{"v/adsb/aircraft/*/v1", "v/adsb/aircraft/151e22/v1", 1, true},
		{"v/adsb/aircraft/*/v1", "v/adsb/aircraft/151e22/v2", 0, false},
		{"v/adsb/aircraft/*/v1", "v/adsb/aircraft/v1", 0, false}, // segment count mismatch
		{"v/a/*/b/*/v1", "v/a/x/b/y/v1", 2, true},
		{"v/a/x/v1", "v/a/x/v1", 0, true},
		{"v/a/*/v1", "v/a/x/y/v1", 0, false},
	}
	for _, tc := range cases {
		wild, ok := patternIntersects(tc.pattern, tc.key)
		if ok != tc.ok || wild != tc.wild {
			t.Errorf("patternIntersects(%q, %q) = (%d, %v), want (%d, %v)",
				tc.pattern, tc.key, wild, ok, tc.wild, tc.ok)
		}
	}
}

func TestGetTopicByVendorKeyWildcardFallback(t *testing.T) {
	ctx := context.Background()
	reg, err := Open(pgtest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.CreateVendor(ctx, &Vendor{Prefix: "caracal", APIKeyHash: "x", DisplayName: "c"}); err != nil {
		t.Fatal(err)
	}
	mk := func(pattern string) int64 {
		id, err := reg.CreateTopic(ctx, &Topic{VendorPrefix: "caracal", KeyPattern: pattern, Format: "json"})
		if err != nil {
			t.Fatalf("CreateTopic(%q): %v", pattern, err)
		}
		return id
	}
	exactID := mk("caracal/adsb/aircraft/151e22/v1")
	wildID := mk("caracal/adsb/aircraft/*/v1")
	doubleWildID := mk("caracal/adsb/*/*/v1")

	// Exact row wins over any intersecting wildcard.
	got, err := reg.GetTopicByVendorKey(ctx, "caracal", "caracal/adsb/aircraft/151e22/v1")
	if err != nil || got.ID != exactID {
		t.Fatalf("exact: got id %v err %v, want %d", got, err, exactID)
	}
	// No exact row: the most specific intersecting pattern resolves.
	got, err = reg.GetTopicByVendorKey(ctx, "caracal", "caracal/adsb/aircraft/ffffff/v1")
	if err != nil || got.ID != wildID {
		t.Fatalf("wildcard: got %v err %v, want id %d", got, err, wildID)
	}
	// Only the broader pattern intersects this one.
	got, err = reg.GetTopicByVendorKey(ctx, "caracal", "caracal/adsb/ship/imo123/v1")
	if err != nil || got.ID != doubleWildID {
		t.Fatalf("broad wildcard: got %v err %v, want id %d", got, err, doubleWildID)
	}
	// Nothing intersects: ErrNotFound, never a false match.
	if _, err = reg.GetTopicByVendorKey(ctx, "caracal", "caracal/other/x/v1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("miss: err = %v, want ErrNotFound", err)
	}
	// A retired wildcard pattern no longer resolves.
	if err := reg.RetireTopic(ctx, wildID); err != nil {
		t.Fatal(err)
	}
	got, err = reg.GetTopicByVendorKey(ctx, "caracal", "caracal/adsb/aircraft/ffffff/v1")
	if err != nil || got.ID != doubleWildID {
		t.Fatalf("retired fallback: got %v err %v, want id %d (the surviving broader pattern)", got, err, doubleWildID)
	}
}
