package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeActionQueue is the console's onboard_actions contract in memory: the
// queued list, an exactly-once claim, and the result writeback.
type fakeActionQueue struct {
	mu           sync.Mutex
	actions      map[string]*action
	legacyClaims map[string]bool
	claims       []string
	results      map[string]actionResultBody
}

func newFakeActionQueue() *fakeActionQueue {
	return &fakeActionQueue{actions: map[string]*action{}, legacyClaims: map[string]bool{}, results: map[string]actionResultBody{}}
}

func (q *fakeActionQueue) add(a action) {
	q.mu.Lock()
	defer q.mu.Unlock()
	cp := a
	q.actions[a.ID] = &cp
}

// install mounts the action routes on top of the proposals fake so one server
// serves both halves of the runner's poll.
func (q *fakeActionQueue) install(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/onboard/actions", func(w http.ResponseWriter, r *http.Request) {
		status := r.URL.Query().Get("status")
		q.mu.Lock()
		out := []action{}
		for _, a := range q.actions {
			if status == "" || a.Status == status {
				out = append(out, *a)
			}
		}
		q.mu.Unlock()
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("POST /api/onboard/actions/{id}/claim", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		q.mu.Lock()
		defer q.mu.Unlock()
		a, ok := q.actions[id]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if a.Status != "queued" {
			w.WriteHeader(http.StatusConflict)
			return
		}
		a.Status = "claimed"
		if !q.legacyClaims[id] {
			a.ClaimToken = "claim-" + id
		}
		q.claims = append(q.claims, id)
		_ = json.NewEncoder(w).Encode(a)
	})
	mux.HandleFunc("POST /api/onboard/actions/{id}/result", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var body actionResultBody
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		q.mu.Lock()
		defer q.mu.Unlock()
		a, ok := q.actions[id]
		if !ok || a.Status != "claimed" || body.ClaimToken != a.ClaimToken {
			w.WriteHeader(http.StatusConflict)
			return
		}
		a.Status = body.Status
		q.results[id] = body
		_ = json.NewEncoder(w).Encode(map[string]string{"id": id, "status": body.Status})
	})
}

func testRunnerWithActions(t *testing.T, f *fakePortal, q *fakeActionQueue) *runner {
	t.Helper()
	mux := f.handler().(*http.ServeMux)
	q.install(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &runner{
		pc:       newPortalClient(srv.URL, "test-key", 5*time.Second),
		repoRoot: t.TempDir(),
		runnerID: "onboard-runner-test",
		log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

const reissueOutput = "  identity:     ok\n  setup key:    DEVICE-KEY  (key `peer-jane-zagros-2026-09-10`)\n  handset key:  NEW-HANDSET  (key `peer-jane-zagros-2026-09-10-handset`)\n"

func TestRunOnce_ReissueHandsetAction(t *testing.T) {
	f := newFakePortal()
	q := newFakeActionQueue()
	q.add(action{ID: "act-1", ProposalID: "prop-1", Kind: actionReissueHandset, Status: "queued", Handle: "jane", Env: "zagros"})
	r := testRunnerWithActions(t, f, q)

	var ran []action
	r.reissue = func(_ context.Context, a action) ([]byte, error) {
		ran = append(ran, a)
		return []byte(reissueOutput), nil
	}
	if err := r.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 1 || ran[0].Handle != "jane" || ran[0].Env != "zagros" {
		t.Fatalf("ran = %+v, want the jane/zagros action once", ran)
	}
	if len(q.claims) != 1 || q.claims[0] != "act-1" {
		t.Fatalf("claims = %v, want [act-1]", q.claims)
	}
	res := q.results["act-1"]
	if res.Status != "done" || res.HandsetKey != "NEW-HANDSET" || res.Error != "" {
		t.Fatalf("result = %+v, want done with NEW-HANDSET", res)
	}
	// A second poll finds nothing queued and runs nothing.
	if err := r.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 1 {
		t.Fatalf("second poll re-ran the action: %d runs", len(ran))
	}
}

func TestRunOnce_LegacyClaimWithoutTokenRunsReissue(t *testing.T) {
	f := newFakePortal()
	q := newFakeActionQueue()
	q.add(action{ID: "act-legacy", Kind: actionReissueHandset, Status: "queued", Handle: "jane", Env: "zagros"})
	q.legacyClaims["act-legacy"] = true
	r := testRunnerWithActions(t, f, q)
	ran := false
	r.reissue = func(context.Context, action) ([]byte, error) {
		ran = true
		return []byte(reissueOutput), nil
	}

	if err := r.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !ran || q.results["act-legacy"].Status != "done" {
		t.Fatalf("legacy reissue ran=%v result=%+v", ran, q.results["act-legacy"])
	}
}

func TestRunOnce_LegacyClaimWithoutTokenDoesNotRunRelease(t *testing.T) {
	f := newFakePortal()
	q := newFakeActionQueue()
	q.add(action{ID: "act-legacy-release", Kind: actionReleaseHold, Status: "queued", Env: "zagros"})
	q.legacyClaims["act-legacy-release"] = true
	r := testRunnerWithActions(t, f, q)
	r.onlyEnv = "zagros"
	r.release = func(context.Context, action) (releaseActionResult, error) {
		t.Fatal("release must not run without a claim token")
		return releaseActionResult{}, nil
	}

	if err := r.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if q.actions["act-legacy-release"].Status != "claimed" || len(q.results) != 0 {
		t.Fatalf("tokenless release status=%q results=%v", q.actions["act-legacy-release"].Status, q.results)
	}
}

func TestRunOnce_ReissueHandsetActionFailsWithoutKeyLine(t *testing.T) {
	f := newFakePortal()
	q := newFakeActionQueue()
	q.add(action{ID: "act-1", ProposalID: "prop-1", Kind: actionReissueHandset, Status: "queued", Handle: "jane", Env: "zagros"})
	q.add(action{ID: "act-2", ProposalID: "prop-2", Kind: actionReissueHandset, Status: "queued", Handle: "sam", Env: "zagros"})
	r := testRunnerWithActions(t, f, q)

	r.reissue = func(_ context.Context, a action) ([]byte, error) {
		if a.Handle == "jane" {
			return []byte("  identity:     ok\n"), nil // goat ran but printed no handset line
		}
		return []byte("warning: audit fallback\nerror: NetBird POST /api/setup-keys failed: 401\n"), errors.New("exit status 1")
	}
	if err := r.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if res := q.results["act-1"]; res.Status != "failed" || !strings.Contains(res.Error, "no `handset key:` line") || res.HandsetKey != "" {
		t.Fatalf("act-1 result = %+v, want failed for a missing key line", res)
	}
	if res := q.results["act-2"]; res.Status != "failed" || !strings.Contains(res.Error, "exit status 1") {
		t.Fatalf("act-2 result = %+v, want failed with the exec error", res)
	}
}

func TestRunOnce_ActionsSkipOtherEnvAndDryRun(t *testing.T) {
	f := newFakePortal()
	q := newFakeActionQueue()
	q.add(action{ID: "act-1", ProposalID: "prop-1", Kind: actionReissueHandset, Status: "queued", Handle: "jane", Env: "zagros-prod"})
	r := testRunnerWithActions(t, f, q)
	r.onlyEnv = "zagros"
	r.reissue = func(context.Context, action) ([]byte, error) {
		t.Fatal("must not run an action for another env")
		return nil, nil
	}
	if err := r.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(q.claims) != 0 {
		t.Fatalf("claimed an action for another env: %v", q.claims)
	}
	r.onlyEnv = ""
	r.dryRun = true
	if err := r.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(q.claims) != 0 {
		t.Fatalf("dry-run claimed an action: %v", q.claims)
	}
}

func TestRunOnce_ReissueDeviceActionWritesTheSetupKey(t *testing.T) {
	f := newFakePortal()
	q := newFakeActionQueue()
	q.add(action{ID: "act-1", ProposalID: "prop-1", Kind: actionReissueDevice, Status: "queued", Handle: "jane", Env: "zagros",
		RequestedBy: "maintainer@example.invalid", Params: `{"uses":5}`})
	q.add(action{ID: "act-2", ProposalID: "prop-2", Kind: actionReissueDevice, Status: "queued", Handle: "sam", Env: "zagros"})
	r := testRunnerWithActions(t, f, q)

	r.reissue = func(_ context.Context, a action) ([]byte, error) {
		if a.Handle == "sam" {
			// goat printed only a handset line: not what a device reissue produces.
			return []byte("  handset key:  H  (key `k-handset`)\n"), nil
		}
		// The exact line `goat admin onboard --reissue-device-key` prints.
		return []byte("reissued device key for `jane` on zagros: 5 use(s), 7 day(s) (1 old key(s) revoked)\n" +
			"  setup key:    NEW-DEVICE  (key `peer-jane-zagros-2026-09-22`)\n"), nil
	}
	if err := r.runOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if res := q.results["act-1"]; res.Status != "done" || res.SetupKey != "NEW-DEVICE" || res.HandsetKey != "" {
		t.Fatalf("act-1 result = %+v, want done with NEW-DEVICE only", res)
	}
	if res := q.results["act-2"]; res.Status != "failed" || !strings.Contains(res.Error, "no `setup key:` line") {
		t.Fatalf("act-2 result = %+v, want failed for a missing setup key line", res)
	}
}

func TestReissueArgsCarryUsesAndRequester(t *testing.T) {
	got := strings.Join(reissueArgs(action{Kind: actionReissueDevice, Handle: "jane", Env: "zagros",
		RequestedBy: "maintainer@example.invalid", Params: `{"uses":7}`}, "/repo"), " ")
	want := "admin onboard jane --native --env zagros --repo-root /repo --reissue-device-key --uses 7 --requested-by maintainer@example.invalid"
	if got != want {
		t.Fatalf("device args:\n got %s\nwant %s", got, want)
	}
	// No params: goat picks the default reuse count; no requester: no flag.
	got = strings.Join(reissueArgs(action{Kind: actionReissueDevice, Handle: "jane", Env: "zagros", Params: "{}"}, "/repo"), " ")
	if strings.Contains(got, "--uses") || strings.Contains(got, "--requested-by") {
		t.Fatalf("default device args carry optional flags: %s", got)
	}
	got = strings.Join(reissueArgs(action{Kind: actionReissueHandset, Handle: "jane", Env: "zagros", RequestedBy: "a@b"}, "/repo"), " ")
	if !strings.HasSuffix(got, "--reissue-handset-key --requested-by a@b") || strings.Contains(got, "--uses") {
		t.Fatalf("handset args: %s", got)
	}
}

func TestRunOnceReleaseHoldWritesStructuredOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name       string
		result     releaseActionResult
		runErr     error
		wantStatus string
		wantError  bool
	}{
		{"released", releaseActionResult{Outcome: "released", AuditOutcome: "recorded"}, nil, "done", false},
		{"already released", releaseActionResult{Outcome: "already_released", AuditOutcome: "recorded"}, nil, "done", false},
		{"terminal audit missing", releaseActionResult{Outcome: "released", AuditOutcome: "failed", AuditError: "bus unavailable"}, nil, "done", false},
		{"refused", releaseActionResult{Outcome: "refused", AuditOutcome: "recorded"}, errors.New("refused"), "failed", true},
		{"partial", releaseActionResult{Outcome: "retryable_partial", AuditOutcome: "recorded"}, errors.New("partial"), "failed", true},
		{"verification partial", releaseActionResult{Outcome: "retryable_partial", Detail: "postcondition verification failed", AuditOutcome: "recorded"}, nil, "failed", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakePortal()
			q := newFakeActionQueue()
			q.add(action{ID: "act-release", Kind: actionReleaseHold, Status: "queued", Env: "zagros", Params: `{}`})
			r := testRunnerWithActions(t, f, q)
			r.release = func(context.Context, action) (releaseActionResult, error) { return tc.result, tc.runErr }
			if err := r.runOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			got := q.results["act-release"]
			if got.Status != tc.wantStatus || got.Release == nil || got.Release.Outcome != tc.result.Outcome || got.Release.AuditOutcome != tc.result.AuditOutcome {
				t.Fatalf("result = %+v, want status %s release %+v", got, tc.wantStatus, tc.result)
			}
			if (got.Error != "") != tc.wantError {
				t.Fatalf("result error = %q, wantError=%v", got.Error, tc.wantError)
			}
		})
	}
}

func releaseVerificationServer(t *testing.T, groups ...string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/peers/peer-1" {
			http.NotFound(w, req)
			return
		}
		body := map[string]any{"groups": []map[string]string{{"name": "All"}}}
		for _, group := range groups {
			body["groups"] = append(body["groups"].([]map[string]string), map[string]string{"name": group})
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
}

func TestVerifyReleasePostconditions(t *testing.T) {
	for _, tc := range []struct {
		name       string
		prior      []string
		live       []string
		recordBody string
		wantErr    string
	}{
		{name: "one prior group", prior: []string{"ZAGROS-DEVELOPER"}, live: []string{"ZAGROS-DEVELOPER"}},
		{name: "zero prior groups", prior: nil, live: nil},
		{name: "several prior groups", prior: []string{"ZAGROS-DEVELOPER", "ZAGROS-P2P"}, live: []string{"ZAGROS-P2P", "ZAGROS-DEVELOPER"}},
		{name: "hold group remains", prior: []string{"ZAGROS-DEVELOPER"}, live: []string{"ZAGROS-DEVELOPER", "ZAGROS-PROBATION-DEVELOPER"}, wantErr: "want exactly"},
		{name: "prior group missing", prior: []string{"ZAGROS-DEVELOPER", "ZAGROS-P2P"}, live: []string{"ZAGROS-DEVELOPER"}, wantErr: "want exactly"},
		{name: "extra hold group", prior: []string{"ZAGROS-DEVELOPER"}, live: []string{"ZAGROS-DEVELOPER", "ZAGROS-QUARANTINED"}, wantErr: "want exactly"},
		{name: "record remains", prior: []string{"ZAGROS-DEVELOPER"}, live: []string{"ZAGROS-DEVELOPER"}, recordBody: `{"peer_id":"peer-1","entered_at":"2026-10-20T10:11:12Z"}` + "\n", wantErr: "exact hold record still exists"},
		{name: "malformed unrelated line is skipped", prior: []string{"ZAGROS-DEVELOPER"}, live: []string{"ZAGROS-DEVELOPER"}, recordBody: "not-json\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nb := releaseVerificationServer(t, tc.live...)
			defer nb.Close()
			records := filepath.Join(t.TempDir(), "hold-records.jsonl")
			if err := os.WriteFile(records, []byte(tc.recordBody), 0o600); err != nil {
				t.Fatal(err)
			}
			r := &runner{mgmtURL: nb.URL, holdRecordsPath: records, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
			p := releaseHoldParams{PeerID: "peer-1", HoldEnteredAt: "2026-10-20T10:11:12Z", PriorGroups: tc.prior}
			err := r.verifyRelease(context.Background(), p, "token")
			if tc.wantErr == "" && err != nil {
				t.Fatalf("verifyRelease: %v", err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("verifyRelease error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestExecReleaseHoldPassesConfiguredNBAPIAndWritesTerminalAudit(t *testing.T) {
	dir := t.TempDir()
	auditLog := filepath.Join(dir, "audit.log")
	goatBin := filepath.Join(dir, "goat")
	if err := os.WriteFile(goatBin, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$AUDIT_LOG\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	scriptDir := filepath.Join(dir, "ops", "scripts")
	// #nosec G301 -- test-only executable fixture directory contains no sensitive data.
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	envLog := filepath.Join(dir, "nb-api")
	lifecycle := "import os\nfrom pathlib import Path\nPath(" + fmt.Sprintf("%q", envLog) + ").write_text(os.environ.get('NB_API', ''))\nprint('1 peer(s) released, 0 failed.')\n"
	if err := os.WriteFile(filepath.Join(scriptDir, "lifecycle-sweep.py"), []byte(lifecycle), 0o755); err != nil {
		t.Fatal(err)
	}
	records := filepath.Join(dir, "hold-records.jsonl")
	if err := os.WriteFile(records, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	nb := releaseVerificationServer(t, "ZAGROS-DEVELOPER")
	defer nb.Close()
	t.Setenv("ZAGROS_NB_API_TOKEN", "test-token")
	t.Setenv("AUDIT_LOG", auditLog)
	r := &runner{repoRoot: dir, goatBin: goatBin, mgmtURL: nb.URL + "/", holdRecordsPath: records, onlyEnv: "zagros",
		log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	p := releaseHoldParams{Site: "zagros", PeerID: "peer-1", HoldEnteredAt: "2026-10-20T10:11:12Z",
		HoldGroup: "ZAGROS-PROBATION-DEVELOPER", PriorGroups: []string{"ZAGROS-DEVELOPER"}, RequestedByHandle: "admin-handle"}
	raw, _ := json.Marshal(p)
	got, err := r.execReleaseHold(context.Background(), action{Env: "zagros", Params: string(raw)})
	if err != nil || got.Outcome != "released" || got.AuditOutcome != "recorded" {
		t.Fatalf("result=%+v err=%v", got, err)
	}
	if b, err := os.ReadFile(envLog); err != nil || string(b) != nb.URL {
		t.Fatalf("NB_API capture=%q err=%v, want %q", b, err, nb.URL)
	}
	b, err := os.ReadFile(auditLog)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "--outcome started") ||
		!strings.Contains(lines[1], "--outcome ok") || !strings.Contains(lines[1], "release_outcome=released") {
		t.Fatalf("audit calls = %q, want started then terminal released", lines)
	}
}

func TestExecReleaseHoldReportsTerminalAuditFailureWithoutChangingReleaseOutcome(t *testing.T) {
	dir := t.TempDir()
	count := filepath.Join(dir, "audit-count")
	goatBin := filepath.Join(dir, "goat")
	goat := "#!/bin/sh\nif [ -f \"$AUDIT_COUNT\" ]; then echo terminal-audit-unavailable >&2; exit 1; fi\ntouch \"$AUDIT_COUNT\"\n"
	if err := os.WriteFile(goatBin, []byte(goat), 0o755); err != nil {
		t.Fatal(err)
	}
	scriptDir := filepath.Join(dir, "ops", "scripts")
	// #nosec G301 -- test-only executable fixture directory contains no sensitive data.
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptDir, "lifecycle-sweep.py"), []byte("print('1 peer(s) released, 0 failed.')\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	records := filepath.Join(dir, "hold-records.jsonl")
	if err := os.WriteFile(records, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	nb := releaseVerificationServer(t, "ZAGROS-DEVELOPER")
	defer nb.Close()
	t.Setenv("ZAGROS_NB_API_TOKEN", "test-token")
	t.Setenv("AUDIT_COUNT", count)
	r := &runner{repoRoot: dir, goatBin: goatBin, mgmtURL: nb.URL, holdRecordsPath: records, onlyEnv: "zagros",
		log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	p := releaseHoldParams{Site: "zagros", PeerID: "peer-1", HoldEnteredAt: "2026-10-20T10:11:12Z",
		HoldGroup: "ZAGROS-PROBATION-DEVELOPER", PriorGroups: []string{"ZAGROS-DEVELOPER"}, RequestedByHandle: "admin-handle"}
	raw, _ := json.Marshal(p)
	got, err := r.execReleaseHold(context.Background(), action{Env: "zagros", Params: string(raw)})
	if err != nil || got.Outcome != "released" || got.AuditOutcome != "failed" || !strings.Contains(got.AuditError, "terminal-audit-unavailable") {
		t.Fatalf("result=%+v err=%v, want released with terminal audit failure", got, err)
	}
}

func TestExecReleaseHoldWritesTerminalAuditWhenSiteTokenIsMissing(t *testing.T) {
	dir := t.TempDir()
	auditLog := filepath.Join(dir, "audit.log")
	goatBin := filepath.Join(dir, "goat")
	if err := os.WriteFile(goatBin, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$AUDIT_LOG\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AUDIT_LOG", auditLog)
	t.Setenv("ZAGROS_NB_API_TOKEN", "")
	r := &runner{repoRoot: dir, goatBin: goatBin, mgmtURL: "https://netbird.example.invalid", onlyEnv: "zagros", log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	p := releaseHoldParams{Site: "zagros", PeerID: "peer-1", HoldEnteredAt: "2026-10-20T10:11:12Z",
		HoldGroup: "ZAGROS-PROBATION-DEVELOPER", PriorGroups: []string{"ZAGROS-DEVELOPER"}, RequestedByHandle: "admin-handle"}
	raw, _ := json.Marshal(p)
	got, err := r.execReleaseHold(context.Background(), action{Env: "zagros", Params: string(raw)})
	if err == nil || got.Outcome != "refused" || got.AuditOutcome != "recorded" {
		t.Fatalf("result=%+v err=%v", got, err)
	}
	b, readErr := os.ReadFile(auditLog)
	if readErr != nil {
		t.Fatal(readErr)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "--outcome started") ||
		!strings.Contains(lines[1], "--outcome failed") || !strings.Contains(lines[1], "release_outcome=refused") ||
		!strings.Contains(lines[1], "ZAGROS_NB_API_TOKEN is unset") {
		t.Fatalf("audit calls = %q, want started then terminal refusal", lines)
	}
}

func TestExecReleaseHoldTerminalAuditCarriesVerificationFailure(t *testing.T) {
	dir := t.TempDir()
	auditLog := filepath.Join(dir, "audit.log")
	goatBin := filepath.Join(dir, "goat")
	if err := os.WriteFile(goatBin, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$AUDIT_LOG\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	scriptDir := filepath.Join(dir, "ops", "scripts")
	// #nosec G301 -- test-only executable fixture directory contains no sensitive data.
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptDir, "lifecycle-sweep.py"), []byte("print('1 peer(s) released, 0 failed.')\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	records := filepath.Join(dir, "hold-records.jsonl")
	if err := os.WriteFile(records, []byte(`{"peer_id":"peer-1","entered_at":"2026-10-20T10:11:12Z"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AUDIT_LOG", auditLog)
	t.Setenv("ZAGROS_NB_API_TOKEN", "test-token")
	r := &runner{repoRoot: dir, goatBin: goatBin, mgmtURL: "https://netbird.example.invalid", holdRecordsPath: records, onlyEnv: "zagros",
		log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	p := releaseHoldParams{Site: "zagros", PeerID: "peer-1", HoldEnteredAt: "2026-10-20T10:11:12Z",
		HoldGroup: "ZAGROS-PROBATION-DEVELOPER", PriorGroups: []string{"ZAGROS-DEVELOPER"}, RequestedByHandle: "admin-handle"}
	raw, _ := json.Marshal(p)
	got, err := r.execReleaseHold(context.Background(), action{Env: "zagros", Params: string(raw)})
	if err != nil || got.Outcome != "retryable_partial" || !strings.Contains(got.Detail, "exact hold record still exists") {
		t.Fatalf("result=%+v err=%v", got, err)
	}
	b, readErr := os.ReadFile(auditLog)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(b), "--outcome failed") ||
		!strings.Contains(string(b), "postcondition verification failed: exact hold record still exists") {
		t.Fatalf("terminal audit did not carry verification failure: %q", b)
	}
}

func TestExecReleaseHoldRefusesUnsetManagementURLBeforeAuditOrMutation(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	goatBin := filepath.Join(dir, "goat")
	if err := os.WriteFile(goatBin, []byte("#!/bin/sh\ntouch \"$MARKER\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MARKER", marker)
	r := &runner{repoRoot: dir, goatBin: goatBin, onlyEnv: "zagros", log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	p := releaseHoldParams{Site: "zagros", PeerID: "peer-1", HoldEnteredAt: "2026-10-20T10:11:12Z", RequestedByHandle: "admin-handle"}
	raw, _ := json.Marshal(p)
	got, err := r.execReleaseHold(context.Background(), action{Env: "zagros", Params: string(raw)})
	if err == nil || got.Outcome != "refused" || !strings.Contains(err.Error(), "before audit or mutation") {
		t.Fatalf("result=%+v err=%v", got, err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("a command ran despite missing management URL: %v", statErr)
	}
}

func TestExecReleaseHoldRefusesBeforeMutationWhenAuditCannotBeRecorded(t *testing.T) {
	dir := t.TempDir()
	goatBin := filepath.Join(dir, "goat")
	if err := os.WriteFile(goatBin, []byte("#!/bin/sh\necho audit-unavailable >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	scriptDir := filepath.Join(dir, "ops", "scripts")
	// #nosec G301 -- test-only executable fixture directory contains no sensitive data.
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "mutation-ran")
	lifecycle := "from pathlib import Path\nPath(" + fmt.Sprintf("%q", marker) + ").write_text('ran')\n"
	if err := os.WriteFile(filepath.Join(scriptDir, "lifecycle-sweep.py"), []byte(lifecycle), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZAGROS_NB_API_TOKEN", "test-token")
	r := &runner{repoRoot: dir, goatBin: goatBin, mgmtURL: "https://netbird.example.invalid", onlyEnv: "zagros", log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	params := releaseHoldParams{Site: "zagros", PeerID: "peer-1", HoldEnteredAt: "2026-10-20T10:11:12Z",
		HoldGroup: "ZAGROS-PROBATION-DEVELOPER", PriorGroups: []string{"ZAGROS-DEVELOPER"}, RequestedByHandle: "admin-handle"}
	raw, _ := json.Marshal(params)
	got, err := r.execReleaseHold(context.Background(), action{Env: "zagros", Params: string(raw)})
	if err == nil || got.Outcome != "refused" || got.AuditOutcome != "failed" || !strings.Contains(got.AuditError, "audit-unavailable") {
		t.Fatalf("result=%+v err=%v, want pre-mutation audit refusal", got, err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("lifecycle mutation ran despite failed audit: stat err=%v", statErr)
	}
}

func TestExecReleaseHoldRefusesRunnerWithoutMatchingGoatnetBeforeAudit(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "audit-ran")
	goatBin := filepath.Join(dir, "goat")
	if err := os.WriteFile(goatBin, []byte("#!/bin/sh\ntouch \"$MARKER\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MARKER", marker)
	p := releaseHoldParams{Site: "zagros", PeerID: "peer-1", HoldEnteredAt: "2026-10-20T10:11:12Z", RequestedByHandle: "admin-handle"}
	raw, _ := json.Marshal(p)
	for _, onlyEnv := range []string{"", "zagros-dev"} {
		r := &runner{repoRoot: dir, goatBin: goatBin, mgmtURL: "https://netbird.example.invalid", onlyEnv: onlyEnv, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
		got, err := r.execReleaseHold(context.Background(), action{Env: "zagros", Params: string(raw)})
		if err == nil || got.Outcome != "refused" || !strings.Contains(err.Error(), "not pinned") {
			t.Fatalf("onlyEnv=%q result=%+v err=%v", onlyEnv, got, err)
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("audit ran for an unpinned runner: %v", err)
	}
}

func TestClassifyReleaseOutputDoesNotUseExitStatusAlone(t *testing.T) {
	if got := classifyReleaseOutput("", nil); got != "retryable_partial" {
		t.Fatalf("empty successful process = %q, want retryable_partial", got)
	}
	if got := classifyReleaseOutput("1 peer(s) released, 0 failed.", nil); got != "released" {
		t.Fatalf("semantic released output = %q", got)
	}
	if got := classifyReleaseOutput("RELEASE FINISHED peer-one", nil); got != "released" {
		t.Fatalf("resumed release output = %q, want released", got)
	}
	if got := classifyReleaseOutput("STALE RECORD peer-one", nil); got != "already_released" {
		t.Fatalf("stale record output = %q, want already_released", got)
	}
	if got := classifyReleaseOutput("REFUSED exact hold changed", errors.New("exit 1")); got != "refused" {
		t.Fatalf("refusal output = %q", got)
	}
}
