package handlers

// Candidate stage-notification intake (onboarding-notifications.md increment
// 3). The portal fires a best-effort POST here on each onboarding transition;
// the console — which owns the email channel (SES mailer) — renders the
// candidate-facing message and sends it. Server-to-server: the portal can't
// present oauth2-proxy group headers, so this route authenticates with the
// shared NOTIFY_TOKEN bearer instead of the admin gate. Everything is
// best-effort by contract — the portal never blocks a transition on us, and
// we never fail one; failures land in the audit log.

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/config"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/welcome"
)

// notifyRequest is the portal's payload — identity + event only. The
// candidate-facing links (mgmt / downloads / portal) are console config, so
// the sender of record owns the words and the URLs.
type notifyRequest struct {
	Event string `json:"event"` // proposed | sponsor_approved | rejected | onboarded
	//                                | ssh_key_needed | ssh_key_installed | ssh_shell_by_identity
	Email     string `json:"email"`
	Name      string `json:"name,omitempty"`
	Env       string `json:"env,omitempty"`
	Handle    string `json:"handle,omitempty"`
	Escalated bool   `json:"escalated,omitempty"`
	Reason    string `json:"reason,omitempty"`    // reviewer note on a rejection
	SetupKey  string `json:"setup_key,omitempty"` // onboarded only — the mesh enrollment key
	// Hosts is the ssh_key_installed list of host:user pairs the key actually
	// landed on. Rendered verbatim: a person should be told WHERE their shell
	// works, not left to discover it.
	Hosts []string `json:"hosts,omitempty"`
}

// handleNotify accepts one stage notification and emails the candidate.
func (s *Server) handleNotify(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(s.Cfg.NotifyToken)
	if token == "" {
		http.NotFound(w, r) // feature off — indistinguishable from absent
		return
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(got)), []byte(token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req notifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Email) == "" {
		http.Error(w, "missing email", http.StatusBadRequest)
		return
	}
	subject, body, ok := s.candidateEmail(req)
	if !ok {
		http.Error(w, "unknown event", http.StatusBadRequest)
		return
	}
	if !s.Mailer.Enabled() {
		// Accepted but undeliverable — honest 503 so the portal's warn-log
		// says why, and the audit trail records the miss.
		s.emitNotifyAudit(r, req, "error", "mailer disabled")
		http.Error(w, "mailer disabled", http.StatusServiceUnavailable)
		return
	}
	if err := s.Mailer.Send(req.Email, subject, body); err != nil {
		s.emitNotifyAudit(r, req, "error", err.Error())
		http.Error(w, "send failed", http.StatusBadGateway)
		return
	}
	s.emitNotifyAudit(r, req, "ok", "")
	w.WriteHeader(http.StatusAccepted)
}

// candidateEmail renders the subject + plaintext body for one stage event.
// Steps remaining derive from the state machine (proposed=2, sponsor
// approved=1), never hardcoded per-template.
func (s *Server) candidateEmail(req notifyRequest) (subject, body string, ok bool) {
	goatnet := req.Env
	if goatnet == "" {
		goatnet = "the data fabric"
	}
	hello := "Hello,"
	if strings.TrimSpace(req.Name) != "" {
		hello = "Hello " + strings.TrimSpace(req.Name) + ","
	}
	foot := "\r\nThis is an automated status update about your onboarding. " +
		"If you weren't expecting it, you can ignore it.\r\n"

	switch req.Event {
	case "proposed":
		subject = "Your " + goatnet + " onboarding was submitted"
		body = hello + "\r\n\r\n" +
			"Your onboarding details were received. Your submission is now with " +
			"your sponsor for review and vouching.\r\n\r\n" +
			"2 approvals remain: your sponsor's, then an administrator's. " +
			"You'll get an email at each step.\r\n" + foot
		return subject, body, true
	case "sponsor_approved":
		subject = "Your sponsor vouched for you — 1 approval remains"
		body = hello + "\r\n\r\n" +
			"Your sponsor reviewed and vouched for your " + goatnet + " onboarding. " +
			"It is now with an administrator for final approval and provisioning.\r\n\r\n" +
			"1 approval remains. You'll get your enrollment instructions by email " +
			"once it completes.\r\n" + foot
		return subject, body, true
	case "rejected":
		subject = "Your " + goatnet + " onboarding was not approved"
		body = hello + "\r\n\r\n" +
			"Your " + goatnet + " onboarding was not approved." +
			func() string {
				if strings.TrimSpace(req.Reason) != "" {
					return " Reviewer note:\r\n\r\n    " + strings.TrimSpace(req.Reason)
				}
				return ""
			}() + "\r\n\r\n" +
			"If you believe this is a mistake, contact your sponsor — they can " +
			"issue a fresh invite.\r\n" + foot
		return subject, body, true
	// --- host-shell access (docs/design/ssh-key-self-service.md) -------------
	// These say plainly what a person HAS and what they must DO. The failure
	// this whole track exists to fix was a welcome email asserting access that
	// did not exist, so none of these three claims a shell that is not there.
	case "ssh_key_needed":
		subject = "Action needed: your SSH public key for " + goatnet
		b := hello + "\r\n\r\n" +
			"You are a platform admin on " + goatnet + ", which means you should have a " +
			"shell on the management hosts. You do not yet, because we have no SSH " +
			"public key for you.\r\n\r\n"
		if u := consoleBase(s.Cfg); u != "" {
			b += "Submit one yourself (you'll need to be on the mesh):\r\n\r\n" +
				"    " + strings.TrimRight(u, "/") + "/ssh-key\r\n\r\n"
		} else {
			b += "Send it to an administrator — they can submit it for you.\r\n\r\n"
		}
		b += "Paste the contents of your PUBLIC key file, usually " +
			"~/.ssh/id_ed25519.pub. It is one line starting ssh-ed25519. If you " +
			"do not have one yet, run:\r\n\r\n" +
			"    ssh-keygen -t ed25519\r\n\r\n" +
			"and submit the .pub file it creates.\r\n\r\n" +
			"NEVER send the file beginning -----BEGIN OPENSSH PRIVATE KEY-----. " +
			"That is the secret itself. The form refuses it, but do not paste it " +
			"anywhere else either.\r\n\r\n" +
			"Submitting does NOT grant access on its own. An admin reviews the key, " +
			"and an installer then places it on the hosts your role can reach. The " +
			"page shows which stage yours is at; you will not be able to log in " +
			"until it says installed. That gap is deliberate — the console is " +
			"reachable with an ordinary login, so if uploading a key authorized it, " +
			"a stolen browser session would become a root-capable shell.\r\n" + foot
		return subject, b, true

	case "ssh_key_installed":
		subject = "Your " + goatnet + " host shell is live"
		b := hello + "\r\n\r\n" +
			"Your SSH key is installed, so you have a shell on the " + goatnet +
			" management hosts now.\r\n\r\n"
		if len(req.Hosts) > 0 {
			for _, h := range req.Hosts {
				if h = strings.TrimSpace(h); h != "" {
					b += "    ssh " + h + "\r\n"
				}
			}
			b += "\r\n"
		}
		b += "Two things worth knowing.\r\n\r\n" +
			"The account is shared, but logins are still attributed to you " +
			"individually: sshd records the fingerprint of the key used, and that " +
			"fingerprint is registered against your handle. Please do not share " +
			"the key.\r\n\r\n" +
			"Hosts your role does not cover are deliberately excluded, so a shell " +
			"that does not work somewhere is a policy decision rather than a " +
			"fault.\r\n" + foot
		return subject, b, true

	case "ssh_shell_by_identity":
		// The network-admin case. Their access does NOT come from an installed
		// key, so telling them to send one would be wrong twice: it would not
		// help, and it would imply they lack access they already have.
		subject = "Your " + goatnet + " network-admin access — how it works"
		b := hello + "\r\n\r\n" +
			"Your access works differently from the platform admins', so this is " +
			"worth reading rather than skimming.\r\n\r\n" +
			"You already have shell access to the network gear you administer. " +
			"There is no key to send and no approval to wait for: it works through " +
			"your mesh identity, and anything else we enroll into your site's " +
			"network group becomes reachable the same way, automatically.\r\n\r\n" +
			"Use the built-in SSH:\r\n\r\n" +
			"    netbird ssh <device>\r\n\r\n" +
			"You do NOT have shell access to the management hosts, and submitting " +
			"an SSH key will not give you any. That boundary is the point of the " +
			"role: you administer the network without administering the platform. " +
			"If you ever need host access, that is a role change, not a key " +
			"upload.\r\n\r\n" +
			"Why the two differ: network gear is reached by mesh identity and " +
			"revoked by group membership, with nothing installed on the device. " +
			"Management hosts use installed SSH keys with an approval step. Each " +
			"device uses exactly one of those, never both — otherwise revoking one " +
			"would leave the other open, and we would be telling you your access " +
			"was removed when it was not.\r\n" + foot
		return subject, b, true

	case "onboarded":
		// The enrollment-instructions email states WHICH environment was joined
		// and (when configured) what it is for — the anti-confusion guard for
		// people who will later also hold access on another goatnet.
		label := goatnet
		if req.Env != "" {
			label = welcome.DisplayLabel(req.Env, s.Cfg.NotifyGoatnetTagline)
		}
		subject = "Welcome to " + label + " — your enrollment instructions"
		b := hello + "\r\n\r\n" +
			"Your " + goatnet + " onboarding is approved and your identity is provisioned.\r\n\r\n"
		if p := strings.TrimSpace(s.Cfg.NotifyGoatnetPurpose); p != "" {
			b += "About this environment — " + label + ": " + p + "\r\n\r\n"
		}
		if strings.TrimSpace(req.SetupKey) != "" && s.Cfg.NotifyMgmtURL != "" {
			// The --profile name falls back to the goatnet this console serves: a
			// payload without env would otherwise print the <goatnet> placeholder
			// beside a real key, and the shell reads it as an input redirect.
			profile := s.emailGoatnet(req.Env)
			b += "Enroll your machine onto the mesh (NetBird client, one time):\r\n\r\n" +
				"    macOS / Linux:\r\n" +
				"    " + welcome.EnrollCommand(s.Cfg.NotifyMgmtURL, strings.TrimSpace(req.SetupKey), s.Cfg.NotifyRosenpass, profile) + "\r\n\r\n" +
				"    Windows (administrator Command Prompt):\r\n" +
				"    " + welcome.WindowsEnrollCommand(s.Cfg.NotifyMgmtURL, strings.TrimSpace(req.SetupKey), s.Cfg.NotifyRosenpass, profile) + "\r\n\r\n"
		} else if strings.TrimSpace(req.SetupKey) != "" {
			b += "Your mesh enrollment key: " + strings.TrimSpace(req.SetupKey) + "\r\n\r\n"
		} else {
			b += "Your sponsor or an administrator will hand you the mesh enrollment key.\r\n\r\n"
		}
		b += "Everything else is fetched once you're on the mesh — deliberately " +
			"not in this email:\r\n"
		if s.Cfg.NotifyGetURL != "" {
			b += "  - tools + client downloads: " + s.Cfg.NotifyGetURL + "\r\n"
		}
		if s.Cfg.NotifyPortalURL != "" {
			b += "  - the portal (certificates, API keys, topics): " + s.Cfg.NotifyPortalURL + "\r\n"
		}
		// The onboarding guide. Omitting it meant landing someone on the mesh
		// with no pointer to the documentation written for exactly that moment —
		// they got downloads and the portal, and were left to guess the rest.
		//
		// This MUST be the guide host, not the console's /docs/onboarding. That
		// console page is behind onboardGate, which admits only matrix-admins
		// and portal-resolved sponsors — so the ordinary developer, observer or
		// partner this mail is mostly sent to followed "start here" and got a
		// 403 telling them to ask an admin for sponsorship. The guide host is
		// unauthenticated by design and renders a view per role.
		if g := welcome.GuidePointer(s.welcomeCatalog(req.Env)); g != "" {
			b += "  - the onboarding guide (start here): " + g + "\r\n"
		}
		b += foot
		return subject, b, true
	}
	return "", "", false
}

// emitNotifyAudit records one candidate-notification attempt. Actor is the
// machine channel, not a human — the portal already audited the transition
// with its human attribution.
func (s *Server) emitNotifyAudit(r *http.Request, req notifyRequest, outcome, errMsg string) {
	s.emitAuditWithInviteToken(r, Actor{Email: "portal-notify"}, "onboard_email_"+req.Event,
		req.Handle, "", nil,
		map[string]interface{}{"to": req.Email, "env": req.Env}, false, outcome, errMsg)
}

// consoleBase returns THE CONSOLE's own public base — the host that serves
// /ssh-key.
//
// Deliberately NOT PortalPublicURL. That is the public intake front desk; it
// serves the invite-accept flow and returns 404 for both of those paths.
// Conflating the two produces a dead /ssh-key link. StepUpReturnToBase is the
// same host and is already set on every goatnet, so it is the fallback until
// CONSOLE_PUBLIC_URL is templated everywhere.
func consoleBase(c *config.Config) string {
	if u := strings.TrimSpace(c.ConsolePublicURL); u != "" {
		return strings.TrimRight(u, "/")
	}
	return strings.TrimRight(strings.TrimSpace(c.StepUpReturnToBase), "/")
}
