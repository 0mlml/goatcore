package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

// relationshipChoice is what the admin gate asks about one membership, decided
// from the organisation the candidate is joining (ADR 1171).
//
// The three relationship values only carry information for a governmental
// organisation. `constituent` means the member's nation is part of what the
// organisation IS, which is only true of a multinational body. `liaison` means
// attached from outside on behalf of another organisation or nation, which is
// a coalition and interagency shape. A commercial or civil-society organisation
// is never constituted by nations and never hosts a liaison in this model: a
// contractor seated in a unit is a member of the contractor, and a government
// representative seated at a plant is a member of the government. Membership
// there is in or out, and in is `assigned`.
//
// So the gate asks only when the organisation is governmental (or when nothing
// says what it is), offers `constituent` only when the organisation is
// multinational, and otherwise implies `assigned` without asking. The
// organisation's registry row decides when it exists; the manifest's own
// sector is the fallback for an unregistered organisation, which is display
// and bookkeeping only because nothing reads the relationship for access yet.
type relationshipChoice struct {
	// Ask renders the dropdown. False means Implied is stored without asking.
	Ask bool
	// ConstituentOK offers `constituent`. Only a multinational organisation
	// can have one; a national organisation's ownership is fixed at creation
	// (plan invariant 1).
	ConstituentOK bool
	// Implied is the value stored when Ask is false.
	Implied string
	// Why is the one sentence the page shows and a refusal quotes.
	Why string
	// OrgID, Sector and Composition are the facts the choice was made from,
	// registry first, manifest as fallback. Empty when unknown.
	OrgID, Sector, Composition string
	// Registered says the facts came from the org registry, not the manifest.
	Registered bool
}

// relationshipChoiceFor decides the gate's shape for one proposal.
func (s *Server) relationshipChoiceFor(p onboardstore.Proposal) relationshipChoice {
	f := orgFactsFromManifest(p.ManifestJSON)
	if f.AffID == "" && f.Sector == "" {
		// A draft still holds the intake record, not an assembled manifest:
		// the sponsor's pre-bound org and sector, and the candidate's own
		// sector, sit under prebound / candidate_submission.
		f.AffID, f.Sector = draftOrgFacts(p.ManifestJSON)
	}
	ch := relationshipChoice{Ask: true, ConstituentOK: true, OrgID: f.AffID, Sector: strings.ToLower(strings.TrimSpace(f.Sector))}
	if s.Orgs != nil && f.AffID != "" {
		if o, err := s.Orgs.GetOrg(f.AffID); err == nil {
			ch.OrgID, ch.Registered = o.ID, true
			ch.Sector = strings.ToLower(strings.TrimSpace(o.Sector))
			ch.Composition = strings.ToLower(strings.TrimSpace(o.Composition))
		}
	}
	name := ch.OrgID
	if name == "" {
		name = "this organisation"
	}
	switch ch.Sector {
	case "commercial", "civil-society":
		ch.Ask, ch.ConstituentOK, ch.Implied = false, false, "assigned"
		ch.Why = fmt.Sprintf("%s is a %s organisation: membership there is in or out, so the relationship is recorded as assigned. constituent and liaison only exist for governmental organisations.", name, sectorWord(ch.Sector))
	case "state":
		ch.Why = "How this person relates to the organisation, not their nationality: a US officer and a Canadian officer at the same US command are both assigned. Their own nationality and the organisation's nation are both on the card above; the two differing is normal."
		if ch.Composition != "multinational" {
			ch.ConstituentOK = false
		}
	default:
		// Sector unknown (org unregistered and the candidate declared none, or a
		// registry row from before the sector migration): ask, as before.
		ch.Why = "How this person relates to the organisation, not their nationality. Their own nationality and the organisation's nation are both on the card above; the two differing is normal."
		if ch.Composition == "national" {
			ch.ConstituentOK = false
		}
	}
	return ch
}

func sectorWord(sector string) string {
	if sector == "civil-society" {
		return "civil-society"
	}
	return sector
}

// resolveRelationship turns what the form sent into what the gate records,
// or refuses. Called before the draft completes, so a refusal leaves nothing
// behind (see decideProposal).
//
//   - Implied choice: blank or `assigned` records `assigned`; anything else is
//     refused, because those values do not exist for that organisation.
//   - Asked choice: the closed vocabulary applies; `constituent` is refused on
//     an organisation the registry says is national (plan invariant 1). Blank
//     stays blank, as before.
func (ch relationshipChoice) resolveRelationship(rel string) (string, *decideError) {
	rel = strings.ToLower(strings.TrimSpace(rel))
	if !ch.Ask {
		if rel == "" || rel == ch.Implied {
			return ch.Implied, nil
		}
		return "", &decideError{status: http.StatusBadRequest,
			msg: fmt.Sprintf("membership relationship %q is not available here: %s", rel, ch.Why)}
	}
	if rel == "" {
		return "", nil
	}
	if _, err := parseRelationship(rel); err != nil {
		return "", &decideError{status: http.StatusBadRequest, msg: err.Error()}
	}
	if rel == "constituent" && !ch.ConstituentOK {
		return "", &decideError{status: http.StatusBadRequest,
			msg: fmt.Sprintf("constituent is refused on %s: a national organisation's ownership is fixed when it is registered, and no membership changes it. Use assigned (a full member whose nation stays their own) or liaison.", ch.OrgID)}
	}
	return rel, nil
}

// draftOrgFacts reads the org id and sector out of an intake record (a draft
// proposal, candidate-intake/v-draft): the sponsor's prebound values first,
// then the candidate's submission.
func draftOrgFacts(manifestJSON string) (orgID, sector string) {
	var m map[string]any
	if err := json.Unmarshal([]byte(manifestJSON), &m); err != nil {
		return "", ""
	}
	get := func(block map[string]any, key string) string {
		if block == nil {
			return ""
		}
		v, _ := block[key].(string)
		return strings.TrimSpace(v)
	}
	pre, _ := m["prebound"].(map[string]any)
	sub, _ := m["candidate_submission"].(map[string]any)
	orgID = get(pre, "org")
	if orgID == "" {
		orgID = get(sub, "org")
	}
	sector = get(pre, "sector")
	if sector == "" {
		sector = get(sub, "sector")
	}
	return orgID, sector
}
