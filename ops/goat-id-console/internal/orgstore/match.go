// Fuzzy org matching (ADR 1112, design doc §7): normalized trigram
// similarity over org ids + display names + accreted aliases, nation-scoped
// when the query carries one. In-process over the loaded catalog — the org
// population is O(10²–10³), so no DB extension is paid.
//
// Policy (fixed by the design; thresholds tuned by the committed corpus
// test): NEVER auto-merge, NEVER refuse — the matcher ranks and bands,
// a human confirms. A recorded negative pair ("create new despite
// candidate X") demotes X out of the exact/high bands for that name
// family but never hides it.
package orgstore

import (
	"context"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Bands. Exact = normalized equality (candidate preselected; one-click
// confirm). High = "did you mean" (collapse offered, never forced).
// Low = listed only.
const (
	BandExact = "exact"
	BandHigh  = "high"
	BandLow   = "low"
)

// HighBandThreshold is the Dice-coefficient floor for the high band.
// Tuned against the real-catalog corpus test (§13.2): known name variants
// land ≥ this; the known-distinct near-identical pair is handled by the
// never-auto-collapse policy (exact band requires full normalized equality,
// and confirm is always a human act).
const HighBandThreshold = 0.55

// Candidate is one ranked match.
type Candidate struct {
	ID          string  `json:"id"`
	DisplayName string  `json:"display_name,omitempty"`
	Nation      string  `json:"nation,omitempty"`
	Parent      string  `json:"parent,omitempty"`
	Score       float64 `json:"score"`
	Band        string  `json:"band"`
	Via         string  `json:"via"` // id | display_name | alias:<alias>
	Demoted     bool    `json:"demoted,omitempty"`
}

var deMark = transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

// NormalizeName folds a name for matching: diacritics stripped
// (Forțele → Fortele), lowercased, every non-alphanumeric run collapsed to
// one space, trimmed. Exported so consumers (tests, the review surface)
// share one definition.
func NormalizeName(s string) string {
	folded, _, err := transform.String(deMark, s)
	if err != nil {
		folded = s // fold is best-effort; matching degrades, never fails
	}
	var b strings.Builder
	prevSpace := true
	for _, r := range strings.ToLower(folded) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevSpace = false
		} else if !prevSpace {
			b.WriteByte(' ')
			prevSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

func trigrams(s string) map[string]int {
	padded := "  " + s + " "
	out := map[string]int{}
	rs := []rune(padded)
	for i := 0; i+3 <= len(rs); i++ {
		out[string(rs[i:i+3])]++
	}
	return out
}

// diceSimilarity is the Sørensen–Dice coefficient over trigram multisets.
func diceSimilarity(a, b string) float64 {
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1
	}
	ta, tb := trigrams(a), trigrams(b)
	shared, total := 0, 0
	for g, na := range ta {
		total += na
		if nb, ok := tb[g]; ok {
			if na < nb {
				shared += na
			} else {
				shared += nb
			}
		}
	}
	for _, nb := range tb {
		total += nb
	}
	if total == 0 {
		return 0
	}
	return 2 * float64(shared) / float64(total)
}

// Suggest ranks the catalog against a typed name. nation "" = unscoped;
// otherwise orgs declaring a DIFFERENT nation are excluded (an org with no
// nation is kept — absence of a fact is not a mismatch). limit caps the
// result (≤ 0 → 5).
func (s *Store) Suggest(q, nation string, limit int) ([]Candidate, error) {
	qn := NormalizeName(q)
	if qn == "" {
		return nil, nil
	}
	orgs, err := s.ListOrgs()
	if err != nil {
		return nil, err
	}
	demoted, err := s.negativePairs(qn)
	if err != nil {
		return nil, err
	}
	return RankCandidates(orgs, demoted, q, nation, limit), nil
}

// RankCandidates is Suggest over a catalog the caller already holds: the same
// ranking, banding and demotion, with no store. demoted is the set of org ids
// recorded as negative pairs for q's name family (nil for none). It lets an
// offline report (tools/org-backfill-dryrun) use the real matcher without a
// database.
func RankCandidates(orgs []OrgView, demoted map[string]bool, q, nation string, limit int) []Candidate {
	if limit <= 0 {
		limit = 5
	}
	qn := NormalizeName(q)
	if qn == "" {
		return nil
	}
	out := make([]Candidate, 0, len(orgs))
	for _, o := range orgs {
		if o.Status == "absorbed" {
			continue // its alias rows route to the winner already
		}
		// Both sides folded. EqualFold alone is case-insensitive and NOT
		// alpha-2/alpha-3 aware, so an org stored `US` was EXCLUDED from a search
		// scoped `USA` — the matcher silently failing to find an org that is
		// registered is how the reviewer is walked toward creating a duplicate,
		// which is the outcome this registry exists to prevent (F-389 through the
		// nation axis). Writes are folded too; this keeps rows written before that.
		if nation != "" && o.Nation != "" &&
			normalizeNation(o.Nation) != normalizeNation(nation) {
			continue
		}
		best, via := 0.0, ""
		consider := func(name, label string) {
			nn := NormalizeName(name)
			if nn == "" {
				return
			}
			sc := diceSimilarity(qn, nn)
			if nn == qn {
				sc = 1
			}
			if sc > best {
				best, via = sc, label
			}
		}
		consider(o.ID, "id")
		consider(o.DisplayName, "display_name")
		for _, a := range o.Aliases {
			consider(a, "alias:"+a)
		}
		if best <= 0 {
			continue
		}
		c := Candidate{
			ID: o.ID, DisplayName: o.DisplayName, Nation: o.Nation,
			Parent: o.Parent, Score: best,
		}
		switch {
		case NormalizeName(o.ID) == qn || NormalizeName(o.DisplayName) == qn || best == 1:
			c.Band = BandExact
		case best >= HighBandThreshold:
			c.Band = BandHigh
		default:
			c.Band = BandLow
		}
		c.Via = via
		// Negative pair: the operator has already said "distinct from this
		// name" — demote out of exact/high, keep listed (never hidden).
		if demoted[o.ID] && c.Band != BandLow {
			c.Band = BandLow
			c.Demoted = true
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Demoted != out[j].Demoted {
			return !out[i].Demoted // demoted sink below equal-scored peers
		}
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].ID < out[j].ID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// RecordNegativePair records "the operator deliberately created/kept a
// DIFFERENT org despite candidate orgID for this typed name" — future
// Suggest calls demote orgID for the name family. Idempotent.
func (s *Store) RecordNegativePair(name, orgID, actor string) error {
	nn := NormalizeName(name)
	if nn == "" {
		return nil
	}
	id, err := s.Resolve(orgID)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(context.Background(), `INSERT INTO org_match_overrides (name_norm, org_id, recorded_at, actor)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT(name_norm, org_id) DO NOTHING`, nn, id, s.Now().Unix(), actor)
	return err
}

func (s *Store) negativePairs(nameNorm string) (map[string]bool, error) {
	var ids []string
	if err := s.db.Select(&ids, `SELECT org_id FROM org_match_overrides WHERE name_norm = $1`, nameNorm); err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}
