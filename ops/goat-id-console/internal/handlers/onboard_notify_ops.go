package handlers

// Reviewer-facing onboarding notifications (onboarding-notifications.md
// increments 1-2, console-native). The candidate half of the blackout was
// closed by the status read on the invite link; this is the other half — the
// two transitions that create a WAITING HUMAN who is not looking at the queue:
//
//	candidate submits  → the SPONSOR must vouch      → mail the sponsor
//	sponsor vouches    → an ADMIN must provision     → mail the admin list
//
// plus the candidate's rejection mail, whose body existed in notify.go but was
// unreachable once the portal stopped driving transitions.
//
// Every send here is BEST-EFFORT by contract: the transition has already been
// recorded when we are called, so a mail failure must never turn a successful
// submit or vouch into an error. Each attempt is audited (ok | error), so a
// notification that did not reach a human is visible rather than silent.

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/welcome"
)

// mailableAddress reports whether a recorded sponsor value can be emailed. A
// participant sponsor's `sponsor` is a UUID org anchor, and machine mints
// record labels like "operator" / "onboard-api" — mailing either is impossible,
// so those proposals simply produce no sponsor notification.
func mailableAddress(v string) bool {
	v = strings.TrimSpace(v)
	return strings.Contains(v, "@") && !strings.ContainsAny(v, " \t")
}

// queueLink deep-links a reviewer at the stage their decision is waiting on.
// "" when the console does not know its own public base — the mail then names
// the stage in words rather than carrying a link to nowhere.
func (s *Server) queueLink(status string) string {
	base := consoleBase(s.Cfg)
	if base == "" {
		return ""
	}
	return base + "/onboarding?status=" + status
}

// sendOnboardNotice delivers one reviewer notification and audits the attempt.
// No recipients = nothing was owed to anyone, so nothing is audited; a disabled
// mailer IS audited, because someone was owed a message and did not get one.
func (s *Server) sendOnboardNotice(r *http.Request, action, handle string, to []string, subject, body string, after map[string]interface{}) {
	if len(to) == 0 {
		return
	}
	audit := func(recipient, outcome, errMsg string) {
		fields := map[string]interface{}{"to": recipient}
		for k, v := range after {
			fields[k] = v
		}
		s.emitAuditWithInviteToken(r, Actor{Email: "console-onboard"}, action,
			handle, "", nil, fields, false, outcome, errMsg)
	}
	if s.Mailer == nil || !s.Mailer.Enabled() {
		for _, addr := range to {
			audit(addr, "error", "mailer disabled — nobody was notified")
		}
		return
	}
	for _, addr := range to {
		if err := s.Mailer.Send(addr, subject, body); err != nil {
			audit(addr, "error", err.Error())
			continue
		}
		audit(addr, "ok", "")
	}
}

// sponsorSubmitEmail is the "your candidate submitted" body. Pure: the subject
// and body are built from values only, so the wording is unit-testable without
// a mailer.
func sponsorSubmitEmail(goatnet, sponsorName, candidateName, handle, candidateEmail string, escalated bool, link string) (subject, body string) {
	who := strings.TrimSpace(candidateName)
	if who == "" {
		who = handle
	}
	subject = "Vouch needed: " + who + " submitted their " + goatnet + " onboarding"
	hello := "Hello,"
	if n := strings.TrimSpace(sponsorName); n != "" && !strings.Contains(n, "@") {
		hello = "Hello " + n + ","
	}
	b := hello + "\r\n\r\n" +
		who + " submitted an onboarding request against an invite you issued, so it " +
		"is now waiting on YOUR vouch. Nothing is provisioned until you vouch and an " +
		"administrator then approves.\r\n\r\n" +
		"    candidate:  " + who + "\r\n" +
		"    handle:     " + handle + "\r\n" +
		"    email:      " + strings.TrimSpace(candidateEmail) + "\r\n" +
		"    goatnet:    " + goatnet + "\r\n"
	if escalated {
		b += "    role:       ESCALATED — an admin-tier role you offered on the invite\r\n"
	}
	b += "\r\n"
	if link != "" {
		b += "Review and vouch here (you'll need to be on the mesh):\r\n\r\n" +
			"    " + link + "\r\n\r\n"
	} else {
		b += "Review it on the onboarding console, under \"Awaiting sponsor\".\r\n\r\n"
	}
	b += "Vouching says you know this person and the access they asked for is " +
		"appropriate. If you do not recognize them, decline — a declined candidate " +
		"can be re-invited, a wrongly-vouched one has to be revoked.\r\n" +
		"\r\nThis is an automated notification about an onboarding you sponsored.\r\n"
	return subject, b
}

// adminVouchedEmail is the "ready for provisioning" body sent to the admin list.
func adminVouchedEmail(goatnet, candidateName, handle, candidateEmail, vouchedBy string, escalated bool, link string) (subject, body string) {
	who := strings.TrimSpace(candidateName)
	if who == "" {
		who = handle
	}
	subject = "Ready for admin approval: " + who + " (" + goatnet + ")"
	if escalated {
		subject = "Ready for admin approval: " + who + " (" + goatnet + ", ESCALATED)"
	}
	b := "Hello,\r\n\r\n" +
		"A sponsor has vouched for " + who + ". The onboarding is now at the ADMIN " +
		"gate — approving it provisions mesh, login and fabric access.\r\n\r\n" +
		"    candidate:  " + who + "\r\n" +
		"    handle:     " + handle + "\r\n" +
		"    email:      " + strings.TrimSpace(candidateEmail) + "\r\n" +
		"    goatnet:    " + goatnet + "\r\n"
	if v := strings.TrimSpace(vouchedBy); v != "" {
		b += "    vouched by: " + v + "\r\n"
	}
	if escalated {
		b += "    role:       ESCALATED — read the requested authorities before approving\r\n"
	}
	b += "\r\n"
	if link != "" {
		b += "Review and approve here:\r\n\r\n    " + link + "\r\n\r\n"
	} else {
		b += "Review it on the onboarding console, under \"Awaiting admin\".\r\n\r\n"
	}
	b += "This is an automated notification for the onboarding admin queue.\r\n"
	return subject, b
}

// notifySponsorSubmitted tells the sponsor a candidate is waiting on their
// vouch. Called from the submit handler after the proposal exists.
func (s *Server) notifySponsorSubmitted(r *http.Request, p *onboardstore.Proposal) {
	if p == nil || !mailableAddress(p.Sponsor) {
		return
	}
	goatnet := welcome.DisplayLabel(s.emailGoatnet(p.Env), s.Cfg.NotifyGoatnetTagline)
	subject, body := sponsorSubmitEmail(goatnet, p.SponsorName, candidateNameOf(p), p.Handle,
		p.Email, p.Escalated, s.queueLink(onboardstore.ProposalProposed))
	s.sendOnboardNotice(r, "onboard_email_sponsor_submit", p.Handle, []string{p.Sponsor},
		subject, body, map[string]interface{}{"env": p.Env, "proposal_id": p.ID})
}

// notifyAdminVouched tells the admin list a proposal cleared the sponsor gate.
// Empty ONBOARD_ADMIN_NOTIFY_EMAILS = feature off (sendOnboardNotice returns on
// an empty recipient list).
func (s *Server) notifyAdminVouched(r *http.Request, p *onboardstore.Proposal, vouchedBy string) {
	if p == nil {
		return
	}
	to := make([]string, 0, len(s.Cfg.OnboardAdminNotifyEmails))
	for _, addr := range s.Cfg.OnboardAdminNotifyEmails {
		if mailableAddress(addr) {
			to = append(to, strings.TrimSpace(addr))
		}
	}
	goatnet := welcome.DisplayLabel(s.emailGoatnet(p.Env), s.Cfg.NotifyGoatnetTagline)
	subject, body := adminVouchedEmail(goatnet, candidateNameOf(p), p.Handle, p.Email, vouchedBy,
		p.Escalated, s.queueLink(onboardstore.ProposalSponsorApproved))
	s.sendOnboardNotice(r, "onboard_email_admin_vouched", p.Handle, to,
		subject, body, map[string]interface{}{"env": p.Env, "proposal_id": p.ID})
}

// notifyCandidateRejected sends the candidate the rejection body from notify.go
// — written for increment 3, and unreachable since the console took over the
// transitions. A rejected candidate whose only other signal is the status page
// they may never revisit otherwise waits forever.
func (s *Server) notifyCandidateRejected(r *http.Request, p *onboardstore.Proposal, note string) {
	if p == nil || !mailableAddress(p.Email) {
		return
	}
	subject, body, ok := s.candidateEmail(notifyRequest{
		Event: "rejected", Email: p.Email, Name: candidateNameOf(p),
		Env: s.emailGoatnet(p.Env), Handle: p.Handle, Reason: note,
	})
	if !ok {
		return
	}
	s.sendOnboardNotice(r, "onboard_email_rejected", p.Handle, []string{p.Email},
		subject, body, map[string]interface{}{"env": p.Env, "proposal_id": p.ID})
}

// candidateNameOf reads the person's name off the stored manifest, falling back
// to the handle. The proposal row itself carries no name column.
func candidateNameOf(p *onboardstore.Proposal) string {
	var m welcomeManifest
	if err := json.Unmarshal([]byte(p.ManifestJSON), &m); err == nil {
		if n := strings.TrimSpace(m.Identity.Name); n != "" {
			return n
		}
	}
	return p.Handle
}
