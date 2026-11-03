package peerset

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateV1_RejectsEmptyName(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.json")
	_ = os.WriteFile(p, []byte(`[{"name": "", "pubkey": "P", "address": "A"}]`), 0o644)
	_, err := Load(p)
	if !errors.Is(err, ErrMigrationFailed) {
		t.Fatalf("err = %v, want ErrMigrationFailed", err)
	}
}

func TestMigrateV1_RejectsEmptyAddress(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.json")
	_ = os.WriteFile(p, []byte(`[{"name": "n", "pubkey": "P", "address": ""}]`), 0o644)
	_, err := Load(p)
	if !errors.Is(err, ErrMigrationFailed) {
		t.Fatalf("err = %v, want ErrMigrationFailed", err)
	}
}

func TestMigrateV1_RejectsMalformedJSON(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.json")
	_ = os.WriteFile(p, []byte(`[{name without quotes}]`), 0o644)
	_, err := Load(p)
	if !errors.Is(err, ErrMigrationFailed) {
		t.Fatalf("err = %v, want ErrMigrationFailed", err)
	}
}

func TestMigrateV1_EmptyArrayMigratesToEmptyV2(t *testing.T) {
	p := filepath.Join(t.TempDir(), "empty-array.json")
	_ = os.WriteFile(p, []byte(`[]`), 0o644)
	ps, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps.Peers) != 0 || ps.SchemaVersion != SchemaVersion {
		t.Fatalf("unexpected envelope: %+v", ps)
	}
}
