package handlers

// Operator-key decision notifications.
//
// Approving an operator key IS the grant — the file lands and the portal
// re-reads its key directory within seconds — but the key still has to be
// COLLECTED, once, from the page the requester filed it on. Without a
// notification the flow reads as "submit and wait" and the waiting never ends.
//
// Mutual approval is the sharp edge — an approver watches their own decision
// land on the queue, so the only person with no signal is the one waiting, and
// two mutual approvals leave BOTH people with no signal.
//
// Same contract as the onboarding reviewer mails in onboard_notify_ops.go:
// sent AFTER the decision is recorded and the key file written, best-effort,
// and audited either way so a notification that did not reach a human is
// visible rather than silent. A mail failure must never fail a decision that
// has already granted or withheld access.

import (
	"context"
	"strings"

	"net/http"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/welcome"
)

// operatorKeyLink deep-links the requester at the page they collect from. ""
// when the console does not know its own public base — the mail then names the
// page in words rather than carrying a link to nowhere, the same way queueLink
// does for the onboarding queue.
func (s *Server) operatorKeyLink() string {
	base := consoleBase(s.Cfg)
	if base == "" {
		return ""
	}
	return base + "/operator-key"
}

// operatorKeyRecipient resolves who to tell about a decision.
//
// A row filed from the console self page always carries the requester's email.
// One filed through the machine door may carry only a handle — `--email` is
// optional on `goat admin operator-key request` — so fall back to the
// onboarding record, the same handle→email lookup the two-person rule uses.
// No resolvable address means nobody is mailed and nothing is audited as sent:
// there is no one to owe a message to.
func (s *Server) operatorKeyRecipient(ctx context.Context, k *onboardstore.OperatorKey) string {
	if k == nil {
		return ""
	}
	if mailableAddress(k.Email) {
		return strings.TrimSpace(k.Email)
	}
	addr, err := s.Onboard.ProposalEmailForHandle(ctx, k.Handle)
	if err != nil || !mailableAddress(addr) {
		return ""
	}
	return strings.TrimSpace(addr)
}

// operatorKeyApprovedEmail is the "it is live, come and get it" body. Pure:
// built from values only, so the wording is testable without a mailer.
//
// The body has one job — get the person back to the page while the secret is
// still collectible — so the collect step leads and everything else follows it.
func operatorKeyApprovedEmail(goatnet, handle, decidedBy, fingerprint, link string) (subject, body string) {
	subject = "Your " + goatnet + " operator key is approved — collect it"
	b := "Hello,\r\n\r\n" +
		"Your operator key on " + goatnet + " has been approved, and it is already " +
		"live: the Portal has reloaded its key directory and accepts the key now. " +
		"There is no install step and no restart to wait for.\r\n\r\n" +
		"One thing is left, and only you can do it — COLLECT the key:\r\n\r\n"
	if link != "" {
		b += "    " + link + "\r\n\r\n"
	} else {
		b += "    your goatnet's identity console, page /operator-key\r\n\r\n"
	}
	b += "    handle:      " + handle + "\r\n" +
		"    goatnet:     " + goatnet + "\r\n"
	if d := strings.TrimSpace(decidedBy); d != "" {
		b += "    approved by: " + d + "\r\n"
	}
	if f := strings.TrimSpace(fingerprint); f != "" {
		b += "    fingerprint: " + f + "\r\n"
	}
	b += "\r\nThe key is shown ONCE and cannot be shown again. Copy it before you " +
		"leave the page and keep it where you keep your other secrets. If you lose " +
		"it, revoke it on that same page and request a new one — that is cheap, and " +
		"a key nobody holds is worse than no key, because it still works and nobody " +
		"can tell who is using it.\r\n\r\n" +
		"This is a SECOND credential. It does not replace the scoped vp_ key that " +
		"signs you into the Portal; the op_ key is what attributes your admin " +
		"actions to you by name.\r\n\r\n" +
		"This is an automated notification about an operator key you requested.\r\n"
	return subject, b
}

// operatorKeyRejectedEmail is the other half of the same blackout. A declined
// requester who is never told waits exactly as long as an approved one — and
// re-requesting is blocked while the row stands, so silence here strands them
// with no way forward they can see.
func operatorKeyRejectedEmail(goatnet, handle, decidedBy, note, link string) (subject, body string) {
	subject = "Your " + goatnet + " operator key request was declined"
	b := "Hello,\r\n\r\n" +
		"Your request for an operator key on " + goatnet + " was declined. Nothing " +
		"was minted and nothing about your existing access changed.\r\n\r\n" +
		"    handle:      " + handle + "\r\n" +
		"    goatnet:     " + goatnet + "\r\n"
	if d := strings.TrimSpace(decidedBy); d != "" {
		b += "    declined by: " + d + "\r\n"
	}
	b += "\r\n"
	if n := strings.TrimSpace(note); n != "" {
		b += "Reason given:\r\n\r\n    " + n + "\r\n\r\n"
	} else {
		b += "No reason was recorded. Ask the admin who decided it before " +
			"requesting again — a second identical request usually gets the same " +
			"answer.\r\n\r\n"
	}
	if link != "" {
		b += "You can file a new request here once you have:\r\n\r\n    " + link + "\r\n\r\n"
	}
	b += "This is an automated notification about an operator key you requested.\r\n"
	return subject, b
}

// notifyOperatorKeyApproved tells the requester their key is live and waiting
// to be collected. Called from BOTH decision doors — the console queue and the
// machine API — after the key file is on disk, so the mail never claims a
// grant that was rolled back.
func (s *Server) notifyOperatorKeyApproved(r *http.Request, k *onboardstore.OperatorKey) {
	if k == nil {
		return
	}
	to := s.operatorKeyRecipient(r.Context(), k)
	if to == "" {
		return
	}
	goatnet := welcome.DisplayLabel(s.emailGoatnet(k.Env), s.Cfg.NotifyGoatnetTagline)
	subject, body := operatorKeyApprovedEmail(goatnet, k.Handle, k.DecidedBy, k.Fingerprint,
		s.operatorKeyLink())
	s.sendOnboardNotice(r, "operator_key_email_approved", k.Handle, []string{to},
		subject, body, map[string]interface{}{"env": k.Env, "operator_key_id": k.ID})
}

// notifyOperatorKeyRejected tells the requester their request was declined.
func (s *Server) notifyOperatorKeyRejected(r *http.Request, k *onboardstore.OperatorKey, note string) {
	if k == nil {
		return
	}
	to := s.operatorKeyRecipient(r.Context(), k)
	if to == "" {
		return
	}
	goatnet := welcome.DisplayLabel(s.emailGoatnet(k.Env), s.Cfg.NotifyGoatnetTagline)
	if strings.TrimSpace(note) == "" {
		note = k.Note
	}
	subject, body := operatorKeyRejectedEmail(goatnet, k.Handle, k.DecidedBy, note,
		s.operatorKeyLink())
	s.sendOnboardNotice(r, "operator_key_email_rejected", k.Handle, []string{to},
		subject, body, map[string]interface{}{"env": k.Env, "operator_key_id": k.ID})
}
