package handlers

// The Mesh holds page (/holds, goat#1042): every peer currently on hold on
// this goatnet, read-only, so an admin told "someone is locked out" can find
// who without shell access to the management host. It reuses the reader the
// member page's Access section built (internal/meshstate: record loader,
// read-only NetBird peer client, grading rules) — there is ONE reader of the
// hold file — and the roster join the Users page uses to link a handle to
// its console page. No buttons, no NetBird write token, nothing credential-
// shaped in the response; the Release button (goat#1044) lands in the
// trailing empty action cell later.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/meshstate"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

// holdKindLabel / holdStateLabel / holdKindClass / holdStateClass back the
// "hold_badges" template define. The label tables live in meshstate beside
// the vocabulary; the classes map onto the console's existing badge styles
// (dormancy on the amber `warn`, security on the red `revoked`). Labels
// always carry the word — color is never the only signal.
func holdKindLabel(kind string) string {
	if l, ok := meshstate.HoldKindLabel[kind]; ok {
		return l
	}
	return kind
}

func holdStateLabel(state string) string {
	if l, ok := meshstate.HoldStateLabel[state]; ok {
		return l
	}
	return state
}

func holdKindClass(kind string) string {
	switch kind {
	case meshstate.HoldKindDormancy:
		return "warn"
	case meshstate.HoldKindSecurity:
		return "revoked"
	}
	return "used"
}

func holdStateClass(state string) string {
	switch state {
	case meshstate.HoldStateReleasing:
		return "info"
	case meshstate.HoldStateStale, meshstate.HoldStateInconsistent, meshstate.HoldStateOrphan:
		return "warn"
	}
	return ""
}

// holdRowView is one table row: the graded hold plus the member it is
// attributed to, joined through the roster (handle → active proposal's
// email → Kratos identity on the default plane).
type holdRowView struct {
	meshstate.HoldRow
	// MemberID is the Kratos identity id the Member cell links to; empty
	// when the row is not attributed or the directory did not answer.
	MemberID       string
	OrgShortTag    string
	OrgDisplayName string
	// NoLoginReason says why an attributed handle has no member link when
	// the directory DID answer (holdsLoginJoin.loginFor words the causes:
	// no login carries its email, the newest onboarding of its login moved to
	// another goatnet or another handle, a machine-door proposal with no
	// email, onboarded on other goatnets only, no active record). Empty
	// when MemberID is set, the row is unattributed, or the directory
	// failed (the banner says so).
	NoLoginReason string
	// EnteredAtDate is the record's entered_at as a UTC date; empty with
	// no record.
	EnteredAtDate string
	// EnteredAtUnix / InactiveSortKey feed the client-side column sorts.
	EnteredAtUnix int64
	// FilterText is the space-joined haystack the text filter matches:
	// handle, org display name and short tag, device name, peer ID.
	FilterText string
	ReleaseURL string
}

// holdsPage is the template-facing shape of /holds.
type holdsPage struct {
	meshstate.HoldsView
	Rows []holdRowView
	// DirectoryError names why the identity directory (Kratos) could not be
	// read: handles still render, but without a link or an organisation.
	DirectoryError string
	// CheckedAtText is the read time in UTC.
	CheckedAtText   string
	Authority       membershipAuthority
	GroupsError     string
	ReleaseQueued   bool
	ReleaseOutcome  string
	ReleaseClass    string
	ReleasePeer     string
	ReleaseFinished string
}

func releaseOutcomePresentation(outcome string) (label, class string) {
	switch outcome {
	case "released":
		return "released", "success"
	case "already_released":
		return "already released", "success"
	case "refused":
		return "refused", "error"
	case "retryable_partial":
		return "retryable partial progress", "warn"
	default:
		return "", ""
	}
}

// ignoreIdP strips an UNKNOWN identity-plane selector before the admin
// gate resolves one: holds are a mesh fact and the page is per goatnet, so
// an `?idp=` on the URL must neither change the page nor refuse it. A
// known selector is left for the gate so the nav keeps the admin's plane
// (Users / Invites link back to the plane they came from); the page's own
// data never reads it.
func (s *Server) ignoreIdP(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if k := r.URL.Query().Get("idp"); r.URL.Query().Has("idp") && !s.knownIdP(k) {
			q := r.URL.Query()
			q.Del("idp")
			r2 := r.Clone(r.Context())
			u := *r.URL
			u.RawQuery = q.Encode()
			r2.URL = &u
			r = r2
		}
		next(w, r)
	}
}

// knownIdP reports whether key names a configured identity plane.
func (s *Server) knownIdP(key string) bool {
	if key == "" {
		return false
	}
	_, ok := s.Cfg.IdP(key)
	return ok
}

func (s *Server) handleHolds(w http.ResponseWriter, r *http.Request) {
	page := s.resolveHolds(r.Context())
	page.ReleaseQueued = r.URL.Query().Get("release") == "queued"
	site := page.Site
	if site == "" {
		site = "(goatnet unset)"
	}
	s.render(w, r, "holds", &pageVars{
		Title: "Mesh holds — " + site,
		Holds: page,
	})
}

// resolveHolds reads the three upstreams (hold-record file, live mesh,
// handle roster), grades every hold, and joins the attributed handles to
// their console identities. Each upstream fails on its own and the page
// says which one; nothing here refuses the page.
func (s *Server) resolveHolds(ctx context.Context) *holdsPage {
	now := time.Now().UTC()
	site := strings.TrimSpace(s.Cfg.NotifyGoatnet)
	if site == "" {
		// Without a goatnet nothing can be graded: no record is this
		// site's and no hold group is read. Say so on both banners rather
		// than render a confident "nobody is on hold", and make no call.
		err := fmt.Errorf("NOTIFY_GOATNET is unset, so this console does not know which goatnet's holds to read")
		hv := meshstate.ResolveHolds(meshstate.HoldsInput{RecordsErr: err, PeersErr: err, Now: now})
		return &holdsPage{HoldsView: hv, CheckedAtText: now.Format("2006-01-02 15:04:05 UTC")}
	}

	// Handle roster: the same full-proposal listing KnownHandles reads,
	// kept here so the handle → email map comes from the one read. A
	// failure means nothing is attributed, and the page says so.
	known := map[string]struct{}{}
	var join holdsLoginJoin
	var knownErr error
	if s.Onboard == nil {
		knownErr = fmt.Errorf("onboarding store is not configured")
	} else if all, err := s.Onboard.ListProposals(ctx, ""); err != nil {
		knownErr = fmt.Errorf("list handles: %w", err)
	} else {
		for _, p := range all {
			if h := strings.TrimSpace(p.Handle); h != "" {
				known[h] = struct{}{}
			}
		}
		join = newHoldsLoginJoin(all, site)
	}

	// Hold records: the one reader (meshstate.LoadHoldRecords).
	var (
		records meshstate.Records
		recErr  error
	)
	path := strings.TrimSpace(s.Cfg.HoldRecordsPath)
	if path == "" {
		recErr = fmt.Errorf("HOLD_RECORDS_PATH is unset")
	} else {
		records, recErr = meshstate.LoadHoldRecords(path)
		if recErr != nil {
			recErr = fmt.Errorf("load hold records from %s: %w", path, recErr)
		} else if records.Skipped > 0 {
			slog.WarnContext(ctx, "hold-records file has bad lines; skipped them, the affected peers read as no-record",
				slog.String("path", path),
				slog.Int("skipped", records.Skipped),
				slog.Any("lines", records.SkippedLines))
		}
	}

	// Live mesh: read-only peer list on the enrollment/Access token.
	var (
		peers    []meshstate.Peer
		peersErr error
	)
	mgmtURL := strings.TrimSpace(s.Cfg.NotifyMgmtURL)
	token := strings.TrimSpace(s.Cfg.EnrollWatchToken)
	if mgmtURL == "" || token == "" {
		cause := "NOTIFY_MGMT_URL is unset"
		if mgmtURL != "" {
			cause = "ENROLL_WATCH_NB_TOKEN is unset"
		}
		peersErr = fmt.Errorf("NetBird mesh state unavailable — %s", cause)
	} else {
		client := &meshstate.Client{MgmtURL: mgmtURL, Token: token}
		peers, peersErr = client.Peers(ctx)
		if peersErr != nil {
			slog.WarnContext(ctx, "Holds page NetBird peer list failed; rendering mesh state as unavailable",
				slog.String("err", peersErr.Error()))
		}
	}

	hv := meshstate.ResolveHolds(meshstate.HoldsInput{
		Site:       site,
		Known:      known,
		KnownErr:   knownErr,
		Records:    records,
		RecordsErr: recErr,
		Peers:      peers,
		PeersErr:   peersErr,
		Now:        now,
	})

	page := &holdsPage{HoldsView: hv, CheckedAtText: now.Format("2006-01-02 15:04:05 UTC"), Authority: s.membershipAuthority()}
	if s.Onboard != nil {
		if actions, err := s.Onboard.ListActions(ctx, ""); err != nil {
			slog.WarnContext(ctx, "latest hold-release result unavailable", slog.String("err", err.Error()))
		} else {
			for i := len(actions) - 1; i >= 0; i-- {
				a := actions[i]
				if a.Kind != onboardstore.ActionReleaseHold || a.ResultJSON == "" || a.FinishedAt == nil {
					continue
				}
				finished := time.Unix(*a.FinishedAt, 0).UTC()
				if finished.Before(now.Add(-24*time.Hour)) || finished.After(now) {
					continue
				}
				var result struct {
					Release struct {
						Outcome string `json:"outcome"`
					} `json:"release"`
				}
				var params releaseHoldParams
				if json.Unmarshal([]byte(a.ResultJSON), &result) == nil && json.Unmarshal([]byte(a.Params), &params) == nil {
					page.ReleaseOutcome, page.ReleaseClass = releaseOutcomePresentation(result.Release.Outcome)
					page.ReleasePeer = strings.TrimSpace(params.PeerName)
					if page.ReleasePeer == "" {
						page.ReleasePeer = params.PeerID
					}
					page.ReleaseFinished = finished.Format("2006-01-02 15:04:05 UTC")
				}
				break
			}
		}
	}
	knownGroups := map[string]bool{}
	if peersErr == nil {
		cli := &meshstate.Client{MgmtURL: mgmtURL, Token: token}
		if groups, err := cli.Groups(ctx); err != nil {
			page.GroupsError = err.Error()
		} else {
			for _, g := range groups {
				knownGroups[strings.ToLower(strings.TrimSpace(g.Name))] = true
			}
		}
	}

	// Directory join, only when some row has a handle to link. Onboarding
	// mints every login on the tenant plane (tenantIdPKey), so that is the
	// plane the handles resolve on; the `idp` selector is ignored by design.
	needDirectory := false
	for _, row := range hv.Rows {
		if row.Handle != "" {
			needDirectory = true
			break
		}
	}
	type member struct{ id, tag, name string }
	byEmail := map[string]member{}
	if needDirectory {
		cli := s.Kratos[tenantIdPKey]
		if cli == nil {
			page.DirectoryError = "identity directory is not configured for the tenant plane"
		} else if ids, err := cli.ListIdentities(ctx); err != nil {
			page.DirectoryError = "identity directory unavailable: " + err.Error()
			slog.WarnContext(ctx, "Holds page identity list failed; member links omitted", slog.String("err", err.Error()))
		} else {
			for _, i := range ids {
				tag, name := i.Org()
				byEmail[strings.ToLower(strings.TrimSpace(i.Email()))] = member{id: i.ID, tag: tag, name: name}
			}
		}
	}

	page.Rows = make([]holdRowView, 0, len(hv.Rows))
	for _, row := range hv.Rows {
		v := holdRowView{HoldRow: row}
		if page.Authority.AllowsLiveRelease() && page.GroupsError == "" && row.HasRecord && row.Kind == meshstate.HoldKindDormancy &&
			(row.State == meshstate.HoldStateHeld || row.State == meshstate.HoldStateReleasing ||
				(row.State == meshstate.HoldStateStale && equalGroupSet(row.LiveGroups, row.PriorGroups))) {
			allKnown := true
			for _, g := range row.PriorGroups {
				if !knownGroups[strings.ToLower(strings.TrimSpace(g))] {
					allKnown = false
				}
			}
			if allKnown {
				q := url.Values{"entered_at": []string{strconv.FormatInt(row.EnteredAt.Unix(), 10)}}
				v.ReleaseURL = "/holds/" + url.PathEscape(row.PeerID) + "/release?" + q.Encode()
			}
		}
		if !row.EnteredAt.IsZero() {
			v.EnteredAtDate = row.EnteredAt.UTC().Format(time.DateOnly)
			v.EnteredAtUnix = row.EnteredAt.Unix()
		}
		if row.Handle != "" {
			email, reason := join.loginFor(row.Handle)
			if m, ok := byEmail[email]; ok && email != "" {
				v.MemberID = m.id
				v.OrgShortTag = m.tag
				v.OrgDisplayName = m.name
			} else if page.DirectoryError == "" {
				if reason == "" {
					reason = "onboarded on this goatnet, no login carries its email"
				}
				v.NoLoginReason = reason
			}
		}
		v.FilterText = strings.ToLower(strings.Join([]string{row.Handle, v.OrgDisplayName, v.OrgShortTag, row.PeerName, row.PeerID}, " "))
		page.Rows = append(page.Rows, v)
	}
	return page
}

// holdsLoginJoin is the handle → login-email side of the roster join, built
// once per page from the full proposal listing. It runs the member page's
// derivation rule (resolveAccess: ActiveProposalForEmail, i.e. the newest
// active proposal for that email across EVERY goatnet, which must then be
// on THIS goatnet and carry the handle) in reverse, so a link from this page
// never lands on a member page that disowns the handle.
type holdsLoginJoin struct {
	site string
	// newestByEmail is the proposal ActiveProposalForEmail would return for
	// each lower-cased email: newest active, any goatnet.
	newestByEmail map[string]onboardstore.Proposal
	// hereByHandle is every active proposal on this goatnet filed under the
	// handle, newest first; activeEnvs is every goatnet the handle has an
	// active proposal on. Both drive the wording when there is no link.
	hereByHandle map[string][]onboardstore.Proposal
	activeEnvs   map[string]map[string]struct{}
}

func newHoldsLoginJoin(all []onboardstore.Proposal, site string) holdsLoginJoin {
	j := holdsLoginJoin{
		site:          site,
		newestByEmail: map[string]onboardstore.Proposal{},
		hereByHandle:  map[string][]onboardstore.Proposal{},
		activeEnvs:    map[string]map[string]struct{}{},
	}
	for _, p := range all {
		switch p.Status {
		case onboardstore.ProposalRejected, onboardstore.ProposalFailed, onboardstore.ProposalOffboarded:
			continue // the store's "active" definition
		}
		h := strings.TrimSpace(p.Handle)
		env := strings.TrimSpace(p.Env)
		email := strings.ToLower(strings.TrimSpace(p.Email))
		if email != "" {
			// Strictly newer only: ListProposals and ActiveProposalForEmail
			// share ORDER BY created_at DESC, id DESC, so on an equal
			// created_at the store's LIMIT 1 is the FIRST row in this order,
			// and so must the join's pick be.
			if cur, ok := j.newestByEmail[email]; !ok || p.CreatedAt > cur.CreatedAt {
				j.newestByEmail[email] = p
			}
		}
		if h == "" {
			continue
		}
		if env != "" {
			if j.activeEnvs[h] == nil {
				j.activeEnvs[h] = map[string]struct{}{}
			}
			j.activeEnvs[h][env] = struct{}{}
		}
		if env == site {
			j.hereByHandle[h] = append(j.hereByHandle[h], p)
		}
	}
	for h := range j.hereByHandle {
		ps := j.hereByHandle[h]
		sort.SliceStable(ps, func(a, b int) bool { return ps[a].CreatedAt > ps[b].CreatedAt })
	}
	return j
}

// loginFor returns the email whose login the handle's Member cell links to,
// or "" and the reason there is none. A reason is returned only when the
// join itself rules the link out; an email that the directory then does
// not know is the caller's case to word. The wordings are the causes the
// Release action (goat#1044) will have to tell apart.
func (j holdsLoginJoin) loginFor(handle string) (email, reason string) {
	here := j.hereByHandle[handle]
	var movedTo []string
	sawEmail, otherHandle, noHandle := false, false, false
	for _, p := range here {
		e := strings.ToLower(strings.TrimSpace(p.Email))
		if e == "" {
			continue
		}
		sawEmail = true
		newest := j.newestByEmail[e]
		newestHandle := strings.TrimSpace(newest.Handle)
		if strings.TrimSpace(newest.Env) == j.site && newestHandle == handle {
			return e, ""
		}
		switch env := strings.TrimSpace(newest.Env); {
		case env != j.site && env != "":
			movedTo = append(movedTo, env)
		case newestHandle == "":
			noHandle = true
		default:
			otherHandle = true
		}
	}
	switch {
	case len(movedTo) > 0:
		sort.Strings(movedTo)
		return "", "onboarded on this goatnet, but the newest onboarding of its login is on " + strings.Join(movedTo, ", ") + ", so the member page would not derive this handle"
	case otherHandle:
		// An email's newest proposal is on this goatnet under ANOTHER
		// handle: the login belongs to that handle now.
		return "", "onboarded on this goatnet, but its login now derives another handle"
	case noHandle:
		// The newest proposal is on this goatnet but carries no handle
		// (machine door): the login derives NO handle, not another one.
		return "", "onboarded on this goatnet, but the newest onboarding of its login carries no handle (machine door), so the member page would not derive this handle"
	case sawEmail:
		return "", "onboarded on this goatnet, but its login now derives another handle"
	case len(here) > 0:
		return "", "onboarded on this goatnet without an email (machine door), no login to link"
	}
	var others []string
	for e := range j.activeEnvs[handle] {
		if e != j.site {
			others = append(others, e)
		}
	}
	if len(others) > 0 {
		sort.Strings(others)
		return "", "onboarded on " + strings.Join(others, ", ") + " only, not this goatnet"
	}
	return "", "no active onboarding record"
}
