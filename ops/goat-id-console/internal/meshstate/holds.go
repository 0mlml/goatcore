package meshstate

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Hold kinds and states: the one vocabulary both console surfaces render
// (the Holds page and the member page's Access section). Kind says WHO
// placed the hold and therefore how it is released; State says what the
// record and the mesh agree on right now. They are two facts: a security
// hold with a stale record and a dormancy hold mid-release are both
// expressible.
const (
	HoldKindDormancy = "dormancy"
	HoldKindSecurity = "security"

	HoldStateHeld         = "held"
	HoldStateReleasing    = "releasing"
	HoldStateStale        = "stale"
	HoldStateInconsistent = "inconsistent"
	HoldStateOrphan       = "orphan"
)

// HoldStateLabel is the human label for each state, rendered by the
// shared badge template. Labels carry the word; color is never the only
// signal.
var HoldStateLabel = map[string]string{
	HoldStateHeld:         "held",
	HoldStateReleasing:    "release in progress",
	HoldStateStale:        "stale record",
	HoldStateInconsistent: "inconsistent, investigate",
	HoldStateOrphan:       "orphan record",
}

// HoldKindLabel is the human label for each kind. The empty kind is the
// hold-record file being unreadable while the peer sits in a hold group:
// the lane cannot be read, and the page says so instead of guessing.
var HoldKindLabel = map[string]string{
	HoldKindDormancy: "dormancy",
	HoldKindSecurity: "security",
	"":               "kind unknown",
}

// NeedsAttention reports whether a state belongs in the page's attention
// band: the states an admin has to investigate rather than wait out.
func NeedsAttention(state string) bool {
	switch state {
	case HoldStateStale, HoldStateInconsistent, HoldStateOrphan:
		return true
	}
	return false
}

// HoldsInput is the pure-data input to ResolveHolds: every hold record
// and every live peer on this goatnet, plus the handle roster used to
// attribute peers to members.
type HoldsInput struct {
	Site string
	// Known is every handle the console knows; KnownErr is set when the
	// roster could not be read in full, in which case NOTHING is
	// attributed (a partial roster credits names to a handle that merely
	// prefixes them, F-358).
	Known    map[string]struct{}
	KnownErr error

	Records    Records
	RecordsErr error

	Peers    []Peer
	PeersErr error

	Now time.Time
}

// HoldRow is one held peer on the Holds page.
type HoldRow struct {
	PeerID   string
	PeerName string
	// Handle is the member the peer is attributed to under the ownership
	// rule the Access section uses (OwnsName over the full roster).
	// Empty = not attributed: an orphan record, a peer no roster handle
	// owns, or a roster that could not be read.
	Handle string

	Kind  string // HoldKindDormancy / HoldKindSecurity / "" (unknown)
	State string // HoldState*

	HasRecord  bool
	HoldGroup  string
	EnteredAt  time.Time
	DaysOnHold string // days on hold, one decimal, as the member page prints it; "" without a record
	// Live is true when the mesh answered AND lists this peer; only then
	// does an empty InactiveDays mean "never seen" rather than "not
	// listed" (an orphan record, whatever state it is graded).
	Live               bool
	LastSeen           time.Time
	LastSeenUnreadable bool   // API supplied last_seen, but it could not be parsed
	InactiveDays       string // days since live last-seen; "" when unavailable or never seen
	PriorGroups        []string
	LiveGroups         []string
	Reason             string
	Actor              string
	// Detail is the muted line under the row: the group names for an
	// inconsistent peer, or why a stale/orphan row is what it is.
	Detail string
}

// Band is the ordering band the row sorts into: 0 attention, 1 security,
// 2 dormancy, 3 kind unknown (the record file was unreadable; the badge
// refuses to guess the kind, so the order must not guess it either). Band
// is ORDER only: the summary counts and quick filters read Kind and State
// as the two facts they are (KindName / Attention), so a quarantined peer
// that also needs attention is still found under Security.
func (r HoldRow) Band() int {
	switch {
	case NeedsAttention(r.State):
		return 0
	case r.Kind == HoldKindSecurity:
		return 1
	case r.Kind == HoldKindDormancy:
		return 2
	default:
		return 3
	}
}

// BandName is the band as the page's quick filters name it.
func (r HoldRow) BandName() string {
	switch r.Band() {
	case 0:
		return "attention"
	case 1:
		return "security"
	case 2:
		return "dormancy"
	default:
		return "unknown"
	}
}

// KindName is the row's kind as the page's quick filters name it:
// "dormancy", "security", or "unknown" when the record file was unreadable.
func (r HoldRow) KindName() string {
	switch r.Kind {
	case HoldKindSecurity:
		return "security"
	case HoldKindDormancy:
		return "dormancy"
	default:
		return "unknown"
	}
}

// Attention reports whether the row's State needs an admin's eyes
// (stale / inconsistent / orphan). Independent of Kind.
func (r HoldRow) Attention() bool {
	return NeedsAttention(r.State)
}

// HoldsView is the template-facing shape of the Holds page.
type HoldsView struct {
	Site string

	RecordsAvailable    bool
	RecordsError        string
	RecordsSkipped      int
	RecordsSkippedLines []int

	MeshAvailable bool
	MeshError     string
	PeersChecked  int

	KnownHandlesError string

	CheckedAt time.Time

	Rows []HoldRow

	// Total is every row. Dormancy + Security + Unknown partition it by
	// Kind; Attention counts rows by State and OVERLAPS the kind counts
	// (a quarantined peer with a leftover role group is one Security and
	// one Attention), so the four never sum to Total by design.
	Total     int
	Dormancy  int
	Security  int
	Attention int
	// Unknown counts hold-group peers whose kind could not be read
	// (record file unreadable). Zero whenever the file was read.
	Unknown int
}

// ResolveHolds grades every hold on the goatnet: one row per held peer,
// a peer being on hold when a record says so or when it sits in a hold
// group on the mesh. Pure function, no I/O. The grading mirrors Resolve
// (the member page) rule for rule so the two surfaces never disagree
// about one peer.
func ResolveHolds(in HoldsInput) HoldsView {
	hv := HoldsView{Site: strings.TrimSpace(in.Site), CheckedAt: in.Now}

	if in.KnownErr != nil {
		hv.KnownHandlesError = in.KnownErr.Error()
	}
	if in.RecordsErr != nil {
		hv.RecordsError = in.RecordsErr.Error()
	} else {
		hv.RecordsAvailable = true
		hv.RecordsSkipped = in.Records.Skipped
		hv.RecordsSkippedLines = append([]int(nil), in.Records.SkippedLines...)
	}
	if in.PeersErr != nil {
		hv.MeshError = in.PeersErr.Error()
	} else {
		hv.MeshAvailable = true
		hv.PeersChecked = len(in.Peers)
	}
	if !hv.RecordsAvailable && !hv.MeshAvailable {
		return hv
	}

	site := hv.Site
	upperSite := strings.ToUpper(site)
	peersByID := map[string]Peer{}
	if hv.MeshAvailable {
		for _, p := range in.Peers {
			peersByID[p.ID] = p
		}
	}
	// Attribution follows the member page: the roster ownership rule over
	// a LIVE peer name. With the roster partial nothing is attributed
	//; with the mesh down there is no live name to own, and a
	// record's peer_name is a hand-editable field, so nothing is attributed
	// then either — the device name still carries the handle for the
	// text filter.
	attribute := func(name string) string {
		if in.KnownErr != nil || !hv.MeshAvailable {
			return ""
		}
		return ownerOf(name, in.Known)
	}
	days := func(from time.Time) string {
		if from.IsZero() || in.Now.IsZero() {
			return ""
		}
		d := in.Now.Sub(from).Hours() / 24
		if d < 0 {
			d = 0
		}
		// One decimal, as the member page prints DaysOnHold, so a hold
		// placed twelve hours ago reads 0.5 on both surfaces.
		return fmt.Sprintf("%.1f", d)
	}

	// One row per held PEER, not per record: a duplicate line for the
	// same peer_id adds no second row. The FIRST line wins, as on the
	// member page, because it is the line the sweep reads, marks and
	// deletes (_lookup_hold_record, mark_hold_record_releasing,
	// delete_hold_record in ops/scripts/lifecycle-sweep.py); the row is
	// graded inconsistent.
	covered := map[string]bool{}
	addRow := func(row HoldRow) {
		noteForeignPrefix(&row, upperSite)
		hv.Rows = append(hv.Rows, row)
	}
	if hv.RecordsAvailable {
		for _, r := range in.Records.All {
			if !sameSite(site, r.Site) || covered[r.PeerID] {
				continue
			}
			covered[r.PeerID] = true
			row := HoldRow{
				PeerID:      r.PeerID,
				PeerName:    r.PeerName,
				Kind:        r.Lane,
				HasRecord:   true,
				HoldGroup:   strings.TrimSpace(r.HoldGroup),
				EnteredAt:   r.EnteredAt.UTC(),
				DaysOnHold:  days(r.EnteredAt),
				PriorGroups: append([]string(nil), r.PriorGroups...),
				Reason:      r.Reason,
				Actor:       r.Actor,
			}

			if !hv.MeshAvailable {
				// Records alone: the record's own facts, no live grading.
				row.Handle = attribute(r.PeerName)
				row.State = HoldStateHeld
				if r.Phase == "releasing" {
					row.State = HoldStateReleasing
				}
				if in.Records.DuplicatePeerIDs[r.PeerID] {
					row.State = HoldStateInconsistent
					row.Detail = "duplicate hold records"
				}
				addRow(row)
				continue
			}

			peer, live := peersByID[r.PeerID]
			if !live {
				// An orphan record has no live name to own; the member page
				// attributes it by the record's peer_name under the same
				// roster rule, so this page does too.
				row.Handle = attribute(r.PeerName)
				row.State = HoldStateOrphan
				row.Detail = "record names a peer the mesh does not list"
				if in.Records.DuplicatePeerIDs[r.PeerID] {
					row.State = HoldStateInconsistent
					row.Detail = "duplicate hold records; record names a peer the mesh does not list"
				}
				addRow(row)
				continue
			}
			row.Live = true
			row.PeerName = peer.Name
			row.Handle = attribute(peer.Name)
			row.LastSeen = peer.LastSeen
			row.LastSeenUnreadable = peer.LastSeenUnreadable
			row.InactiveDays = days(peer.LastSeen)
			gNames := peerGroupNames(peer.Groups)
			row.LiveGroups = gNames
			holdPresent := containsGroup(gNames, r.HoldGroup)
			others := otherSiteGroups(upperSite, gNames)
			var extraHolds []string
			for _, g := range holdGroupsOf(upperSite, gNames) {
				if !strings.EqualFold(g, row.HoldGroup) {
					extraHolds = append(extraHolds, g)
				}
			}
			unlisted := unlistedGroups(gNames, r)

			switch {
			case in.Records.DuplicatePeerIDs[r.PeerID]:
				row.State = HoldStateInconsistent
				row.Detail = "duplicate hold records"
			case len(extraHolds) > 0:
				row.State = HoldStateInconsistent
				row.Detail = "in more than one hold group: " + strings.Join(append([]string{row.HoldGroup}, extraHolds...), ", ")
				if !holdPresent {
					row.Detail = "record says " + row.HoldGroup + ", peer in other hold group(s): " + strings.Join(extraHolds, ", ")
				}
				if len(others) > 0 {
					row.Detail += "; other groups also present: " + strings.Join(others, ", ")
				}
			case r.Phase == "releasing" && holdPresent && len(unlisted) > 0:
				row.State = HoldStateInconsistent
				row.Detail = "release in progress, but the peer also holds group(s) its record never listed: " + strings.Join(unlisted, ", ")
			case r.Phase == "releasing":
				row.State = HoldStateReleasing
			case holdPresent && len(others) > 0:
				row.State = HoldStateInconsistent
				row.Detail = "hold group " + row.HoldGroup + " and other groups both present: " + strings.Join(others, ", ")
			case holdPresent:
				row.State = HoldStateHeld
			default:
				row.State = HoldStateStale
				row.Detail = "record says " + row.HoldGroup + ", peer is not in it"
			}
			addRow(row)
		}
	}

	if hv.MeshAvailable {
		for _, p := range in.Peers {
			if covered[p.ID] {
				continue
			}
			gNames := peerGroupNames(p.Groups)
			holdGroups := holdGroupsOf(upperSite, gNames)
			if len(holdGroups) == 0 {
				continue
			}
			row := HoldRow{
				PeerID:             p.ID,
				PeerName:           p.Name,
				Handle:             attribute(p.Name),
				Live:               true,
				LastSeen:           p.LastSeen,
				LastSeenUnreadable: p.LastSeenUnreadable,
				InactiveDays:       days(p.LastSeen),
				LiveGroups:         gNames,
				HoldGroup:          holdGroups[0],
			}
			others := otherSiteGroups(upperSite, gNames)
			// The contract's no-record rule names the kind: no record ⇒
			// security. That needs a readable record file; without one the
			// kind is unknown, on every no-record row alike.
			noRecordKind := HoldKindSecurity
			if !hv.RecordsAvailable {
				noRecordKind = ""
			}
			switch {
			case len(holdGroups) > 1:
				// Two hold groups is the contradiction to investigate.
				row.Kind = noRecordKind
				row.State = HoldStateInconsistent
				row.Detail = "in more than one hold group: " + strings.Join(holdGroups, ", ")
				if len(others) > 0 {
					row.Detail += "; other groups also present: " + strings.Join(others, ", ")
				}
			case len(others) > 0:
				row.Kind = noRecordKind
				row.State = HoldStateInconsistent
				row.Detail = "hold group " + holdGroups[0] + " and other groups both present: " + strings.Join(others, ", ")
			case hv.RecordsAvailable:
				row.Kind = HoldKindSecurity
				row.State = HoldStateHeld
				row.Detail = "no hold record for this peer"
			default:
				// Records unreadable: the lane cannot be read, and the page
				// says so rather than guessing it.
				row.Kind = ""
				row.State = HoldStateHeld
				row.Detail = "hold-record file unavailable, kind unknown"
			}
			addRow(row)
		}
	}

	sortHoldRows(hv.Rows)

	for _, r := range hv.Rows {
		hv.Total++
		if r.Attention() {
			hv.Attention++
		}
		switch r.Kind {
		case HoldKindSecurity:
			hv.Security++
		case HoldKindDormancy:
			hv.Dormancy++
		default:
			hv.Unknown++
		}
	}
	return hv
}

// sortHoldRows applies the page's default order: attention rows first,
// then security, then dormancy; newest hold first within a band; one
// member's several devices adjacent. Adjacency and newest-first are
// reconciled by sorting members (not rows) by their newest hold, then
// rows within a member by their own hold time.
func sortHoldRows(rows []HoldRow) {
	groupKey := func(r HoldRow) string {
		if r.Handle != "" {
			return "h:" + r.Handle
		}
		return "p:" + r.PeerID
	}
	// Newest is per (band, member): a member's row in another band must
	// not pull their rows in this band ahead of a newer hold.
	type bandKey struct {
		band int
		key  string
	}
	newest := map[bandKey]time.Time{}
	for _, r := range rows {
		k := bandKey{r.Band(), groupKey(r)}
		if r.EnteredAt.After(newest[k]) {
			newest[k] = r.EnteredAt
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Band() != b.Band() {
			return a.Band() < b.Band()
		}
		ka, kb := groupKey(a), groupKey(b)
		if ka != kb {
			na, nb := newest[bandKey{a.Band(), ka}], newest[bandKey{b.Band(), kb}]
			if !na.Equal(nb) {
				return na.After(nb)
			}
			return ka < kb
		}
		if !a.EnteredAt.Equal(b.EnteredAt) {
			return a.EnteredAt.After(b.EnteredAt)
		}
		return a.PeerName < b.PeerName
	})
}

// ownerOf returns the handle that owns name under OwnsName's rule — the
// LONGEST known handle prefixing `peer-<handle>-` — or "" when no known
// handle claims it. Same rule as the member page, run the other way round.
func ownerOf(name string, known map[string]struct{}) string {
	best := ""
	for h := range known {
		if h == "" || !strings.HasPrefix(name, "peer-"+h+"-") {
			continue
		}
		if len(h) > len(best) {
			best = h
		}
	}
	return best
}

// noteForeignPrefix appends the foreign-prefix note to a row's Detail. A
// hold group is a hold whatever its site prefix (an earlier site id, or
// another site on a shared management host); the row says so when the
// prefix is not this goatnet's, so the count is readable rather than a
// surprise — on record-backed and no-record rows alike, the same peer
// must not be explained differently depending on whether a record exists.
func noteForeignPrefix(row *HoldRow, upperSite string) {
	g := strings.TrimSpace(row.HoldGroup)
	if g == "" || underSitePrefix(upperSite, g) {
		return
	}
	note := "hold group " + g + " is not under this goatnet's prefix " + upperSite + "-"
	if row.Detail == "" {
		row.Detail = note
		return
	}
	row.Detail += "; " + note
}

// underSitePrefix reports whether hold group g is this site's own: the site
// prefix followed DIRECTLY by the lane group (SITE- must not admit
// SITE-PROD-QUARANTINED), the same rule validate-hold-records.py applies to
// a record's hold_group.
func underSitePrefix(upperSite, g string) bool {
	rest, ok := strings.CutPrefix(strings.ToUpper(strings.TrimSpace(g)), upperSite+"-")
	if !ok || upperSite == "" {
		return false
	}
	return rest == "QUARANTINED" || strings.HasPrefix(rest, "PROBATION-")
}
