package pubsubstate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakePortal stands in for the real portal API. Maintains in-memory
// state, satisfies the subset of routes the reconciler needs, and
// records every mutation for assertions.
type fakePortal struct {
	mu       sync.Mutex
	vendors  map[string]VendorRow
	topics   map[string]map[int64]TopicRow // vendor -> id -> row
	schemas  map[int64]map[string]SchemaRow
	examples map[int64]map[string]string
	nextID   int64
	mutLog   []string
}

func newFakePortal() *fakePortal {
	return &fakePortal{
		vendors:  map[string]VendorRow{},
		topics:   map[string]map[int64]TopicRow{},
		schemas:  map[int64]map[string]SchemaRow{},
		examples: map[int64]map[string]string{},
		nextID:   1,
	}
}

func (f *fakePortal) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/vendors", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		out := []VendorRow{}
		for _, v := range f.vendors {
			out = append(out, v)
		}
		writeJSON(w, 200, map[string]any{"vendors": out})
	})
	mux.HandleFunc("POST /api/vendors", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Prefix      string `json:"prefix"`
			DisplayName string `json:"display_name"`
			Synthetic   bool   `json:"synthetic"`
			AutoApprove bool   `json:"auto_approve"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.mutLog = append(f.mutLog, "POST /api/vendors "+req.Prefix)
		if _, exists := f.vendors[req.Prefix]; exists {
			http.Error(w, "exists", http.StatusConflict)
			return
		}
		row := VendorRow{Prefix: req.Prefix, DisplayName: req.DisplayName, Synthetic: req.Synthetic}
		if req.AutoApprove {
			now := int64(1)
			approver := "operator"
			row.ApprovedAt = &now
			row.Approver = &approver
		}
		f.vendors[req.Prefix] = row
		f.topics[req.Prefix] = map[int64]TopicRow{}
		writeJSON(w, 201, row)
	})
	mux.HandleFunc("PATCH /api/vendors/{vendor}", func(w http.ResponseWriter, r *http.Request) {
		vendor := r.PathValue("vendor")
		var req patchVendorReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.mutLog = append(f.mutLog, "PATCH /api/vendors/"+vendor)
		v, ok := f.vendors[vendor]
		if !ok {
			http.Error(w, "notfound", 404)
			return
		}
		v.DisplayName = req.DisplayName
		f.vendors[vendor] = v
		w.WriteHeader(204)
	})
	mux.HandleFunc("DELETE /api/vendors/{vendor}", func(w http.ResponseWriter, r *http.Request) {
		vendor := r.PathValue("vendor")
		f.mu.Lock()
		defer f.mu.Unlock()
		f.mutLog = append(f.mutLog, "DELETE /api/vendors/"+vendor)
		if len(f.topics[vendor]) > 0 {
			http.Error(w, "has topics", 409)
			return
		}
		delete(f.vendors, vendor)
		delete(f.topics, vendor)
		w.WriteHeader(204)
	})
	mux.HandleFunc("GET /api/vendors/{vendor}/topics", func(w http.ResponseWriter, r *http.Request) {
		vendor := r.PathValue("vendor")
		f.mu.Lock()
		defer f.mu.Unlock()
		out := []TopicRow{}
		for _, t := range f.topics[vendor] {
			out = append(out, t)
		}
		writeJSON(w, 200, map[string]any{"topics": out})
	})
	mux.HandleFunc("POST /api/vendors/{vendor}/topics", func(w http.ResponseWriter, r *http.Request) {
		vendor := r.PathValue("vendor")
		var req createTopicReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.mutLog = append(f.mutLog, "POST /api/vendors/"+vendor+"/topics "+req.KeyPattern)
		// Conflict if same key_pattern already exists.
		for _, t := range f.topics[vendor] {
			if t.KeyPattern == req.KeyPattern {
				http.Error(w, "exists", 409)
				return
			}
		}
		id := f.nextID
		f.nextID++
		row := TopicRow{ID: id, VendorPrefix: vendor, KeyPattern: req.KeyPattern,
			Format: req.Format, SchemaRef: req.SchemaRef,
			Description: req.Description, PublishToStorage: req.PublishToStorage}
		f.topics[vendor][id] = row
		writeJSON(w, 201, row)
	})
	mux.HandleFunc("PATCH /api/vendors/{vendor}/topics/{id}", func(w http.ResponseWriter, r *http.Request) {
		vendor := r.PathValue("vendor")
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		var req updateTopicReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.mutLog = append(f.mutLog, fmt.Sprintf("PATCH /api/vendors/%s/topics/%d", vendor, id))
		t := f.topics[vendor][id]
		t.Format = req.Format
		t.SchemaRef = req.SchemaRef
		t.Description = req.Description
		t.PublishToStorage = req.PublishToStorage
		f.topics[vendor][id] = t
		w.WriteHeader(204)
	})
	mux.HandleFunc("DELETE /api/vendors/{vendor}/topics/{id}", func(w http.ResponseWriter, r *http.Request) {
		vendor := r.PathValue("vendor")
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.mutLog = append(f.mutLog, fmt.Sprintf("DELETE /api/vendors/%s/topics/%d", vendor, id))
		delete(f.topics[vendor], id)
		delete(f.schemas, id)
		delete(f.examples, id)
		w.WriteHeader(204)
	})
	mux.HandleFunc("POST /api/vendors/{vendor}/topics/{id}/schemas", func(w http.ResponseWriter, r *http.Request) {
		vendor := r.PathValue("vendor")
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		// Multipart parse — just record the upload + version field.
		if err := r.ParseMultipartForm(2 << 20); err != nil {
			http.Error(w, "parse", 400)
			return
		}
		version := r.FormValue("version")
		f.mu.Lock()
		defer f.mu.Unlock()
		f.mutLog = append(f.mutLog, fmt.Sprintf("POST /api/vendors/%s/topics/%d/schemas %s", vendor, id, version))
		if f.schemas[id] == nil {
			f.schemas[id] = map[string]SchemaRow{}
		}
		if _, exists := f.schemas[id][version]; exists {
			http.Error(w, "exists", 409)
			return
		}
		f.schemas[id][version] = SchemaRow{ID: int64(len(f.schemas[id]) + 1), TopicID: id, Version: version,
			Compatibility: r.FormValue("compatibility"), Format: r.FormValue("format")}
		writeJSON(w, 201, f.schemas[id][version])
	})
	mux.HandleFunc("GET /api/vendors/{vendor}/topics/{id}/schemas", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		f.mu.Lock()
		defer f.mu.Unlock()
		out := []SchemaRow{}
		for _, s := range f.schemas[id] {
			out = append(out, s)
		}
		writeJSON(w, 200, map[string]any{"schemas": out})
	})
	mux.HandleFunc("GET /api/vendors/{vendor}/topics/{id}/examples", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		f.mu.Lock()
		defer f.mu.Unlock()
		out := []ExampleRow{}
		for lang, code := range f.examples[id] {
			out = append(out, ExampleRow{TopicID: id, Language: lang, Code: code})
		}
		writeJSON(w, 200, map[string]any{"examples": out})
	})
	mux.HandleFunc("DELETE /api/vendors/{vendor}/topics/{id}/examples/{lang}", func(w http.ResponseWriter, r *http.Request) {
		vendor := r.PathValue("vendor")
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		lang := r.PathValue("lang")
		f.mu.Lock()
		defer f.mu.Unlock()
		f.mutLog = append(f.mutLog, fmt.Sprintf("DELETE /api/vendors/%s/topics/%d/examples/%s", vendor, id, lang))
		delete(f.examples[id], lang)
		w.WriteHeader(204)
	})
	mux.HandleFunc("PUT /api/vendors/{vendor}/topics/{id}/examples/{lang}", func(w http.ResponseWriter, r *http.Request) {
		vendor := r.PathValue("vendor")
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		lang := r.PathValue("lang")
		var req putExampleReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.mutLog = append(f.mutLog, fmt.Sprintf("PUT /api/vendors/%s/topics/%d/examples/%s", vendor, id, lang))
		if f.examples[id] == nil {
			f.examples[id] = map[string]string{}
		}
		f.examples[id][lang] = req.Code
		w.WriteHeader(204)
	})
	return mux
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

// TestReconcileFromEmpty applies a non-trivial repo against an empty
// portal and verifies the resulting state + mutation log.
func TestReconcileFromEmpty(t *testing.T) {
	fp := newFakePortal()
	srv := httptest.NewServer(fp.Handler())
	defer srv.Close()

	client, _ := NewClient(srv.URL, "test-key")
	desired := map[string]*VendorState{
		"notional": {
			Vendor: VendorDoc{Prefix: "notional", DisplayName: "Notional", Synthetic: true},
			Topics: map[string]*TopicState{
				"notional/tarsier/v1": {
					Topic: TopicDoc{KeyPattern: "notional/tarsier/v1", Format: "protobuf:Foo", Description: "T", PublishToStorage: true},
					Schemas: map[string]SchemaBlob{
						"v1": {Meta: SchemaMeta{Version: "v1", Compatibility: "backward", Format: "protobuf-fds"}, Body: []byte("FDS")},
					},
					Examples: map[string]ExampleBlob{
						"python": {Meta: ExampleMeta{Language: "python"}, Code: "import zenoh"},
					},
				},
			},
		},
	}
	rec := &Reconciler{Client: client, Desired: desired, AutoApprove: true}
	plan, err := rec.Apply(context.Background())
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if plan.IsEmpty() {
		t.Fatal("plan should not be empty on first apply")
	}

	// Verify the portal now matches.
	if _, ok := fp.vendors["notional"]; !ok {
		t.Error("vendor not created")
	}
	if len(fp.topics["notional"]) != 1 {
		t.Errorf("expected 1 topic, got %d", len(fp.topics["notional"]))
	}

	// Second apply should be a no-op.
	plan2, err := rec.Apply(context.Background())
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if !plan2.IsEmpty() {
		t.Errorf("second apply should be no-op; got %d actions", len(plan2.Actions))
	}
}

// TestReconcileDeletesExtras verifies that current-state-only semantics
// retire topics + delete vendors absent from the desired repo.
func TestReconcileDeletesExtras(t *testing.T) {
	fp := newFakePortal()
	// Seed with a vendor + topic that should be removed.
	fp.vendors["zombie"] = VendorRow{Prefix: "zombie", DisplayName: "Zombie"}
	fp.topics["zombie"] = map[int64]TopicRow{
		7: {ID: 7, VendorPrefix: "zombie", KeyPattern: "zombie/x/v1", Format: "json"},
	}
	srv := httptest.NewServer(fp.Handler())
	defer srv.Close()

	client, _ := NewClient(srv.URL, "test-key")
	rec := &Reconciler{Client: client, Desired: map[string]*VendorState{}}
	plan, err := rec.Apply(context.Background())
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if plan.IsEmpty() {
		t.Fatal("plan should not be empty when extras present")
	}

	// Both retire + delete should have fired.
	if _, ok := fp.vendors["zombie"]; ok {
		t.Error("zombie vendor should have been deleted")
	}
}

// TestReconcileDryRun verifies that dry-run executes reads but no
// mutations.
func TestReconcileDryRun(t *testing.T) {
	fp := newFakePortal()
	srv := httptest.NewServer(fp.Handler())
	defer srv.Close()

	client, _ := NewClient(srv.URL, "test-key")
	client.SetDryRun(func(method, path string) {})

	desired := map[string]*VendorState{
		"notional": {
			Vendor: VendorDoc{Prefix: "notional", DisplayName: "X"},
			Topics: map[string]*TopicState{},
		},
	}
	rec := &Reconciler{Client: client, Desired: desired, AutoApprove: true}
	_, err := rec.Apply(context.Background())
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(fp.mutLog) != 0 {
		t.Errorf("dry-run should not mutate; log = %v", fp.mutLog)
	}
}

// Suppress unused imports during incremental editing.
var _ io.Reader
var _ strings.Builder
