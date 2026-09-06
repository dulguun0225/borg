package gate

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/score"
)

var (
	// ErrFiringIncomplete is returned by [Gate.Fire] for a firing missing
	// something its row always has, or naming something its row never has.
	ErrFiringIncomplete = errors.New("gate: the firing is missing something its row always has")
	// ErrReasonMissing is returned for a reject or a hold with no reason. The
	// reason is what goes back up the pipeline, so a reject without it decides
	// nothing the item's next attempt can use. The log's writer refuses the same
	// close, which is where the refusal belongs; this one is so that a caller is
	// told before a transaction is opened.
	ErrReasonMissing = errors.New("gate: a reject and a hold each carry a reason")
	// ErrHumanDecides is returned by [Gate.AutoPass] for a firing that put a
	// human at the row. Nothing in the design removes a human from a gate, so
	// the factory may not close a decision it was not asked to make.
	ErrHumanDecides = errors.New("gate: this firing put a human at the row, and the factory does not decide over one")
	// ErrCheckMissing is returned by [Gate.AutoReject] for a rejection that does
	// not name the check that rejected and what it found.
	ErrCheckMissing = errors.New("gate: a mechanical reject names the check that rejected and what it found")
	// ErrIntentStops is returned by [Gate.Fire] and [Gate.FireSet] for an item
	// whose intent is unrefined, re-decomposing, escalated, or dropped. No open
	// event is appended, so nothing is decided, no attempt is counted, and the
	// score learns nothing.
	ErrIntentStops = errors.New("gate: the item's intent stops every component that could move it")
	// ErrRowPending is returned by [Gate.Fire] where an open event on that row
	// and that item is already pending. The one exception is the open event Edit
	// in place appends naming the row it supersedes.
	ErrRowPending = errors.New("gate: an open event on this row and this item is pending")
	// ErrIntentStateNotComposed is returned where a firing on an item is asked
	// of a gate composed with no reader of the intent's state. The state is read
	// before every such firing, so a gate that cannot read it fires nothing
	// rather than firing without the read.
	ErrIntentStateNotComposed = errors.New("gate: this gate has no reader of the intent's state, and a row on an item is not fired without one")
	// ErrDispatchNotComposed is returned by [Gate.EnforceAttemptLimit] where the
	// gate holds no dispatch to write the escalation with.
	ErrDispatchNotComposed = errors.New("gate: this gate has no dispatch, and the escalation is dispatch's write")
)

// Score is what the gate asks about a change before it fires: the vector, the
// two halves, the number, and the sample. It is an interface so that a test can
// hold a fake where the real score would read the whole graph; package score is
// the implementation.
type Score interface {
	// AssessUnder is the vector, the two halves and the number under one score
	// version. The version is a parameter because a version that redefined the
	// number does not decide a gate an authored threshold binds until its owner
	// has confirmed it, so the firing computes under the version in force at its
	// own scope and not always the newest.
	AssessUnder(ctx context.Context, version score.Version, c score.Change) (score.Assessment, error)
	// HoldOut is whether the score holds this item out of the gate the firing
	// would otherwise put a human at. It is asked after the policy has answered,
	// because the question is about a gate the score itself would have gated and
	// the score does not know the threshold in force, and it is handed the rate
	// in force after every safeguard clamping it. It may pass nothing a
	// safeguard put a human at and nothing a resolved vector put one at.
	HoldOut(ctx context.Context, itemID string, rate float64,
		wouldGate, bySafeguard bool, resolved []score.Resolution) (score.Selection, error)
	// Version is the score version every decision this gate opens names. It is
	// asked of the score rather than read off an assessment because three rows
	// read no factor set at all, and a decision still names the version in
	// force at its firing.
	Version() score.Version
}

// Policy is what the gate asks about the values in force: the threshold for this
// row, where it came from, whether a safeguard adds a human, the policy version
// the firing is decided under, the two sample rates, and the attempt limit.
// Package policy is the implementation.
type Policy interface {
	AtGate(ctx context.Context, p principal.Principal, s policy.Subjects) (policy.Applied, error)
	// HeldOutSampleRate is how often the score auto-passes a change it would
	// have gated, a field of the factory-wide settings record with a safeguard
	// as a ceiling over it.
	HeldOutSampleRate(ctx context.Context, s policy.Subjects) (policy.Effective, error)
	// ReviewSampleRate is how often a change the score would have auto-passed is
	// put in front of that duty's human anyway. It is per duty, so the subjects
	// name the duty the row waits on, and a safeguard is a floor under it.
	ReviewSampleRate(ctx context.Context, s policy.Subjects) (policy.Effective, error)
	// AttemptLimit is how many attempts one stage gets.
	AttemptLimit(ctx context.Context, s policy.Subjects) (policy.Effective, error)
	// ExposureBound is where the exposure factor stops being weighed and puts a
	// human at the row instead. The score supplies a value for that row and may
	// not read what an owner authored, so the gate reads it here and hands it to
	// the score with the change.
	ExposureBound(ctx context.Context, s policy.Subjects) (policy.Effective, error)
}

// DriftDetector is what the gate asks about the drift detector's own store when
// a deploy to production row fires: whether a mismatch stands for this service,
// and what disagrees. It is an interface because that store is not the factory's
// — no factory component may write it, and a gate that imported the package
// owning it would be a gate holding a second pool. [NoDriftDetector] is what a
// factory with none installed is composed with.
type DriftDetector interface {
	// Mismatch is whether an uncleared mismatch stands for the service, and what
	// disagrees, in words a human reads on the open event.
	Mismatch(ctx context.Context, serviceID string) (bool, string, error)
}

// NoDriftDetector is the answer of a factory with no drift detector installed:
// no mismatch, ever. It is a value rather than a nil interface, so that a
// factory composed without one says so and a caller cannot forget to check.
//
// What it costs is what the design says installing the drift detector buys:
// with none installed, every check the factory makes reads a record the factory
// wrote, so a factory whose records are wrong reports itself healthy and nothing
// contradicts it.
type NoDriftDetector struct{}

// Mismatch is never one.
func (NoDriftDetector) Mismatch(context.Context, string) (bool, string, error) { return false, "", nil }

// IntentState is the read of the state of the intent an item was decomposed
// from, performed before every firing on that item. It is a function the
// composition supplies rather than a read this package makes, because what it
// takes is the item's intent and this package decides events rather than
// following records from one to the next.
type IntentState func(ctx context.Context, itemID string) (intent.State, error)

// RaisedByTheHealthMonitor reports whether the intent the item was decomposed
// from is one the health monitor raised, which is what a halt's two exceptions
// come to: a revert is an item of the intent the health monitor raised at the
// rollback, and an item it raised on the service is an item of such an intent
// too. The reading is the intent's source and the component that called intake,
// and it is the same one the merge queue's own stop makes.
//
// It is a function the composition supplies for the reason [IntentState] is: what
// it takes is the item's intent, and this package decides events rather than
// following records from one to the next. A nil value excepts nothing, so every
// item holds while a halt stands.
type RaisedByTheHealthMonitor func(ctx context.Context, itemID string) (bool, error)

// Notifier is the one call a gate makes on the component that reaches humans:
// the acknowledged event of a page a row already fired. It is an interface
// because the notifier's callers hand it a wait rather than the other way
// round, so nothing that creates one is imported there.
//
// The wait an escalation leaves is not made here. ../../end-goal/components.md
// gives that call to dispatch, and this component's own row names the notifier
// only where a decision waits on a human.
type Notifier interface {
	// Acknowledged is the page's acknowledged event, written where the row that
	// was acknowledged also pages: one act at Work writes both.
	Acknowledged(ctx context.Context, openID string, human record.Actor) error
}

// NoNotifier is what a factory composed with no notifier uses: nothing is
// delivered. What it costs is that an acknowledgement is a row of the log and
// nothing else.
type NoNotifier struct{}

// Acknowledged delivers nothing.
func (NoNotifier) Acknowledged(context.Context, string, record.Actor) error { return nil }

// decisionFormatVersion is the format version every row of a decision — the open
// event, the close event, the abandonment and the acknowledgement, over one
// item's build or over a set — is appended with. It names
// [decisionlog.ShapeDecision] through [decisionlog.Formats].
const decisionFormatVersion = "decision/1"

// Composition is what a gate is built from. It is a struct rather than ten
// arguments so that every one is named where the gate is composed, and so that a
// factory composed without the drift detector, the notifier, or anything
// computing holds says which of them it is without.
type Composition struct {
	Pool   *pgxpool.Pool
	Token  lease.Token
	Log    *decisionlog.Writer
	Score  Score
	Policy Policy
	// Holds computes the factory's own holds at a deploy row. A nil value is
	// [NoHolds].
	Holds Holds
	// DriftDetector is the independent store's read. A nil value is
	// [NoDriftDetector].
	DriftDetector DriftDetector
	// IntentState is the read every firing on an item performs first. A nil
	// value fires no row on an item.
	IntentState IntentState
	// RaisedByTheHealthMonitor is the read of whether the item's intent is one
	// the health monitor raised, which is a halt's two exceptions. A nil value
	// excepts nothing.
	RaisedByTheHealthMonitor RaisedByTheHealthMonitor
	// Draw is where the review sample's randomness comes from. A nil value is
	// [NeverDraw], which is a factory that runs no review sample.
	Draw Draw
	// Notifier is what the acknowledgement reaches a human through. A nil
	// value is [NoNotifier].
	Notifier Notifier
	// Dispatch is the item's writer, which the gate calls to write an escalation
	// onto an item that exceeded the attempt limit. A nil value refuses that
	// call with [ErrDispatchNotComposed].
	Dispatch *item.Dispatch
}

// Gate is the gate component: it fires a row, asks the score and the policy what
// applies, and appends the rows of that firing's decision through the log's
// writer.
type Gate struct {
	pool          *pgxpool.Pool
	token         lease.Token
	log           *decisionlog.Writer
	score         Score
	policy        Policy
	holds         Holds
	driftdetector DriftDetector
	intentState   IntentState
	// raisedByTheHealthMonitor is the read a halt's two exceptions are made
	// with, and is nil in a factory composed without one.
	raisedByTheHealthMonitor RaisedByTheHealthMonitor
	draw                     Draw
	notifier                 Notifier
	dispatch                 *item.Dispatch
}

// New returns the gate over one composition, with the three optional components
// replaced by the value that says a factory was composed without them.
func New(c Composition) *Gate {
	if c.Holds == nil {
		c.Holds = NoHolds{}
	}
	if c.DriftDetector == nil {
		c.DriftDetector = NoDriftDetector{}
	}
	if c.Notifier == nil {
		c.Notifier = NoNotifier{}
	}
	if c.Draw == nil {
		c.Draw = NeverDraw{}
	}
	return &Gate{
		pool: c.Pool, token: c.Token, log: c.Log,
		score: c.Score, policy: c.Policy, holds: c.Holds,
		driftdetector: c.DriftDetector, intentState: c.IntentState,
		raisedByTheHealthMonitor: c.RaisedByTheHealthMonitor,
		draw:                     c.Draw,
		notifier:                 c.Notifier,
		dispatch:                 c.Dispatch,
	}
}

// Component is the actor an open event is written as, and a close event too
// where the factory decides for itself: the gate component firing that row.
//
// It is exported because a mechanical rejection has a consequence outside this
// package — the item goes back to a stage — and whatever performs that has to
// name the same actor the close event does.
func Component(row Row) record.Actor {
	return record.Actor{Kind: record.KindComponent, Key: "gate." + row.String(), Basis: record.BasisClaimed}
}

// component is [Component], for this package's own calls.
func component(row Row) record.Actor { return Component(row) }

// ComponentPrincipal is who a read made on behalf of that gate row is made as:
// the gate component, calling as itself. It is the principal beside
// [Component]'s actor, because a read event names the principal and a record
// names the actor, and it is exported for the reason [Component] is.
func ComponentPrincipal(row Row) principal.Principal {
	return principal.OfComponent("gate." + row.String())
}

// componentPrincipal is [ComponentPrincipal], for this package's own calls.
func componentPrincipal(row Row) principal.Principal { return ComponentPrincipal(row) }

// stops reports whether the intent's state stops every component that could move
// the item. The four are the design's own list, and refined and delivered are
// the two that do not stop one.
func stops(state intent.State) bool {
	switch state {
	case intent.StateUnrefined, intent.StateReDecomposing, intent.StateEscalated, intent.StateDropped:
		return true
	default:
		return false
	}
}
