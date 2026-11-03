package handlers

// Operator-key decision notifications.
//
// When two platform admins each request a key and approve the OTHER's, both keys
// go live within seconds and neither person is told, because approving is the
// only event the console surfaces and each of them only sees the other's.
//
// As in onboard_notify_ops_test.go, the console under test has no SMTP, so a
// send lands as an audited attempt rather than a message. What is asserted is
// WHETHER a notification was owed and TO WHOM — delivery is the mailer's
// concern — plus the claims the body makes, because a body that misdescribes a
// credential is how somebody ends up waiting for a step that does not exist.

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/config"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

func notifyTestServer(t *testing.T) *Server {
	t.Helper()
	s := newOnboardTestServer(t)
	s.Cfg.DevBypassAuth = false
	s.Cfg.NotifyGoatnet = "zagros-prod"
	s.Cfg.ConsolePublicURL = "https://admin.auth.zagros-prod.examplebird.zagros-prod.example/"
	// The intake front desk, set deliberately to the wrong-for-this-purpose
	// value: it does not serve /operator-key, so a link built from it is dead.
	s.Cfg.PortalPublicURL = "https://intake.zagros-prod.example"
	return s
}

// The whole point: approving tells the person waiting.
func TestOperatorKeyApproveNotifiesRequester(t *testing.T) {
	s := notifyTestServer(t)
	h := s.Routes()
	row := seedOperatorKeyRequest(t, s, "handle-a", "waiting@example.org")

	form := url.Values{"csrf": {s.csrfNonce}}
	if w := browserForm(t, h, "other-admin@example.org", "ADMINS",
		"/operator-keys/"+row.ID+"/approve", form); w.Code != http.StatusOK {
		t.Fatalf("approve = %d; %.300s", w.Code, w.Body.String())
	}
	if got := noticeTo(s, "operator_key_email_approved"); len(got) != 1 || got[0] != "waiting@example.org" {
		t.Fatalf("approved notice recipients = %v, want [waiting@example.org]", got)
	}
}

// The machine door leaves the same person waiting — more so, because nobody is
// looking at a queue page there at all. `goat admin operator-key approve` must
// notify exactly as the console does.
func TestOperatorKeyApproveViaAPINotifiesRequester(t *testing.T) {
	s := notifyTestServer(t)
	h := s.Routes()
	row := seedOperatorKeyRequest(t, s, "handle-b", "waiting-b@example.org")

	if w := machineReq(t, h, "runner-key", "POST",
		"/api/onboard/operator-keys/"+row.ID+"/approve", ""); w.Code != http.StatusOK {
		t.Fatalf("api approve = %d; %.300s", w.Code, w.Body.String())
	}
	if got := noticeTo(s, "operator_key_email_approved"); len(got) != 1 || got[0] != "waiting-b@example.org" {
		t.Fatalf("approved notice recipients = %v, want [waiting-b@example.org]", got)
	}
}

// A row filed through the machine door may carry only a handle (--email is
// optional). Falling back to the onboarding record is what stops those people
// being the silent case all over again.
func TestOperatorKeyApproveResolvesEmailFromOnboardingRecord(t *testing.T) {
	s := notifyTestServer(t)
	h := s.Routes()
	// seedProposal derives handle cand-<id> and email <id>@example.org.
	seedProposal(t, s, "recorded", "onboard-runner", "platform-admin", onboardstore.ProposalOnboarded)
	row := seedOperatorKeyRequest(t, s, "cand-recorded", "")

	if w := machineReq(t, h, "runner-key", "POST",
		"/api/onboard/operator-keys/"+row.ID+"/approve", ""); w.Code != http.StatusOK {
		t.Fatalf("api approve = %d; %.300s", w.Code, w.Body.String())
	}
	if got := noticeTo(s, "operator_key_email_approved"); len(got) != 1 || got[0] != "recorded@example.org" {
		t.Fatalf("recipients = %v, want [recorded@example.org] resolved from the onboarding record", got)
	}
}

// No address anywhere: nobody is owed a message, so nothing is audited as one.
// The audit must not carry a row implying somebody was told.
func TestOperatorKeyApproveWithNoAddressNoAudit(t *testing.T) {
	s := notifyTestServer(t)
	h := s.Routes()
	row := seedOperatorKeyRequest(t, s, "no-address-anywhere", "")

	if w := machineReq(t, h, "runner-key", "POST",
		"/api/onboard/operator-keys/"+row.ID+"/approve", ""); w.Code != http.StatusOK {
		t.Fatalf("api approve = %d", w.Code)
	}
	if got := noticeTo(s, "operator_key_email_approved"); len(got) != 0 {
		t.Fatalf("recipients = %v, want none", got)
	}
	// The approval itself still happened — the notification is best-effort and
	// must never gate the grant.
	if cur, _ := s.Onboard.OperatorKey(context.Background(), row.ID); cur.Status != onboardstore.OperatorKeyActive {
		t.Fatalf("row = %q, want active: a missing address must not block a grant", cur.Status)
	}
}

// A declined requester waits exactly as long as an approved one, and cannot
// re-request while the row stands.
func TestOperatorKeyRejectNotifiesRequesterWithTheNote(t *testing.T) {
	s := notifyTestServer(t)
	h := s.Routes()
	row := seedOperatorKeyRequest(t, s, "handle-c", "declined@example.org")

	form := url.Values{"csrf": {s.csrfNonce}, "note": {"use the shared runner for this"}}
	if w := browserForm(t, h, "other-admin@example.org", "ADMINS",
		"/operator-keys/"+row.ID+"/reject", form); w.Code != http.StatusOK {
		t.Fatalf("reject = %d; %.300s", w.Code, w.Body.String())
	}
	if got := noticeTo(s, "operator_key_email_rejected"); len(got) != 1 || got[0] != "declined@example.org" {
		t.Fatalf("rejected notice recipients = %v, want [declined@example.org]", got)
	}
	_, body := operatorKeyRejectedEmail("ZAGROS-PROD", "handle-c", "other-admin@example.org",
		"use the shared runner for this", "https://console/operator-key")
	if !strings.Contains(body, "use the shared runner for this") {
		t.Error("the reviewer's note must reach the person it was written for")
	}
	if !strings.Contains(body, "Nothing was minted") {
		t.Error("a decline must say plainly that no credential exists")
	}
}

// Revoking is de-escalation and already visible to the person it affects (the
// key stops working). It is deliberately NOT mailed; asserting that keeps a
// later "notify on every transition" edit from turning routine hygiene into
// mail nobody reads.
func TestOperatorKeyRevokeIsNotMailed(t *testing.T) {
	s := notifyTestServer(t)
	h := s.Routes()
	row := seedOperatorKeyRequest(t, s, "handle-d", "holder@example.org")
	form := url.Values{"csrf": {s.csrfNonce}}
	if w := browserForm(t, h, "other-admin@example.org", "ADMINS",
		"/operator-keys/"+row.ID+"/approve", form); w.Code != http.StatusOK {
		t.Fatalf("approve = %d", w.Code)
	}
	if w := browserForm(t, h, "other-admin@example.org", "ADMINS",
		"/operator-keys/"+row.ID+"/revoke", form); w.Code != http.StatusOK {
		t.Fatalf("revoke = %d", w.Code)
	}
	if got := noticeTo(s, "operator_key_email_revoked"); len(got) != 0 {
		t.Fatalf("revoke mailed %v; revocation is deliberately not notified", got)
	}
}

// The body is the fix. It has to move somebody who believes they are waiting
// on a queue back to a page, so it must lead with the collect step, and it must
// not describe an install step that does not exist.
func TestOperatorKeyApprovedEmailBody(t *testing.T) {
	s := notifyTestServer(t)
	subj, body := operatorKeyApprovedEmail("ZAGROS-PROD (production data fabric)",
		"user363x", "second-admin@example.org", "sha256:bb06854a5bf4847f", s.operatorKeyLink())

	if !strings.Contains(subj, "collect") {
		t.Errorf("subject must name the outstanding action: %q", subj)
	}
	for _, must := range []string{
		"already live",                // approval IS the grant
		"no install step",             // the SSH-key flow's shape does not apply here
		"COLLECT the key",             // the one thing only they can do
		"shown ONCE",                  // the irreversible bit
		"revoke it on that same page", // the way out of a lost key
		"SECOND credential",           // it does not replace the vp_ key
		"user363x",                 // whose key this is
		"sha256:bb06854a5bf4847f",     // identifies it without being it
	} {
		if !strings.Contains(body, must) {
			t.Errorf("missing %q in:\n%s", must, body)
		}
	}
	// The link must be the CONSOLE, which serves /operator-key — not the
	// intake front desk, which 404s.
	if !strings.Contains(body, "https://admin.auth.zagros-prod.examplebird.zagros-prod.example/operator-key") {
		t.Errorf("collect link must be console base + /operator-key; got:\n%s", body)
	}
	if strings.Contains(body, "intake.zagros-prod.example") {
		t.Error("must never build the collect link from the intake front desk")
	}
	// The failure this whole change exists to end: telling somebody to wait.
	for _, forbidden := range []string{"wait for", "will be installed", "restart"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(forbidden)) &&
			!strings.Contains(body, "no restart to wait for") {
			t.Errorf("body implies a further wait via %q:\n%s", forbidden, body)
		}
	}
}

// No configured console base: name the page in words rather than ship a link
// to nowhere.
func TestOperatorKeyLinkFallsBackToWords(t *testing.T) {
	s := &Server{Cfg: &config.Config{NotifyGoatnet: "zagros"}}
	if got := s.operatorKeyLink(); got != "" {
		t.Fatalf("operatorKeyLink with no base = %q, want empty", got)
	}
	_, body := operatorKeyApprovedEmail("ZAGROS", "someone", "", "", "")
	if !strings.Contains(body, "page /operator-key") {
		t.Errorf("with no base the body must name the page in words:\n%s", body)
	}
	if strings.Contains(body, "http") {
		t.Errorf("with no base the body must carry no link:\n%s", body)
	}
}
