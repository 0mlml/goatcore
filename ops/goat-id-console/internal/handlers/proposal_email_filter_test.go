package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

func TestAPIListProposalsEmailFilterPreservesSponsorScope(t *testing.T) {
	s := newOnboardTestServer(t)
	for _, row := range []struct{ email, sponsor string }{
		{"Person+tag@example.test", "ours"},
		{"PERSON+TAG@example.test", "theirs"},
		{"other@example.test", "ours"},
	} {
		p := &onboardstore.Proposal{Sponsor: row.sponsor, Env: "test", Handle: row.sponsor,
			Email: row.email, ManifestJSON: "{}"}
		if err := s.Onboard.CreateProposal(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name, path  string
		operator    bool
		code, count int
	}{
		{"operator", "/?email=person%2Btag%40example.test", true, 200, 2},
		{"sponsor", "/?email=person%2Btag%40example.test", false, 200, 1},
		{"unfiltered sponsor", "/", false, 200, 2},
		{"blank email", "/?email=%20", true, 400, 0},
		{"empty email", "/?email=", true, 400, 0},
		{"status intersection", "/?email=person%2Btag%40example.test&status=archived", true, 200, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, tc.path, nil)
			r = r.WithContext(context.WithValue(r.Context(), ctxKeyOnboardPrincipal,
				onboardPrincipal{IsOperator: tc.operator, Handle: "ours"}))
			w := httptest.NewRecorder()
			s.apiListProposals(w, r)
			if w.Code != tc.code {
				t.Fatalf("got %d want %d: %s", w.Code, tc.code, w.Body.String())
			}
			if tc.code != 200 {
				return
			}
			var rows []onboardstore.Proposal
			if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
				t.Fatal(err)
			}
			if len(rows) != tc.count {
				t.Fatalf("got %d rows want %d: %+v", len(rows), tc.count, rows)
			}
		})
	}
}
