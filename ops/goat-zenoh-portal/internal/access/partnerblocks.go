package access

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// PartnerSlotBlock is one pre-cut range of the slot pool bound to a lane-set
// (ADR 1138). The ROUTER renders the partner subject + rules for every slot in
// the range before anyone holds the cert; the PORTAL allocates a partner INTO
// the matching range so the minted CN matches a subject that already exists —
// no ACL edit, no fabric restart.
//
// Both halves MUST read the same declaration. The router reads
// `zenoh_partner_slot_blocks` from group_vars; the goat-zenoh-portal role
// renders that same variable to the file this package loads. If they disagree,
// the portal hands out a slot the router does not treat as a partner and the
// person reads the whole fabric, silently.
type PartnerSlotBlock struct {
	// Lanes is the release-set names this block's occupants may READ, e.g.
	// ["REL-ALL","REL-PARTNER"]. Order-insensitive; normalized upper-case.
	Lanes []string `yaml:"lanes"`
	From  int64    `yaml:"from"`
	To    int64    `yaml:"to"`
}

type partnerBlocksFile struct {
	Version int                `yaml:"version"`
	Blocks  []PartnerSlotBlock `yaml:"partner_slot_blocks"`
}

// LoadPartnerSlotBlocks reads the rendered partner-slot-block declaration.
//
// Fail-closed, deliberately: a missing or malformed file is an error to the
// caller, never an empty list. "No blocks declared" must be a DECLARED empty
// file. The failure mode this protects against is a read error being read as
// "this goatnet has no partner blocks", which would silently route every
// partner mint down the ordinary lowest-free-slot path — the over-sharing bug.
//
// Validation is strict for the same reason: overlapping or inverted ranges mean
// the portal and the router could disagree about which lane-set a slot carries.
func LoadPartnerSlotBlocks(path string) ([]PartnerSlotBlock, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read partner-slot-blocks %s: %w", path, err)
	}
	var pf partnerBlocksFile
	if err := yaml.Unmarshal(raw, &pf); err != nil {
		return nil, fmt.Errorf("parse partner-slot-blocks %s: %w", path, err)
	}

	out := make([]PartnerSlotBlock, 0, len(pf.Blocks))
	for i, b := range pf.Blocks {
		if b.To < b.From {
			return nil, fmt.Errorf("partner-slot-blocks %s: block %d has to(%d) < from(%d)", path, i, b.To, b.From)
		}
		if b.From < 0 {
			return nil, fmt.Errorf("partner-slot-blocks %s: block %d has negative from(%d)", path, i, b.From)
		}
		lanes := make([]string, 0, len(b.Lanes))
		seen := map[string]bool{}
		for _, l := range b.Lanes {
			l = strings.ToUpper(strings.TrimSpace(l))
			if l == "" || seen[l] {
				continue
			}
			seen[l] = true
			lanes = append(lanes, l)
		}
		if len(lanes) == 0 {
			return nil, fmt.Errorf("partner-slot-blocks %s: block %d [%d,%d] declares no lanes — a block with no lane-set grants a partner nothing and would silently strand them", path, i, b.From, b.To)
		}
		sort.Strings(lanes)
		out = append(out, PartnerSlotBlock{Lanes: lanes, From: b.From, To: b.To})
	}

	// Overlap check. Two blocks covering one slot means its lane-set depends on
	// iteration order — the router picks one, the portal might pick the other.
	sorted := append([]PartnerSlotBlock(nil), out...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].From < sorted[j].From })
	for i := 1; i < len(sorted); i++ {
		if sorted[i].From <= sorted[i-1].To {
			return nil, fmt.Errorf("partner-slot-blocks %s: blocks [%d,%d] and [%d,%d] overlap; a slot in two blocks has an order-dependent lane-set",
				path, sorted[i-1].From, sorted[i-1].To, sorted[i].From, sorted[i].To)
		}
	}
	return out, nil
}

// MatchPartnerBlock returns the block whose lane-set equals want (set equality,
// order-insensitive). Returns false when no block matches.
//
// The caller MUST refuse the mint on false rather than falling back to an
// ordinary slot: a partner outside a block matches no partner-<anchor> subject
// and reads the whole fabric. Refusing is the safe failure.
func MatchPartnerBlock(blocks []PartnerSlotBlock, want []string) (PartnerSlotBlock, bool) {
	norm := make([]string, 0, len(want))
	seen := map[string]bool{}
	for _, l := range want {
		l = strings.ToUpper(strings.TrimSpace(l))
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		norm = append(norm, l)
	}
	sort.Strings(norm)
	if len(norm) == 0 {
		return PartnerSlotBlock{}, false
	}
	for _, b := range blocks {
		if len(b.Lanes) != len(norm) {
			continue
		}
		same := true
		for i := range norm {
			if b.Lanes[i] != norm[i] {
				same = false
				break
			}
		}
		if same {
			return b, true
		}
	}
	return PartnerSlotBlock{}, false
}

// DeclaredLaneSets renders the available lane-sets for an error message, so a
// refused mint tells the operator what they COULD have asked for.
func DeclaredLaneSets(blocks []PartnerSlotBlock) string {
	if len(blocks) == 0 {
		return "(none declared on this goatnet)"
	}
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		parts = append(parts, fmt.Sprintf("[%s] slots %d-%d", strings.Join(b.Lanes, "+"), b.From, b.To))
	}
	return strings.Join(parts, ", ")
}
