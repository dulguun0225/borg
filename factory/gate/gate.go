package gate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dulguun0225/borg/factory/criterion"
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
	// human at the row. Nothing in the tree removes a human from a gate, so the
	// factory may not close a decision it was not asked to make.
	ErrHumanDecides = errors.New("gate: this firing put a human at the row, and the factory does not decide over one")
)

// Score is what the gate asks about a change before it fires: the vector, the
// two halves, and the number. It is an interface so that a test can hold a fake
// where the real score would read the whole graph; package score is the
// implementation.
type Score interface {
	Assess(ctx context.Context, c score.Change) (score.Assessment, error)
}

// Policy is what the gate asks about the values in force: the threshold for this
// row, where it came from, whether a pin adds a human, and the policy version
// the firing is decided under. Package policy is the implementation.
type Policy interface {
	AtGate(ctx context.Context, s policy.Subjects) (policy.Applied, error)
}

// Reconciler is what the gate asks about the reconciler's own store when the
// production deploy row fires: whether a mismatch stands for this service, and
// what disagrees. It is an interface because that store is not the factory's — no
// factory component may write it, and a gate that imported the package owning it
// would be a gate holding a second pool. [NoReconciler] is what a factory with
// none installed is composed with.
//
// The design puts this read at the moment the row fires and nowhere else, which
// is the same rule every other check a gate makes keeps.
type Reconciler interface {
	// Mismatch is whether an uncleared mismatch stands for the service, and what
	// disagrees, in words a human reads on the opening row.
	Mismatch(ctx context.Context, serviceID string) (bool, string, error)
}

// NoReconciler is the answer of a factory with no reconciler installed: no
// mismatch, ever. It is a value rather than a nil interface, so that a factory
// composed without one says so and a caller cannot forget to check.
//
// What it costs is what the design says installing the reconciler buys: with none
// installed, every check the factory makes reads a record the factory wrote, so a
// factory whose records are wrong reports itself healthy and nothing contradicts
// it. That is visible in what the crude interface prints and nowhere else.
type NoReconciler struct{}

// Mismatch is never one.
func (NoReconciler) Mismatch(context.Context, string) (bool, string, error) { return false, "", nil }

// Gate is the gate component: it appends a decision's two rows through the log's
// writer, asking the score and the policy before the first.
type Gate struct {
	log        *decisionlog.Writer
	score      Score
	policy     Policy
	reconciler Reconciler
}

// New returns the gate over the log, the score, the policy, and the reconciler's
// store. A nil reconciler is [NoReconciler]: composing a factory without one is
// something a caller does deliberately, and a gate that panicked on it would make
// the reconciler required where the design makes installing it the owner's.
func New(log *decisionlog.Writer, s Score, p Policy, r Reconciler) *Gate {
	if r == nil {
		r = NoReconciler{}
	}
	return &Gate{log: log, score: s, policy: p, reconciler: r}
}

// Firing is what fires the gate: the row, the records it decides over, what the
// acceptance criteria in force produced against the build, and the build's
// measurement, which the component that built took and the score cannot read.
type Firing struct {
	Row           Row
	ItemID        string
	BuildID       string
	ArtifactID    string
	ServiceID     string
	AreaID        string
	EnvironmentID string
	// CriteriaInForce is how many criteria the build is decided against, which is
	// what the coverage factor reads. It is separate from Criteria because at the
	// candidate deploy row the count is known and no outcome is: the run that
	// decides them is what that deploy is for.
	CriteriaInForce int
	// Criteria is what deciding each of them produced, and is empty at the
	// candidate deploy row.
	Criteria    []CriterionResult
	Measurement score.Measurement
}

// CriterionResult is one criterion decided against the build: the criterion's id
// and what its encoding produced. The JSON tags are the field names the opening
// payload stores.
type CriterionResult struct {
	CriterionID string            `json:"criterion_id"`
	Outcome     criterion.Outcome `json:"outcome"`
}

// Opened is what [Gate.Fire] returns and the two closing calls take: the opening
// row as it was appended, what the score and the policy answered, and whether a
// human decides. The verdict is given over this and not over an id, so the
// caller deciding is the caller that saw the vector.
type Opened struct {
	Gate         Row
	Row          decisionlog.Row
	Assessment   score.Assessment
	Applied      policy.Applied
	HumanDecides bool
	// WhyHuman is what put a human at the row, and is empty where none is.
	WhyHuman string
	// Mismatch is what the reconciler found disagreeing with what runs, and is
	// empty where it found nothing and at every row but the production deploy. It
	// is a field of its own beside WhyHuman because a human deciding here has to
	// read what disagrees and not only that something does.
	Mismatch string
}

// The two reasons a human decides, in the words the opening row stores.
const (
	// WhyOverThreshold is the number being at or above the threshold in force.
	WhyOverThreshold = "the number is at or above the threshold in force"
	// WhyPinned is a pin adding a human, which a pin may do whatever the number
	// reads.
	WhyPinned = "a pin adds a human at this row"
	// WhyBoth is both at once, which is worth telling apart from either: an
	// owner withdrawing the pin would not remove the human.
	WhyBoth = "the number is at or above the threshold in force, and a pin adds a human"
	// WhyMismatch is a record the reconciler found disagreeing with what runs. It
	// is the one reason a human decides that is neither the score's nor an owner's,
	// and it is appended to whichever of the three above also holds — an owner
	// clearing the mismatch would not remove a human the number put there.
	WhyMismatch = HoldReconcilerMismatch
)

// OpeningPayload is what the opening row says. It names the row, the records
// decided over, the criteria results, the whole vector and the number it reduced
// to, the values actually applied, and what the row waits on.
//
// [score.Subject] is embedded rather than restated: the score reads the item and
// the artifact back off this payload when it counts outcomes, so the two field
// names are declared once, in the package that reads them.
type OpeningPayload struct {
	score.Subject
	Gate           string            `json:"gate"`
	BuildID        string            `json:"build_id"`
	ServiceID      string            `json:"service_id"`
	AreaID         string            `json:"area_id"`
	EnvironmentID  string            `json:"environment_id"`
	Criteria       []CriterionResult `json:"criteria"`
	Vector         []score.Factor    `json:"vector"`
	FormulaVersion string            `json:"formula_version"`
	Likelihood     float64           `json:"likelihood"`
	Impact         float64           `json:"impact"`
	Exposure       float64           `json:"exposure"`
	Number         float64           `json:"number"`
	Threshold      float64           `json:"threshold"`
	ThresholdFrom  string            `json:"threshold_from"`
	Pins           []string          `json:"pins"`
	Unavailable    []string          `json:"unavailable_factors"`
	HumanDecides   bool              `json:"human_decides"`
	WhyHuman       string            `json:"why_human"`
	WaitsOn        string            `json:"waits_on"`
	// Mismatch is what the reconciler found disagreeing with what runs, and is
	// empty on every row that found none. It is on the opening row because a human
	// approving through it is saying the record is wrong and the deploy should
	// proceed anyway, which is a verdict nobody can read against a row that does
	// not say what disagreed.
	Mismatch string `json:"reconciler_mismatch,omitempty"`
}

// ClosingPayload is what the closing row says: the verdict, what the human typed
// with it, the stage a reject returns the item to, and what auto-passed the
// firing where the factory decided for itself.
//
// Feedback is required on a reject, that action being "Reject with feedback",
// and is a note on a hold — what a human held for is worth showing beside the
// wait, and nothing reads it.
//
// AutoPassedBy reads "threshold" and nothing else here. The design's other value
// is the score's held-out sample, and nothing selects one yet, so the field says
// which of the two it was on every auto-pass and one of the two is unreachable.
type ClosingPayload struct {
	Verdict      string `json:"verdict"`
	Feedback     string `json:"feedback"`
	ReturnsTo    string `json:"returns_to"`
	AutoPassedBy string `json:"auto_passed_by"`
}

// AutoPassedByThreshold is what the closing row says of an auto-pass: the number
// was under the threshold in force.
const AutoPassedByThreshold = "threshold"

// Fire fires the gate: it asks the score about the change and the policy about
// the values in force, decides whether a human decides, composes the opening
// payload, and appends the opening row as the gate component. The vector is
// written here and never recomputed, because it has to exist while a human is
// deciding and the score version moves as outcomes arrive.
func (g *Gate) Fire(ctx context.Context, f Firing) (Opened, error) {
	if err := complete(f); err != nil {
		return Opened{}, err
	}

	assessment, err := g.score.Assess(ctx, score.Change{
		ItemID:          f.ItemID,
		ServiceID:       f.ServiceID,
		AreaID:          f.AreaID,
		Measurement:     f.Measurement,
		CriteriaInForce: f.CriteriaInForce,
		CriteriaFailed:  blocked(f.Criteria),
	})
	if err != nil {
		return Opened{}, fmt.Errorf("gate: assessing the change: %w", err)
	}

	applied, err := g.policy.AtGate(ctx, policy.Subjects{
		GateRow:       string(f.Row),
		EnvironmentID: f.EnvironmentID,
		ServiceID:     f.ServiceID,
		AreaID:        f.AreaID,
	})
	if err != nil {
		return Opened{}, fmt.Errorf("gate: reading what applies at %s: %w", f.Row, err)
	}

	// The reconciler's store, read at the production deploy row and at no other:
	// what it holds is a disagreement about what is running in production, and no
	// other row decides a deploy into it. A mismatch puts a human here whatever the
	// number reads, because nothing the factory can decide on the record is worth
	// deciding while the record is the thing in doubt.
	mismatch := ""
	if f.Row == DeployToProduction {
		found, why, err := g.reconciler.Mismatch(ctx, f.ServiceID)
		if err != nil {
			return Opened{}, fmt.Errorf("gate: reading the reconciler's store for %s: %w", f.ServiceID, err)
		}
		if found {
			mismatch = why
		}
	}

	overThreshold := assessment.Number >= applied.Threshold
	opened := Opened{
		Gate:         f.Row,
		Assessment:   assessment,
		Applied:      applied,
		HumanDecides: overThreshold || applied.HumanPinned || mismatch != "",
		WhyHuman:     why(overThreshold, applied.HumanPinned, mismatch != ""),
		Mismatch:     mismatch,
	}

	waitsOn := ""
	if opened.HumanDecides {
		waitsOn = WaitsOn(f.Row)
	}
	payload, err := json.Marshal(OpeningPayload{
		Subject:        score.Subject{ItemID: f.ItemID, ArtifactID: f.ArtifactID},
		Gate:           string(f.Row),
		BuildID:        f.BuildID,
		ServiceID:      f.ServiceID,
		AreaID:         f.AreaID,
		EnvironmentID:  f.EnvironmentID,
		Criteria:       f.Criteria,
		Vector:         assessment.Vector,
		FormulaVersion: assessment.FormulaVersion,
		Likelihood:     assessment.Likelihood,
		Impact:         assessment.Impact,
		Exposure:       assessment.Exposure,
		Number:         assessment.Number,
		Threshold:      applied.Threshold,
		ThresholdFrom:  string(applied.ThresholdFrom),
		Pins:           applied.Pins,
		Unavailable:    assessment.UnavailableFactors(),
		HumanDecides:   opened.HumanDecides,
		WhyHuman:       opened.WhyHuman,
		WaitsOn:        waitsOn,
		Mismatch:       opened.Mismatch,
	})
	if err != nil {
		return Opened{}, fmt.Errorf("gate: marshalling the opening payload: %w", err)
	}

	row, err := g.log.AppendDecisionOpening(ctx, decisionlog.Entry{
		Actor:         component(f.Row),
		Payload:       string(payload),
		PolicyVersion: applied.PolicyVersion,
		ScoreVersion:  assessment.Version,
	})
	if err != nil {
		return Opened{}, err
	}
	opened.Row = row
	return opened, nil
}

// Decide gives a human's verdict: it appends the closing row as the deciding
// human, naming the opening row it closes. A verdict the row does not offer is
// refused, and so is a reject with no feedback. A second verdict over one opening
// is refused by the log's store, not here.
func (g *Gate) Decide(ctx context.Context, opened Opened, actor record.Actor, verdict Verdict, feedback string) (decisionlog.Row, error) {
	if err := permits(opened.Gate, verdict); err != nil {
		return decisionlog.Row{}, err
	}
	if verdict == VerdictReject && feedback == "" {
		return decisionlog.Row{}, fmt.Errorf("%w: the reject of %s carries none", ErrFeedbackMissing, opened.Row.ID)
	}

	returnsTo := ""
	if verdict == VerdictReject {
		returnsTo = ReturnsTo
	}
	return g.close(ctx, opened, actor, ClosingPayload{
		Verdict:   string(verdict),
		Feedback:  feedback,
		ReturnsTo: returnsTo,
	})
}

// AutoPass gives the factory's own verdict, which is what closes a firing that
// put no human at the row. The closing row's actor is the gate component, and
// the payload says what auto-passed it. A firing that did put a human there is
// refused with [ErrHumanDecides].
func (g *Gate) AutoPass(ctx context.Context, opened Opened) (decisionlog.Row, error) {
	if opened.HumanDecides {
		return decisionlog.Row{}, fmt.Errorf("%w: %s", ErrHumanDecides, opened.WhyHuman)
	}
	return g.close(ctx, opened, component(opened.Gate), ClosingPayload{
		Verdict:      string(VerdictApprove),
		AutoPassedBy: AutoPassedByThreshold,
	})
}

func (g *Gate) close(ctx context.Context, opened Opened, actor record.Actor, closing ClosingPayload) (decisionlog.Row, error) {
	payload, err := json.Marshal(closing)
	if err != nil {
		return decisionlog.Row{}, fmt.Errorf("gate: marshalling the closing payload: %w", err)
	}
	return g.log.AppendDecisionClosing(ctx, decisionlog.Entry{
		Actor:   actor,
		Payload: string(payload),
		Closes:  opened.Row.ID,
	})
}

// component is the actor an opening row is written as, and a closing row too
// where the factory decides for itself: the gate component firing that row.
func component(row Row) record.Actor {
	return record.Actor{Kind: record.KindComponent, Name: "gate." + string(row)}
}

// complete refuses a firing missing something its row always has. The artifact
// is required at the merge row and refused at the deploy row: there is no
// artifact under decision at a deploy, and one named there would say a version
// was decided over when nothing was.
func complete(f Firing) error {
	if _, err := Actions(f.Row); err != nil {
		return err
	}
	for _, required := range []struct{ what, value string }{
		{"item", f.ItemID}, {"build", f.BuildID},
		{"service", f.ServiceID}, {"environment", f.EnvironmentID},
	} {
		if required.value == "" {
			return fmt.Errorf("%w: %s names no %s", ErrFiringIncomplete, f.Row, required.what)
		}
	}
	if f.Row == MergeToMaster && f.ArtifactID == "" {
		return fmt.Errorf("%w: %s names no artifact version under decision", ErrFiringIncomplete, f.Row)
	}
	if f.Row != MergeToMaster && f.ArtifactID != "" {
		return fmt.Errorf("%w: %s names an artifact, and no artifact is under decision at a deploy",
			ErrFiringIncomplete, f.Row)
	}
	return nil
}

// blocked is how many of the criteria decided against the build stop it at the
// merge gate. Undecided is counted with failed, which is what the design says of
// it: an encoding that produced a failure and a pass over the same build decided
// nothing, and it is read there the way a failure is.
func blocked(criteria []CriterionResult) int {
	n := 0
	for _, c := range criteria {
		if c.Outcome.Blocks() {
			n++
		}
	}
	return n
}

// why is what put a human at the row. The score's number and a pin are the two
// the design gives every row, and their four combinations are the three constants
// above; a mismatch is appended rather than replacing either, because clearing it
// would not remove a human the number put there.
func why(overThreshold, pinned, mismatch bool) string {
	reason := ""
	switch {
	case overThreshold && pinned:
		reason = WhyBoth
	case overThreshold:
		reason = WhyOverThreshold
	case pinned:
		reason = WhyPinned
	}
	switch {
	case !mismatch:
		return reason
	case reason == "":
		return WhyMismatch
	default:
		return reason + ", and " + WhyMismatch
	}
}
