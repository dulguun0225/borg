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
	HoldOut(ctx context.Context, itemID string, wouldGate, pinned bool) (score.Selection, error)
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
	// HeldOut is whether the score selected this item into its held-out sample.
	// It is written on every decision on the item from the selection onward, so a
	// row that reads held out where the number is under the threshold is an item
	// selected at an earlier gate and passed here on the number.
	HeldOut bool
	// WhyHeldOut is which of the two ways it came to be held out, and is empty
	// where it is not.
	WhyHeldOut string
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
// [score.Opening] is embedded rather than restated: the score reads the item, the
// artifact, the row, the number, the threshold, and the selection back off this
// payload when it learns from outcomes, so every one of those field names is
// declared once, in the package that reads them.
type OpeningPayload struct {
	score.Opening
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
	ThresholdFrom  string            `json:"threshold_from"`
	Pins           []string          `json:"pins"`
	Unavailable    []string          `json:"unavailable_factors"`
	HumanDecides   bool              `json:"human_decides"`
	WhyHuman       string            `json:"why_human"`
	// WhyHeldOut is which of the two ways the item came to be held out, and is
	// empty where it is not. The selection itself is a field of the embedded
	// opening, because the score reads that one back.
	WhyHeldOut string `json:"why_held_out,omitempty"`
	WaitsOn    string `json:"waits_on"`
	// Mismatch is what the reconciler found disagreeing with what runs, and is
	// empty on every row that found none. It is on the opening row because a human
	// approving through it is saying the record is wrong and the deploy should
	// proceed anyway, which is a verdict nobody can read against a row that does
	// not say what disagreed.
	Mismatch string `json:"reconciler_mismatch,omitempty"`
}

// ClosingPayload is what the closing row says: the verdict, what the human typed
// with it, the stage a reject returns the item to, and what auto-passed or
// auto-rejected the firing where the factory decided for itself.
//
// Feedback is required on a reject, that action being "Reject with feedback",
// and is a note on a hold — what a human held for is worth showing beside the
// wait, and nothing reads it.
//
// [score.Closing] is embedded for the reason [score.Opening] is: the verdict and
// what auto-passed the firing are both read back by the score when it learns, and
// the threshold's own calibration turns on telling an auto-pass on the number
// apart from one its own sample made, so those two field names are declared once
// in the package that reads them.
type ClosingPayload struct {
	score.Closing
	Feedback  string `json:"feedback"`
	ReturnsTo string `json:"returns_to"`
	// AutoRejectedBy is which mechanical check rejected, and is empty on every
	// closing row but [Gate.AutoReject]'s.
	AutoRejectedBy string `json:"auto_rejected_by,omitempty"`
}

// The mechanical checks that reject on their own terms at the merge row, in the
// words a closing row names one by. They are constants here so that a caller
// cannot report a rejection under a name of its own, which is the arrangement the
// five holds already have; what computes each of them reads the contracts and the
// declarations, and this package imports neither.
const (
	// AutoRejectedByContractDiff is the producer's own diff: the form the
	// candidate publishes against the version its service's current release
	// publishes, breaking, with the migration not shipped ahead of it.
	AutoRejectedByContractDiff = "the producer's own contract diff"
	// AutoRejectedByDeclaration is a consumer's declaration in force that the
	// candidate does not satisfy, decided against the candidate's own run.
	AutoRejectedByDeclaration = "a consumer's declaration"
	// AutoRejectedByPinnedPredicate is a pinned predicate naming an element the
	// candidate removes. It is told apart from a declaration because an owner
	// placed it and a derivation did not, and what a reader of the rejection needs
	// is the pin and its author.
	AutoRejectedByPinnedPredicate = "a pinned predicate"
)

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

	// The score's own sample, asked after the policy answered and before the human
	// test: what it may pass is a gate the score itself would have gated, which is
	// the number against the threshold in force, and it may pass nothing a pin put
	// a human at.
	overThreshold := assessment.Number >= applied.Threshold
	selection, err := g.score.HoldOut(ctx, f.ItemID, overThreshold, applied.HumanPinned)
	if err != nil {
		return Opened{}, fmt.Errorf("gate: asking the score whether %s is held out: %w", f.ItemID, err)
	}

	// A held-out item removes the human the number put at the row and no other. A
	// pin's human and a mismatch's stand: the sample is the score holding itself
	// out of its own gate, and nothing in the tree lets it out of anyone else's.
	gatedByNumber := overThreshold && !selection.HeldOut
	opened := Opened{
		Gate:         f.Row,
		Assessment:   assessment,
		Applied:      applied,
		HumanDecides: gatedByNumber || applied.HumanPinned || mismatch != "",
		WhyHuman:     why(gatedByNumber, applied.HumanPinned, mismatch != ""),
		HeldOut:      selection.HeldOut,
		WhyHeldOut:   selection.Why,
		Mismatch:     mismatch,
	}

	waitsOn := ""
	if opened.HumanDecides {
		waitsOn = WaitsOn(f.Row)
	}
	payload, err := json.Marshal(OpeningPayload{
		Opening: score.Opening{
			ItemID:     f.ItemID,
			ArtifactID: f.ArtifactID,
			Gate:       string(f.Row),
			Number:     assessment.Number,
			Threshold:  applied.Threshold,
			HeldOut:    opened.HeldOut,
		},
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
		ThresholdFrom:  string(applied.ThresholdFrom),
		Pins:           applied.Pins,
		Unavailable:    assessment.UnavailableFactors(),
		HumanDecides:   opened.HumanDecides,
		WhyHuman:       opened.WhyHuman,
		WhyHeldOut:     opened.WhyHeldOut,
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
	if verdict == VerdictReject && opened.Gate != Decomposition {
		// Decomposition names nothing: its reject re-cuts the set rather than
		// sending an item anywhere, which is the one reject in the tree with no
		// stage on the other end of it.
		returnsTo = ReturnsTo
	}
	return g.close(ctx, opened, actor, ClosingPayload{
		Closing:   score.Closing{Verdict: string(verdict)},
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
		Closing: score.Closing{
			Verdict:      string(VerdictApprove),
			AutoPassedBy: autoPassedBy(opened),
		},
	})
}

// AutoReject gives the factory's own reject, which is what a mechanical check
// that failed closes a firing with: the closing row's actor is the gate component,
// the payload names which check rejected, and the feedback is what it found — which
// is what goes back up the pipeline, a reject being "Reject with feedback".
//
// It is allowed whatever the firing decided about a human, and [Gate.AutoPass] is
// not. That asymmetry is the whole of the difference between the two: the factory
// may not approve over a human, because nothing in the tree removes a human from a
// gate; and it rejects before a human is asked, because a mechanical check rejects
// on its own terms before anyone gives a verdict. A human at the row who was going
// to approve is not being overruled — there is nothing left to approve, and the
// check is not a judgment they could have made differently.
//
// A row that does not offer reject refuses this, which is the production deploy
// row: by then the merge has happened and the number is assigned, so there is
// nothing left to reject to.
func (g *Gate) AutoReject(ctx context.Context, opened Opened, check, found string) (decisionlog.Row, error) {
	if err := permits(opened.Gate, VerdictReject); err != nil {
		return decisionlog.Row{}, err
	}
	if check == "" || found == "" {
		return decisionlog.Row{}, fmt.Errorf("%w: check %q, what it found %q", ErrCheckMissing, check, found)
	}
	returnsTo := ReturnsTo
	if opened.Gate == Decomposition {
		// Decomposition names nothing at all: its reject re-cuts the set rather
		// than sending an item anywhere, so the field its closing row would carry
		// stays unwritten.
		returnsTo = ""
	}
	return g.close(ctx, opened, component(opened.Gate), ClosingPayload{
		Closing:        score.Closing{Verdict: string(VerdictReject)},
		Feedback:       found,
		ReturnsTo:      returnsTo,
		AutoRejectedBy: check,
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

// Component is the actor an opening row is written as, and a closing row too
// where the factory decides for itself: the gate component firing that row.
//
// It is exported because a mechanical rejection has a consequence outside this
// package — the item goes back to a stage — and whatever performs that has to name
// the same actor the closing row does. A caller composing the name itself would be
// a second place the convention lives.
func Component(row Row) record.Actor {
	return record.Actor{Kind: record.KindComponent, Name: "gate." + string(row)}
}

// component is [Component], for this package's own calls.
func component(row Row) record.Actor { return Component(row) }

// complete refuses a firing missing something its row always has. The artifact
// is required at the merge row and refused at the deploy row: there is no
// artifact under decision at a deploy, and one named there would say a version
// was decided over when nothing was.
//
// Decomposition is refused outright: it decides over a set and not over one item's
// build, so it is fired through [Gate.FireSet] and a [Firing] naming it is a
// caller's defect.
func complete(f Firing) error {
	if _, err := Actions(f.Row); err != nil {
		return err
	}
	if f.Row == Decomposition {
		return fmt.Errorf("%w: %s decides over a set and is fired through FireSet",
			ErrFiringIncomplete, f.Row)
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

// autoPassedBy is what the closing row says passed the firing. It reads the
// threshold at a gate the score would have passed anyway, whether or not the item
// is held out, and the sample only where the number was at or above the threshold
// — which is the one case the sample is evidence about, and the only case the
// threshold's own calibration counts.
func autoPassedBy(opened Opened) string {
	if opened.HeldOut && opened.Assessment.Number >= opened.Applied.Threshold {
		return score.AutoPassedBySample
	}
	return score.AutoPassedByThreshold
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
