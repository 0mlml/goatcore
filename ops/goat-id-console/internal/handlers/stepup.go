package handlers

// Step-up (AAL2) for the highest-blast console mutations — ADR 0860's IA-2
// compensating control, finished here (issue #174; absorbs the always-403 bug
// #175). Design: docs/design/id-admin-console.md §"Auth + step-up".
//
// THE SIGNAL — why this reads Kratos, not a header
//
// ADR 0860 and #174 Story A both assumed the actor's AAL would arrive as an
// `acr` claim, minted by Hydra and mapped to a request header by the
// oauth2-proxy sidecar. Neither half of that is true on this stack, verified
// against the pinned upstreams:
//
//  1. Kratos NEVER SETS acr. Kratos is the OAuth2 login provider here
//     (kratos.yaml `oauth2_provider.url: http://hydra:4445`), so it — not the
//     selfservice-ui — is what calls Hydra's acceptOAuth2LoginRequest. In
//     kratos v1.3.1 `hydra/hydra.go:AcceptLoginRequest` populates Amr (the
//     authentication methods) and leaves Acr unset. No amount of hydra.yaml
//     configuration produces an acr claim, because nothing upstream supplies
//     one.
//
//  2. oauth2-proxy CANNOT map an arbitrary claim to a header. The alpha
//     `injectRequestHeaders` config that #174 proposed resolves its `claim:`
//     against SessionState.GetClaim, which in v7.6.0
//     (pkg/apis/sessions/session_state.go) is a CLOSED switch over
//     access_token / id_token / created_at / expires_on / refresh_token /
//     email / user / groups / preferred_username. Any other claim name —
//     `acr`, `amr`, `auth_time` — falls through to the default branch and
//     returns an empty slice, so the header is simply never emitted.
//
// A third reason makes the header route wrong even if both had worked: an OIDC
// claim is a snapshot of the login, but the oauth2-proxy cookie session lives
// 8h and only refreshes hourly. An admin who completes step-up mid-session
// would keep presenting the stale pre-step-up token, so the mutation they just
// re-authenticated for would still be refused. A step-up gate needs a LIVE
// signal.
//
// So the AAL comes from the Kratos admin API instead: the console already
// holds an admin client per IdP, and the actor's identity ID arrives in the
// unforgeable X-Forwarded-User header. Kratos's own session record carries the
// per-factor ledger (authentication_methods[].aal + .completed_at), which is
// live, precisely timestamped, and — being read server-to-server over the
// admin API — not forgeable by the browser at all. That sidesteps the F-244
// header-trust question rather than re-litigating it.
//
// WHERE THE IDENTITY ID COMES FROM — X-Forwarded-User, and why (goat#247).
//
// X-Forwarded-User carries the OIDC `sub` claim, which Kratos-as-OAuth2-
// provider fills with the identity UUID. The chain has one condition: `sub`
// is the raw identity UUID only for OAuth2 clients with subject_type=public.
// Every client on this stack is public (see the
// site_name/pairwise-salt caveat in
// ops/ansible/roles/ory/defaults/main.yml). Registering the console's client
// as PAIRWISE is therefore a BREAKING CHANGE for this gate: the subject
// becomes a per-client obfuscation that resolves against no identity.
// identityIDFromSubject (handlers.go) is the runtime guard for exactly that —
// a non-UUID subject yields an empty identity ID and the gate fails closed
// with a log line naming the subject, rather than querying Kratos with junk.
//
// Two alternatives were rejected, deliberately:
//
//   - X-Forwarded-Preferred-Username (the original source) CANNOT WORK here:
//     oauth2-proxy only emits it from a `preferred_username` claim, the
//     consent UI only mints that claim from identity.traits.username, and the
//     goat identity schema has no username trait. The header was empty for
//     every operator, so the gate failed closed for everyone and the
//     interstitial mis-rendered that as "no second factor enrolled".
//   - Resolving by EMAIL (kratosIdentityIDForEmail, the strategy
//     fabriccred_page.go uses on purpose so it does not depend on IdP claim
//     mapping). Right there, wrong here: this resolver runs on every gated
//     mutation AND every banner render (cachedStepUpState), so an extra
//     per-resolution identity search is real cost — and `sub` is the
//     authoritative subject where email is a mutable trait. The two surfaces'
//     divergent strategies are deliberate, not drift.
//
// SCOPE OF THE CHECK. Kratos scopes AAL to a session, and the console cannot
// see WHICH Kratos session backs the caller's Hydra token (Hydra stores the
// identity_provider_session_id but does not surface it downstream). So the
// resolver takes the best active session for the identity. The one-shot nonce
// closes the resulting gap in the direction that matters: a challenge is only
// satisfied by an aal2 authentication that completed AFTER the challenge was
// issued, so a pre-existing aal2 session elsewhere cannot silently satisfy a
// fresh gate.

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// aal2 is the assurance level a step-up-gated mutation demands.
const aal2 = "aal2"

// csrfFormField is the form field validateCSRF reads. Named here so the
// challenge capture can drop it from a replayed body.
const csrfFormField = "csrf"

// stepUpNonceTTL bounds how long an issued challenge can be redeemed. Longer
// than a realistic TOTP round-trip, far shorter than a session.
const stepUpNonceTTL = 10 * time.Minute

// stepUpEnrolNonceTTL is the deadline a challenge is EXTENDED to when the
// interstitial offers enrolment instead of verify. A first-time enroller's
// round trip is install-an-app + scan-the-QR + type-a-code, which routinely
// outlives the 10-minute verify budget — and the enroll page promises the
// pinned action will resume, a promise an expired nonce silently breaks.
// Fixed cap from IssuedAt (not from render time), so re-rendering the
// interstitial cannot keep a challenge alive indefinitely. The security
// properties are untouched: one-shot redemption, actor binding, and the
// aal2-must-postdate-IssuedAt check all still hold; only how long the pinned
// action waits in memory widens, and only for unenrolled actors.
const stepUpEnrolNonceTTL = 30 * time.Minute

// --- actor AAL state ---------------------------------------------------

// stepUpState is the resolved, live step-up posture of the actor, made of two
// DELIBERATELY separate facts (goat#247 defect 2 was conflating them):
//
//   - Enrolled — the identity CARRIES a second factor (credential truth, read
//     from the identity record). This drives the enroll-vs-verify UI split.
//   - Fresh / At — a second factor was PRESENTED recently (session-ledger
//     truth). This is the only thing the gate accepts.
//
// An enrolled admin whose live sessions are all password-only is
// Enrolled && !Fresh: they must verify, but must never be told to enroll.
//
// The zero value is the honest "nothing established" answer, which is also
// what an unreachable Kratos yields — the gate fails closed.
type stepUpState struct {
	Enrolled  bool
	Fresh     bool
	At        time.Time // newest aal2 authentication across live sessions
	ExpiresAt time.Time // At + freshness window; zero when !Fresh
	// Locked records that the actor deliberately ended their own window via
	// "Lock now". Display-only: it lets the banner say "you locked this"
	// rather than the ambiguous "not verified recently". The gate treats it
	// identically to any other !Fresh state.
	Locked bool
	// Err records why the posture could not be established (Kratos down, no
	// identity ID). Non-nil means "unknown": the gate treats it as "no", and
	// the UI must render it as "could not verify" — NOT as "not enrolled"
	// (goat#247 defect 3).
	Err error
}

// Remaining is how much of the freshness window is left, floored at zero. The
// header banner renders this.
func (s stepUpState) Remaining() time.Duration {
	if !s.Fresh {
		return 0
	}
	if d := time.Until(s.ExpiresAt); d > 0 {
		return d
	}
	return 0
}

// resolveStepUpState asks Kratos for the actor's live step-up posture.
//
// The lookup always runs against the console's SSO realm (Cfg.StepUpSSOIdP) —
// the realm oauth2-proxy authenticates admins against, and therefore the only
// realm where the forwarded actor identity ID resolves. It must NOT use the
// ?idp= administered-realm selector: an admin administering the other identity
// plane would resolve against a realm that has never heard of them, fail
// closed, and loop through challenges forever.
//
// Deliberately best-effort-shaped but fail-CLOSED: every error path returns a
// state with Err set and both booleans false, so a Kratos outage refuses
// gated mutations rather than admitting them. Errors are logged HERE — this is
// the one chokepoint every posture read goes through, and an unlogged Err used
// to surface to the operator as a false "not enrolled" with no server-side
// trace at all (goat#247 defect 3). The non-gated 95% of the console never
// calls this.
func (s *Server) resolveStepUpState(ctx context.Context, actor Actor) stepUpState {
	fail := func(err error) stepUpState {
		slog.Warn("step-up posture unresolved (gate stays closed)",
			slog.String("actor", actor.Email),
			slog.String("err", err.Error()))
		return stepUpState{Err: err}
	}
	if actor.IdentityID == "" {
		// No identity ID means no way to ask Kratos who this is. Either the
		// sidecar did not forward a subject at all, or it forwarded one that
		// is not an identity UUID (a pairwise-subject client — see
		// identityIDFromSubject). Not an attack, but it cannot satisfy the
		// gate.
		if actor.Username != "" {
			return fail(fmt.Errorf("forwarded subject %q is not a Kratos identity UUID (pairwise-subject OAuth2 client?)", actor.Username))
		}
		return fail(fmt.Errorf("actor carries no identity ID (oauth2-proxy did not set X-Forwarded-User)"))
	}
	cli := s.Kratos[s.Cfg.StepUpSSOIdP]
	if cli == nil {
		return fail(fmt.Errorf("SSO realm %q is unavailable", s.Cfg.StepUpSSOIdP))
	}

	// Enrollment is credential truth from the identity record, NOT session
	// history: a factor enrolled five minutes ago counts even though no
	// session has presented it yet, and a factor that was reset away stops
	// counting even while an old aal2 session is still live.
	ident, err := cli.GetIdentity(ctx, actor.IdentityID)
	if err != nil {
		return fail(fmt.Errorf("kratos identity lookup failed: %w", err))
	}

	sessions, err := cli.ListIdentitySessions(ctx, actor.IdentityID)
	if err != nil {
		return fail(fmt.Errorf("kratos session lookup failed: %w", err))
	}

	st := stepUpState{Enrolled: ident.HasSecondFactor()}
	now := time.Now()
	for _, sess := range sessions {
		if !sess.Active || (!sess.ExpiresAt.IsZero() && sess.ExpiresAt.Before(now)) {
			continue
		}
		at, ok := sess.FreshAAL2At()
		if !ok {
			// A session without a method ledger proves nothing usable here:
			// freshness needs a per-factor timestamp, and enrollment now comes
			// from the identity record above rather than session history.
			continue
		}
		if at.After(st.At) {
			st.At = at
		}
	}
	if !st.At.IsZero() {
		// A deliberate re-lock ends the window early. The authentication it
		// superseded still happened — Enrolled is untouched — but it no longer
		// satisfies the gate, so the next high-risk action challenges. Only an
		// aal2 authentication completed AFTER the lock re-opens the window.
		if lockedAt, locked := s.relockedAt(actor.IdentityID); locked && !st.At.After(lockedAt) {
			st.Locked = true
			return st
		}
		st.ExpiresAt = st.At.Add(s.Cfg.AAL2FreshWindow)
		st.Fresh = st.ExpiresAt.After(now)
		if !st.Fresh {
			st.ExpiresAt = time.Time{}
		}
	}
	return st
}

// stepUpLockRetention bounds how long a lock is remembered, so the map does not
// grow one entry per operator forever.
//
// It MUST outlive the freshness window. A lock is only redundant once the aal2
// event it superseded could no longer be fresh on its own; prune sooner and the
// lock silently expires and RE-OPENS the window the operator deliberately
// closed. AAL2_FRESH_WINDOW_SEC is operator-configurable, so this is derived
// from it rather than fixed — a hardcoded hour would have been wrong the moment
// anyone set the window above an hour.
func (s *Server) stepUpLockRetention() time.Duration {
	const floor = time.Hour
	if d := 2 * s.Cfg.AAL2FreshWindow; d > floor {
		return d
	}
	return floor
}

// lockStepUp ends the actor's freshness window now. Idempotent, and safe to
// call when the actor was not fresh: the gate is strictly stricter afterwards,
// never looser, which is why this needs no privilege beyond being the actor.
//
// Reports whether a lock was actually recorded. An actor with no identity ID
// cannot be locked (there is nothing to key on), and the caller must say so
// rather than redirect as though it worked.
//
// SCOPE, stated plainly: this state is per-process and in-memory, so a console
// restart forgets every lock, and a multi-replica deployment would only honor
// a lock on the replica that took it. That is a real limit, but not a security
// regression — forgetting a lock returns the operator to exactly the freshness
// they would have had without this control, which is the behavior shipped
// today. It is a convenience and a determinism aid, not a containment boundary.
func (s *Server) lockStepUp(identityID string) bool {
	if identityID == "" {
		return false
	}
	now := time.Now()
	retention := s.stepUpLockRetention()
	s.stepUpLockMu.Lock()
	if s.stepUpLocks == nil {
		s.stepUpLocks = map[string]time.Time{}
	}
	for k, at := range s.stepUpLocks {
		if now.Sub(at) > retention {
			delete(s.stepUpLocks, k)
		}
	}
	s.stepUpLocks[identityID] = now
	s.stepUpLockMu.Unlock()

	// Drop the DISPLAY cache so the banner reflects the lock immediately
	// rather than up to stepUpStateTTL later.
	s.dropStepUpCache(identityID)
	return true
}

// relockedAt reports the actor's most recent deliberate lock, if it is still
// within the retention window.
func (s *Server) relockedAt(identityID string) (time.Time, bool) {
	if identityID == "" {
		return time.Time{}, false
	}
	s.stepUpLockMu.Lock()
	defer s.stepUpLockMu.Unlock()
	at, ok := s.stepUpLocks[identityID]
	if !ok || time.Since(at) > s.stepUpLockRetention() {
		return time.Time{}, false
	}
	return at, true
}

// handleStepUpLock is the "Lock now" control in the posture strip. It ends the
// operator's own freshness window so the next high-risk action re-challenges.
//
// Deliberately fail-closed-only: it can make the gate stricter and never
// looser, so unlike a bypass it needs no additional authorization beyond being
// the signed-in actor. Two reasons it exists:
//
//   - hygiene — after finishing privileged work an operator can re-lock instead
//     of leaving the window open for the remainder of AAL2_FRESH_WINDOW_SEC;
//   - determinism — verifying the gate (the #188 rollout live-fires all seven
//     gated paths) otherwise means waiting out the window or re-logging in,
//     because enrolling a factor itself opens one.
func (s *Server) handleStepUpLock(w http.ResponseWriter, r *http.Request) {
	if err := s.validateCSRF(r); err != nil {
		s.renderError(w, r, 400, err.Error())
		return
	}
	actor := actorFromCtx(r.Context())
	if !s.lockStepUp(actor.IdentityID) {
		// No identity ID means a misconfigured sidecar. Redirecting as though
		// the lock took would tell the operator their window is closed when it
		// is not — the one lie this control must never tell.
		s.renderError(w, r, 500, "Could not lock: this session carries no identity ID "+
			"(no Kratos identity UUID arrived on X-Forwarded-User). Your step-up window is unchanged.")
		return
	}

	// A deliberate re-lock changes the actor's own step-up posture, so record it
	// like any other operator action. StepUpVerified is false: locking needs no
	// second factor precisely because it can only tighten the gate.
	s.emitAudit(r, actor, "step_up_lock", nil, "", nil, nil, false, "ok", "")

	// return_to here is operator-supplied (a hidden field on the strip's form),
	// so this is the load-bearing check, not defense in depth.
	http.Redirect(w, r, safeReturnPath(r.FormValue("return_to")), http.StatusSeeOther)
}

// isLocalURL keeps the redirect on this console: a same-origin, absolute
// path with no scheme and no protocol-relative prefix.
//
// Backslashes are rejected outright, not just in the leading pair: browsers
// normalise "\" to "/" in the authority position, so "/\evil.example" and
// "/\/evil.example" both leave the origin despite passing a naive
// starts-with-"/" and not-"//" test.
//
// Control characters are rejected for the same reason: browsers strip tab, CR
// and LF from a URL before parsing it, so "/\t/evil.example" arrives as
// "//evil.example". Go writes the tab into the Location header unchanged.
//
// The leading pair is checked byte by byte (first is "/", second is neither "/"
// nor "\\") so the shape matches what static redirect analysis recognizes. The
// name matters for the same reason: CodeQL treats a true isLocalURL(x) as a
// guard on x, which is what clears the redirect sinks fed by safeReturnPath.
func isLocalURL(p string) bool {
	if len(p) == 0 || p[0] != '/' {
		return false
	}
	if len(p) > 1 && (p[1] == '/' || p[1] == '\\') {
		return false
	}
	return !strings.ContainsAny(p, "\\") && !strings.ContainsFunc(p, func(r rune) bool {
		return r < 0x20 || r == 0x7f
	})
}

// safeReturnPath is isLocalURL as a normaliser, for the callers that want
// a usable path rather than a verdict. It exists so the guard can be applied
// at every sink without repeating the fallback.
//
// A stored challenge's ReturnTo reaches FOUR navigation targets — the
// redemption redirect, the replayed form action, and the interstitial's
// "Cancel and go back" link on both the initial and the failed-verify render.
// Guarding only the redirect and the form action leaves the same value flowing
// unchecked into the other two, so this is applied at all four AND at capture
// (requireStepUp), where it is what actually establishes the invariant:
// nothing a guard would reject is ever stored in the first place.
func safeReturnPath(p string) string {
	if !isLocalURL(p) {
		return "/"
	}
	return p
}

// stepUpStateTTL bounds how stale the DISPLAY copy of the posture may be.
//
// 60s, not seconds: resolveStepUpState's GetIdentity carries
// include_credential, so every cache miss makes Kratos decrypt and ship the
// actor's TOTP seed and backup codes just to fill the banner's one boolean
// (review on goat#252 flagged the per-render fetch as cost/hygiene — the
// secrets are discarded at decode, but fetching them 12×/min per admin to
// render a badge is waste). The banner stays honest at 60s because everything
// it renders is either absolute (Remaining() computes from ExpiresAt against
// live time, so the countdown cannot lie) or slow-moving (enrolment), and the
// two in-console transitions that must show instantly — a completed verify
// and a deliberate lock — both drop the cache entry directly
// (dropStepUpCache). The residual staleness is a factor enrolled or reset
// OUTSIDE the console showing up to 60s late, on a display surface only.
//
// The enforcement path (requireStepUp / requireAAL2) deliberately does NOT
// read this cache — a gate must see the live answer.
const stepUpStateTTL = 60 * time.Second

// dropStepUpCache forgets the DISPLAY copy of one actor's posture, so the
// next render re-resolves instead of waiting out stepUpStateTTL. Call it
// whenever the console itself just changed what the banner should say.
func (s *Server) dropStepUpCache(identityID string) {
	s.stepUpCacheMu.Lock()
	delete(s.stepUpCache, identityID)
	s.stepUpCacheMu.Unlock()
}

type cachedState struct {
	state stepUpState
	at    time.Time
}

// cachedStepUpState is the DISPLAY-ONLY accessor used by render. Never call it
// from a gate.
func (s *Server) cachedStepUpState(ctx context.Context, actor Actor) stepUpState {
	if actor.IdentityID == "" {
		// No identity means we could not CHECK, which is not the same as "no
		// second factor" — conflating those two IS goat#247. Returning the zero
		// stepUpState here would say PostureUnknown:false + HasSecondFactor:false,
		// rendering the persistent banner's "enroll an authenticator first" at an
		// already-enrolled admin on every page, silently, while the interstitial
		// correctly said we could not check — the same defect this change fixes,
		// relocated from the gate to the banner.
		//
		// Fall through to resolveStepUpState instead of short-circuiting: it
		// already returns the honest error for this case AND logs the diagnosis,
		// which the short-circuit skipped. It fails before any Kratos call, so
		// this costs nothing and is deliberately not cached — there is no
		// identity to key the cache on.
		return s.resolveStepUpState(ctx, actor)
	}
	key := actor.IdentityID
	s.stepUpCacheMu.Lock()
	if s.stepUpCache == nil {
		s.stepUpCache = map[string]cachedState{}
	}
	if c, ok := s.stepUpCache[key]; ok && time.Since(c.at) < stepUpStateTTL {
		s.stepUpCacheMu.Unlock()
		return c.state
	}
	s.stepUpCacheMu.Unlock()

	st := s.resolveStepUpState(ctx, actor)

	s.stepUpCacheMu.Lock()
	s.stepUpCache[key] = cachedState{state: st, at: time.Now()}
	s.stepUpCacheMu.Unlock()
	return st
}

// --- one-shot challenge nonces -----------------------------------------

// stepUpChallenge is an issued, not-yet-redeemed step-up demand. It pins the
// exact mutation the operator was attempting so the return leg replays THAT
// action and nothing else, and records IssuedAt so redemption can require an
// aal2 authentication newer than the challenge itself.
type stepUpChallenge struct {
	Action     string     // matrix action name, for the interstitial copy + audit
	Method     string     // HTTP method of the pinned mutation
	ReturnTo   string     // path+query of the pinned mutation
	Form       url.Values // POST body to replay, if any
	ActorEmail string     // binds the challenge to the operator who raised it
	IdPKey     string     // administered realm of the pinned mutation; carried through both legs
	IssuedAt   time.Time
	// Deadline is when the challenge stops being redeemable. IssuedAt +
	// stepUpNonceTTL at issue; extended once to IssuedAt + stepUpEnrolNonceTTL
	// when the interstitial routes the actor to enrolment (see that const).
	Deadline time.Time
}

// stepUpStore holds live challenges. In-memory by design: a challenge is
// single-use, expires in minutes, and is worthless after a restart (the
// operator simply re-clicks). This mirrors the invite store's v1 posture —
// nothing here is worth a Postgres table.
type stepUpStore struct {
	mu sync.Mutex
	m  map[string]stepUpChallenge
}

func newStepUpStore() *stepUpStore { return &stepUpStore{m: map[string]stepUpChallenge{}} }

func (st *stepUpStore) issue(c stepUpChallenge) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	nonce := base64.RawURLEncoding.EncodeToString(b)
	c.IssuedAt = time.Now()
	c.Deadline = c.IssuedAt.Add(stepUpNonceTTL)
	st.mu.Lock()
	defer st.mu.Unlock()
	st.sweepLocked()
	st.m[nonce] = c
	return nonce, nil
}

// redeem removes and returns the challenge. One-shot: a second redemption of
// the same nonce always misses, so a leaked return URL cannot replay the
// mutation.
func (st *stepUpStore) redeem(nonce string) (stepUpChallenge, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.sweepLocked()
	c, ok := st.m[nonce]
	if ok {
		delete(st.m, nonce)
	}
	return c, ok
}

// peek reads a challenge without consuming it — the interstitial renders from
// this, since the operator has not completed step-up yet.
func (st *stepUpStore) peek(nonce string) (stepUpChallenge, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	c, ok := st.m[nonce]
	return c, ok
}

// extendForEnrol widens the challenge's deadline to the enroll budget. Called
// when the interstitial offers enrolment rather than verify. Idempotent — the
// deadline is a fixed cap from IssuedAt, so repeated renders do not push it.
func (st *stepUpStore) extendForEnrol(nonce string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if c, ok := st.m[nonce]; ok {
		if d := c.IssuedAt.Add(stepUpEnrolNonceTTL); d.After(c.Deadline) {
			c.Deadline = d
			st.m[nonce] = c
		}
	}
}

func (st *stepUpStore) sweepLocked() {
	now := time.Now()
	for k, v := range st.m {
		if now.After(v.Deadline) {
			delete(st.m, k)
		}
	}
}

// --- the gate ----------------------------------------------------------

// stepUpOutcome is what the gate decided, so callers can audit honestly.
type stepUpOutcome struct {
	// Verified is true ONLY when a genuine, fresh, second-factor
	// authentication backed this request. It is what audit.Event's
	// StepUpVerified must carry — never "we ran the gate", never "policy let
	// it through".
	Verified bool
	// Handled means the gate already wrote a response (a redirect to the
	// interstitial, or a 403) and the caller must return immediately.
	Handled bool
}

// requireStepUp is THE entry point for a step-up-gated mutation on a
// browser-driven surface. It either lets the mutation proceed (returning
// whether a real step-up backed it) or takes over the response — redirecting
// the operator into the explain-then-reauthenticate flow.
//
// Contrast requireAAL2, which is the non-redirecting predicate kept for
// machine/API callers that have no browser to send anywhere.
func (s *Server) requireStepUp(w http.ResponseWriter, r *http.Request, actor Actor, action string) stepUpOutcome {
	if s.Cfg.DevBypassAuth {
		// Dev bypass short-circuits authentication entirely, so nothing was
		// actually verified. Let the mutation run; do not claim a factor.
		return stepUpOutcome{}
	}
	idp := idpFromCtx(r.Context())
	state := s.resolveStepUpState(r.Context(), actor)

	if state.Fresh {
		return stepUpOutcome{Verified: true}
	}
	if !s.Cfg.StepUpEnforced {
		// Per-goatnet policy escape (#1858). The action proceeds and is still
		// audited — but no second factor was presented, so Verified stays
		// false. This is the honesty rule #174 requires.
		return stepUpOutcome{}
	}

	// Enforced and not fresh: send the operator through step-up rather than
	// dead-ending on a 403. A non-GET-initiated caller that cannot follow a
	// redirect still gets a redirect here by design — every one of these
	// handlers is reached from a console form submit, so the browser follows
	// it and lands on the interstitial.
	nonce, err := s.StepUps.issue(stepUpChallenge{
		Action:     action,
		Method:     r.Method,
		ReturnTo:   safeReturnPath(r.URL.RequestURI()),
		Form:       captureForm(r),
		ActorEmail: actor.Email,
		IdPKey:     idp.Key,
	})
	if err != nil {
		slog.Error("step-up challenge mint failed", slog.String("err", err.Error()))
		s.renderError(w, r, 500, "Could not start the step-up flow. Retry, or contact a platform admin.")
		return stepUpOutcome{Handled: true}
	}
	http.Redirect(w, r, "/step-up?nonce="+url.QueryEscape(nonce)+"&idp="+url.QueryEscape(idp.Key), http.StatusSeeOther)
	return stepUpOutcome{Handled: true}
}

// stepUpChallengeAudit is the error_message recorded when a gate issues a
// challenge instead of completing. It is deliberately NOT phrased as a denial:
// the operator was redirected to verify, not turned away.
const stepUpChallengeAudit = "step-up required — AAL2 challenge issued, operator redirected to re-authenticate"

// aalLabel renders the AAL an action actually ran at, for the audit actor
// record. Only a fresh, verified second factor earns "aal2".
func aalLabel(verified bool) string {
	if verified {
		return aal2
	}
	return "aal1"
}

// captureForm snapshots a mutation's POST body so the return leg can replay it
// verbatim. r.ParseForm has usually already run (CSRF validation), so this is
// cheap; the CSRF token is dropped because the replay re-validates against the
// operator's live cookie rather than the captured one.
func captureForm(r *http.Request) url.Values {
	if r.Method == http.MethodGet {
		return nil
	}
	if err := r.ParseForm(); err != nil {
		return nil
	}
	out := url.Values{}
	for k, v := range r.PostForm {
		if k == csrfFormField {
			continue
		}
		out[k] = v
	}
	return out
}

// --- interstitial + return legs ----------------------------------------

// handleStepUpChallenge renders the plain-language interstitial: what the
// operator was trying to do, why it needs a second factor, and the one button
// that starts re-authentication. This page is the whole point of the "admins
// know exactly that they need AAL2" requirement — the requirement is never
// discovered as an error.
func (s *Server) handleStepUpChallenge(w http.ResponseWriter, r *http.Request) {
	actor := actorFromCtx(r.Context())
	nonce := r.URL.Query().Get("nonce")
	ch, ok := s.StepUps.peek(nonce)
	if !ok {
		s.renderError(w, r, 400, "That step-up request expired or was already used. Return to the page and try the action again.")
		return
	}
	if ch.ActorEmail != actor.Email {
		s.renderError(w, r, 403, "That step-up request belongs to a different operator.")
		return
	}
	state := s.resolveStepUpState(r.Context(), actor)

	if state.Err == nil && !state.Enrolled {
		// The page below routes this actor to ENROLMENT, whose round trip
		// (install app, scan QR, type code) routinely outlives the verify
		// budget — and the page promises the action will resume.
		s.StepUps.extendForEnrol(nonce)
	}

	s.render(w, r, "step_up", &pageVars{
		Title:  "Extra verification needed",
		StepUp: s.stepUpVars(state),
		StepUpChallenge: &stepUpChallengeVars{
			Nonce:       nonce,
			Action:      ch.Action,
			ActionLabel: stepUpActionLabel(ch.Action),
			Why:         stepUpActionWhy(ch.Action),
			ReturnTo:    safeReturnPath(ch.ReturnTo),
			// The Kratos login flow that actually collects the factor. aal2
			// tells Kratos to demand a SECOND factor even though the browser
			// already holds a valid aal1 session; refresh=true forces a fresh
			// credential presentation rather than silently reusing one.
			VerifyURL:       s.stepUpVerifyURL(nonce, ch.IdPKey),
			HasSecondFactor: state.Enrolled,
			EnrollURL:       s.stepUpEnrollURL(nonce, ch.IdPKey),
			PostureUnknown:  state.Err != nil,
			WindowMinutes:   int(s.Cfg.AAL2FreshWindow / time.Minute),
		},
	})
}

// stepUpVerifyURL builds the Kratos self-service login URL that collects the
// second factor and returns the browser to our redemption endpoint.
//
// StepUpReturnToBase is the console's own public origin (it previously served
// only invite-accept links — #174 Story B wires it to its named purpose).
func (s *Server) stepUpVerifyURL(nonce, idpKey string) string {
	returnTo := strings.TrimSuffix(s.Cfg.StepUpReturnToBase, "/") +
		"/step-up/return?nonce=" + url.QueryEscape(nonce) + "&idp=" + url.QueryEscape(idpKey)
	q := url.Values{
		"aal":       {aal2},
		"refresh":   {"true"},
		"return_to": {returnTo},
	}
	return s.authOrigin() + "/login?" + q.Encode()
}

// stepUpSettingsURL points an admin with NO second factor at the account
// settings page where they can enroll one (#174 Story D). Used by the header
// banner, where there is no pending challenge to return to; the interstitial
// uses stepUpEnrollURL instead so enrolment flows back into the action.
func (s *Server) stepUpSettingsURL() string { return s.authOrigin() + "/settings" }

// stepUpEnrollURL is the interstitial's enroll link: it opens the Kratos
// settings flow with a return_to back to our redemption endpoint, so an admin
// who enrolls a factor mid-challenge is carried straight back and their pinned
// action resumes (goat#247 defect 6 — the link used to be a dead end into
// /settings).
//
// Two deliberate choices:
//   - the flow is initialized at Kratos's /self-service/settings/browser
//     endpoint (Traefik routes /self-service to kratos:4433) rather than the
//     UI's /settings page, so honoring return_to does not depend on the UI
//     forwarding the query param;
//   - the return target is /step-up/return, not the interstitial: enrolling a
//     factor opens a freshness window (see handleStepUpLock's rationale), so
//     redemption succeeds and the action resumes exactly as after a verify.
//     If it ever did not, redemption re-renders the interstitial rather than
//     admitting anything.
//
// The console origin must be in Kratos's allowed_return_urls for this (and
// for stepUpVerifyURL) to work — goat#247 defect 5, fixed in the ory role
// alongside this code.
func (s *Server) stepUpEnrollURL(nonce, idpKey string) string {
	returnTo := strings.TrimSuffix(s.Cfg.StepUpReturnToBase, "/") +
		"/step-up/return?nonce=" + url.QueryEscape(nonce) + "&idp=" + url.QueryEscape(idpKey)
	return s.authOrigin() + "/self-service/settings/browser?return_to=" + url.QueryEscape(returnTo)
}

// authOrigin is the Ory self-service UI origin (auth.<site>.<zone>), derived
// from the console's own admin.auth.<site>.<zone> origin by dropping the
// leading `admin.` label. Both are rendered from the same site_id/site_dns_zone
// pair by the ansible roles, so the relationship is structural, not a guess.
func (s *Server) authOrigin() string {
	base := strings.TrimSuffix(s.Cfg.StepUpReturnToBase, "/")
	if i := strings.Index(base, "://admin."); i >= 0 {
		// Keep the scheme and everything after the `admin.` label:
		// https://admin.auth.<site>.<zone> -> https://auth.<site>.<zone>
		return base[:i+len("://")] + base[i+len("://admin."):]
	}
	return base
}

// handleStepUpReturn is the redemption leg: Kratos has (hopefully) just
// collected a second factor and bounced the browser back here with the nonce.
//
// It re-resolves the LIVE Kratos posture rather than trusting anything in the
// URL, and requires the aal2 authentication to have completed AFTER the
// challenge was issued — so an old aal2 session cannot satisfy a new gate.
func (s *Server) handleStepUpReturn(w http.ResponseWriter, r *http.Request) {
	actor := actorFromCtx(r.Context())
	nonce := r.URL.Query().Get("nonce")

	ch, ok := s.StepUps.peek(nonce)
	if !ok {
		s.renderError(w, r, 400, "That step-up request expired or was already used. Return to the page and try the action again.")
		return
	}
	if ch.ActorEmail != actor.Email {
		s.renderError(w, r, 403, "That step-up request belongs to a different operator.")
		return
	}

	state := s.resolveStepUpState(r.Context(), actor)
	if !state.Fresh || state.At.Before(ch.IssuedAt) {
		// Kratos did not record a new second factor. Keep the challenge alive
		// (do NOT redeem) and put the operator back on the interstitial with
		// an explanation, so a mistyped TOTP code is a retry, not a restart.
		if state.Err == nil && !state.Enrolled {
			// Still unenrolled — the re-rendered page offers enrolment again,
			// so it gets the enroll deadline too (fixed cap, idempotent).
			s.StepUps.extendForEnrol(nonce)
		}
		s.render(w, r, "step_up", &pageVars{
			Title:  "Extra verification needed",
			StepUp: s.stepUpVars(state),
			StepUpChallenge: &stepUpChallengeVars{
				Nonce:           nonce,
				Action:          ch.Action,
				ActionLabel:     stepUpActionLabel(ch.Action),
				Why:             stepUpActionWhy(ch.Action),
				ReturnTo:        safeReturnPath(ch.ReturnTo),
				VerifyURL:       s.stepUpVerifyURL(nonce, ch.IdPKey),
				HasSecondFactor: state.Enrolled,
				EnrollURL:       s.stepUpEnrollURL(nonce, ch.IdPKey),
				PostureUnknown:  state.Err != nil,
				WindowMinutes:   int(s.Cfg.AAL2FreshWindow / time.Minute),
				Failed:          true,
			},
		})
		return
	}

	// Verified. Burn the nonce and hand the operator back to their mutation.
	s.StepUps.redeem(nonce)

	// The banner must flip to "verified" on the very next render, not up to
	// stepUpStateTTL later — the operator just presented a factor and the
	// display copy predates it.
	s.dropStepUpCache(actor.IdentityID)

	// ReturnTo was already normalised at capture, so this is defense in depth
	// on the stored value rather than the first line of defense.
	back := safeReturnPath(ch.ReturnTo)

	if ch.Method == http.MethodGet {
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	// A POST cannot be resumed by a redirect, so render a one-button
	// auto-submitting replay of the captured form. The operator sees a
	// "resuming…" page, not a silent re-POST.
	// CSRFToken is left blank on purpose — render fills it from the live
	// server nonce, so the replayed POST carries a currently-valid token
	// rather than the (dropped) captured one.
	s.render(w, r, "step_up_resume", &pageVars{
		Title: "Resuming your action",
		StepUpResume: &stepUpResumeVars{
			ActionLabel: stepUpActionLabel(ch.Action),
			PostTo:      back,
			Fields:      ch.Form,
		},
	})
}

// --- view models + copy -------------------------------------------------

// stepUpVars drives the persistent header banner on every console page: is the
// operator currently step-up-fresh, and for how long. Making the window
// visible is what stops "I already did that" confusion when it lapses
// mid-task.
type stepUpVars struct {
	Enforced        bool
	Fresh           bool
	HasSecondFactor bool
	RemainingSec    int
	WindowMinutes   int
	EnrollURL       string
	// Locked is set when the actor ended their own window via "Lock now", so
	// the strip can say so instead of the ambiguous "not verified recently".
	Locked bool
	// PostureUnknown is set when the posture could not be established at all
	// (state.Err). The strip must say so rather than render the enroll nudge —
	// "couldn't check" and "not enrolled" are different facts (goat#247).
	PostureUnknown bool
}

type stepUpChallengeVars struct {
	Nonce           string
	Action          string
	ActionLabel     string
	Why             string
	ReturnTo        string
	VerifyURL       string
	EnrollURL       string
	HasSecondFactor bool
	// PostureUnknown supersedes HasSecondFactor in the template: when the
	// lookup failed, neither the verify nor the enroll branch is truthful.
	PostureUnknown bool
	WindowMinutes  int
	Failed         bool
}

type stepUpResumeVars struct {
	ActionLabel string
	PostTo      string
	Fields      url.Values
}

func (s *Server) stepUpVars(state stepUpState) *stepUpVars {
	return &stepUpVars{
		Enforced:        s.Cfg.StepUpEnforced,
		Fresh:           state.Fresh,
		HasSecondFactor: state.Enrolled,
		RemainingSec:    int(state.Remaining() / time.Second),
		WindowMinutes:   int(s.Cfg.AAL2FreshWindow / time.Minute),
		EnrollURL:       s.stepUpSettingsURL(),
		Locked:          state.Locked,
		PostureUnknown:  state.Err != nil,
	}
}

// stepUpActionLabel and stepUpActionWhy turn a matrix action name into copy an
// operator can act on. The console already knows WHICH action is gated; the
// operator needs to know what it does and why it is being singled out.
//
// Unknown actions get a truthful generic — the gate must never be blocked on a
// missing translation.
func stepUpActionLabel(action string) string {
	switch action {
	case "delete_identity":
		return "Delete identity"
	case "revoke_sessions":
		return "Revoke all sessions"
	case "create_identity":
		return "Create an admin identity"
	case "patch_state":
		return "Deactivate an admin"
	case "mint_invite":
		return "Mint an admin invite"
	case "reset_2fa":
		return "Reset second factor"
	case "grant_ADMINS", "grant_PLATFORM-ADMIN":
		return "Grant platform-admin"
	case "revoke_ADMINS", "revoke_PLATFORM-ADMIN":
		return "Revoke platform-admin"
	}
	return "This action"
}

func stepUpActionWhy(action string) string {
	switch action {
	case "delete_identity":
		return "Deleting an identity is irreversible and removes the person's access everywhere at once."
	case "revoke_sessions":
		return "Revoking sessions signs the person out of every device immediately."
	case "create_identity", "mint_invite":
		return "This creates an account that carries administrative privilege."
	case "patch_state":
		return "Deactivating an admin removes an administrator from service."
	case "reset_2fa":
		return "Clearing someone's second factor lowers their account security until they enroll a new one."
	case "grant_ADMINS", "grant_PLATFORM-ADMIN":
		return "Granting platform-admin gives the person full control of this environment."
	case "revoke_ADMINS", "revoke_PLATFORM-ADMIN":
		return "Revoking platform-admin removes an administrator's control of this environment."
	}
	return "This action is one of the highest-impact operations in this console."
}
