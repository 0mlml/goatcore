package handlers

import (
	"errors"
	"strconv"
	"strings"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/coalition"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

// onboard_effective.go is the one place the sponsor's prebound set and the
// candidate's submission fold into the effective inputs the assembler consumes.
// The submit path runs it on a fresh submission; draft completion
// (onboard_draft.go) runs it on the stored intake record, so a re-assembled
// draft follows exactly the rules a new submission would, including any rule
// that changed after the draft was stored.

// errTierRequired: the sponsor left the access level open and the candidate
// chose none. Nothing downstream can set a tier afterwards — ADR 1137 froze it
// at review — so a submission with no tier would only ever become a draft
// nobody can complete. Refusing here, with the link still usable, puts the
// choice back in front of the one person who can make it.
var errTierRequired = errors.New("your sponsor left the access level to you — choose one before submitting")

// errHandleRequired: no handle was given and the email's local part slugs to
// nothing, so there is no name to mint the account under. A missing handle is
// not review-editable, so such a submission could only ever become a draft
// nobody can complete. Refusing here, with the link still usable, puts the
// choice back in front of the candidate. The intake page has no handle input,
// so the one thing the candidate can change is the address; the message names
// that and nothing else.
var errHandleRequired = errors.New("your email address cannot form an account name; use a different address")

// intakeEffective resolves sponsor-locked ⊕ candidate-filled into the effective
// inputs and the petitions (candidate asked for a different value on a locked
// field). It refuses, fail-closed, an enrollee-set escalated tier, a tier above
// the sponsor's cap, and a blank tier.
func (s *Server) intakeEffective(hints *intakeHints, inv *onboardstore.Invite, sub candidateSubmission) (effectiveInputs, []petition, error) {
	pb, err := inv.Prebound()
	if err != nil {
		return effectiveInputs{}, nil, err
	}
	// Escalation guard: an escalated tier may only come from the sponsor's
	// invite — never as an enrollee value OR a petition. Fail closed.
	if isEscalatedTier(sub.Tier) && !isEscalatedTier(pb.Tier) {
		return effectiveInputs{}, nil, errEnrolleeEscalation
	}
	petitions := s.intakePetitions(pb, sub)

	// Effective = sponsor-locked wins (petitions don't apply), else enrollee-filled.
	effTier := firstNonEmpty(pb.Tier, sub.Tier)
	effAff := firstNonEmpty(pb.Affiliation, sub.Affiliation)
	effSector := firstNonEmpty(pb.Sector, sub.Sector)
	effSite := firstNonEmpty(pb.Site, sub.Site, inv.Env)
	// The candidate's number and the sponsor's number are not the same
	// capability, so they do not flow to the same place.
	//
	// machine_quota is an AUTHORIZATION (ADR 1096) — custody over that many
	// OTHER machines. It comes from the server-held invite and nowhere else
	// (ADR 1062 §2). This used to fall through to sub.MachineQuota when the
	// sponsor left it blank, which let a candidate self-grant custody,
	// uncapped, from the public form; three records in the zagros store carry a
	// 20 that no sponsor typed.
	effMachineQuota := pb.MachineQuota
	// netbird.usage_limit is a CAPACITY — how many of the candidate's own
	// devices their key may enroll. Asking the person how many laptops they
	// have is safe, so their number stands when the sponsor left it open,
	// clamped to the range the intake form documents. Both blank leaves the
	// intake-form default (5) standing in assembleManifest.
	effUsageLimit := pb.MachineQuota
	if effUsageLimit == 0 {
		effUsageLimit = clampUsageLimit(sub.MachineQuota)
	}
	// Sponsorship cap (ADR 1071): the invite carries the minting sponsor's
	// ceiling; an enrollee-picked tier above it is refused fail-closed.
	if cap := strings.TrimSpace(pb.SponsorMaxTier); cap != "" && strings.TrimSpace(effTier) != "" {
		if !hints.tierWithin(effTier, cap) {
			return effectiveInputs{}, nil, errTierExceedsSponsorship
		}
	}
	if strings.TrimSpace(effTier) == "" {
		return effectiveInputs{}, nil, errTierRequired
	}

	handle := strings.TrimSpace(sub.Handle)
	if handle == "" {
		handle = deriveHandle(sub.Email)
	}
	if handle == "" {
		return effectiveInputs{}, nil, errHandleRequired
	}

	in := effectiveInputs{
		Tier: effTier, Affiliation: effAff, Site: effSite,
		// The goatnet this console serves — the fallback for Site, so a
		// manifest minted here never names another goatnet's site.
		Goatnet: s.Cfg.NotifyGoatnet,
		Handle:  handle, Name: sub.Name, Email: sub.Email,
		// Sponsor feeds identity.added_by (email-formatted) — the human label
		// there, never a participant sponsor's raw UUID anchor.
		Organization: sub.Organization, Sponsor: firstNonEmpty(inv.SponsorName, inv.Sponsor),
		// AffiliationID is the REGISTERED org id from the invite (ADR 1112).
		// Set, assembly writes it straight to affiliation.id; blank, assembly
		// falls back to slug(Organization) exactly as before. This is the whole
		// point of pinning an org at mint: the enrollee's typing stops being
		// the thing that names an organisation.
		AffiliationID: strings.TrimSpace(pb.Org),
		// affiliation.nation is the ORG's nation, an org-registry fact (ADR
		// 1171). The pinned org's registered nation wins; the candidate's own
		// answer (a legacy field the page no longer sends) is only a fallback.
		Nation:        firstNonEmpty(s.preboundOrgNation(pb.Org), sub.Nation),
		PrincipalType: sub.PrincipalType,
		UnitCode:      sub.UnitCode, UnitCodeScheme: sub.UnitCodeScheme,
		MachineQuota: effMachineQuota, UsageLimit: effUsageLimit,
		// github + coalition are enrollee-only fields (no sponsor lock in the
		// invite); the manifest captures an explicit value only. Sector is
		// sponsor-lockable like tier: a prebound sector is the effective one.
		GitHubUsername: sub.GitHubUsername, Sector: effSector,
		// Standing FIRST, Coalition as the deprecated alias. The literal used
		// to pass only Coalition — the legacy key the intake page has never
		// sent — so in.Standing was always empty and assembleManifest's write
		// never fired. alliance_standing could not be stored on this path at
		// all, which is why zagros-dev's first proposal carried none.
		Standing: sub.Standing, Coalition: sub.Coalition,
		// gov_branch had no field on candidateSubmission, so it was dropped
		// at decode. The assembler only writes it under sector=state, which
		// is the contradiction check, not a reason to drop it here.
		GovBranch: sub.GovBranch,
		// The person's own citizenship, distinct from the org's nation.
		Nationality: sub.Nationality,
		// Whether this goatnet accepts the non-gov sector cluster; a non-gov
		// value submitted while the gate is off is dropped, not stamped.
		NongovSectors: s.Cfg.SectorsNongovEnabled,
		// The other side of the organic test that derives a state-sector kind.
		OperatingSovereign: s.Cfg.OperatingSovereign,
		// collab is a sponsor decision locked at invite mint (prebound); the
		// candidate does not set it. Stamped into the manifest either way.
		Collab: pb.Collab,
	}
	return in, petitions, nil
}

// intakePetitions records every sponsor-locked field the candidate proposed a
// DIFFERENT value for. Recorded + flagged; the effective manifest keeps the
// sponsor's value.
func (s *Server) intakePetitions(pb onboardstore.Prebound, sub candidateSubmission) []petition {
	petitions := []petition{}
	add := func(field, locked, got string) {
		locked, got = strings.TrimSpace(locked), strings.TrimSpace(got)
		if locked == "" || got == "" || strings.EqualFold(got, locked) {
			return
		}
		petitions = append(petitions, petition{Field: field, SponsorValue: locked, RequestedValue: got})
	}
	add("tier", pb.Tier, sub.Tier)
	add("affiliation", pb.Affiliation, sub.Affiliation)
	add("sector", pb.Sector, sub.Sector)
	// The invite named a registered org and the candidate typed a different
	// employer. Their words are kept as a petition for the reviewer; the org
	// the sponsor pinned is what the manifest carries, because a typed string
	// is exactly what mints a duplicate org. The comparison is against
	// both the id and the label the page showed them, so someone who leaves the
	// pre-filled name alone never generates a spurious petition.
	if locked := strings.TrimSpace(pb.Org); locked != "" {
		got := strings.TrimSpace(sub.Organization)
		shown := s.preboundOrgLabel(locked)
		if got != "" && !strings.EqualFold(got, locked) && !strings.EqualFold(got, shown) {
			petitions = append(petitions, petition{Field: "organization", SponsorValue: shown, RequestedValue: got})
		}
	}
	if pb.MachineQuota > 0 && sub.MachineQuota > 0 && sub.MachineQuota != pb.MachineQuota {
		petitions = append(petitions, petition{Field: "machine_quota", SponsorValue: strconv.Itoa(pb.MachineQuota), RequestedValue: strconv.Itoa(sub.MachineQuota)})
	}
	return petitions
}

// preboundOrgNation is the registered nation of the org the invite pinned, or
// "" when no org was pinned, the registry is not configured, or the org carries
// no nation. It is read-only and never guesses from the candidate's typing.
func (s *Server) preboundOrgNation(org string) string {
	org = strings.TrimSpace(org)
	if org == "" || s.Orgs == nil {
		return ""
	}
	v, err := s.Orgs.GetOrg(org)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(v.Nation)
}

// resolveAffiliationKind is ADR 1151's read-side rule for the deprecated
// affiliation.kind: an explicit value wins; otherwise derive from the stamped
// sector plus the organic test (org nation vs operating sovereign); when neither
// decides, return "" and let the caller leave the key out.
func resolveAffiliationKind(explicit, sector, nation, operatingSovereign string) string {
	if k := strings.ToLower(strings.TrimSpace(explicit)); k != "" {
		return k
	}
	organic, known := coalition.Organic(nation, operatingSovereign)
	kind, ok := coalition.DeriveKind(sector, organic, known)
	if !ok {
		return ""
	}
	return kind
}
