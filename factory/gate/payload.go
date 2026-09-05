package gate

import (
	"encoding/json"
	"fmt"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/score"
)

// What an open event says, and how a row of the log is read back into the
// [Opened] the closing calls take.

// CriterionResult is one criterion decided against the build: the criterion's
// id, what its encoding produced, and whether the service's own unreliable bound
// marks it unreliable — an unreliable criterion reads as absent rather than as a
// failure, which is what [criterion.Outcome.Blocks] already decides.
type CriterionResult struct {
	CriterionID string            `json:"criterion_id"`
	Outcome     criterion.Outcome `json:"outcome"`
	Unreliable  bool              `json:"unreliable,omitempty"`
}

// OpeningPayload is what the open event says. It names the row, the records
// decided over, the artifact version by identifier and content digest, the
// criteria results, the whole vector and the number it reduced to, the values
// actually applied, the holds standing, the mark that put a human there, the
// strategy the score picked, and who the row waits on.
//
// [score.OpenEvent] is embedded rather than restated: the score reads the item,
// the artifact, the row, the number, the threshold, and the selection back off
// this payload when it learns from outcomes, so every one of those field names
// is declared once, in the package that reads them.
type OpeningPayload struct {
	score.OpenEvent
	// ArtifactDigest is the hash the artifact store computed over the version's
	// text when it wrote it. The identifier says which version was decided over
	// and the digest says what it said, so the chain covers the text and not
	// only the name.
	ArtifactDigest string `json:"artifact_digest,omitempty"`
	// IntentID is the intent the Decomposition row decided over, written by
	// [Gate.FireSet] into its own payload and read back here so that a pending
	// set reads as the intent's.
	IntentID      string            `json:"intent_id,omitempty"`
	BuildID       string            `json:"build_id"`
	ServiceID     string            `json:"service_id"`
	AreaID        string            `json:"area_id"`
	EnvironmentID string            `json:"environment_id"`
	ReleaseID     string            `json:"release_id,omitempty"`
	Criteria      []CriterionResult `json:"criteria"`
	// CriteriaInForce is how many criteria the build was decided against, and
	// CriteriaFailed how many of them stopped it — an undecided criterion read
	// the way a failure is, and one the service's unreliable bound marks
	// unreliable read as absent. Neither is a factor: test coverage is
	// deliberately not among them, and these two are what a human at the merge
	// row reads about the run.
	CriteriaInForce int `json:"criteria_in_force"`
	CriteriaFailed  int `json:"criteria_failed"`
	// CouldNotDerive is every derivation that produced no result at this
	// firing, each putting a human at the row the way an undecided criterion
	// does.
	CouldNotDerive   []string `json:"could_not_derive,omitempty"`
	FormulaVersion   string   `json:"formula_version"`
	Likelihood       float64  `json:"likelihood"`
	Impact           float64  `json:"impact"`
	DiscountedImpact float64  `json:"discounted_impact"`
	ThresholdFrom    string   `json:"threshold_from"`
	Safeguards       []string `json:"safeguards"`
	// Resolutions is every factor this firing resolved rather than valued, each
	// with its cause and why, so a human at the row reads what the number could
	// not weigh. The count the score reads back is the embedded opening's own.
	Resolutions  []score.Resolution `json:"resolutions,omitempty"`
	HumanDecides bool               `json:"human_decides"`
	// Marks is what put a human at the row, and is empty where none is. A row
	// waiting on a human that carries none of the five the design names is one
	// the number sent them to, which [MarkTheNumber] writes out.
	Marks []Mark `json:"marks,omitempty"`
	// WhyHeldOut is which of the two ways the item came to be held out, and is
	// empty where it is not. The selection itself is a field of the embedded
	// opening, because the score reads that one back.
	WhyHeldOut string `json:"why_held_out,omitempty"`
	// ReviewSampleRate is the rate in force for the duty this row waits on, and
	// what [MarkReviewSample] was drawn against.
	ReviewSampleRate float64 `json:"review_sample_rate"`
	// Holds is every hold standing at this firing, in the order [HoldsAt] lists
	// them. The row stays open while one stands, and an approve names the set.
	Holds []string `json:"holds,omitempty"`
	// Strategy is the rollout strategy this row picked, present at a deploy to
	// production row and empty everywhere else. The deployer reads it off here.
	Strategy Pick `json:"strategy,omitzero"`
	// WaitsOn is the duty, the named human, and the holders the row waits on.
	WaitsOn Waits `json:"waits_on,omitzero"`
	// Mismatch is what the drift detector found disagreeing with what runs, and
	// is empty on every row that found none. It is on the open event because a
	// human approving through it is saying the record is wrong and the deploy
	// should proceed anyway.
	Mismatch string `json:"drift_mismatch,omitempty"`
	// Unmeasured is which of the deployer's four fields the service is missing,
	// in words, and is empty where it is missing none. A service missing one
	// cannot auto-pass the production deploy row whatever the score computes.
	Unmeasured string `json:"unmeasured,omitempty"`
	// Supersedes is the open event an Edit in place superseded, and is empty on
	// every other firing. The superseded row is ended by an abandonment.
	Supersedes string `json:"supersedes,omitempty"`
	// ReferredFrom is the open event a refer re-fired this row from, and is
	// empty on every other firing.
	ReferredFrom string `json:"referred_from,omitempty"`
	// Referrers is every holder who has referred this row, carried forward from
	// one firing to the next so that a refer with nobody left is refused
	// without a walk back through the chain.
	Referrers []string `json:"referrers,omitempty"`
}

// Opened is what [Gate.Fire] returns and the closing calls take: the opening row
// as it was appended, what the score and the policy answered, and what put a
// human at the row. The verdict is given over this and not over an id, so the
// caller deciding is the caller that saw the vector.
type Opened struct {
	Gate Row
	// Subject is the records the firing decided over, which is what the holds
	// are recomputed against at every re-evaluation and at the close.
	Subject    Subjects
	Row        decisionlog.Row
	Assessment score.Assessment
	Applied    policy.Applied
	// HumanDecides is whether a human decides this row.
	HumanDecides bool
	// Marks is what put a human there, and is empty where none is.
	Marks []Mark
	// HeldOut is whether the score selected this item into its held-out sample.
	HeldOut bool
	// WhyHeldOut is which of the two ways it came to be held out, and is empty
	// where it is not.
	WhyHeldOut string
	// Holds is every hold standing at the firing, in the order [HoldsAt] lists
	// them. An approve names this set.
	Holds []string
	// Strategy is the rollout strategy the row picked, and is empty at every row
	// but a deploy to production.
	Strategy Pick
	// WaitsOn is who the row waits on.
	WaitsOn Waits
	// Mismatch is what the drift detector found disagreeing with what runs, and
	// is empty where it found nothing and at every row but a deploy to
	// production. It is a field of its own beside Marks because a human deciding
	// here has to read what disagrees and not only that something does.
	Mismatch string
	// ArtifactID is the version under decision, and is empty at an event gate.
	ArtifactID string
	// Referrers is every holder who has referred this row.
	Referrers []string
}

// Holding reports whether a hold stands, which is what says the row stays open
// rather than going to a verdict.
func (o Opened) Holding() bool { return len(o.Holds) > 0 }

// Pages reports whether the row fired a page. One condition inside the holds
// does: a mismatch the drift detector found waits on a human and on nothing
// else, where the conditions beside it lift themselves and page nobody.
func (o Opened) Pages() bool { return o.Mismatch != "" }

// openedFrom reads one row of the log back into the [Opened] the closing calls
// and [Gate.Reevaluate] take. It is what a caller holding a pending row rather
// than the firing that wrote it uses.
//
// A payload this package cannot read is an error here and not a skip, because
// the caller named this row: [Gate.Pending] is where a row some other component
// wrote in a shape this package does not know is passed over.
func openedFrom(row decisionlog.Row) (Opened, error) {
	if row.Shape != decisionlog.ShapeDecision || row.Part != decisionlog.PartOpen {
		return Opened{}, fmt.Errorf("%w: %s is shape %q part %q",
			decisionlog.ErrNotAnOpening, row.ID, row.Shape, row.Part)
	}
	var opening OpeningPayload
	if err := json.Unmarshal([]byte(row.Payload), &opening); err != nil {
		return Opened{}, fmt.Errorf("gate: reading the opening payload of %s: %w", row.ID, err)
	}
	gateRow, err := RowFrom(opening.Gate)
	if err != nil {
		return Opened{}, fmt.Errorf("gate: reading the row %s decided at: %w", row.ID, err)
	}
	return Opened{
		Gate: gateRow,
		Subject: Subjects{
			Row: gateRow, IntentID: opening.IntentID,
			ItemID: opening.ItemID, BuildID: opening.BuildID,
			ServiceID: opening.ServiceID, AreaID: opening.AreaID,
			EnvironmentID: opening.EnvironmentID, ReleaseID: opening.ReleaseID,
		},
		Row: row,
		Assessment: score.Assessment{
			Version:          row.ScoreVersion,
			FormulaVersion:   opening.FormulaVersion,
			FactorSet:        opening.FactorSet,
			Vector:           opening.Vector,
			Resolved:         opening.Resolutions,
			Likelihood:       opening.Likelihood,
			Impact:           opening.Impact,
			DiscountedImpact: opening.DiscountedImpact,
			Number:           opening.Number,
		},
		Applied: policy.Applied{
			PolicyVersion: row.PolicyVersion,
			Threshold:     opening.Threshold,
			ThresholdFrom: policy.Source(opening.ThresholdFrom),
			Safeguards:    opening.Safeguards,
		},
		HumanDecides: opening.HumanDecides,
		Marks:        opening.Marks,
		HeldOut:      opening.HeldOut,
		WhyHeldOut:   opening.WhyHeldOut,
		Holds:        opening.Holds,
		Strategy:     opening.Strategy,
		WaitsOn:      opening.WaitsOn,
		Mismatch:     opening.Mismatch,
		ArtifactID:   opening.ArtifactID,
		Referrers:    opening.Referrers,
	}, nil
}
