package repoaccess

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// cannedGitHub answers every request with one status and body.
type cannedGitHub struct {
	status int
	body   string
}

func (c cannedGitHub) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: c.status, Body: io.NopCloser(strings.NewReader(c.body)), Header: http.Header{}}, nil
}

type downGitHub struct{}

func (downGitHub) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("dial tcp: connection refused")
}

// #2107: a refused grant keeps GitHub's status in a typed error, so the caller
// can persist WHY it failed without keeping the response body.
func TestAddCollaborator_refusalCarriesStatusAndClass(t *testing.T) {
	cases := []struct {
		status int
		class  string
	}{
		{http.StatusUnauthorized, "token_rejected"},
		{http.StatusForbidden, "token_forbidden"},
		{http.StatusNotFound, "repo_not_visible_to_token"},
		{http.StatusUnprocessableEntity, "request_rejected"},
		{http.StatusInternalServerError, "github_error"},
	}
	for _, tc := range cases {
		c := &http.Client{Transport: cannedGitHub{status: tc.status, body: `{"message":"fixture"}`}}
		outcome, err := AddCollaborator(context.Background(), c, "fixture-token", "example-org", "example-repo", "octocat", "pull")
		if outcome != OutcomeFailed || err == nil {
			t.Fatalf("HTTP %d: outcome %q err %v, want failed + error", tc.status, outcome, err)
		}
		var he *HTTPError
		if !errors.As(err, &he) || he.Status != tc.status {
			t.Fatalf("HTTP %d: error %v does not carry the status", tc.status, err)
		}
		if got := FailureClass(err); got != tc.class {
			t.Errorf("HTTP %d: class %q, want %q", tc.status, got, tc.class)
		}
		if strings.Contains(FailureClass(err), "fixture") {
			t.Errorf("HTTP %d: the class leaks the response body", tc.status)
		}
	}
}

func TestAddCollaborator_successAndTransportFailure(t *testing.T) {
	for status, want := range map[int]Outcome{http.StatusCreated: OutcomeInvited, http.StatusNoContent: OutcomeAdded} {
		c := &http.Client{Transport: cannedGitHub{status: status}}
		outcome, err := AddCollaborator(context.Background(), c, "t", "o", "r", "u", "pull")
		if err != nil || outcome != want {
			t.Fatalf("HTTP %d: outcome %q err %v, want %q", status, outcome, err, want)
		}
		if FailureClass(err) != "" {
			t.Errorf("HTTP %d: a success has a failure class", status)
		}
	}
	c := &http.Client{Transport: downGitHub{}}
	outcome, err := AddCollaborator(context.Background(), c, "t", "o", "r", "u", "pull")
	if outcome != OutcomeFailed || err == nil {
		t.Fatalf("transport failure: outcome %q err %v", outcome, err)
	}
	var he *HTTPError
	if errors.As(err, &he) {
		t.Errorf("transport failure reported an HTTP status %d", he.Status)
	}
	if got := FailureClass(err); got != "no_response" {
		t.Errorf("transport failure class %q, want no_response", got)
	}
}
