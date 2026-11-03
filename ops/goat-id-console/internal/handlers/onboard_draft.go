package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/audit"
	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/onboardstore"
)

// onboard_draft.go completes a minimal-burden draft.
//
// A draft is a proposal whose manifest_json holds the candidate-intake/v-draft
// record (invite, prebound set, the candidate's answers) instead of an operator
// manifest, because the assembler could not form a complete one at submit.
// Completion re-runs the same fold and assembly a fresh submission gets, under
// the CURRENT rules, and persists the result with complete=1. Nothing is
// granted here that the sponsor and candidate did not already state; both
// human gates still follow.
//
// Why it exists: the decide gate has refused incomplete drafts with "complete
// it in the console", but no console path ever cleared the flag, so every
// draft was terminal. The rows the blank-kind rule stranded complete through
// this path the first time anything touches them: a decision, a field edit,
// or the boot sweep.
//
// Every completion is a state change in its own right and writes one
// onboard_draft_complete audit row naming who caused it, whichever of the
// five paths ran it (the two review decisions, the field-edit path, the
// boot sweep, or an org-match attach/create).

// intakeRecord is the stored shape onboardstore.AssembleIntakeRecord writes.
type intakeRecord struct {
	SchemaVersion string          `json:"schema_version"`
	InviteID      string          `json:"invite_id"`
	Sponsor       string          `json:"sponsor"`
	SponsorName   string          `json:"sponsor_name"`
	Env           string          `json:"env"`
	Prebound      json.RawMessage `json:"prebound"`
	Submission    json.RawMessage `json:"candidate_submission"`
}

const intakeRecordSchema = "candidate-intake/v-draft"

// draftOutcome is what completeDraft found.
type draftOutcome int

const (
	// draftCompleted: the row is now complete; the caller's *Proposal was
	// updated in place.
	draftCompleted draftOutcome = iota
	// draftNoRecord: manifest_json is not an intake record, so there is nothing
	// to assemble from. The row keeps whatever it holds.
	draftNoRecord
	// draftBlocked: an intake record is present but still cannot assemble; the
	// accompanying reason names what is missing.
	draftBlocked
)

// The submit path's draft marker, removed once the draft completes so the
// queue note stops contradicting the row. Greedy match from the marker to the
// trailing "; admin to complete" means a blocker reason that itself contains
// parentheses (e.g. "(ADR 1137)") does not leave a tail behind on repeat.
var draftNoteRe = regexp.MustCompile(` — INCOMPLETE \(.*\); admin to complete`)

// draftCompleter names who caused a completion, for the audit row: the
// deciding or editing actor on a request, the console itself on the boot
// sweep.
type draftCompleter struct {
	actor     Actor
	requestID string
}

// requestCompleter resolves the completer for r: the browser session when
// there is one, otherwise the machine principal (see auditActor).
func requestCompleter(r *http.Request) draftCompleter {
	return draftCompleter{actor: auditActor(r), requestID: requestID(r)}
}

// sweepCompleter is the boot sweep's actor, the same label the console's
// other unattended acts carry.
var sweepCompleter = draftCompleter{actor: Actor{Email: "console-onboard"}}

// errDraftChanged: the row changed between the caller's read and the
// completion update, and not into a completed draft: another admin decided it,
// or it is gone. The caller answers 409 so the admin reloads.
var errDraftChanged = errors.New("the proposal changed while this request was in flight; reload and decide again")

// auditActor is the actor an audit row names for r. On the browser realm that
// is the session; on the machine realm the session is empty, so the resolved
// principal's handle stands in (or the realm label when there is none), the
// same value the store's decided_by column receives.
func auditActor(r *http.Request) Actor {
	if a := actorFromCtx(r.Context()); a.Email != "" {
		return a
	}
	return Actor{Email: onboardAPIActor(r)}
}

// completeDraft tries to turn p into a complete proposal on behalf of by. An
// error is returned only for store or config failures; a record that cannot
// assemble is an outcome, not an error.
func (s *Server) completeDraft(ctx context.Context, p *onboardstore.Proposal, by draftCompleter) (draftOutcome, string, error) {
	if s.testCompleteDraftOverride != nil {
		return s.testCompleteDraftOverride(ctx, p, by)
	}
	if p.Complete {
		return draftCompleted, "", nil
	}
	a, err := s.assembleDraft(p, loadIntakeHints)
	if err != nil {
		return draftBlocked, "", err
	}
	if a.outcome != draftCompleted {
		return a.outcome, a.why, nil
	}
	out, err := json.Marshal(a.manifest)
	if err != nil {
		return draftBlocked, "", fmt.Errorf("encode manifest: %w", err)
	}
	note := draftNoteRe.ReplaceAllString(p.Note, "") + " — draft completed"
	if err := s.Onboard.CompleteProposal(ctx, p.ID, a.handle, string(out), note); err != nil {
		if errors.Is(err, onboardstore.ErrNotFound) {
			return s.adoptRacedCompletion(ctx, p)
		}
		return draftBlocked, "", err
	}
	p.ManifestJSON, p.Complete, p.Note, p.Handle = string(out), true, note, a.handle
	s.Audit.Emit(ctx, audit.Event{
		Actor: audit.Actor{
			Email: by.actor.Email, IdentityID: by.actor.IdentityID,
			AAL: by.actor.AAL, SourceIP: by.actor.SourceIP,
		},
		Action:    "onboard_draft_complete",
		Target:    audit.Target{InviteTokenPrefix: p.ID},
		After:     map[string]interface{}{"handle": p.Handle, "tier": p.Tier},
		RequestID: by.requestID, Result: "ok",
	})
	return draftCompleted, "", nil
}

// draftAssembly is what assembleDraft found: on draftCompleted, the manifest
// the record forms and the handle it settles on; otherwise the outcome and
// its reason, worded for the admin who reads the queue note.
type draftAssembly struct {
	outcome  draftOutcome
	why      string
	manifest map[string]any
	handle   string
}

// intakeHintsSource supplies the parsed intake hints to assembleDraft.
// completeDraft passes loadIntakeHints; a queue render passes one
// sync.OnceValues(loadIntakeHints) for every row, so N drafts on a page cost
// one parse of the hints, not N.
type intakeHintsSource func() (*intakeHints, error)

// assembleDraft forms the manifest an intake record would complete to, and
// persists nothing. completeDraft writes the result; the review editor reads
// it to prefill its boxes on a draft, so what an admin sees before Apply is
// what Apply would write (docs/design/onboarding-review-field-edits.md). An
// error is returned only for config failures; a record that cannot assemble
// is an outcome, not an error.
func (s *Server) assembleDraft(p *onboardstore.Proposal, loadHints intakeHintsSource) (draftAssembly, error) {
	blocked := func(why string) draftAssembly { return draftAssembly{outcome: draftBlocked, why: why} }
	var rec intakeRecord
	if err := json.Unmarshal([]byte(p.ManifestJSON), &rec); err != nil || rec.SchemaVersion != intakeRecordSchema {
		return draftAssembly{outcome: draftNoRecord, why: "no intake record to assemble from"}, nil //nolint:nilerr // an unreadable record is a draft outcome, not a store failure (see doc comment)
	}
	hints, err := loadHints()
	if err != nil {
		return blocked(""), fmt.Errorf("intake hints: %w", err)
	}
	sub, err := decodeCandidateSubmission(rec.Submission)
	if err != nil {
		return blocked("the stored candidate submission is unreadable"), nil //nolint:nilerr // outcome, not error: the note tells the admin what blocks completion
	}
	inv := &onboardstore.Invite{
		ID:           rec.InviteID,
		Sponsor:      firstNonEmpty(rec.Sponsor, p.Sponsor),
		SponsorName:  firstNonEmpty(rec.SponsorName, p.SponsorName),
		Env:          firstNonEmpty(rec.Env, p.Env),
		PreboundJSON: string(rec.Prebound),
	}
	in, _, err := s.intakeEffective(hints, inv, sub)
	// The two intake refusals are worded for the candidate at submit. Here the
	// reader is an admin, so each names the gap and the one remedy that closes
	// it: a tier is the sponsor's to set on the re-invite; a handle is nobody's
	// to set (Prebound carries none, and review cannot edit it), so the
	// candidate has to come back under an address that forms one.
	if errors.Is(err, errTierRequired) {
		return blocked("missing tier: neither the sponsor nor the candidate set one, and tier cannot be set at review (ADR 1137); re-invite with the tier set by the sponsor"), nil
	}
	if errors.Is(err, errHandleRequired) {
		return blocked("missing handle: the candidate's email address cannot form an account name, and a handle cannot be set at review; re-invite and have them submit under a different address"), nil
	}
	if err != nil {
		return blocked(err.Error()), nil //nolint:nilerr // intake refusal is the draft's blocker text, not a failure of assembleDraft
	}
	manifest, err := assembleManifest(hints, in)
	var inc *incompleteManifestError
	if errors.As(err, &inc) {
		return blocked("missing " + strings.Join(inc.missing, ", ")), nil
	}
	if err != nil {
		return blocked(err.Error()), nil //nolint:nilerr // assembly refusal is the draft's blocker text, not a failure of assembleDraft
	}
	return draftAssembly{outcome: draftCompleted, manifest: manifest, handle: firstNonEmpty(in.Handle, p.Handle)}, nil
}

// adoptRacedCompletion handles a completion update that matched zero rows: the
// row is no longer an incomplete draft in a review stage. Two admins clicking
// at once is the expected cause, so when the row is now complete the loser
// adopts the winner's row and writes no second audit row; the winner's names
// who completed it. Any other change is reported as errDraftChanged.
func (s *Server) adoptRacedCompletion(ctx context.Context, p *onboardstore.Proposal) (draftOutcome, string, error) {
	cur, err := s.Onboard.GetProposal(ctx, p.ID)
	if errors.Is(err, onboardstore.ErrNotFound) {
		return draftBlocked, "", errDraftChanged
	}
	if err != nil {
		return draftBlocked, "", fmt.Errorf("re-read after a zero-row completion: %w", err)
	}
	if !cur.Complete {
		return draftBlocked, "", errDraftChanged
	}
	*p = *cur
	return draftCompleted, "", nil
}

// advanceDraft completes a draft in place ahead of an advancing decision, or
// returns the decideError that stops the decision. Callers run it only after
// the per-action status check has passed, so a decision the store would
// refuse never writes a completion first.
func (s *Server) advanceDraft(r *http.Request, p *onboardstore.Proposal) *decideError {
	if p.Complete {
		return nil
	}
	outcome, why, err := s.completeDraft(r.Context(), p, requestCompleter(r))
	if errors.Is(err, errDraftChanged) {
		return &decideError{status: http.StatusConflict, msg: err.Error()}
	}
	if err != nil {
		return &decideError{status: http.StatusInternalServerError, msg: "could not complete the draft: " + err.Error()}
	}
	switch outcome {
	case draftCompleted:
		return nil
	case draftNoRecord:
		return &decideError{status: http.StatusBadRequest,
			msg: "this proposal is an incomplete draft with no intake record to assemble from — it cannot advance; reject it"}
	default:
		// why carries its own remedy (completeDraft), so the wrapper does not
		// add one: "set by the sponsor" is wrong for a handle.
		return &decideError{status: http.StatusBadRequest,
			msg: fmt.Sprintf("this proposal is an incomplete draft — %s — so it cannot advance; reject it", why)}
	}
}

// CompleteDrafts sweeps the review-stage queue at boot and completes every
// draft the current rules can assemble, so the queue's "incomplete draft" flags
// are honest without waiting for someone to click. Returns how many completed
// and how many remain blocked. A row that fails hard is reported in the joined
// error and skipped; it never stops the rows behind it.
func (s *Server) CompleteDrafts(ctx context.Context) (completed, remaining int, err error) {
	if s.Onboard == nil {
		return 0, 0, nil
	}
	var errs []error
	for _, status := range []string{onboardstore.ProposalProposed, onboardstore.ProposalSponsorApproved} {
		rows, lerr := s.Onboard.ListProposals(ctx, status)
		if lerr != nil {
			errs = append(errs, fmt.Errorf("list %s proposals: %w", status, lerr))
			continue
		}
		done, left, serr := s.sweepDrafts(ctx, rows)
		completed += done
		remaining += left
		if serr != nil {
			errs = append(errs, serr)
		}
	}
	return completed, remaining, errors.Join(errs...)
}

func (s *Server) sweepDrafts(ctx context.Context, rows []onboardstore.Proposal) (completed, remaining int, err error) {
	var errs []error
	for i := range rows {
		p := &rows[i]
		if p.Complete {
			continue
		}
		outcome, why, cerr := s.completeDraft(ctx, p, sweepCompleter)
		switch {
		case cerr != nil:
			errs = append(errs, fmt.Errorf("draft %s: %w", p.ID, cerr))
			slog.Error("onboarding draft sweep: row failed", slog.String("proposal", p.ID), slog.Any("err", cerr))
		case outcome == draftCompleted:
			completed++
			slog.Info("onboarding draft completed", slog.String("proposal", p.ID), slog.String("handle", p.Handle))
		default:
			remaining++
			slog.Warn("onboarding draft still incomplete", slog.String("proposal", p.ID), slog.String("why", why))
		}
	}
	return completed, remaining, errors.Join(errs...)
}
