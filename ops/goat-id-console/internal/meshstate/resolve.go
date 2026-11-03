package meshstate

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ResolveInput is the pure-data input to Resolve for one
// member. It carries the derived handle + site, the name
// ownership universe, the hold-record set or its load error,
// and the live NetBird peers/setup-keys or their fetch errors.
type ResolveInput struct {
	Handle string
	Site   string
	Known  map[string]struct{}
	// KnownErr is set when the roster behind Known could not be read
	// in full; Known is then a partial set and name ownership is
	// weaker. Surfaced on the view, never hidden.
	KnownErr error

	Records    Records
	RecordsErr error

	Peers    []Peer
	PeersErr error

	Keys    []SetupKey
	KeysErr error

	Now time.Time
}

// AccessView is the template-facing shape of the Access section
// on the user-detail page. It reports section-level availability
// plus per-peer and per-setup-key verdicts.
type AccessView struct {
	Handle string
	Site   string

	// HandleError names why a handle could not be derived for
	// this identity (no proposal row, wrong env, store
	// unavailable, ...). When non-empty, Resolve is not called
	// and the Access section renders this explanation instead of
	// any hold/mesh verdicts.
	HandleError string

	// KnownHandlesError names why the roster used to disambiguate
	// peer and key names could not be read in full. Non-empty means
	// a name may be credited to a handle that merely prefixes it.
	KnownHandlesError string

	// Hold records half. RecordsSkipped counts bad lines the loader
	// skipped (ops/lifecycle/README.md "A bad line is skipped, never
	// fatal, and always counted"); RecordsSkippedLines names them.
	// A skipped line leaves its peer with no record, which reads as a
	// security hold, so the section shows the count whenever nonzero.
	RecordsAvailable    bool
	RecordsError        string
	RecordsSkipped      int
	RecordsSkippedLines []int

	// Live mesh half (peers + groups).
	MeshAvailable bool
	MeshError     string

	// Setup-key half. KeysAvailable false ⇒ the key facts are
	// unavailable and NoKeyForHandle is not asserted.
	KeysAvailable bool
	KeysError     string

	// Inconsistent marks contradictions the truth table names as
	// "investigate": duplicate hold records, record vs mesh
	// disagreement, a held peer whose live groups contradict the
	// record, or a peer in more than one hold group at once.
	Inconsistent       bool
	InconsistentDetail string

	// Per-peer verdicts for the member's peers.
	Peers []PeerView

	// Per-setup-key verdicts for keys owned by this handle.
	Keys []KeyView
	// NoKeyForHandle is true when KeysAvailable is true and no
	// setup key belongs to this handle.
	NoKeyForHandle bool
}

// PeerView summarizes the hold/mesh posture of one peer.
type PeerView struct {
	ID   string
	Name string

	Connected bool
	LastSeen  string
	// LastSeenUnreadable means the API supplied a timestamp that could not
	// be parsed. It is distinct from a peer that has never been seen.
	LastSeenUnreadable bool
	Groups             []string

	// Hold-record half, when present.
	HasRecord    bool
	Lane         string
	Phase        string // "releasing" while a release is in progress
	HoldGroup    string
	PriorGroups  []string
	Actor        string
	Reason       string
	EnteredAt    string
	DaysOnHold   string
	DaysInactive string // "unknown" when last_seen is absent

	// Verdict labels are small enums rendered as badges in the
	// template: "on_hold", "releasing", "on_hold_no_record",
	// "in_hold_group", "not_on_hold", "inconsistent". VerdictDetail
	// carries the human-facing explainer ("record says held, mesh
	// disagrees", "duplicate hold records", ...).
	Verdict       string
	VerdictDetail string

	// Kind and State are the shared hold vocabulary (HoldKind* /
	// HoldState*), rendered by the "hold_badges" template define on this
	// page and on the Holds page from one label table. Both empty for a
	// peer that is not on hold.
	Kind       string
	State      string
	ReleaseURL string
}

// releasingDetail is the explainer for a record whose release is in
// progress: the schema says a reader treats it as a release to finish,
// never as a stranded hold, so the page names the command instead of
// grading the live shape.
const releasingDetail = "release in progress; the live groups are mid-change — finish it by running the release again (lifecycle-sweep.py --apply --lane release --peer <id>)"

// releasingSecurityDetail replaces releasingDetail for a security
// record: the sweep refuses every security record whatever its phase
// (ADR 1091), so naming its release command would send the admin to a
// refusal.
const releasingSecurityDetail = "release in progress on a security hold; the live groups are mid-change — the sweep never releases a security hold (ADR 1091), so finish it through the containment's own close-out"

// KeyView summarizes one setup key owned by this handle.
type KeyView struct {
	ID        string
	Name      string
	UsedTimes int

	// Status is one of "valid", "revoked", "expired",
	// "expiry unreadable" (the API sent an expiry that did not
	// parse, so validity is unknown and is never inferred as valid),
	// "invalid" (fallback when Valid is false with no expiry).
	Status string
	// ExpiresAt is the expiry timestamp when present.
	ExpiresAt string
}

// Resolve assembles the AccessView for one member from the hold
// records and live NetBird state, applying the exhaustive
// verdict table from the story spec. Pure function — it performs
// no I/O.
func Resolve(in ResolveInput) AccessView {
	av := AccessView{
		Handle: in.Handle,
		Site:   in.Site,
	}

	// Helper to accumulate distinct inconsistent details so the
	// section-level banner can name every cause observed across a
	// member's peers.
	inconsistentDetails := map[string]struct{}{}
	addInconsistentDetail := func(detail string) {
		detail = strings.TrimSpace(detail)
		if detail == "" {
			return
		}
		if _, ok := inconsistentDetails[detail]; ok {
			return
		}
		inconsistentDetails[detail] = struct{}{}
		if av.InconsistentDetail == "" {
			av.InconsistentDetail = detail
		} else {
			av.InconsistentDetail = av.InconsistentDetail + "; " + detail
		}
	}
	// Section-level availability flags.
	if in.KnownErr != nil {
		// Without the full roster, OwnsName is a bare prefix test and
		// dave's page would show dave-2's peers, records and keys
		//. Attributing under that rule with a caveat is still
		// a wrong answer on a surface used to decide a release, so
		// nothing is attributed: the live halves render unavailable
		// and the section says why.
		av.KnownHandlesError = in.KnownErr.Error()
		cause := "handle roster unavailable, so peers and keys cannot be attributed to this member safely: " + in.KnownErr.Error()
		in.PeersErr = errors.New(cause)
		in.KeysErr = errors.New(cause)
	}
	if in.RecordsErr != nil {
		av.RecordsAvailable = false
		av.RecordsError = in.RecordsErr.Error()
	} else {
		av.RecordsAvailable = true
		av.RecordsSkipped = in.Records.Skipped
		av.RecordsSkippedLines = append([]int(nil), in.Records.SkippedLines...)
	}
	if in.PeersErr != nil {
		av.MeshAvailable = false
		av.MeshError = in.PeersErr.Error()
	} else {
		av.MeshAvailable = true
	}
	if in.KeysErr != nil {
		av.KeysAvailable = false
		av.KeysError = in.KeysErr.Error()
	} else {
		av.KeysAvailable = true
	}

	// Setup-key half is independent of the hold/peer verdicts.
	if av.KeysAvailable {
		av.Keys, av.NoKeyForHandle = resolveKeys(in.Handle, in.Known, in.Keys, in.Now)
	}

	// When both records and mesh are unavailable, we can say
	// nothing about holds; AccessView still carries the
	// unavailable reasons and any key facts above.
	if !av.RecordsAvailable && !av.MeshAvailable {
		return av
	}

	// Build the peer universe keyed by ID, and the member's own
	// peers keyed by ID, when mesh is available.
	peersByID := map[string]Peer{}
	memberPeers := []Peer{}
	memberPeerIDs := map[string]struct{}{}
	if av.MeshAvailable {
		for _, p := range in.Peers {
			peersByID[p.ID] = p
			if OwnsName(p.Name, in.Handle, in.Known) {
				memberPeers = append(memberPeers, p)
				memberPeerIDs[p.ID] = struct{}{}
			}
		}
	}

	site := strings.TrimSpace(in.Site)

	// Helper to append a PeerView to av.Peers, de-duplicating by
	// peer id so a record and a no-record verdict do not both
	// create rows for the same peer.
	addPeerView := func(pv PeerView) {
		for i := range av.Peers {
			if av.Peers[i].ID == pv.ID && pv.ID != "" {
				av.Peers[i] = pv
				return
			}
		}
		av.Peers = append(av.Peers, pv)
	}

	// Record-driven verdicts when the file is available. A record
	// belongs on this member's page when its peer_id is one of the
	// member's own peers. The record's `handle` field is never the
	// key: the sweep writes it only when the peer name is unambiguous
	// and null otherwise, and a hand-edited or foreign-writer line can
	// carry any handle — matching on it would put one member's hold
	// on another member's page. One exception, the same one the sweep
	// makes: a record whose peer_id is no live peer at all (deleted,
	// or an old enrolment's id) is attributed by peer_name under the
	// roster-disambiguated ownership rule (OwnsName), so an orphan
	// record still surfaces on the right page as a contradiction.
	// When the mesh is unavailable there is no peer list to own
	// against, so no record is attributed and the section reads
	// "mesh state unavailable" rather than a hold credited by name.
	var recordsForMember []HoldRecord
	if av.RecordsAvailable && av.MeshAvailable {
		for _, r := range in.Records.All {
			if !sameSite(site, r.Site) {
				continue
			}
			if _, ok := memberPeerIDs[r.PeerID]; ok {
				recordsForMember = append(recordsForMember, r)
				continue
			}
			if _, live := peersByID[r.PeerID]; !live && OwnsName(r.PeerName, in.Handle, in.Known) {
				recordsForMember = append(recordsForMember, r)
			}
		}
	}

	// First: record-backed verdicts. A peer_id on more than one line is
	// graded from its FIRST line, the one the sweep reads, marks and
	// deletes (_lookup_hold_record in ops/scripts/lifecycle-sweep.py), as
	// on the Holds page.
	recordSeen := map[string]bool{}
	for _, r := range recordsForMember {
		if recordSeen[r.PeerID] {
			continue
		}
		recordSeen[r.PeerID] = true
		pv := PeerView{
			ID:          r.PeerID,
			Name:        r.PeerName,
			HasRecord:   true,
			Lane:        r.Lane,
			HoldGroup:   r.HoldGroup,
			PriorGroups: append([]string(nil), r.PriorGroups...),
			Actor:       r.Actor,
			Reason:      r.Reason,
			EnteredAt:   r.EnteredAt.UTC().Format(time.RFC3339),
		}
		if !in.Now.IsZero() {
			dh := in.Now.Sub(r.EnteredAt).Hours() / 24
			if dh < 0 {
				dh = 0
			}
			pv.DaysOnHold = fmt.Sprintf("%.1f", dh)
		}

		pv.Phase = r.Phase
		pv.Kind = r.Lane

		// Orphan record: attributed by peer_name ownership above,
		// its peer is not on the mesh. Duplicate record lines outrank
		// the orphan grade, as they do on the Holds page, so one peer
		// reads the same on both surfaces.
		peer, ok := peersByID[r.PeerID]
		if !ok {
			av.Inconsistent = true
			detail := "record names a peer the mesh does not list"
			pv.State = HoldStateOrphan
			if in.Records.DuplicatePeerIDs[r.PeerID] {
				detail = "duplicate hold records; " + detail
				pv.State = HoldStateInconsistent
			}
			addInconsistentDetail(detail)
			pv.Verdict = "inconsistent"
			pv.VerdictDetail = detail
			addPeerView(pv)
			continue
		}

		// Mesh side of this peer.
		pv.Connected = peer.Connected
		pv.LastSeenUnreadable = peer.LastSeenUnreadable
		if peer.LastSeenUnreadable {
			pv.DaysInactive = ""
		} else if !peer.LastSeen.IsZero() {
			pv.LastSeen = peer.LastSeen.UTC().Format(time.RFC3339)
			if !in.Now.IsZero() {
				di := in.Now.Sub(peer.LastSeen).Hours() / 24
				if di < 0 {
					di = 0
				}
				pv.DaysInactive = fmt.Sprintf("%.1f", di)
			}
		} else {
			pv.DaysInactive = "unknown"
		}
		gNames := peerGroupNames(peer.Groups)
		pv.Groups = gNames

		upperSite := strings.ToUpper(site)
		holdPresent := containsGroup(gNames, r.HoldGroup)
		others := otherSiteGroups(upperSite, gNames)
		// A second hold group beside the record's own is a
		// contradiction too: QUARANTINED on top of a PROBATION hold
		// (or the reverse) is not a clean single hold.
		var extraHolds []string
		for _, g := range holdGroupsOf(upperSite, gNames) {
			if !strings.EqualFold(g, strings.TrimSpace(r.HoldGroup)) {
				extraHolds = append(extraHolds, g)
			}
		}
		unlisted := unlistedGroups(gNames, r)

		switch {
		case in.Records.DuplicatePeerIDs[r.PeerID]:
			av.Inconsistent = true
			addInconsistentDetail("duplicate hold records")
			pv.Verdict = "inconsistent"
			pv.VerdictDetail = "duplicate hold records"
			pv.State = HoldStateInconsistent
		case len(extraHolds) > 0:
			// Checked before a release in progress: a containment placed
			// on top of a half-done release is one the sweep refuses to
			// finish (it would restore role groups under a live
			// containment), so the page must not offer the re-run.
			detail := "peer in more than one hold group: " + strings.Join(append([]string{strings.TrimSpace(r.HoldGroup)}, extraHolds...), ", ")
			if !holdPresent {
				detail = "record says " + strings.TrimSpace(r.HoldGroup) + ", peer in other hold group(s): " + strings.Join(extraHolds, ", ")
			}
			if len(others) > 0 {
				detail += "; other site groups also present: " + strings.Join(others, ", ")
			}
			if r.Phase == "releasing" {
				detail += "; a release is in progress and the sweep refuses to finish it while another hold group is present"
			}
			av.Inconsistent = true
			addInconsistentDetail(detail)
			pv.Verdict = "inconsistent"
			pv.VerdictDetail = detail
			pv.State = HoldStateInconsistent
		case r.Phase == "releasing" && holdPresent && len(unlisted) > 0:
			// Mid-release the live shape is mixed, but only with the
			// record's own groups. A group the record never listed,
			// beside a hold group still present, is one the sweep
			// refuses to resume past ("resolve by hand").
			detail := "release in progress, but the peer also holds group(s) its record never listed: " + strings.Join(unlisted, ", ") + "; the sweep refuses to finish it"
			av.Inconsistent = true
			addInconsistentDetail(detail)
			pv.Verdict = "inconsistent"
			pv.VerdictDetail = detail
			pv.State = HoldStateInconsistent
		case r.Phase == "releasing":
			// Mid-release the live shape is legitimately mixed
			// (prior groups back, hold group not yet removed, or
			// the reverse), so it is not graded: the answer is
			// "finish the release", not "investigate". The sweep
			// never releases a security hold (ADR 1091), so that
			// lane is pointed at its own close-out, not the sweep.
			pv.Verdict = "releasing"
			pv.VerdictDetail = releasingDetail
			if r.Lane == "security" {
				pv.VerdictDetail = releasingSecurityDetail
			}
			pv.State = HoldStateReleasing
		case holdPresent && len(others) > 0:
			// A held peer must hold ONLY the hold group on this
			// site. Any other live site group — a prior one that
			// was never removed, or one added after the hold —
			// is a contradiction, not a side to pick.
			detail := "hold group and other site groups both present: " + strings.Join(others, ", ")
			av.Inconsistent = true
			addInconsistentDetail(detail)
			pv.Verdict = "inconsistent"
			pv.VerdictDetail = detail
			pv.State = HoldStateInconsistent
		case holdPresent:
			pv.Verdict = "on_hold"
			pv.State = HoldStateHeld
		default:
			av.Inconsistent = true
			addInconsistentDetail("record says held, mesh disagrees")
			pv.Verdict = "inconsistent"
			pv.VerdictDetail = "record says held, mesh disagrees"
			pv.State = HoldStateStale
		}

		addPeerView(pv)
	}

	// Second: no-record verdicts driven by live mesh state when
	// the mesh is available.
	if av.MeshAvailable {
		upperSite := strings.ToUpper(strings.TrimSpace(site))
		for _, p := range memberPeers {
			// Skip peers already covered by a record row.
			if hasRecordForPeer(recordsForMember, p.ID) {
				continue
			}
			gNames := peerGroupNames(p.Groups)
			holdGroups := holdGroupsOf(upperSite, gNames)
			holdGroupName := ""
			if len(holdGroups) > 0 {
				holdGroupName = holdGroups[0]
			}

			pv := PeerView{
				ID:        p.ID,
				Name:      p.Name,
				Connected: p.Connected,
				Groups:    gNames,
			}
			pv.LastSeenUnreadable = p.LastSeenUnreadable
			if p.LastSeenUnreadable {
				pv.DaysInactive = ""
			} else if !p.LastSeen.IsZero() {
				pv.LastSeen = p.LastSeen.UTC().Format(time.RFC3339)
				if !in.Now.IsZero() {
					di := in.Now.Sub(p.LastSeen).Hours() / 24
					if di < 0 {
						di = 0
					}
					pv.DaysInactive = fmt.Sprintf("%.1f", di)
				}
			} else {
				pv.DaysInactive = "unknown"
			}

			switch {
			case len(holdGroups) == 0:
				// Only render "not on hold" when records are
				// also available — the table's "no record |
				// found | neither" row.
				if av.RecordsAvailable {
					pv.Verdict = "not_on_hold"
				}
			case len(holdGroups) > 1:
				// Two hold groups at once is a contradiction,
				// not a first-match hold: naming only the one
				// that sorts first would call a quarantined
				// peer a dormancy hold.
				detail := "peer in more than one hold group: " + strings.Join(holdGroups, ", ")
				if others := otherSiteGroups(upperSite, gNames); len(others) > 0 {
					detail += "; other site groups also present: " + strings.Join(others, ", ")
				}
				av.Inconsistent = true
				addInconsistentDetail(detail)
				pv.Verdict = "inconsistent"
				pv.VerdictDetail = detail
				if av.RecordsAvailable {
					pv.Kind = HoldKindSecurity
				}
				pv.State = HoldStateInconsistent
			default:
				if others := otherSiteGroups(upperSite, gNames); len(others) > 0 {
					// Same rule as the record-backed branch: a
					// hold group beside other live site groups is
					// a contradiction whether or not a record exists.
					detail := fmt.Sprintf("peer in hold group %s and other site groups: %s", holdGroupName, strings.Join(others, ", "))
					av.Inconsistent = true
					addInconsistentDetail(detail)
					pv.Verdict = "inconsistent"
					pv.VerdictDetail = detail
					if av.RecordsAvailable {
						pv.Kind = HoldKindSecurity
					}
					pv.State = HoldStateInconsistent
				} else if av.RecordsAvailable {
					// Contract's no-record rule when the hold
					// records are available and this peer has no
					// record: membership in a hold group with no
					// record is a security hold.
					pv.Verdict = "on_hold_no_record"
					pv.VerdictDetail = fmt.Sprintf("peer in hold group %s with no hold record", holdGroupName)
					pv.Kind = HoldKindSecurity
					pv.State = HoldStateHeld
				} else {
					// The hold-record file is unavailable, so the
					// presence or absence of a record is unknown.
					// Classify this as "in hold group" and name
					// the degraded lane.
					pv.Verdict = "in_hold_group"
					pv.VerdictDetail = fmt.Sprintf("peer in hold group %s; hold record unavailable, lane unknown", holdGroupName)
					pv.State = HoldStateHeld
				}
			}

			addPeerView(pv)
		}
	}

	// Sort peers by name for stable rendering.
	sort.Slice(av.Peers, func(i, j int) bool { return av.Peers[i].Name < av.Peers[j].Name })

	return av
}

// resolveKeys builds KeyView rows and the NoKeyForHandle flag.
func resolveKeys(handle string, known map[string]struct{}, keys []SetupKey, now time.Time) ([]KeyView, bool) {
	out := []KeyView{}
	for _, k := range keys {
		if !OwnsName(k.Name, handle, known) {
			continue
		}
		kv := KeyView{
			ID:        k.ID,
			Name:      k.Name,
			UsedTimes: k.UsedTimes,
		}
		switch {
		case k.Revoked:
			kv.Status = "revoked"
		case k.ExpiresUnreadable:
			kv.Status = "expiry unreadable"
		case !k.Expires.IsZero() && now.After(k.Expires):
			kv.Status = "expired"
		case k.Valid:
			kv.Status = "valid"
		default:
			kv.Status = "invalid"
		}
		if !k.Expires.IsZero() {
			kv.ExpiresAt = k.Expires.UTC().Format(time.RFC3339)
		}
		out = append(out, kv)
	}
	return out, len(out) == 0
}

// HandleString returns the record's handle as a string, treating
// null as empty. It is display/corroboration only — Resolve never
// matches a record to a member by it.
func (r HoldRecord) HandleString() string {
	if r.Handle == nil {
		return ""
	}
	return *r.Handle
}
