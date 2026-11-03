package registry_test

import (
	"context"
	"errors"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-zenoh-portal/internal/registry"
)

// makeSpec returns a minimally-valid draft spec for use in tests. The
// payload + cadence JSON shapes are not parsed here — the registry stores
// them verbatim. The API layer validates structure before insertion.
func makeSpec(slug string) *registry.TrashgoatSpec {
	return &registry.TrashgoatSpec{
		Slug:            slug,
		Description:     "test spec " + slug,
		TopicPattern:    "trashgoat/" + slug + "/v1",
		PayloadSpecJSON: `{"kind":"bytes","size_bytes":1024,"content":"random"}`,
		CadenceSpecJSON: `{"kind":"oneshot"}`,
		CreatedBy:       "operator",
	}
}

func TestTrashgoat_CreateGetList(t *testing.T) {
	r := open(t)
	ctx := context.Background()
	spec := makeSpec("first-shot")
	id, err := r.CreateTrashgoatSpec(ctx, spec)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id == 0 {
		t.Fatal("expected nonzero id after create")
	}
	if spec.State != registry.TrashgoatStateDraft {
		t.Errorf("state = %q, want %q", spec.State, registry.TrashgoatStateDraft)
	}
	got, err := r.GetTrashgoatSpec(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Slug != "first-shot" {
		t.Errorf("Slug = %q", got.Slug)
	}
	if got.PayloadSpecJSON != spec.PayloadSpecJSON {
		t.Errorf("PayloadSpecJSON mismatch: got %q", got.PayloadSpecJSON)
	}

	gotBySlug, err := r.GetTrashgoatSpecBySlug(ctx, "first-shot")
	if err != nil {
		t.Fatalf("get-by-slug: %v", err)
	}
	if gotBySlug.ID != id {
		t.Errorf("get-by-slug id = %d, want %d", gotBySlug.ID, id)
	}

	// add a second spec, then list
	if _, err := r.CreateTrashgoatSpec(ctx, makeSpec("second-shot")); err != nil {
		t.Fatalf("create second: %v", err)
	}
	specs, err := r.ListTrashgoatSpecs(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(specs) != 2 {
		t.Errorf("list len = %d, want 2", len(specs))
	}
}

func TestTrashgoat_SlugUnique(t *testing.T) {
	r := open(t)
	ctx := context.Background()
	if _, err := r.CreateTrashgoatSpec(ctx, makeSpec("dup")); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := r.CreateTrashgoatSpec(ctx, makeSpec("dup"))
	if !errors.Is(err, registry.ErrSpecExists) {
		t.Errorf("dup create: want ErrSpecExists, got %v", err)
	}
}

func TestTrashgoat_SlugValidation(t *testing.T) {
	r := open(t)
	ctx := context.Background()
	bad := []string{"", "-leading", "trailing-", "UPPER", "under_score", "with space"}
	for _, slug := range bad {
		spec := makeSpec("placeholder")
		spec.Slug = slug
		spec.TopicPattern = "trashgoat/" + slug + "/v1"
		_, err := r.CreateTrashgoatSpec(ctx, spec)
		if err == nil {
			t.Errorf("CreateTrashgoatSpec(%q): expected error, got nil", slug)
		}
	}
}

func TestTrashgoat_TopicPatternRequiresPrefix(t *testing.T) {
	r := open(t)
	ctx := context.Background()
	spec := makeSpec("wrong-prefix")
	spec.TopicPattern = "notional/whatever/v1"
	_, err := r.CreateTrashgoatSpec(ctx, spec)
	if err == nil {
		t.Fatal("expected error when topic_pattern doesn't start with trashgoat/<slug>/")
	}
}

func TestTrashgoat_TopicPatternMustMatchSlug(t *testing.T) {
	r := open(t)
	ctx := context.Background()
	spec := makeSpec("alpha")
	spec.TopicPattern = "trashgoat/beta/v1"
	_, err := r.CreateTrashgoatSpec(ctx, spec)
	if err == nil {
		t.Fatal("expected error when topic_pattern second segment != slug")
	}
}

func TestTrashgoat_LifecycleHappyPath(t *testing.T) {
	r := open(t)
	ctx := context.Background()
	spec := makeSpec("lifecycle")
	id, err := r.CreateTrashgoatSpec(ctx, spec)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// draft → active
	if err := r.ActivateTrashgoatSpec(ctx, id); err != nil {
		t.Fatalf("activate from draft: %v", err)
	}
	got, _ := r.GetTrashgoatSpec(ctx, id)
	if got.State != registry.TrashgoatStateActive {
		t.Errorf("state = %q, want active", got.State)
	}
	if got.LastActivatedAt == nil {
		t.Error("last_activated_at not stamped on activate")
	}

	// active → paused
	if err := r.PauseTrashgoatSpec(ctx, id); err != nil {
		t.Fatalf("pause: %v", err)
	}
	got, _ = r.GetTrashgoatSpec(ctx, id)
	if got.State != registry.TrashgoatStatePaused {
		t.Errorf("state = %q, want paused", got.State)
	}
	if got.LastPausedAt == nil {
		t.Error("last_paused_at not stamped on pause")
	}

	// paused → active (resume)
	if err := r.ActivateTrashgoatSpec(ctx, id); err != nil {
		t.Fatalf("resume: %v", err)
	}
	got, _ = r.GetTrashgoatSpec(ctx, id)
	if got.State != registry.TrashgoatStateActive {
		t.Errorf("state = %q, want active (resumed)", got.State)
	}

	// active → retired
	if err := r.RetireTrashgoatSpec(ctx, id); err != nil {
		t.Fatalf("retire: %v", err)
	}
	got, _ = r.GetTrashgoatSpec(ctx, id)
	if got.State != registry.TrashgoatStateRetired {
		t.Errorf("state = %q, want retired", got.State)
	}
	if got.RetiredAt == nil {
		t.Error("retired_at not stamped on retire")
	}

	// retire is idempotent
	if err := r.RetireTrashgoatSpec(ctx, id); err != nil {
		t.Errorf("idempotent retire: %v", err)
	}

	// retired → active rejected
	err = r.ActivateTrashgoatSpec(ctx, id)
	if !errors.Is(err, registry.ErrInvalidTransition) {
		t.Errorf("activate-from-retired: want ErrInvalidTransition, got %v", err)
	}
}

func TestTrashgoat_IllegalTransitions(t *testing.T) {
	r := open(t)
	ctx := context.Background()

	// draft → paused — not allowed (must activate first)
	id, _ := r.CreateTrashgoatSpec(ctx, makeSpec("must-activate-first"))
	if err := r.PauseTrashgoatSpec(ctx, id); !errors.Is(err, registry.ErrInvalidTransition) {
		t.Errorf("pause-from-draft: want ErrInvalidTransition, got %v", err)
	}

	// active → active — not allowed (caller should detect)
	if err := r.ActivateTrashgoatSpec(ctx, id); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if err := r.ActivateTrashgoatSpec(ctx, id); !errors.Is(err, registry.ErrInvalidTransition) {
		t.Errorf("activate-from-active: want ErrInvalidTransition, got %v", err)
	}

	// paused → paused — not allowed
	if err := r.PauseTrashgoatSpec(ctx, id); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if err := r.PauseTrashgoatSpec(ctx, id); !errors.Is(err, registry.ErrInvalidTransition) {
		t.Errorf("pause-from-paused: want ErrInvalidTransition, got %v", err)
	}
}

func TestTrashgoat_CleanseFromDraftRejected(t *testing.T) {
	r := open(t)
	ctx := context.Background()
	id, _ := r.CreateTrashgoatSpec(ctx, makeSpec("never-published"))
	err := r.RecordTrashgoatCleanse(ctx, id)
	if !errors.Is(err, registry.ErrInvalidTransition) {
		t.Errorf("cleanse-from-draft: want ErrInvalidTransition, got %v", err)
	}
}

func TestTrashgoat_CleanseStampsWithoutStateChange(t *testing.T) {
	r := open(t)
	ctx := context.Background()
	id, _ := r.CreateTrashgoatSpec(ctx, makeSpec("active-cleanse"))
	if err := r.ActivateTrashgoatSpec(ctx, id); err != nil {
		t.Fatalf("activate: %v", err)
	}
	beforeState := mustGetTrashgoatState(t, r, id)
	if err := r.RecordTrashgoatCleanse(ctx, id); err != nil {
		t.Fatalf("cleanse: %v", err)
	}
	got, _ := r.GetTrashgoatSpec(ctx, id)
	if got.State != beforeState {
		t.Errorf("state changed by cleanse: %q -> %q", beforeState, got.State)
	}
	if got.LastCleansedAt == nil {
		t.Error("last_cleansed_at not stamped by cleanse")
	}

	// Cleanse a paused spec — state must stay paused.
	if err := r.PauseTrashgoatSpec(ctx, id); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if err := r.RecordTrashgoatCleanse(ctx, id); err != nil {
		t.Fatalf("cleanse-paused: %v", err)
	}
	got, _ = r.GetTrashgoatSpec(ctx, id)
	if got.State != registry.TrashgoatStatePaused {
		t.Errorf("cleanse changed paused → %q", got.State)
	}
}

func TestTrashgoat_UpdateOnlyOnDraft(t *testing.T) {
	r := open(t)
	ctx := context.Background()
	id, _ := r.CreateTrashgoatSpec(ctx, makeSpec("editme"))

	// update on draft — allowed
	upd := makeSpec("editme")
	upd.ID = id
	upd.Description = "new description"
	upd.PayloadSpecJSON = `{"kind":"bytes","size_bytes":2048,"content":"zeros"}`
	upd.Notes = "edited"
	if err := r.UpdateTrashgoatSpec(ctx, upd); err != nil {
		t.Fatalf("update on draft: %v", err)
	}
	got, _ := r.GetTrashgoatSpec(ctx, id)
	if got.Description != "new description" {
		t.Errorf("Description = %q", got.Description)
	}
	if got.PayloadSpecJSON != upd.PayloadSpecJSON {
		t.Errorf("PayloadSpecJSON not updated")
	}

	// activate, then update — refused
	if err := r.ActivateTrashgoatSpec(ctx, id); err != nil {
		t.Fatalf("activate: %v", err)
	}
	err := r.UpdateTrashgoatSpec(ctx, upd)
	if !errors.Is(err, registry.ErrSpecImmutable) {
		t.Errorf("update-on-active: want ErrSpecImmutable, got %v", err)
	}
}

func TestTrashgoat_DeleteOnlyOnRetired(t *testing.T) {
	r := open(t)
	ctx := context.Background()
	id, _ := r.CreateTrashgoatSpec(ctx, makeSpec("delete-me"))

	// delete-from-draft — refused
	err := r.DeleteTrashgoatSpec(ctx, id)
	if !errors.Is(err, registry.ErrSpecNotRetired) {
		t.Errorf("delete-from-draft: want ErrSpecNotRetired, got %v", err)
	}

	// activate → delete still refused
	if err := r.ActivateTrashgoatSpec(ctx, id); err != nil {
		t.Fatalf("activate: %v", err)
	}
	err = r.DeleteTrashgoatSpec(ctx, id)
	if !errors.Is(err, registry.ErrSpecNotRetired) {
		t.Errorf("delete-from-active: want ErrSpecNotRetired, got %v", err)
	}

	// retire → delete OK
	if err := r.RetireTrashgoatSpec(ctx, id); err != nil {
		t.Fatalf("retire: %v", err)
	}
	if err := r.DeleteTrashgoatSpec(ctx, id); err != nil {
		t.Fatalf("delete-from-retired: %v", err)
	}
	if _, err := r.GetTrashgoatSpec(ctx, id); !errors.Is(err, registry.ErrNotFound) {
		t.Errorf("after delete: want ErrNotFound, got %v", err)
	}
}

func TestTrashgoat_GetNotFound(t *testing.T) {
	r := open(t)
	ctx := context.Background()
	_, err := r.GetTrashgoatSpec(ctx, 99999)
	if !errors.Is(err, registry.ErrNotFound) {
		t.Errorf("get-missing: want ErrNotFound, got %v", err)
	}
	_, err = r.GetTrashgoatSpecBySlug(ctx, "nope")
	if !errors.Is(err, registry.ErrNotFound) {
		t.Errorf("get-by-slug-missing: want ErrNotFound, got %v", err)
	}
}

func mustGetTrashgoatState(t *testing.T, r *registry.Registry, id int64) string {
	t.Helper()
	s, err := r.GetTrashgoatSpec(context.Background(), id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	return s.State
}
