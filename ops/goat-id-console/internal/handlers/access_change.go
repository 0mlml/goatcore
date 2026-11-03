package handlers

// Role-change notification (docs/design/onboarding-notifications.md, the
// role-change increment). The console group patch is the in-band role-change
// lever (an escalation like developer→platform-admin, or a reduction). The
// person whose access changed now receives an email that (1) explicitly frames
// the change as an update to their EXISTING identity — nothing re-provisioned,
// already-done onboarding steps marked as such — and (2) still carries the
// complete role-appropriate reference (surfaces, CLI, fabric, admin sections),
// rendered by the same internal/welcome package as new-enrollee provisioning so
// the two paths can never drift. Best-effort by contract: a mail failure never
// fails the patch; every attempt lands in the audit log as access_change_email.

import (
	"errors"
	"net/http"
	"strings"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/kratos"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/welcome"
)

// tenantIdPKey is the goatnet participant realm — the only realm whose
// identities hold an onboarding package worth re-framing. Group flips on other
// realms (e.g. the obs-utility IdP) notify nobody.
const tenantIdPKey = "tenant"

// accessChangeRecipient resolves the welcome Recipient for a group patch:
// enriched from the person's onboarded proposal when one exists (name, mesh
// role, fabric access, github, repo grants), with the console-plane
// capabilities recomputed from the NEW group set — the change being announced.
// Pure; no I/O.
func accessChangeRecipient(before *kratos.Identity, newGroups []string, prop *onboardstore.Proposal) welcome.Recipient {
	var r welcome.Recipient
	if prop != nil {
		r = recipientFromProposal(prop)
	}
	if r.Name == "" {
		r.Name = before.Name()
	}
	r.Email = strings.TrimSpace(before.Email())
	admin := groupsGrantAdminConsole(newGroups)
	r.AdminConsole = admin
	r.AdminOperator = admin
	r.Tier = tierForGroups(newGroups, r.Tier)
	return r
}

// groupsGrantAdminConsole reports whether a group set admits the admin
// console: platform-admin (canonical or legacy name) or IAM-ADMIN — the same
// set the tier presets grant it to.
func groupsGrantAdminConsole(groups []string) bool {
	for _, g := range groups {
		switch strings.ToUpper(strings.TrimSpace(g)) {
		case platformAdminGroup, legacyAdminGroup, "IAM-ADMIN":
			return true
		}
	}
	return false
}

// tierForGroups maps the NEW Ory group set onto the tier label the reference
// presents. The admin groups name their tier directly; otherwise the onboarded
// tier stands — unless it was an escalated tier whose admin group was just
// removed, where keeping the label would overstate.
func tierForGroups(groups []string, onboardedTier string) string {
	for _, g := range groups {
		switch strings.ToUpper(strings.TrimSpace(g)) {
		case platformAdminGroup, legacyAdminGroup:
			return "platform-admin"
		}
	}
	for _, g := range groups {
		if strings.EqualFold(strings.TrimSpace(g), "IAM-ADMIN") {
			return "iam-admin"
		}
	}
	if onboardedTier == "iam-admin" || onboardedTier == "platform-admin" {
		return "participant"
	}
	return onboardedTier
}

// sendReferenceEmail emails an existing identity the complete current-role
// reference — the catch-up path for people whose access was set up or changed
// before the role-change emails existed (or who lost theirs). Same renderer as
// the upgrade email with an empty delta (→ "access reference" framing); no
// one-time secrets. Audited as access_reference_email either way.
func (s *Server) sendReferenceEmail(r *http.Request, idpKey string, id *kratos.Identity, actor Actor) error {
	if s.Mailer == nil || !s.Mailer.Enabled() {
		return errors.New("mailer is not configured")
	}
	if idpKey != tenantIdPKey {
		return errors.New("role reference emails exist for the tenant realm only")
	}
	email := strings.TrimSpace(id.Email())
	if email == "" {
		return errors.New("identity has no email")
	}
	groups := id.Groups()
	var prop *onboardstore.Proposal
	env := ""
	if s.Onboard != nil {
		if p, found, err := s.Onboard.ActiveProposalForEmail(r.Context(), email); err == nil && found && p.Status == onboardstore.ProposalOnboarded {
			prop, env = p, p.Env
		}
	}
	rcpt := accessChangeRecipient(id, groups, prop)
	pkg := welcome.RenderUpgradePackage(s.welcomeCatalog(env), rcpt, welcome.Upgrade{PrevGroups: groups, NewGroups: groups})
	goatnet := welcome.DisplayLabel(s.emailGoatnet(env), s.Cfg.NotifyGoatnetTagline)
	subject := "Your " + goatnet + " access — the complete reference for your role"
	outcome, errMsg := "ok", ""
	err := s.Mailer.SendMultipart(email, subject, pkg.Text, pkg.HTML)
	if err != nil {
		outcome, errMsg = "error", err.Error()
	}
	s.emitAudit(r, actor, "access_reference_email", id, idpKey,
		nil, map[string]interface{}{"groups": groups, "to": email}, false, outcome, errMsg)
	return err
}

// sendAccessChangeEmail emails the person whose groups an admin just patched.
// Fires only on an effective change on the tenant realm, mailer permitting.
func (s *Server) sendAccessChangeEmail(r *http.Request, idpKey string, before *kratos.Identity, newGroups []string) {
	if s.Mailer == nil || !s.Mailer.Enabled() || idpKey != tenantIdPKey || before == nil {
		return
	}
	email := strings.TrimSpace(before.Email())
	if email == "" {
		return
	}
	u := welcome.Upgrade{PrevGroups: before.Groups(), NewGroups: newGroups}
	if len(u.Added()) == 0 && len(u.Removed()) == 0 {
		return // no-op patch — nothing to announce
	}

	// Enrich from the person's onboarding record when one exists (best-effort;
	// the email degrades gracefully without it — no fabricated capabilities).
	var prop *onboardstore.Proposal
	env := ""
	if s.Onboard != nil {
		if p, found, err := s.Onboard.ActiveProposalForEmail(r.Context(), email); err == nil && found && p.Status == onboardstore.ProposalOnboarded {
			prop, env = p, p.Env
		}
	}
	rcpt := accessChangeRecipient(before, newGroups, prop)
	pkg := welcome.RenderUpgradePackage(s.welcomeCatalog(env), rcpt, u)

	goatnet := welcome.DisplayLabel(s.emailGoatnet(env), s.Cfg.NotifyGoatnetTagline)
	subject := "Your " + goatnet + " access changed — updated reference"
	if len(u.Added()) > 0 {
		subject = "Your " + goatnet + " access was upgraded — updated reference"
	}
	outcome, errMsg := "ok", ""
	if err := s.Mailer.SendMultipart(email, subject, pkg.Text, pkg.HTML); err != nil {
		outcome, errMsg = "error", err.Error()
	}
	s.emitAudit(r, Actor{Email: "console-notify"}, "access_change_email", before, idpKey,
		map[string]interface{}{"groups": before.Groups()},
		map[string]interface{}{"groups": newGroups, "to": email}, false, outcome, errMsg)
}
