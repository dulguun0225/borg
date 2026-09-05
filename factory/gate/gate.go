package gate

import (
	"context"
	"errors"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/score"
)

var (
	// ErrFiringIncomplete is returned by [Gate.Fire] for a firing missing
	// something its row always has. Every row names an item, a build, a service,
	// and the environment whose threshold decides them; the merge row also names
	// the artifact version under decision and neither deploy row names one, there
	// being no artifact under decision at a deploy.
	ErrFiringIncomplete = errors.New("gate: the firing is missing something its row always has")
	// ErrVerdictUnknown is returned for a verdict the row does not offer.
	ErrVerdictUnknown = errors.New("gate: the row does not offer that verdict")
	// ErrFeedbackMissing is returned for a reject with no feedback. The action is
	// "Reject with feedback": the feedback is what goes back up the pipeline, so
	// a reject without it decides nothing the item's next attempt can use.
	ErrFeedbackMissing = errors.New("gate: a reject carries feedback")
	// ErrHumanDecides is returned by [Gate.AutoPass] for a firing that put a
	// human at the row. Nothing in the design removes a human from a gate, so the
	// factory may not close a decision it was not asked to make.
	ErrHumanDecides = errors.New("gate: this firing put a human at the row, and the factory does not decide over one")
	// ErrCheckMissing is returned by [Gate.AutoReject] for a rejection that does
	// not name the check that rejected. A mechanical rejection is only readable
	// against the check it came from, and one that names none is a reject a human
	// cannot tell from a verdict.
	ErrCheckMissing = errors.New("gate: a mechanical reject names the check that rejected and what it found")
)

// Score is what the gate asks about a change before it fires: the vector, the
// two halves, and the number. It is an interface so that a test can hold a fake
// where the real score would read the whole graph; package score is the
// implementation.
type Score interface {
	Assess(ctx context.Context, c score.Change) (score.Assessment, error)
	// HoldOut is whether the score holds this item out of the gate the firing
	// would otherwise put a human at. It is asked after the policy has answered,
	// because the question is about a gate the score itself would have gated and
	// the score does not know the threshold in force.
	HoldOut(ctx context.Context, itemID string, wouldGate, bySafeguard bool) (score.Selection, error)
}

// Policy is what the gate asks about the values in force: the threshold for this
// row, where it came from, whether a safeguard adds a human, and the policy
// version the firing is decided under. Package policy is the implementation.
type Policy interface {
	AtGate(ctx context.Context, s policy.Subjects) (policy.Applied, error)
}

// DriftDetector is what the gate asks about the drift detector's own store when the
// production deploy row fires: whether a mismatch stands for this service, and
// what disagrees. It is an interface because that store is not the factory's — no
// factory component may write it, and a gate that imported the package owning it
// would be a gate holding a second pool. [NoDriftDetector] is what a factory with
// none installed is composed with.
//
// The design puts this read at the moment the row fires and nowhere else, which
// is the same rule every other check a gate makes keeps.
type DriftDetector interface {
	// Mismatch is whether an uncleared mismatch stands for the service, and what
	// disagrees, in words a human reads on the open event.
	Mismatch(ctx context.Context, serviceID string) (bool, string, error)
}

// NoDriftDetector is the answer of a factory with no drift detector installed: no
// mismatch, ever. It is a value rather than a nil interface, so that a factory
// composed without one says so and a caller cannot forget to check.
//
// What it costs is what the design says installing the drift detector
// buys: with none installed, every check the factory makes reads a record the
// factory wrote, so a factory whose records are wrong reports itself healthy
// and nothing contradicts it. That is visible in what the crude interface
// prints and nowhere else.
type NoDriftDetector struct{}

// Mismatch is never one.
func (NoDriftDetector) Mismatch(context.Context, string) (bool, string, error) { return false, "", nil }

// decisionFormatVersion is the format version every row of a decision — the
// open event and the close event, over one item's build or over a set — is
// appended with. It names [decisionlog.ShapeDecision] through
// [decisionlog.Formats].
const decisionFormatVersion = "decision/1"

// Gate is the gate component: it appends a decision's two rows through the log's
// writer, asking the score and the policy before the first.
type Gate struct {
	log           *decisionlog.Writer
	score         Score
	policy        Policy
	driftdetector DriftDetector
}

// New returns the gate over the log, the score, the policy, and the independent
// driftdetector's store. A nil driftdetector is [NoDriftDetector]: composing a factory without
// one is something a caller does deliberately, and a gate that panicked on it
// would make the drift detector required where the design makes installing
// it the owner's.
func New(log *decisionlog.Writer, s Score, p Policy, r DriftDetector) *Gate {
	if r == nil {
		r = NoDriftDetector{}
	}
	return &Gate{log: log, score: s, policy: p, driftdetector: r}
}

// Component is the actor an open event is written as, and a close event too
// where the factory decides for itself: the gate component firing that row.
//
// It is exported because a mechanical rejection has a consequence outside this
// package — the item goes back to a stage — and whatever performs that has to name
// the same actor the close event does. A caller composing the name itself would be
// a second place the convention lives.
func Component(row Row) record.Actor {
	return record.Actor{Kind: record.KindComponent, Key: "gate." + string(row)}
}

// component is [Component], for this package's own calls.
func component(row Row) record.Actor { return Component(row) }
