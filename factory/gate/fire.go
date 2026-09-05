package gate

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/score"
)

// [Gate.Fire], one item's build against one row: what fires it, what it asks
// the score and the policy, what it opens, and the payload the open event
// stores. set.go is the same firing over a set instead of one item's build.

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
	// Mismatch is what the drift detector found disagreeing with what runs, and is
	// empty where it found nothing and at every row but the production deploy. It
	// is a field of its own beside WhyHuman because a human deciding here has to
	// read what disagrees and not only that something does.
	Mismatch string
}

// OpeningPayload is what the open event says. It names the row, the records
// decided over, the criteria results, the whole vector and the number it reduced
// to, the values actually applied, and what the row waits on.
//
// [score.OpenEvent] is embedded rather than restated: the score reads the item, the
// artifact, the row, the number, the threshold, and the selection back off this
// payload when it learns from outcomes, so every one of those field names is
// declared once, in the package that reads them.
type OpeningPayload struct {
	score.OpenEvent
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
	Safeguards     []string          `json:"safeguards"`
	Unavailable    []string          `json:"unavailable_factors"`
	HumanDecides   bool              `json:"human_decides"`
	WhyHuman       string            `json:"why_human"`
	// WhyHeldOut is which of the two ways the item came to be held out, and is
	// empty where it is not. The selection itself is a field of the embedded
	// opening, because the score reads that one back.
	WhyHeldOut string `json:"why_held_out,omitempty"`
	WaitsOn    string `json:"waits_on"`
	// Mismatch is what the drift detector found disagreeing with what runs, and is
	// empty on every row that found none. It is on the open event because a human
	// approving through it is saying the record is wrong and the deploy should
	// proceed anyway, which is a verdict nobody can read against a row that does
	// not say what disagreed.
	Mismatch string `json:"drift_mismatch,omitempty"`
}

// Fire fires the gate: it asks the score about the change and the policy about
// the values in force, decides whether a human decides, composes the opening
// payload, and appends the open event as the gate component. The vector is
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

	// Drift detection's store, read at the production deploy row and at no other:
	// what it holds is a disagreement about what is running in production, and no
	// other row decides a deploy into it. A mismatch puts a human here whatever the
	// number reads, because nothing the factory can decide on the record is worth
	// deciding while the record is the thing in doubt.
	mismatch := ""
	if f.Row == DeployToProduction {
		found, why, err := g.driftdetector.Mismatch(ctx, f.ServiceID)
		if err != nil {
			return Opened{}, fmt.Errorf("gate: reading the drift detector's store for %s: %w", f.ServiceID, err)
		}
		if found {
			mismatch = why
		}
	}

	// The score's own sample, asked after the policy answered and before the human
	// test: what it may pass is a gate the score itself would have gated, which is
	// the number against the threshold in force, and it may pass nothing a
	// safeguard put a human at.
	overThreshold := assessment.Number >= applied.Threshold
	selection, err := g.score.HoldOut(ctx, f.ItemID, overThreshold, applied.HumanBySafeguard)
	if err != nil {
		return Opened{}, fmt.Errorf("gate: asking the score whether %s is held out: %w", f.ItemID, err)
	}

	// A held-out item removes the human the number put at the row and no other. A
	// safeguard's human and a mismatch's stand: the sample is the score holding
	// itself out of its own gate, and nothing in the design lets it out of anyone
	// else's.
	gatedByNumber := overThreshold && !selection.HeldOut
	opened := Opened{
		Gate:         f.Row,
		Assessment:   assessment,
		Applied:      applied,
		HumanDecides: gatedByNumber || applied.HumanBySafeguard || mismatch != "",
		WhyHuman:     why(gatedByNumber, applied.HumanBySafeguard, mismatch != ""),
		HeldOut:      selection.HeldOut,
		WhyHeldOut:   selection.Why,
		Mismatch:     mismatch,
	}

	waitsOn := ""
	if opened.HumanDecides {
		waitsOn = WaitsOn(f.Row)
	}
	payload, err := json.Marshal(OpeningPayload{
		OpenEvent: score.OpenEvent{
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
		Safeguards:     applied.Safeguards,
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

	row, err := g.log.AppendDecisionOpen(ctx, decisionlog.Entry{
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
// Merge to master gate. Undecided is counted with failed, which is what the design says of
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
