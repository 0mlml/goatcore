package kratos

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestPatchStateVerifiesStoredIdentity(t *testing.T) {
	const identity = "00000000-0000-4000-8000-000000000001"
	for _, tc := range []struct {
		name, requested, echoed, stored, returnedID string
		patchStatus, readStatus                     int
		wantError                                   bool
	}{
		{"activation persisted", "active", "active", "active", identity, 200, 200, false},
		{"deactivation persisted", "inactive", "inactive", "inactive", identity, 200, 200, false},
		{"readback overrides stale patch response", "active", "inactive", "active", identity, 200, 200, false},
		{"guard refused activation", "active", "active", "inactive", identity, 200, 200, true},
		{"deactivation did not persist", "inactive", "inactive", "active", identity, 200, 200, true},
		{"readback returned another identity", "active", "active", "active", "00000000-0000-4000-8000-000000000002", 200, 200, true},
		{"readback unavailable", "active", "active", "active", identity, 200, 503, true},
		{"patch failed", "active", "active", "active", identity, 503, 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var reads atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/admin/identities/"+identity {
					t.Errorf("unexpected identity path: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.Method {
				case http.MethodPatch:
					w.WriteHeader(tc.patchStatus)
					_ = json.NewEncoder(w).Encode(map[string]any{"id": identity, "state": tc.echoed})
				case http.MethodGet:
					reads.Add(1)
					w.WriteHeader(tc.readStatus)
					_ = json.NewEncoder(w).Encode(map[string]any{"id": tc.returnedID, "state": tc.stored})
				default:
					w.WriteHeader(http.StatusMethodNotAllowed)
				}
			}))
			t.Cleanup(srv.Close)
			client := NewClient(srv.URL)
			got, err := client.PatchState(t.Context(), identity, tc.requested)
			if (err != nil) != tc.wantError {
				t.Fatalf("PatchState error = %v, want error %v", err, tc.wantError)
			}
			if err == nil && (got.ID != identity || got.State != tc.requested) {
				t.Fatalf("returned identity does not match stored state: %+v", got)
			}
			wantReads := int32(1)
			if tc.patchStatus != http.StatusOK {
				wantReads = 0
			}
			if reads.Load() != wantReads {
				t.Fatalf("readback count = %d, want %d", reads.Load(), wantReads)
			}
		})
	}
}
