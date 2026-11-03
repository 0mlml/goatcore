package handlers

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/repoaccess"
)

// #2107: the stored grant row must say why GitHub refused, survive the JSON
// round trip the welcome-download path takes, and carry no secret.
func TestRepoGrantRecord_failureIsDurableAndSecretFree(t *testing.T) {
	refused := &repoaccess.HTTPError{Owner: "example-org", Repo: "example-repo", Status: 404,
		Body: `{"message":"Not Found","token":"fixture-secret"}`}
	rows := []any{
		repoGrantRecord("example-org", "example-repo", "pull", "policy", repoaccess.OutcomeFailed, refused),
		repoGrantRecord("example-org", "other-repo", "pull", "policy", repoaccess.OutcomeFailed, errors.New("dial tcp: timeout")),
		repoGrantRecord("example-org", "ok-repo", "pull", "policy", repoaccess.OutcomeInvited, nil),
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "fixture-secret") || strings.Contains(string(raw), "Not Found") {
		t.Fatalf("the persisted record carries the response body: %s", raw)
	}
	got := parseRepoGrants(string(raw))
	if len(got) != 3 {
		t.Fatalf("round trip lost rows: %s", raw)
	}
	if got[0].Outcome != "failed" || got[0].HTTPStatus != 404 || got[0].Failure != "repo_not_visible_to_token" {
		t.Errorf("refused grant row = %+v, want failed/404/repo_not_visible_to_token", got[0])
	}
	if got[1].HTTPStatus != 0 || got[1].Failure != "no_response" {
		t.Errorf("unanswered grant row = %+v, want status 0 / no_response", got[1])
	}
	if got[2].Outcome != "invited" || got[2].HTTPStatus != 0 || got[2].Failure != "" || !got[2].Granted() {
		t.Errorf("successful grant row = %+v, want invited with no failure fields", got[2])
	}
	// Legacy rows (written before #2107) still parse, with empty failure fields.
	legacy := parseRepoGrants(`[{"Owner":"example-org","Repo":"r","Permission":"pull","Outcome":"failed","Source":"policy"}]`)
	if len(legacy) != 1 || legacy[0].Outcome != "failed" || legacy[0].Failure != "" {
		t.Errorf("legacy row did not parse: %+v", legacy)
	}
}
