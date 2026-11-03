// applier.go — the apply-pipeline contract for the wg-cp0 declarative
// substrate orchestrator (ADR 0322 / Block 22R.2).
//
// This file is the load-bearing interface surface that all subcommand
// bodies build against. Three concurrent sessions are authoring this
// binary in parallel; this file is owned by Session A and lands first
// so Session B (lifecycle subcommands: onboard / rotate / remove /
// resurrect) and Session C (metadata + release: update-endpoints /
// update-ttl / add-relay + release pipeline) can pull the contract as
// the starting point for their work.
//
// The pipeline is intentionally small and explicit:
//
//   1. The operator's subcommand parses argv into a concrete Intent
//      value (ReconcileIntent, OnboardIntent, etc.). Each Intent kind
//      is owned by one session — A owns Reconcile, B owns the
//      lifecycle set, C owns the metadata set.
//
//   2. The subcommand calls applier.Plan(ctx, intent), which loads
//      the PeerSet from the JSON-of-record (peerset.Load), reads the
//      live state from every relay over SSH-over-wg-cp0 (bounded
//      concurrency, partial-failure non-fatal), then dispatches to
//      a planner function that returns the ordered Mutation list +
//      the structural diff (PeerSetDiff + LiveStateDiff).
//
//   3. The subcommand prints the diff via the diff_formatter package
//      (text by default, --json for scripting), then exits if
//      --dry-run / no --apply.
//
//   4. With --apply the subcommand calls applier.Apply(ctx, plan),
//      which:
//        - re-runs pre-flight + re-reads live state to detect parallel
//          operator drift between plan-time and apply-time;
//        - writes the PeerSet via peerset.Save (atomic temp+rename);
//        - executes Mutations in order under bounded concurrency;
//        - re-reads live state and verifies the apply landed;
//        - emits one AuditRecord row to the JSONL audit log.
//
// Properties this pipeline enforces by construction (ADR 0322 §2):
//
//   - Single source of truth — every mutation reads + writes the
//     PeerSet via peerset.Load + peerset.Save. No tool that bypasses
//     the accessor library is in the apply path.
//
//   - Dry-run by default — Plan never opens a wg-set, never restarts
//     a daemon, never writes a file outside the dry-run report. The
//     diff is the structural catch for partial-input rewrites.
//
//   - Upsert-not-rewrite — removal requires explicit --remove and is
//     enumerated in the dry-run before any wire move.
//
//   - Audit-record per apply — exactly one JSONL row per --apply
//     invocation; dry-runs do not append.
//
// The interface stays small on purpose: B and C should be able to
// write their planners against the types here without further
// coordination. Extensions (richer audit metadata, batched mutation
// channels, etc.) compose on top.

package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/usareur-af/goatcore/ops/enrollment/peerset"
)

// osHostnameOrEmpty returns os.Hostname() or "" on error.
func osHostnameOrEmpty() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}

// resolveCommandLine returns os.Args concatenated. Captured at
// audit-time so the full operator invocation is in the record.
func resolveCommandLine() string {
	return strings.Join(os.Args, " ")
}

// defaultRandReader returns the crypto/rand reader. Indirection lets
// tests stub deterministic randomness for correlation IDs.
func defaultRandReader() io.Reader { return rand.Reader }

// Intent is the operator's logical request. Each subcommand
// constructs a concrete Intent value (ReconcileIntent, OnboardIntent,
// RotateIntent, RemoveIntent, ResurrectIntent, UpdateEndpointsIntent,
// UpdateTTLIntent, AddRelayIntent) and hands it to Applier.Plan.
//
// The marker interface keeps the dispatch table at the package level
// rather than in init() (no global side-effects); concrete Intent
// types live next to their subcommand bodies (cmd_onboard.go etc.).
type Intent interface {
	// Kind returns the intent's discriminator. Used by the Applier's
	// dispatch table and by the audit record.
	Kind() string
}

// ReconcileIntent walks the full intended PeerSet against live state
// and produces the union diff. Owned by Session A.
type ReconcileIntent struct {
	// Site narrows the reconcile to peers + relays at one site.
	// Empty string means fleet-wide.
	Site string

	// AllowRemove permits the reconcile to drop peers that appear on
	// relays but not in the declared PeerSet. Default false (upsert-
	// only). The --remove flag flips this true; the operator sees the
	// removal list in the dry-run before --apply lands.
	AllowRemove bool

	// AllowAdd permits the reconcile to add declared peers that are
	// missing from one or more relays. Default true. --no-add inverts.
	AllowAdd bool
}

// Kind implements Intent.
func (ReconcileIntent) Kind() string { return "reconcile" }

// Mutation is one transactional change in an ApplyPlan. Concrete
// Mutation implementations live next to their owning planner (Session
// A authors the reconcile mutations: RelayAddPeerMutation,
// RelayRemovePeerMutation, RelayRotatePubkeyMutation, StateMutation).
// B and C may author additional mutation types if a lifecycle path
// needs a wire-side step the reconcile primitives do not cover (e.g.
// a bundle-mint step that calls bundle-create internally).
//
// The Applier executes mutations in the order they appear in
// ApplyPlan.Mutations. PreFlight runs against every mutation before
// any Apply call lands so a doomed plan fails before any wire move.
// Apply executes one wire-side step; per-mutation failure is captured
// and the Applier may continue with subsequent mutations depending on
// the FailFast setting in ApplyOptions.
type Mutation interface {
	// Kind returns the mutation discriminator:
	//   "state"      — peerset.json write
	//   "relay-add"  — wg set peer <pk> allowed-ips <ip>/32
	//   "relay-rotate" — wg set peer <new>; wg set peer <old> remove
	//   "relay-remove" — wg set peer <pk> remove
	//   "endpoint-update" — bundle re-render with same keypair
	//   "ttl-update" — peerset-only TTL extension
	//   "add-relay" — relays[] insert + cascade endpoint updates
	Kind() string

	// Describe returns a single line operators can scan in 5s.
	// Example: "wg set wg-cp0 peer K8x9... allowed-ips 198.18.0.10/32 on relay-rla"
	Describe() string

	// PreFlight validates the mutation against current state without
	// touching the wire. Called before any Apply on any mutation.
	// Returns nil on success.
	PreFlight(ctx context.Context) error

	// Apply executes the mutation. Returns nil on success. On
	// failure, the Applier records the error in StepOutcome and (per
	// ApplyOptions.FailFast) either aborts or continues.
	Apply(ctx context.Context) error
}

// ApplyPlan is what Plan returns. The DryRun flag is set true at Plan
// time; Apply flips it false on entry and stamps AppliedAt so a plan
// printed at dry-run-time and again at apply-time is distinguishable.
type ApplyPlan struct {
	// Intent is the operator's original request. Preserved for audit
	// emission + apply-time re-validation.
	Intent Intent

	// DryRun is true until Apply consumes the plan. Plan always
	// returns DryRun=true.
	DryRun bool

	// Mutations is the ordered list of intended changes. Applier
	// executes in slice order; concurrency at the per-mutation level
	// is the mutation's responsibility (e.g. RelayAddPeerMutation
	// fans out across N relays under bounded concurrency internally).
	Mutations []Mutation

	// StatePlan is the PeerSet delta (additive form). Used by the
	// diff_formatter for the "WOULD ADD / REMOVE / ROTATE / KEEP"
	// preview block. Nil only when the mutation does not touch the
	// JSON-of-record (which is rare; even endpoint updates touch the
	// Relay endpoint field).
	StatePlan *PeerSetDiff

	// LivePlan is the relay-side wire-state delta. Per-relay so
	// partial reachability surfaces as Unreachable=true on the
	// individual entries rather than as a fatal error.
	LivePlan *LiveStateDiff

	// PlannedAt is the wall-clock at Plan() entry. AppliedAt is
	// stamped by Apply.
	PlannedAt time.Time
	AppliedAt time.Time

	// CorrelationID is a monotonic identifier the audit log carries
	// so apply-time records can be cross-referenced with future audit
	// consumers (Block 17D integrity tier, Block 27 wide-events).
	CorrelationID string
}

// IsEmpty returns true when the plan would change nothing. Used by
// `wg-cp0 diff` to set the exit code (0 = clean, 1 = drift).
func (p *ApplyPlan) IsEmpty() bool {
	if p == nil {
		return true
	}
	if len(p.Mutations) > 0 {
		return false
	}
	if p.StatePlan != nil && !p.StatePlan.IsEmpty() {
		return false
	}
	if p.LivePlan != nil && !p.LivePlan.IsEmpty() {
		return false
	}
	return true
}

// PeerSetDiff is the delta against the JSON-of-record (additive form).
// Each list holds rows describing the before/after of one peer or
// relay. Used by the diff_formatter.
type PeerSetDiff struct {
	Added            []PeerDelta
	Removed          []PeerDelta
	Rotated          []PeerDelta
	EndpointsUpdated []PeerDelta
	TTLUpdated       []PeerDelta
	RelaysAdded      []RelayDelta
	Unchanged        []PeerDelta
}

// IsEmpty returns true when no field has any entries.
func (d *PeerSetDiff) IsEmpty() bool {
	if d == nil {
		return true
	}
	return len(d.Added) == 0 &&
		len(d.Removed) == 0 &&
		len(d.Rotated) == 0 &&
		len(d.EndpointsUpdated) == 0 &&
		len(d.TTLUpdated) == 0 &&
		len(d.RelaysAdded) == 0
}

// PeerDelta is one row in a PeerSetDiff. Before is nil for additions;
// After is nil for removals. Reason carries the operator-supplied
// explanation on rotates / removes (audit signal).
type PeerDelta struct {
	Name   string
	Before *peerset.Peer
	After  *peerset.Peer
	Reason string
}

// RelayDelta is the relay-side analog of PeerDelta. Used by
// AddRelayIntent.
type RelayDelta struct {
	Name   string
	Before *peerset.Relay
	After  *peerset.Relay
}

// LiveStateDiff is the per-relay wg-set delta. The "per relay"
// shape is load-bearing — partial reachability is a recovery case,
// not a fatal error, and the audit log must capture which relays were
// touched.
type LiveStateDiff struct {
	PerRelay []RelayLiveDiff
}

// IsEmpty returns true when every RelayLiveDiff is empty.
func (d *LiveStateDiff) IsEmpty() bool {
	if d == nil {
		return true
	}
	for _, r := range d.PerRelay {
		if !r.IsEmpty() {
			return false
		}
	}
	return true
}

// RelayLiveDiff describes the wire-state delta for one relay.
// Unreachable=true means the relay was not reachable at plan time;
// the dry-run formatter surfaces this as "WOULD SKIP relay-X
// (unreachable: <err>)" so the operator can decide to proceed with
// partial fleet coverage or to investigate first.
type RelayLiveDiff struct {
	Relay          string // matches peerset.Relay.Name
	WouldAdd       []LivePeerOp
	WouldRotate    []LivePeerOp
	WouldRemove    []LivePeerOp
	Unchanged      []LivePeerOp
	Unreachable    bool
	UnreachableErr string
}

// IsEmpty returns true when this relay row carries no operator-
// visible information. An Unreachable relay is NOT empty — the
// operator must see "WOULD SKIP relay-X (unreachable: <err>)" in the
// dry-run and decide whether partial-fleet apply is acceptable. The
// `wg-cp0 diff` exit code (0=clean, 1=drift) treats unreachability
// as drift so a cron-grade probe can alert on it.
func (r RelayLiveDiff) IsEmpty() bool {
	if r.Unreachable {
		return false
	}
	return len(r.WouldAdd) == 0 &&
		len(r.WouldRotate) == 0 &&
		len(r.WouldRemove) == 0
}

// LivePeerOp describes one wg-set operation a relay would receive.
type LivePeerOp struct {
	PeerName   string
	NewPubkey  string // for add / rotate
	OldPubkey  string // for rotate / remove
	AllowedIPs string // e.g. "198.18.0.10/32"
}

// Applier owns the planning + apply pipeline. One Applier per
// orchestrator invocation. The concrete implementation is private
// (constructed via NewApplier); B and C consume the interface from
// their subcommand bodies.
type Applier interface {
	// Plan expands an Intent into an ApplyPlan against the current
	// PeerSet + live state. Pure read; safe to call multiple times.
	// The returned plan's DryRun field is true; callers must invoke
	// Apply to mutate.
	Plan(ctx context.Context, intent Intent) (*ApplyPlan, error)

	// Apply executes the plan. The plan must have been returned by
	// the same Applier (the implementation caches the live-state
	// snapshot used at plan time for the apply-time drift check).
	// Returns a non-nil ApplyResult on every code path so the audit
	// record captures even partial-failure state.
	Apply(ctx context.Context, plan *ApplyPlan) (*ApplyResult, error)
}

// ApplyResult is what Apply returns. Outcome is "ok" on every-step
// success, "partial" if some succeeded + some failed, "fail" if zero
// succeeded. PerStep is one row per Mutation, in the order they ran.
type ApplyResult struct {
	Outcome     string // "ok" | "partial" | "fail"
	PerStep     []StepOutcome
	StartedAt   time.Time
	FinishedAt  time.Time
	AuditRecord *AuditRecord
	Err         error // non-nil iff Outcome != "ok"
}

// StepOutcome describes one mutation's execution result.
type StepOutcome struct {
	Kind       string `json:"kind"`
	Describe   string `json:"describe"`
	Outcome    string `json:"outcome"` // "ok" | "fail" | "skipped"
	DurationMs int64  `json:"duration_ms"`
	Err        string `json:"err,omitempty"`
}

// ApplierOptions configures a new Applier.
type ApplierOptions struct {
	// PeerSetPath is the JSON-of-record (today
	// ops/enrollment/wg-cp0-bundle-allowlist.json).
	PeerSetPath string

	// Live reads the live wg-set state from each relay. Required for
	// Plan against non-empty PeerSet.Relays; tests may substitute a
	// MockLiveStateReader for unit-level coverage.
	Live LiveStateReader

	// Relay executes wire-side changes. Required for Apply; tests
	// substitute a MockRelayMutator that records calls without
	// touching real SSH.
	Relay RelayMutator

	// Audit appends one record per --apply invocation. Required for
	// Apply when the caller wants durable provenance. Pass a Noop
	// emitter in tests that do not care about audit shape.
	Audit AuditEmitter

	// Stdout is used for tty detection by the diff formatter. May be
	// nil in tests (defaults to os.Stdout).
	Stdout io.Writer

	// Now is the wall-clock source. Test hooks inject a fake clock
	// for deterministic round-trip evidence; production callers pass
	// nil and Applier uses time.Now.
	Now func() time.Time
}

// Planner expands one Intent kind into an ApplyPlan against the
// loaded PeerSet + LiveState. Session A registers planReconcile for
// ReconcileIntent; B + C register their lifecycle / metadata planners.
//
// The PlanEnv carries the shared state every planner consumes —
// loading the PeerSet once and reading the live state once amortizes
// the SSH round-trip across multiple plan attempts in one shell
// session (the design doc §3.3 cache hint).
type Planner func(env *PlanEnv, intent Intent) (*ApplyPlan, error)

// PlanEnv is the shared state Plan handlers consume. Read once per
// Applier.Plan call.
type PlanEnv struct {
	// PeerSet is the loaded JSON-of-record. Plans MUST NOT mutate
	// the in-place PeerSet; instead they construct a copy, mutate
	// the copy, and embed the copy in a StateWriteMutation.
	PeerSet *peerset.PeerSet

	// Live is the per-relay wg-set state snapshot. Each relay's
	// reachability is in PerRelay[name].Reachable; planners must
	// surface unreachable relays in the LiveStateDiff rather than
	// silently skip them (the audit + dry-run formatter rely on
	// every-relay coverage).
	Live LiveState

	// Now is the wall-clock at PlanEnv construction. Planners stamp
	// rotated_at / removed_at fields from this so a re-run produces
	// identical timestamps on the same input.
	Now time.Time

	// PeerSetPath is the on-disk path the StateWriteMutation will
	// write to. Carried here so planners do not have to thread it
	// through their own argument list.
	PeerSetPath string

	// Mutator is the RelayMutator the planner attaches to each
	// wire-side Mutation it constructs (RelayAddPeerMutation,
	// RelayRotatePubkeyMutation, RelayRemovePeerMutation,
	// RelayPersistMutation). Nil in pure-read / dry-run-only paths
	// (`wg-cp0 diff` etc.); planners that build wire mutations
	// against nil should construct a "would-touch-relay-X" diff row
	// but no Mutation (the diff is the operator-visible plan, the
	// Mutation is only created when there is a mutator to run it).
	Mutator RelayMutator
}

// applierImpl is the concrete Applier. NewApplier returns this.
type applierImpl struct {
	opts     ApplierOptions
	planners map[string]Planner
	hostname string // resolved once at construction
}

// NewApplier constructs the concrete applier with its load-bearing
// dependencies wired in. Returns the interface (Applier) so callers
// program against the contract; cast to *applierImpl is the seam for
// RegisterPlanner (session B + C call it on the value returned).
func NewApplier(opts ApplierOptions) (Applier, error) {
	if opts.PeerSetPath == "" {
		return nil, errors.New("wg-cp0 applier: PeerSetPath required")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	a := &applierImpl{
		opts:     opts,
		planners: map[string]Planner{},
		hostname: osHostnameOrEmpty(),
	}
	// Register Session A's reconcile planner by default. B + C
	// register theirs by calling RegisterPlanner on the value
	// returned from NewApplier (after type-asserting to *applierImpl
	// or via the helper RegisterPlanner free function below).
	a.planners[(ReconcileIntent{}).Kind()] = planReconcile
	return a, nil
}

// RegisterPlanner installs a planner on the given Applier. Free
// function (not a method on the interface) so B + C call it without
// importing the unexported applierImpl type. Panics on duplicate kind
// (caller bug — two subcommands claimed the same intent kind).
func RegisterPlanner(a Applier, kind string, p Planner) {
	impl, ok := a.(*applierImpl)
	if !ok {
		panic(fmt.Sprintf("wg-cp0: RegisterPlanner called on unknown Applier type %T", a))
	}
	if _, dup := impl.planners[kind]; dup {
		panic("wg-cp0: duplicate planner for intent kind " + kind)
	}
	impl.planners[kind] = p
}

// Plan implements Applier. Loads the PeerSet, reads the live state,
// dispatches to the registered planner for the intent's kind.
func (a *applierImpl) Plan(ctx context.Context, intent Intent) (*ApplyPlan, error) {
	ps, err := peerset.Load(a.opts.PeerSetPath)
	if err != nil {
		return nil, fmt.Errorf("wg-cp0 plan: load peerset: %w", err)
	}
	var live LiveState
	if a.opts.Live != nil {
		live, err = a.opts.Live.Read(ctx, ps.Relays)
		if err != nil {
			return nil, fmt.Errorf("wg-cp0 plan: read live state: %w", err)
		}
	} else {
		// No live reader supplied (e.g. validate/list/show paths). The
		// planner still runs, but LivePlan ends up empty.
		live = LiveState{PerRelay: map[string]*RelayLiveState{}, FetchedAt: a.opts.Now()}
	}
	env := &PlanEnv{
		PeerSet:     ps,
		Live:        live,
		Now:         a.opts.Now(),
		PeerSetPath: a.opts.PeerSetPath,
		Mutator:     a.opts.Relay,
	}

	planner, ok := a.planners[intent.Kind()]
	if !ok {
		return nil, fmt.Errorf("wg-cp0 plan: no planner registered for intent kind %q", intent.Kind())
	}
	plan, err := planner(env, intent)
	if err != nil {
		return nil, err
	}
	if plan == nil {
		return nil, fmt.Errorf("wg-cp0 plan: planner returned nil plan for kind %q", intent.Kind())
	}
	plan.Intent = intent
	plan.DryRun = true
	plan.PlannedAt = env.Now
	if plan.CorrelationID == "" {
		plan.CorrelationID = MakeCorrelationID(env.Now, defaultRandReader())
	}
	return plan, nil
}

// Apply implements Applier. Runs PreFlight on every Mutation, then
// Apply on each in order. PreFlight failure aborts before any wire
// move. Per-Mutation Apply failures are captured but do not stop
// subsequent mutations (so partial-fleet apply state is reachable
// and visible in the audit). Outcome rolls "ok" → "partial" → "fail"
// per the count of successful steps.
func (a *applierImpl) Apply(ctx context.Context, plan *ApplyPlan) (*ApplyResult, error) {
	if plan == nil {
		return nil, errors.New("wg-cp0 apply: nil plan")
	}
	if plan.Intent == nil {
		return nil, errors.New("wg-cp0 apply: plan has no Intent (was Plan called?)")
	}
	result := &ApplyResult{
		StartedAt: a.opts.Now(),
		PerStep:   []StepOutcome{},
	}
	plan.DryRun = false
	plan.AppliedAt = result.StartedAt

	// Pre-flight phase — every mutation runs PreFlight before any
	// Apply lands. A doomed plan fails here rather than mid-execution.
	for _, m := range plan.Mutations {
		if err := m.PreFlight(ctx); err != nil {
			result.Outcome = "fail"
			result.Err = fmt.Errorf("preflight %s: %w", m.Kind(), err)
			result.FinishedAt = a.opts.Now()
			result.PerStep = append(result.PerStep, StepOutcome{
				Kind:     m.Kind(),
				Describe: m.Describe(),
				Outcome:  "fail",
				Err:      result.Err.Error(),
			})
			a.emitAudit(plan, result)
			return result, result.Err
		}
	}

	// Apply phase — sequential, per-Mutation failure is captured but
	// does not stop the pipeline.
	okCount, failCount := 0, 0
	for _, m := range plan.Mutations {
		stepStart := a.opts.Now()
		err := m.Apply(ctx)
		durMs := a.opts.Now().Sub(stepStart).Milliseconds()
		step := StepOutcome{
			Kind:       m.Kind(),
			Describe:   m.Describe(),
			DurationMs: durMs,
		}
		if err != nil {
			step.Outcome = "fail"
			step.Err = err.Error()
			failCount++
		} else {
			step.Outcome = "ok"
			okCount++
		}
		result.PerStep = append(result.PerStep, step)
	}

	switch {
	case failCount == 0:
		result.Outcome = "ok"
	case okCount == 0:
		result.Outcome = "fail"
		result.Err = errors.New("wg-cp0 apply: every step failed")
	default:
		result.Outcome = "partial"
		result.Err = fmt.Errorf("wg-cp0 apply: %d of %d steps failed", failCount, okCount+failCount)
	}
	result.FinishedAt = a.opts.Now()
	a.emitAudit(plan, result)
	return result, result.Err
}

// emitAudit appends the record to the configured AuditEmitter. Audit
// emission errors are surfaced to stderr but do not fail Apply — the
// operator should still see the wire-side outcome even if the audit
// log is misconfigured.
func (a *applierImpl) emitAudit(plan *ApplyPlan, result *ApplyResult) {
	if a.opts.Audit == nil {
		return
	}
	record := AuditRecord{
		Timestamp:     result.StartedAt,
		Operator:      ResolveOperator(),
		Host:          a.hostname,
		Command:       resolveCommandLine(),
		IntentKind:    plan.Intent.Kind(),
		CorrelationID: plan.CorrelationID,
		PlanSummary:   SummarizePlan(plan),
		Outcome:       result.Outcome,
		PerStep:       result.PerStep,
		Track:         ResolveTrack(),
	}
	result.AuditRecord = &record
	if err := a.opts.Audit.Emit(record); err != nil {
		// Don't fail Apply for audit-emit errors; this is observability,
		// not control. Surface as a one-line warning.
		fmt.Fprintf(os.Stderr, "wg-cp0 audit emit: %v\n", err)
	}
}
