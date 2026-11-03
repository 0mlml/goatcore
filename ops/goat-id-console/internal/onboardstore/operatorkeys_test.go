package onboardstore

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/usareur-af/goatcore/ops/goat-id-console/internal/pgtest"
)

func opStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(pgtest.DSN(t))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// Requesting must not grant. If a request carried a usable secret, "requested"
// and "granted" would be indistinguishable to anyone who got hold of the row —
// and the admin approval this whole flow is built around would be decoration.
func TestRequestOperatorKey_mintsNothing(t *testing.T) {
	ctx := context.Background()
	s := opStore(t)
	row, err := s.RequestOperatorKey(ctx, &OperatorKey{Handle: "ada", Env: "zagros"})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if row.Status != OperatorKeyRequested {
		t.Errorf("status = %q, want %q", row.Status, OperatorKeyRequested)
	}
	if row.Secret != "" || row.Fingerprint != "" {
		t.Error("a request must carry no key material")
	}
}

// Two live keys for one person is two things to revoke, and one of them gets
// missed.
func TestRequestOperatorKey_doesNotStackDuplicates(t *testing.T) {
	ctx := context.Background()
	s := opStore(t)
	first, err := s.RequestOperatorKey(ctx, &OperatorKey{Handle: "ada", Env: "zagros"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.RequestOperatorKey(ctx, &OperatorKey{Handle: "ada", Env: "zagros"})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != first.ID {
		t.Errorf("re-request created a second live row (%s vs %s)", again.ID, first.ID)
	}
	// A different goatnet is a different key, and must not be blocked.
	other, err := s.RequestOperatorKey(ctx, &OperatorKey{Handle: "ada", Env: "zagros-prod"})
	if err != nil {
		t.Fatalf("second goatnet: %v", err)
	}
	if other.ID == first.ID {
		t.Error("a request for another goatnet must be its own row")
	}
}

// The secret exists in the database only between approval and its single
// delivery. A console database that permanently held live bearer tokens would
// be a strictly worse thing to lose.
func TestCollectOperatorKey_deliversOnceThenForgets(t *testing.T) {
	ctx := context.Background()
	s := opStore(t)
	row, _ := s.RequestOperatorKey(ctx, &OperatorKey{Handle: "ada", Env: "zagros"})
	// The decider presents a handle as well as an email: this row records no
	// email, so an email-only decider could not be compared to it at all and
	// is refused (see TestOperatorKeyDecisions_refuseUncomparableDecider).
	approved, err := s.ApproveOperatorKey(ctx, row.ID, "admin@example.test", []string{"admin"})
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if approved.Secret == "" || approved.Fingerprint == "" {
		t.Fatal("approval must mint a key and record its fingerprint")
	}

	got, err := s.CollectOperatorKey(ctx, row.ID, "ada")
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if got != approved.Secret {
		t.Error("collect returned a different key than approval minted")
	}

	if _, err := s.CollectOperatorKey(ctx, row.ID, "ada"); !errors.Is(err, ErrOperatorKeyAlreadyDelivered) {
		t.Errorf("second collect err = %v, want ErrOperatorKeyAlreadyDelivered", err)
	}
	after, _ := s.OperatorKey(ctx, row.ID)
	if after.Secret != "" {
		t.Error("the secret must be gone from the row after delivery")
	}
	if after.Fingerprint == "" {
		t.Error("the fingerprint must survive — it is how the key is named in audit")
	}
	if after.Status != OperatorKeyActive {
		t.Errorf("delivery must not change the key's validity, status = %q", after.Status)
	}
}

// Collection is bound to the requester. Without that, any signed-in person who
// could name an id would be handed somebody else's credential.
func TestCollectOperatorKey_refusesAnotherPersonsKey(t *testing.T) {
	ctx := context.Background()
	s := opStore(t)
	row, _ := s.RequestOperatorKey(ctx, &OperatorKey{Handle: "ada", Env: "zagros"})
	if _, err := s.ApproveOperatorKey(ctx, row.ID, "admin", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CollectOperatorKey(ctx, row.ID, "mallory"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound for a key belonging to somebody else", err)
	}
	// And the rightful owner is unaffected by the attempt.
	if _, err := s.CollectOperatorKey(ctx, row.ID, "ada"); err != nil {
		t.Errorf("owner collect after a refused attempt: %v", err)
	}
}

// Revocation drops the key from the set the portal should accept, and clears
// any undelivered secret so a revoked request cannot still be collected.
func TestRevokeOperatorKey_leavesNothingCollectableOrActive(t *testing.T) {
	ctx := context.Background()
	s := opStore(t)
	row, _ := s.RequestOperatorKey(ctx, &OperatorKey{Handle: "ada", Env: "zagros"})
	if _, err := s.ApproveOperatorKey(ctx, row.ID, "admin", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RevokeOperatorKey(ctx, row.ID, "admin", "left the team"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := s.CollectOperatorKey(ctx, row.ID, "ada"); err == nil {
		t.Error("a revoked key must not be collectable")
	}
	handles, err := s.ActiveHandles(ctx, "zagros")
	if err != nil {
		t.Fatal(err)
	}
	if len(handles) != 0 {
		t.Errorf("active handles after revoke = %v, want none", handles)
	}
	// Revoking twice is not an error worth raising at a caller who is trying to
	// make sure something is gone.
	if _, err := s.RevokeOperatorKey(ctx, row.ID, "admin", ""); err != nil {
		t.Errorf("second revoke: %v", err)
	}
	// And the person can then request afresh — revocation frees the slot.
	if _, err := s.RequestOperatorKey(ctx, &OperatorKey{Handle: "ada", Env: "zagros"}); err != nil {
		t.Errorf("re-request after revoke: %v", err)
	}
}

// A rejected request mints nothing, and cannot later be approved into one.
func TestRejectOperatorKey_closesTheRequest(t *testing.T) {
	ctx := context.Background()
	s := opStore(t)
	row, _ := s.RequestOperatorKey(ctx, &OperatorKey{Handle: "ada", Env: "zagros"})
	rejected, err := s.RejectOperatorKey(ctx, row.ID, "admin", "no portal work yet", nil)
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if rejected.Status != OperatorKeyRejected || rejected.Secret != "" {
		t.Errorf("rejected row = %+v", rejected)
	}
	if _, err := s.ApproveOperatorKey(ctx, row.ID, "admin", nil); !errors.Is(err, ErrOperatorKeyWrongState) {
		t.Errorf("approving a rejected request err = %v, want ErrOperatorKeyWrongState", err)
	}
}

// The two-person rule (#210): the person whose request it is cannot be the
// one who decides it, however their identity happens to be presented — by
// handle, by email, in any case. Requesting a key and then approving it
// yourself must be refused.
func TestOperatorKeyDecisions_refuseSelfDecision(t *testing.T) {
	ctx := context.Background()
	s := opStore(t)
	row, err := s.RequestOperatorKey(ctx, &OperatorKey{
		Handle: "examplehandle890", Email: "Tyler@Example.test", Env: "zagros"})
	if err != nil {
		t.Fatal(err)
	}

	// Every identity form of the requester is refused, on both decisions.
	for _, decider := range []struct {
		decidedBy string
		ids       []string
	}{
		{"examplehandle890", nil},                            // handle as decidedBy
		{"tyler@example.test", nil},                      // email, different case
		{"admin@example.test", []string{"Examplehandle890"}}, // matched via a resolved form
	} {
		if _, err := s.ApproveOperatorKey(ctx, row.ID, decider.decidedBy, decider.ids); !errors.Is(err, ErrOperatorKeySelfDecision) {
			t.Errorf("self-approve as %v = %v, want ErrOperatorKeySelfDecision", decider, err)
		}
		if _, err := s.RejectOperatorKey(ctx, row.ID, decider.decidedBy, "", decider.ids); !errors.Is(err, ErrOperatorKeySelfDecision) {
			t.Errorf("self-reject as %v = %v, want ErrOperatorKeySelfDecision", decider, err)
		}
	}

	// A refused decision must leave the row untouched and decidable.
	cur, err := s.OperatorKey(ctx, row.ID)
	if err != nil || cur.Status != OperatorKeyRequested || cur.Secret != "" {
		t.Fatalf("row after refusals = %+v, %v; want untouched requested row", cur, err)
	}

	// A DIFFERENT admin still decides normally.
	approved, err := s.ApproveOperatorKey(ctx, row.ID, "other-admin@example.test", []string{"other-admin"})
	if err != nil {
		t.Fatalf("different-admin approve: %v", err)
	}
	if approved.Status != OperatorKeyActive {
		t.Fatalf("status = %q, want active", approved.Status)
	}

	// Self-REVOKE stays allowed: removing your own live key is de-escalation.
	revoked, err := s.RevokeOperatorKey(ctx, row.ID, "examplehandle890", "lost it")
	if err != nil {
		t.Fatalf("self-revoke: %v", err)
	}
	if revoked.Status != OperatorKeyRevoked {
		t.Fatalf("status = %q, want revoked", revoked.Status)
	}
}

// No decider identity at all is refused fail-closed — an unverifiable decider
// cannot be shown to be a second person. It refuses with the NAMED sentinel,
// not a bare error: the doors turn this into a 403 that says what happened,
// and a generic error would surface as a 500 the caller would retry forever.
func TestOperatorKeyDecisions_refuseAnonymousDecider(t *testing.T) {
	ctx := context.Background()
	s := opStore(t)
	row, err := s.RequestOperatorKey(ctx, &OperatorKey{Handle: "ada", Env: "zagros"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApproveOperatorKey(ctx, row.ID, "", nil); !errors.Is(err, ErrOperatorKeyUnverifiedDecider) {
		t.Fatalf("approve with no decider identity = %v, want ErrOperatorKeyUnverifiedDecider", err)
	}
	if _, err := s.RejectOperatorKey(ctx, row.ID, "", "", []string{" "}); !errors.Is(err, ErrOperatorKeyUnverifiedDecider) {
		t.Fatalf("reject with no decider identity = %v, want ErrOperatorKeyUnverifiedDecider", err)
	}
}

// THE HOLE THE FIRST CUT LEFT OPEN. A key requested through the machine door
// records a handle and no email; a console admin whose login resolves to no
// handle presents only an email. Comparing those two produced a non-match —
// and a non-match was read as "a different person", so the requester could
// approve their own key through the browser exactly as before. The owner side
// is now resolved through the onboarding record first, so the two identities
// meet in the same dimension and the self-decision is caught.
func TestOperatorKeyDecisions_selfDecisionAcrossIdentityForms(t *testing.T) {
	ctx := context.Background()
	s := opStore(t)
	if err := s.CreateProposal(ctx, &Proposal{ID: "p-ada", Sponsor: "s", Env: "zagros",
		Handle: "ada", Email: "Ada@example.test", Status: ProposalOnboarded, ManifestJSON: "{}"}); err != nil {
		t.Fatal(err)
	}
	// Handle only — exactly what `goat admin operator-key request` records.
	row, err := s.RequestOperatorKey(ctx, &OperatorKey{Handle: "ada", Env: "zagros"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.ApproveOperatorKey(ctx, row.ID, "ada@example.test", nil); !errors.Is(err, ErrOperatorKeySelfDecision) {
		t.Fatalf("self-approve by resolved email = %v, want ErrOperatorKeySelfDecision", err)
	}
	if _, err := s.RejectOperatorKey(ctx, row.ID, "ada@example.test", "", nil); !errors.Is(err, ErrOperatorKeySelfDecision) {
		t.Fatalf("self-reject by resolved email = %v, want ErrOperatorKeySelfDecision", err)
	}
	if cur, _ := s.OperatorKey(ctx, row.ID); cur.Status != OperatorKeyRequested || cur.Secret != "" {
		t.Fatalf("row after refusals = %+v, want untouched requested", cur)
	}

	// Somebody else's email is now comparable too, and passes.
	if _, err := s.ApproveOperatorKey(ctx, row.ID, "other@example.test", nil); err != nil {
		t.Fatalf("different-admin approve by email: %v", err)
	}
}

// When the two sides genuinely cannot be compared — no onboarding record to
// resolve the owner's handle, no email on the row, no handle on the decider —
// the decision is REFUSED rather than allowed. A non-match between an email
// and a handle is not evidence of two people, and the whole point of this rule
// is that a second person must be shown, not assumed.
func TestOperatorKeyDecisions_refuseUncomparableDecider(t *testing.T) {
	ctx := context.Background()
	s := opStore(t)
	row, err := s.RequestOperatorKey(ctx, &OperatorKey{Handle: "ada", Env: "zagros"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ApproveOperatorKey(ctx, row.ID, "admin@example.test", nil)
	if !errors.Is(err, ErrOperatorKeyUnverifiedDecider) {
		t.Fatalf("email-only decider vs handle-only row = %v, want ErrOperatorKeyUnverifiedDecider", err)
	}
	// The refusal names both dimensions, because that is what tells the
	// operator which of the two ends to fix.
	if msg := err.Error(); !strings.Contains(msg, "email") || !strings.Contains(msg, "handle") {
		t.Errorf("refusal must name both dimensions; got %q", msg)
	}

	// The same person presenting a handle as well IS comparable, and passes.
	if _, err := s.ApproveOperatorKey(ctx, row.ID, "admin@example.test", []string{"admin"}); err != nil {
		t.Fatalf("decider with a handle form: %v", err)
	}
}

// State is checked before the two-person rule. Re-clicking Approve on a row
// that is already decided must say so — answering "that is your own request"
// would name a rule that is not why the click failed.
func TestOperatorKeyDecisions_stateBeatsTheTwoPersonRule(t *testing.T) {
	ctx := context.Background()
	s := opStore(t)
	row, err := s.RequestOperatorKey(ctx, &OperatorKey{
		Handle: "ada", Email: "ada@example.test", Env: "zagros"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApproveOperatorKey(ctx, row.ID, "other@example.test", nil); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if _, err := s.ApproveOperatorKey(ctx, row.ID, "ada@example.test", nil); !errors.Is(err, ErrOperatorKeyWrongState) {
		t.Errorf("owner re-approving a decided row = %v, want ErrOperatorKeyWrongState", err)
	}
	if _, err := s.RejectOperatorKey(ctx, row.ID, "ada@example.test", "", nil); !errors.Is(err, ErrOperatorKeyWrongState) {
		t.Errorf("owner rejecting a decided row = %v, want ErrOperatorKeyWrongState", err)
	}
}
