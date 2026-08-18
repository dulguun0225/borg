package gate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/record"
)

// MergeToMaster names the one gate row M1 builds, and is what the opening
// payload's gate field says.
const MergeToMaster = "merge_to_master"

// PolicyVersion is the gate policy every opening row names in M1. An opening
// row requires a policy version and gate policy is authored from M2, so the
// name says nothing was authored rather than passing an empty string off as a
// version.
const PolicyVersion = "policy-unauthored-m1"

// WaitsOn is what the opening row waits on: duty 7, UAT, which is the owner's
// in M1. The opening row names it so a reader of a pending decision knows who
// the verdict is waited on from.
const WaitsOn = "duty 7, UAT — the owner"

// component is the actor every opening row is written as: the gate component
// firing the Merge to master row. The closing row is written as the deciding
// human instead, who is its actor.
var component = record.Actor{Kind: record.KindComponent, Name: "gate." + MergeToMaster}

var (
	// ErrFiringIncomplete is returned by [Gate.Fire] for a firing that names
	// no item, no build, or no artifact version. Merge to master always has
	// all three, so a blank is a caller's defect and not a case to store.
	ErrFiringIncomplete = errors.New("gate: a firing names an item, a build, and an artifact version")
	// ErrVerdictUnknown is returned by [Gate.Decide] for a verdict that is
	// neither approve nor reject, which are the two actions Merge to master
	// has.
	ErrVerdictUnknown = errors.New("gate: a verdict is approve or reject")
	// ErrFeedbackMissing is returned by [Gate.Decide] for a reject with no
	// feedback. The action is "Reject with feedback": the feedback is what
	// goes back up the pipeline, so a reject without it decides nothing the
	// item's next attempt can use.
	ErrFeedbackMissing = errors.New("gate: a reject carries feedback")
)

// Gate is the gate component: it appends a decision's two rows through the
// log's writer, asking a [Score] before the first.
type Gate struct {
	log   *decisionlog.Writer
	score Score
}

// New returns the gate over log and score.
func New(log *decisionlog.Writer, score Score) *Gate {
	return &Gate{log: log, score: score}
}

// Firing is what fires the gate: the item, the build, the artifact version
// under decision, and each acceptance criterion's result from the candidate's
// own run.
type Firing struct {
	ItemID     string
	BuildID    string
	ArtifactID string
	Criteria   []CriterionResult
}

// CriterionResult is one criterion decided against the build: the criterion's
// id and whether its encoding passed. The JSON tags are the field names the
// opening payload stores.
type CriterionResult struct {
	CriterionID string `json:"criterion_id"`
	Passed      bool   `json:"passed"`
}

// Opened is what [Gate.Fire] returns and [Gate.Decide] takes: the opening row
// as it was appended, and the assessment it was appended with. The verdict is
// given over this and not over an id, so the caller deciding is the caller
// that saw the vector.
type Opened struct {
	Row        decisionlog.Row
	Assessment Assessment
}

// OpeningPayload is what the opening row says, marshalled to JSON by
// [Gate.Fire]. It names the gate row, the item, the build, the artifact
// version under decision, the criteria results, the vector, the number, and
// what the row waits on. The artifact version is named because artifacts are
// editable while a row waits, so a verdict over only the item would point at
// whatever the artifact says when someone reads it rather than at what was
// decided over.
type OpeningPayload struct {
	Gate       string            `json:"gate"`
	ItemID     string            `json:"item_id"`
	BuildID    string            `json:"build_id"`
	ArtifactID string            `json:"artifact_id"`
	Criteria   []CriterionResult `json:"criteria"`
	Vector     []Factor          `json:"vector"`
	Number     string            `json:"number"`
	WaitsOn    string            `json:"waits_on"`
}

// ClosingPayload is what the closing row says, marshalled to JSON by
// [Gate.Decide]: the verdict and the feedback, which is the empty string on
// an approve.
type ClosingPayload struct {
	Verdict  string `json:"verdict"`
	Feedback string `json:"feedback"`
}

// Verdict is what a decision closes with. Merge to master has two actions —
// Approve and Reject with feedback — and no third.
type Verdict string

const (
	// VerdictApprove admits the candidate to the merge; in M1 the caller
	// performs the merge itself, there being no merge queue until M3.
	VerdictApprove Verdict = "approve"
	// VerdictReject sends the item back up the pipeline, and requires
	// feedback.
	VerdictReject Verdict = "reject"
)

// Fire fires the gate: it asks the score about the change, composes the
// opening payload, and appends the opening row as the gate component, naming
// the assessment's score version and [PolicyVersion]. The vector is written
// here and never recomputed, because it has to exist while a human is
// deciding and the score version moves as outcomes arrive.
func (g *Gate) Fire(ctx context.Context, f Firing) (Opened, error) {
	if f.ItemID == "" || f.BuildID == "" || f.ArtifactID == "" {
		return Opened{}, fmt.Errorf("%w: item %q, build %q, artifact %q",
			ErrFiringIncomplete, f.ItemID, f.BuildID, f.ArtifactID)
	}

	assessment, err := g.score.Assess(ctx, Change{
		ItemID:     f.ItemID,
		BuildID:    f.BuildID,
		ArtifactID: f.ArtifactID,
	})
	if err != nil {
		return Opened{}, fmt.Errorf("gate: assessing the change: %w", err)
	}

	payload, err := json.Marshal(OpeningPayload{
		Gate:       MergeToMaster,
		ItemID:     f.ItemID,
		BuildID:    f.BuildID,
		ArtifactID: f.ArtifactID,
		Criteria:   f.Criteria,
		Vector:     assessment.Vector,
		Number:     assessment.Number,
		WaitsOn:    WaitsOn,
	})
	if err != nil {
		return Opened{}, fmt.Errorf("gate: marshalling the opening payload: %w", err)
	}

	row, err := g.log.AppendDecisionOpening(ctx, decisionlog.Entry{
		Actor:         component,
		Payload:       string(payload),
		PolicyVersion: PolicyVersion,
		ScoreVersion:  assessment.Version,
	})
	if err != nil {
		return Opened{}, err
	}
	return Opened{Row: row, Assessment: assessment}, nil
}

// Decide gives the verdict: it appends the closing row as the deciding actor
// — the human at the gate — naming the opening row it closes. A reject with
// no feedback is refused with [ErrFeedbackMissing], because the action is
// "Reject with feedback". A second verdict over one opening is refused by the
// log's store, not here.
func (g *Gate) Decide(ctx context.Context, opened Opened, actor record.Actor, verdict Verdict, feedback string) (decisionlog.Row, error) {
	switch verdict {
	case VerdictApprove, VerdictReject:
	default:
		return decisionlog.Row{}, fmt.Errorf("%w: %q", ErrVerdictUnknown, verdict)
	}
	if verdict == VerdictReject && feedback == "" {
		return decisionlog.Row{}, fmt.Errorf("%w: the reject of %s carries none", ErrFeedbackMissing, opened.Row.ID)
	}

	payload, err := json.Marshal(ClosingPayload{
		Verdict:  string(verdict),
		Feedback: feedback,
	})
	if err != nil {
		return decisionlog.Row{}, fmt.Errorf("gate: marshalling the closing payload: %w", err)
	}

	return g.log.AppendDecisionClosing(ctx, decisionlog.Entry{
		Actor:   actor,
		Payload: string(payload),
		Closes:  opened.Row.ID,
	})
}
