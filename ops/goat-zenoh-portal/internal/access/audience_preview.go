package access

// audience_preview.go aggregates per-anchor wrap decisions into the counts a
// PRODUCER sees before encrypting: "who could decrypt this?", answered as a
// total plus a breakdown by nationality and by organisation, and never as a
// list of people.
//
// WHY AGGREGATE AND NOT ENUMERATE. docs/operators/roster.json is public-safe by
// contract and ops/ci/check-roster-registry.py enforces that. A preview that
// named handles would move identifying data onto a surface a producer consults
// casually, dozens of times a day, before every publish. Counts answer the
// question actually being asked — "is my restriction set right?" — without
// moving a single identifier.
//
// WHAT THIS FILE IS NOT. It performs no policy. Every verdict it counts was
// decided by DataRoster.ClassifyOrgMaterial, the same call the real audience
// resolver makes. This file only tallies and redacts.

import "sort"

// KAnonymityFloor is the smallest cell this surface will show. Below it, a
// breakdown row identifies a person to anyone who knows the org: "1 holder, CAN,
// usareur-af" is a name to that org's colleagues even though no name is printed.
//
// Five is chosen rather than left to the caller because a caller who has to pick
// a floor will pick 1 — the number that makes the surface most useful and least
// safe — and because the decision belongs with the data, not with each consumer.
// It is deliberately NOT configurable: a knob here is a knob for turning
// re-identification back on, and every deployment reads the same roster shape.
const KAnonymityFloor = 5

// AudienceCell is one row of a breakdown: a dimension value and how many
// holders sit under it. Withheld rows carry Value "" and Withheld true.
type AudienceCell struct {
	Value    string `json:"value,omitempty"`
	Holders  int    `json:"holders"`
	Withheld bool   `json:"withheld,omitempty"`
	// Cells is how many distinct values were folded into a withheld row, so a
	// reader can tell "one small org" from "eleven small orgs".
	Cells int `json:"cells,omitempty"`
}

// AudiencePreview is the producer-facing answer.
type AudiencePreview struct {
	// Holders is the number of holders that WOULD receive a wrap slot. Exact
	// and never suppressed: a total is not identifying, and suppressing it
	// would defeat the only question the surface exists to answer.
	Holders int `json:"holders"`

	// ByNationality and ByOrg are the breakdowns, largest first, with cells
	// below KAnonymityFloor folded into a single withheld row.
	ByNationality []AudienceCell `json:"by_nationality"`
	ByOrg         []AudienceCell `json:"by_org"`

	// Refused splits the members who are IN the org but earn no wrap slot, by
	// why. Read Excluded and the two Undeclared counts as different facts: see
	// WrapRefusal. This is the heart of the surface.
	Refused RefusalCounts `json:"refused"`

	// Evaluable reports whether the preview could actually apply the rule to
	// anyone. False when every member was undeclared on one side or the other —
	// the state the registry is in today — meaning a zero here says "not
	// answerable", not "nobody qualifies".
	Evaluable bool `json:"evaluable"`

	// Notes are honest sentences about what the reader is looking at.
	Notes []string `json:"notes,omitempty"`
}

// RefusalCounts is the refusal split. Every field is a count of member anchors
// inside the grantee org(s) that earn no wrap slot.
type RefusalCounts struct {
	// Excluded — declared on both sides and they do not intersect. The rule
	// working as intended.
	Excluded int `json:"excluded"`
	// OrgNationUndeclared — the organisation never declared an owning nation.
	OrgNationUndeclared int `json:"org_nation_undeclared"`
	// HolderNationalityUndeclared — the holder never declared a nationality.
	HolderNationalityUndeclared int `json:"holder_nationality_undeclared"`
	// FrameworkUnavailable — a multinational org with no framework loaded.
	FrameworkUnavailable int `json:"framework_unavailable"`
	// Total is the sum, for a reader who only wants "how many members missed out".
	Total int `json:"total"`
}

// PreviewInput is one anchor's decided outcome, as the resolver produced it.
type PreviewInput struct {
	Anchor      string
	Org         string
	Admitted    bool
	Refusal     WrapRefusal
	Nationality string // the holder's own; "" when undeclared
}

// BuildAudiencePreview tallies decided outcomes into the producer-facing view.
//
// It never re-decides anything: pass the outcomes the audience resolver
// produced. Anchors are counted once even if two grants reach them, because a
// holder receives one wrap slot, not one per grant.
func BuildAudiencePreview(in []PreviewInput) AudiencePreview {
	var p AudiencePreview

	admitted := map[string]bool{}
	refusedSeen := map[string]WrapRefusal{}
	natCount := map[string]int{}
	orgCount := map[string]int{}

	for _, d := range in {
		if d.Admitted {
			if admitted[d.Anchor] {
				continue
			}
			admitted[d.Anchor] = true
			// An admitted holder always has a declared nationality — the rule
			// cannot admit an undeclared one — so no "unknown" bucket is
			// possible here by construction.
			natCount[d.Nationality]++
			if d.Org != "" {
				orgCount[d.Org]++
			}
			continue
		}
		// Refusals are counted per anchor, keeping the FIRST reason seen, so an
		// anchor refused under two grants is one refused holder rather than two.
		// An anchor admitted anywhere is not refused: one wrap slot is enough.
		if _, dup := refusedSeen[d.Anchor]; !dup {
			refusedSeen[d.Anchor] = d.Refusal
		}
	}

	for anchor, why := range refusedSeen {
		if admitted[anchor] {
			continue // admitted under another grant — not a refusal
		}
		switch why {
		case WrapNationalityExcluded:
			p.Refused.Excluded++
		case WrapOrgNationUndeclared:
			p.Refused.OrgNationUndeclared++
		case WrapHolderNationalityUndeclared:
			p.Refused.HolderNationalityUndeclared++
		case WrapFrameworkUnavailable:
			p.Refused.FrameworkUnavailable++
		}
	}
	p.Refused.Total = p.Refused.Excluded + p.Refused.OrgNationUndeclared +
		p.Refused.HolderNationalityUndeclared + p.Refused.FrameworkUnavailable

	p.Holders = len(admitted)
	p.ByNationality = foldCells(natCount, p.Holders)
	p.ByOrg = foldCells(orgCount, p.Holders)

	// The rule was actually applied to somebody when anyone was admitted or
	// anyone was excluded on declared facts. If every outcome was an undeclared
	// one, nothing was evaluated and the zero means "unanswerable".
	p.Evaluable = p.Holders > 0 || p.Refused.Excluded > 0

	if !p.Evaluable && p.Refused.Total > 0 {
		p.Notes = append(p.Notes,
			"NOT EVALUABLE — no member could be tested against the rule, so this zero means "+
				"\"nobody has declared\", NOT \"nobody qualifies\". Read it as an unanswered "+
				"question: the registry has to be filled in before this surface can tell you "+
				"whether a restriction is right.")
	}
	if p.Refused.HolderNationalityUndeclared > 0 {
		p.Notes = append(p.Notes, plural(p.Refused.HolderNationalityUndeclared,
			"holder")+" declare no nationality of their own. That is fixed in their identity "+
			"manifest and by regenerating the roster projection — not by changing this grant.")
	}
	if p.Refused.OrgNationUndeclared > 0 {
		p.Notes = append(p.Notes, plural(p.Refused.OrgNationUndeclared,
			"member")+" sit under an organisation that declares no owning nation. That is an "+
			"org-registry edit; no grant can work around it.")
	}
	if p.Refused.Excluded > 0 {
		p.Notes = append(p.Notes, plural(p.Refused.Excluded,
			"member")+" of the grantee organisation hold its READ access but will not receive a "+
			"wrap slot, because their own nationality is not one the organisation is owned by. "+
			"They can see that the stream exists and cannot open its payload — this is the "+
			"designed split between access and decryption, not a fault.")
	}
	if p.Holders > 0 && p.Holders < KAnonymityFloor {
		p.Notes = append(p.Notes,
			"The audience is small enough that its breakdown is withheld: naming a nationality "+
				"or organisation over this few holders identifies a person to anyone who knows "+
				"the organisation.")
	}
	return p
}

// foldCells sorts a dimension's counts largest-first and folds every cell below
// the floor into one withheld row.
//
// When the TOTAL is itself below the floor, no cell can be safe — suppressing
// individual rows while the total stands would still name the group — so the
// whole breakdown is withheld. The total stays exact and visible; it is the
// breakdown that re-identifies.
func foldCells(counts map[string]int, total int) []AudienceCell {
	if len(counts) == 0 {
		return []AudienceCell{}
	}
	if total < KAnonymityFloor {
		small := 0
		for _, n := range counts {
			small += n
		}
		return []AudienceCell{{Holders: small, Withheld: true, Cells: len(counts)}}
	}
	out := make([]AudienceCell, 0, len(counts))
	withheld, withheldCells := 0, 0
	for v, n := range counts {
		if n < KAnonymityFloor {
			withheld += n
			withheldCells++
			continue
		}
		out = append(out, AudienceCell{Value: v, Holders: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Holders != out[j].Holders {
			return out[i].Holders > out[j].Holders
		}
		return out[i].Value < out[j].Value
	})
	if withheldCells > 0 {
		out = append(out, AudienceCell{Holders: withheld, Withheld: true, Cells: withheldCells})
	}
	return out
}

func plural(n int, noun string) string {
	s := itoa(n) + " " + noun
	if n != 1 {
		s += "s"
	}
	return s
}

// itoa avoids pulling strconv in for one call site.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
