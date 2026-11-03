package pubsubstate

import (
	"path/filepath"
	"reflect"
	"testing"
)

// TestWriteReadRoundTrip verifies that an in-memory state survives a
// write-then-read cycle byte-for-byte.
func TestWriteReadRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	now := int64(1_700_000_000)

	state := map[string]*VendorState{
		"notional": {
			Vendor: VendorDoc{
				Prefix:      "notional",
				DisplayName: "Notional Trial Vendor",
				Synthetic:   true,
				ApprovedAt:  ptrInt64(now - 3600),
				Approver:    ptrStr("operator"),
			},
			Topics: map[string]*TopicState{
				"notional/tarsier/alpha-fob/cam/v1": {
					Topic: TopicDoc{
						KeyPattern:       "notional/tarsier/alpha-fob/cam/v1",
						Format:           "protobuf:notional.tracks_demo.v1.Entity",
						SchemaRef:        "notional/tarsier/alpha-fob/cam/v1@v1",
						Description:      "Notional EO/IR track stream",
						PublishToStorage: true,
					},
					Schemas: map[string]SchemaBlob{
						"v1": {
							Meta: SchemaMeta{
								Version:       "v1",
								Compatibility: "backward",
								Format:        "protobuf-fds",
								SizeBytes:     12,
								SHA256:        "deadbeef",
								UploadedAt:    now - 1800,
								UploadedBy:    "notional",
							},
							Body: []byte("FDSBYTES1234"),
						},
					},
					Examples: map[string]ExampleBlob{
						"python": {
							Meta: ExampleMeta{Language: "python", UpdatedAt: now - 600, UpdatedBy: "vendor"},
							Code: "import zenoh\n# example\n",
						},
					},
				},
			},
		},
	}

	mfst, err := WriteRepo(tmp, state, "0.1.0-test", now)
	if err != nil {
		t.Fatalf("WriteRepo: %v", err)
	}
	if mfst.VendorCount != 1 || mfst.TopicCount != 1 || mfst.SchemaCount != 1 || mfst.ExampleCount != 1 {
		t.Errorf("manifest counts wrong: %+v", mfst)
	}
	if mfst.ContentSHA256 == "" {
		t.Error("manifest content_sha256 empty")
	}

	read, err := ReadRepo(tmp)
	if err != nil {
		t.Fatalf("ReadRepo: %v", err)
	}
	if !reflect.DeepEqual(read, state) {
		t.Errorf("round-trip mismatch:\nwrote %+v\nread  %+v", state, read)
	}

	// Re-reading + re-writing must produce the same hash.
	mfst2, err := WriteRepo(tmp, state, "0.1.0-test", now)
	if err != nil {
		t.Fatalf("second WriteRepo: %v", err)
	}
	if mfst2.ContentSHA256 != mfst.ContentSHA256 {
		t.Errorf("non-deterministic hash: %s vs %s", mfst.ContentSHA256, mfst2.ContentSHA256)
	}
}

// TestReadEmptyRepo returns an empty map without erroring when the
// vendors/ subdir doesn't exist.
func TestReadEmptyRepo(t *testing.T) {
	tmp := t.TempDir()
	state, err := ReadRepo(tmp)
	if err != nil {
		t.Fatalf("ReadRepo on empty dir: %v", err)
	}
	if len(state) != 0 {
		t.Errorf("expected empty state, got %d vendors", len(state))
	}
}

// TestKeyPatternToPathSegments rejects malformed keys.
func TestKeyPatternToPathSegments(t *testing.T) {
	cases := []struct {
		name    string
		vendor  string
		key     string
		want    []string
		wantErr bool
	}{
		{"happy", "notional", "notional/tarsier/alpha-fob/cam/v1", []string{"tarsier", "alpha-fob", "cam", "v1"}, false},
		{"vendor-mismatch", "notional", "caracal/tarsier/v1", nil, true},
		{"empty-segment", "notional", "notional//cam/v1", nil, true},
		{"too-short", "notional", "notional", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := topicKeyToPathSegments(tc.vendor, tc.key)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestHashTreeDeterministic verifies that the same content produces the
// same hash regardless of file write order.
func TestHashTreeDeterministic(t *testing.T) {
	tmp := t.TempDir()
	state := map[string]*VendorState{
		"a": {
			Vendor: VendorDoc{Prefix: "a", DisplayName: "A"},
			Topics: map[string]*TopicState{
				"a/x/v1": {Topic: TopicDoc{KeyPattern: "a/x/v1", Format: "json"}},
				"a/y/v1": {Topic: TopicDoc{KeyPattern: "a/y/v1", Format: "json"}},
			},
		},
		"b": {
			Vendor: VendorDoc{Prefix: "b", DisplayName: "B"},
			Topics: map[string]*TopicState{
				"b/z/v1": {Topic: TopicDoc{KeyPattern: "b/z/v1", Format: "json"}},
			},
		},
	}
	if _, err := WriteRepo(tmp, state, "v", 0); err != nil {
		t.Fatal(err)
	}
	h1, err := HashTree(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WriteRepo(tmp, state, "v", 0); err != nil {
		t.Fatal(err)
	}
	h2, err := HashTree(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Errorf("hash drift: %s vs %s", h1, h2)
	}
}

func ptrInt64(v int64) *int64    { return &v }
func ptrStr(s string) *string    { return &s }
var _ = filepath.Join // keep import stable for future tests
