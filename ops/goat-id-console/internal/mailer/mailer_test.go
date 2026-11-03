package mailer

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestComposeInvite_environmentIdentity(t *testing.T) {
	m := New("", "", "", "", "", "")
	msg := m.ComposeInvite("a@b.test", "https://x/invite/tok", "sponsor-1", "zagros",
		"integration gateway", "ZAGROS is the integration gateway; zagros-prod is separate.", 0)
	for _, want := range []string{
		"Subject: You've been invited to onboard to zagros (integration gateway)",
		"What you're joining — zagros (integration gateway): ZAGROS is the integration gateway",
		"https://x/invite/tok",
		"by sponsor-1",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("invite missing %q", want)
		}
	}
}

func TestComposeInvite_blankPurposeRendersNameOnly(t *testing.T) {
	m := New("", "", "", "", "", "")
	msg := m.ComposeInvite("a@b.test", "https://x/invite/tok", "operator", "zagros", "", "", 0)
	if strings.Contains(msg, "What you're joining") {
		t.Error("purpose paragraph must be absent when unconfigured")
	}
	if !strings.Contains(msg, "Subject: You've been invited to onboard to zagros\r\n") {
		t.Error("blank tagline must leave the subject name-only")
	}
}

// The invite tells the candidate which answers have consequences. Three of the
// four are labeled "optional" on the form, which reads as "unimportant" — a
// wrong organization was a dead end for the reviewer, and a blank GitHub
// username silently means no repositories.
func TestComposeInvite_namesTheConsequentialFields(t *testing.T) {
	msg := (&Mailer{}).ComposeInvite("c@example.test", "https://intake.example/onboard.html?t=tok",
		"sponsor@example.test", "zagros", "sandbox", "partner onboarding", 0)

	for _, want := range []string{
		"Organization — your employer or unit, NOT your own name",
		"Leave it blank and you get no repositories",
		"YOUR OWN citizenship, required",
		"allowance on your enrollment key",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("invite body missing %q", want)
		}
	}
	// Still one message, still carrying the link and the single-use warning.
	if !strings.Contains(msg, "https://intake.example/onboard.html?t=tok") {
		t.Error("invite lost the link")
	}
	if !strings.Contains(msg, "single-use and time-limited") {
		t.Error("invite lost the single-use warning")
	}
}

// F-363: "time-limited" told the candidate nothing and told the sponsor nothing.
// A one-hour link and a three-day link produced byte-identical wording, so the
// unintended TTL was invisible on both ends until the link was already dead.
func TestComposeInvite_statesTheActualDeadline(t *testing.T) {
	exp := time.Now().Add(3 * time.Hour).Unix()
	msg := (&Mailer{}).ComposeInvite("c@example.test", "https://x/t", "sponsor@example.test", "zagros", "", "", exp)

	if !strings.Contains(msg, "expires in 2 hours") && !strings.Contains(msg, "expires in 3 hours") {
		t.Errorf("invite must state how long the candidate has; got:\n%s", msg)
	}
	if !strings.Contains(msg, time.Unix(exp, 0).UTC().Format("2006-01-02 15:04")+" UTC") {
		t.Error("invite must state the absolute deadline, not just a relative one")
	}
	if strings.Contains(msg, "single-use and time-limited") {
		t.Error("the vague wording must not survive when a real expiry is known")
	}
}

// A caller with no expiry to give (an .eml composed from a form that predates
// the field) must degrade to the old wording rather than invent a deadline.
func TestComposeInvite_unknownExpiryStaysVague(t *testing.T) {
	msg := (&Mailer{}).ComposeInvite("c@example.test", "https://x/t", "s@example.test", "zagros", "", "", 0)
	if !strings.Contains(msg, "single-use and time-limited") {
		t.Error("zero expiry must fall back to the vague wording")
	}
}

func TestInviteExpiryPhrase_roundsDownNeverUp(t *testing.T) {
	// 47h59m must not read "2 days" — that promises time the link does not have.
	got := inviteExpiryPhrase(time.Now().Add(47*time.Hour + 59*time.Minute).Unix())
	if !strings.Contains(got, "hours") {
		t.Errorf("just under 48h must render in hours, got %q", got)
	}
	if got := inviteExpiryPhrase(time.Now().Add(-time.Hour).Unix()); !strings.Contains(got, "already expired") {
		t.Errorf("a past expiry must say so, got %q", got)
	}
}

// A machine-minted invite can carry the sponsor's raw registry anchor when no
// display name was resolved. The email names a sponsor only when it holds a
// name — "invited by 34ed7992…" told the candidate nothing (live).
func TestComposeInvite_anchorSponsorIsNotNamed(t *testing.T) {
	msg := (&Mailer{}).ComposeInvite("c@example.test", "https://x/t", "34ed79927aeb0074b337cd05f5ff56c2", "zagros", "", "", 0)
	if strings.Contains(msg, " by 34ed7992") {
		t.Errorf("a bare anchor must not be presented as the sponsor:\n%s", msg)
	}
	if !strings.Contains(msg, "onboard to the zagros data fabric.") {
		t.Errorf("sponsor-less phrasing expected:\n%s", msg)
	}
	named := (&Mailer{}).ComposeInvite("c@example.test", "https://x/t", "Desert GOAT", "zagros", "", "", 0)
	if !strings.Contains(named, " by Desert GOAT") {
		t.Errorf("a real name must still be presented:\n%s", named)
	}
}

// The invite describes the form's fields. Since ADR 1171 the form asks the
// PERSON for their own nationality (required), never for the
// organisation's nation — the invite must say the same thing the form does.
func TestComposeInvite_describesNationalityNotOrgNation(t *testing.T) {
	msg := (&Mailer{}).ComposeInvite("c@example.test", "https://x/t", "s", "zagros", "", "", 0)
	if !strings.Contains(msg, "Nationality — YOUR OWN citizenship, required") {
		t.Errorf("invite must describe the person's own, required nationality:\n%s", msg)
	}
	if strings.Contains(msg, "your organization's controlling nation") {
		t.Errorf("invite still describes the retired org-nation question:\n%s", msg)
	}
}

// The sender is a send-only SES address: the public zone carries no MX for it,
// so a candidate who hits Reply gets "Domain has no MX records" and reads it as
// the system being down (vendor onboarding). With no
// monitored address configured, the invite says so and points at the sponsor.
func TestComposeInvite_saysAReplyIsNotDelivered(t *testing.T) {
	msg := New("", "", "", "", "onboarding@example.test", "").ComposeInvite(
		"c@example.test", "https://x/t", "Desert GOAT", "zagros", "", "", 0)
	for _, want := range []string{
		"A reply to this message is not delivered",
		"Questions go to the person who invited you",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("invite body missing %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "Reply-To:") {
		t.Errorf("no Reply-To header without a monitored address:\n%s", msg)
	}
}

// With a monitored address configured, the invite carries it as Reply-To so a
// plain reply lands somewhere, and the body names it.
func TestComposeInvite_replyToNamesTheMonitoredAddress(t *testing.T) {
	msg := New("", "", "", "", "onboarding@example.test", "sponsors@example.test").ComposeInvite(
		"c@example.test", "https://x/t", "Desert GOAT", "zagros", "", "", 0)
	for _, want := range []string{
		"Reply-To: sponsors@example.test\r\n",
		"Questions go to sponsors@example.test",
		"A reply to this message is delivered there",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("invite missing %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "is not delivered") {
		t.Errorf("the bounce warning must not survive when replies are routed:\n%s", msg)
	}
}

// A machine-minted invite can carry a registry anchor as its sponsor, and a
// console with no SMTP_REPLY_TO then composes a mail that names nobody to
// reply to. Sending it strands the candidate at their first question, which
// is the fault Reply-To exists to prevent, so SendInvite refuses before the
// dial and names the fix. The .eml hand-over still composes: the operator's
// own mailbox sends it and takes the reply.
func TestSendInvite_refusesWhenNobodyTakesAReply(t *testing.T) {
	// Port 1 on loopback: a send that got past the check fails on the dial,
	// not with ErrNoReplyRoute, so the assertion tells the two apart.
	noReplyTo := New("127.0.0.1", "1", "", "", "onboarding@example.test", "")
	for _, sponsor := range []string{"34ed79927aeb0074b337cd05f5ff56c2", "operator", ""} {
		err := noReplyTo.SendInvite("c@example.test", "https://x/t", sponsor, "zagros", "", "", 0)
		if !errors.Is(err, ErrNoReplyRoute) {
			t.Errorf("sponsor %q with no Reply-To: want ErrNoReplyRoute, got %v", sponsor, err)
			continue
		}
		if !strings.Contains(err.Error(), "SMTP_REPLY_TO") || !strings.Contains(err.Error(), ".eml") {
			t.Errorf("refusal must name the fix and the hand-over path: %v", err)
		}
	}

	// A named sponsor is a person the candidate can ask, so the send proceeds
	// (and here fails on the dial, which is the point: it got past the check).
	err := noReplyTo.SendInvite("c@example.test", "https://x/t", "Desert GOAT", "zagros", "", "", 0)
	if errors.Is(err, ErrNoReplyRoute) {
		t.Errorf("a named sponsor is a reply route; the send must not be refused: %v", err)
	}
	// A configured Reply-To is a route whatever the sponsor field holds.
	routed := New("127.0.0.1", "1", "", "", "onboarding@example.test", "sponsors@example.test")
	err = routed.SendInvite("c@example.test", "https://x/t", "34ed79927aeb0074b337cd05f5ff56c2", "zagros", "", "", 0)
	if errors.Is(err, ErrNoReplyRoute) {
		t.Errorf("Reply-To is configured; the send must not be refused: %v", err)
	}
	// The .eml path composes the same mail regardless.
	msg := noReplyTo.ComposeInvite("c@example.test", "https://x/t", "34ed79927aeb0074b337cd05f5ff56c2", "zagros", "", "", 0)
	if !strings.Contains(msg, "Open this one-time link") {
		t.Errorf(".eml composition must still work without a reply route:\n%s", msg)
	}
}

// headerLines returns the header block of msg, one field per element.
func headerLines(t *testing.T, msg string) []string {
	t.Helper()
	head, _, ok := strings.Cut(msg, "\r\n\r\n")
	if !ok {
		t.Fatalf("message has no header/body separator:\n%s", msg)
	}
	return strings.Split(head, "\r\n")
}

func TestComposeInvite_lineBreaksCannotAddHeaders(t *testing.T) {
	m := New("", "", "", "", "onboarding@example.invalid", "help@example.invalid\r\nBcc: reply@example.invalid")
	msg := m.ComposeInvite("candidate@example.invalid\r\nBcc: to@example.invalid", "https://example.invalid/i/abc",
		"Alice", "zagros\r\nBcc: env@example.invalid", "gateway\nX-Injected: tagline", "", 0)
	for _, line := range headerLines(t, msg) {
		if strings.HasPrefix(line, "Bcc:") || strings.HasPrefix(line, "X-Injected:") {
			t.Fatalf("injected header field %q in:\n%s", line, msg)
		}
	}
	if !strings.Contains(msg, "Subject: You've been invited to onboard to zagros  Bcc: env@example.invalid (gateway X-Injected: tagline)\r\n") {
		t.Fatalf("subject not kept on one line:\n%s", msg)
	}
}

func TestComposeMultipart_lineBreaksCannotAddHeaders(t *testing.T) {
	msg, err := ComposeMultipart("from@example.invalid", "to@example.invalid",
		"Welcome\r\n\r\nforged body", "text", "<p>html</p>")
	if err != nil {
		t.Fatal(err)
	}
	lines := headerLines(t, msg)
	if lines[len(lines)-1] != `Content-Type: multipart/alternative; boundary="=_goat_welcome_alt_7f3c9a1b"` {
		t.Fatalf("a subject line break ended the headers early:\n%s", msg)
	}
	if !strings.Contains(msg, "Subject: Welcome    forged body\r\n") {
		t.Fatalf("subject not kept on one line:\n%s", msg)
	}
}

func TestSend_refusesARecipientWithALineBreak(t *testing.T) {
	// Port 1 on loopback: the refusal must come before any dial.
	m := New("127.0.0.1", "1", "", "", "from@example.invalid", "")
	to := "to@example.invalid\r\nBcc: other@example.invalid"
	for name, send := range map[string]func() error{
		"Send":          func() error { return m.Send(to, "s", "b") },
		"SendMultipart": func() error { return m.SendMultipart(to, "s", "t", "h") },
		"SendInvite":    func() error { return m.SendInvite(to, "https://example.invalid/i", "Alice", "zagros", "", "", 0) },
	} {
		err := send()
		if err == nil || !strings.Contains(err.Error(), "cannot contain a line break") {
			t.Errorf("%s: got %v, want the line-break refusal", name, err)
		}
	}
}
