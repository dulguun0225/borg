package gate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/score"
)

// The Decomposition row, which is the one row that decides over a set rather
// than over one item's build. Everything below it is per item, so everything
// else in this package takes one [Firing]; this file is what the set costs.

// ErrSetIncomplete is returned by [Gate.FireSet] for a firing missing the intent
// or naming fewer than two items. Fewer than two is not an error of shape but of
// occasion: the row fires where decomposition yielded more than one item, and
// putting it in front of a single-item decomposition would be a decision about
// nothing.
var ErrSetIncomplete = errors.New("gate: the set firing is missing something every one has")

// SetMember is one item of a decomposition as the row decides over it: the item,
// the service it changes, the area it is in, how many of the intent's
// requirements it answers, and what it waits on. There is no build and no
// artifact — decomposition writes items and the gate decides over records that
// already exist, so what is decided is the shape of the set.
type SetMember struct {
	ItemID    string
	ServiceID string
	AreaID    string
	// Requirements is how many of the intent's requirements this item answers,
	// which is the unit decomposition sets and the unit the item-size target is
	// authored in. It is what the change group is computed from at this row,
	// there being no build and no diff, so a member that names none is refused.
	Requirements int
	WaitsOn      []string
}

// SetFiring is what fires the Decomposition row: the intent decomposition was
// over, production's environment — whose record holds the threshold every row
// reads — and every item decomposition produced.
type SetFiring struct {
	IntentID      string
	EnvironmentID string
	Members       []SetMember
}

// SetMemberPayload is one member as the open event stores it.
type SetMemberPayload struct {
	ItemID    string `json:"item_id"`
	ServiceID string `json:"service_id"`
	AreaID    string `json:"area_id"`
	// Requirements is how many of the intent's requirements this item answers,
	// which is what the change group was computed from here.
	Requirements int      `json:"requirements"`
	WaitsOn      []string `json:"waits_on"`
}

// SetOpeningPayload is what the Decomposition row's open event says: the intent,
// the whole set, the vector of the member the number came from, the values
// applied, and the mark that put a human at the row.
//
// It does not embed [score.OpenEvent], which every other open event does, and
// the omission is the point: decomposition proposes a set rather than an
// artifact, so a verdict here is an outcome on no author's work and on no one
// item.
// NumberFrom names the member whose number was applied so a reader can see what
// drove it.
type SetOpeningPayload struct {
	Gate     string             `json:"gate"`
	IntentID string             `json:"intent_id"`
	Set      []SetMemberPayload `json:"set"`
	// NumberFrom is the item whose assessment the row applied, which is the
	// highest of the set's.
	NumberFrom       string             `json:"number_applied_from_item"`
	Vector           []score.Factor     `json:"vector"`
	FormulaVersion   string             `json:"formula_version"`
	Likelihood       float64            `json:"likelihood"`
	Impact           float64            `json:"impact"`
	DiscountedImpact float64            `json:"discounted_impact"`
	Number           float64            `json:"number"`
	Threshold        float64            `json:"threshold"`
	ThresholdFrom    string             `json:"threshold_from"`
	Safeguards       []string           `json:"safeguards"`
	Resolutions      []score.Resolution `json:"resolutions,omitempty"`
	HumanDecides     bool               `json:"human_decides"`
	Marks            []Mark             `json:"marks,omitempty"`
	WaitsOn          Waits              `json:"waits_on,omitzero"`
}

// FireSet fires the Decomposition row over one decomposition. It asks the score
// about each member and applies the highest of their numbers, because approving
// the set approves every item in it — a set is as risky as its riskiest item.
//
// The change group here is computed from the set decomposition proposed and not
// from a diff: how many of the intent's requirements each item answers, and how
// many services the set spans. There is no build at this row and there was never
// going to be one, so nothing is resolved on its account — a factor the gate was
// never going to have, treated as missing, would put a human at every
// decomposition forever.
// What that costs is that a set of ten small items and one large one is decided
// at the large one's number, which is the safe direction and is a real loss of
// throughput.
//
// The intent's own state is not read here, where every other firing reads it.
// This row is the one gate above a timeline rather than on it, and the state it
// would refuse — re-decomposing — is the state this firing itself puts the
// intent in: a row that refused it could never close the re-decomposition it was
// opened for.
//
// The score's held-out sample is not asked here either, and that is the one
// thing this row does not do that every row below it does. The sample selects an
// item, and one draw over a set would select several on one number that is none
// of theirs. What it costs is that this row produces no unbiased evidence, so
// the threshold the score supplies for it can fall and never rise.
func (g *Gate) FireSet(ctx context.Context, f SetFiring) (Opened, error) {
	if f.IntentID == "" {
		return Opened{}, fmt.Errorf("%w: it names no intent", ErrSetIncomplete)
	}
	if f.EnvironmentID == "" {
		return Opened{}, fmt.Errorf("%w: it names no environment to read the threshold from", ErrSetIncomplete)
	}
	if len(f.Members) < 2 {
		return Opened{}, fmt.Errorf("%w: it names %d item(s), and this row fires where decomposition yielded more than one",
			ErrSetIncomplete, len(f.Members))
	}
	if err := g.noSetPending(ctx, f.IntentID); err != nil {
		return Opened{}, err
	}

	span := servicesSpanned(f)

	var applied score.Assessment
	from := ""
	members := make([]SetMemberPayload, 0, len(f.Members))
	for _, m := range f.Members {
		if m.ItemID == "" || m.ServiceID == "" {
			return Opened{}, fmt.Errorf("%w: a member names item %q and service %q",
				ErrSetIncomplete, m.ItemID, m.ServiceID)
		}
		if m.Requirements <= 0 {
			return Opened{}, fmt.Errorf("%w: member %s answers %d requirement(s), and the change group here is computed from the set proposed",
				ErrSetIncomplete, m.ItemID, m.Requirements)
		}
		assessment, err := g.score.Assess(ctx, score.Change{
			ItemID:    m.ItemID,
			ServiceID: m.ServiceID,
			AreaID:    m.AreaID,
			FactorSet: score.SetAboveABuild,
			Measurement: score.Measurement{
				RequirementsProposed: m.Requirements,
				ServicesProposed:     span,
			},
		})
		if err != nil {
			return Opened{}, fmt.Errorf("gate: assessing item %s of decomposition: %w", m.ItemID, err)
		}
		if from == "" || assessment.Number > applied.Number {
			applied, from = assessment, m.ItemID
		}
		members = append(members, SetMemberPayload{
			ItemID: m.ItemID, ServiceID: m.ServiceID, AreaID: m.AreaID,
			Requirements: m.Requirements, WaitsOn: m.WaitsOn,
		})
	}

	// The policy is read against the row and production's environment, which is
	// what every row reads its threshold from. It is not read against a service:
	// one decomposition changes several, and a threshold per service would make
	// one row read two.
	policyApplied, err := g.policy.AtGate(ctx, component(Decomposition), subjectsFor(Decomposition, f))
	if err != nil {
		return Opened{}, fmt.Errorf("gate: reading what applies at %s: %w", Decomposition, err)
	}

	overThreshold := applied.Number >= policyApplied.Threshold
	resolved := len(applied.Resolved) > 0
	marks := marksOn(overThreshold, resolved, policyApplied.HumanBySafeguard, false, false, false)
	waits, err := g.waitsOn(ctx, Decomposition, nil, RoutedTo{})
	if err != nil {
		return Opened{}, err
	}
	opened := Opened{
		Gate:         Decomposition,
		Subject:      Subjects{Row: Decomposition, IntentID: f.IntentID, EnvironmentID: f.EnvironmentID},
		Assessment:   applied,
		Applied:      policyApplied,
		HumanDecides: len(marks) > 0,
		Marks:        marks,
		WaitsOn:      waits,
	}
	if !opened.HumanDecides {
		opened.WaitsOn = Waits{}
	}

	payload, err := json.Marshal(SetOpeningPayload{
		Gate:             Decomposition.String(),
		IntentID:         f.IntentID,
		Set:              members,
		NumberFrom:       from,
		Vector:           applied.Vector,
		FormulaVersion:   applied.FormulaVersion,
		Likelihood:       applied.Likelihood,
		Impact:           applied.Impact,
		DiscountedImpact: applied.DiscountedImpact,
		Number:           applied.Number,
		Threshold:        policyApplied.Threshold,
		ThresholdFrom:    string(policyApplied.ThresholdFrom),
		Safeguards:       policyApplied.Safeguards,
		Resolutions:      applied.Resolved,
		HumanDecides:     opened.HumanDecides,
		Marks:            opened.Marks,
		WaitsOn:          opened.WaitsOn,
	})
	if err != nil {
		return Opened{}, fmt.Errorf("gate: marshalling the opening payload of decomposition: %w", err)
	}
	row, err := g.log.AppendDecisionOpen(ctx, decisionlog.Entry{
		Actor:         component(Decomposition),
		Payload:       string(payload),
		FormatVersion: decisionFormatVersion,
		PolicyVersion: policyApplied.PolicyVersion,
		ScoreVersion:  applied.Version,
	})
	if err != nil {
		return Opened{}, err
	}
	opened.Row = row
	return opened, nil
}

// noSetPending refuses a second Decomposition firing over one intent while the
// first is still open, which is the same rule [Gate.Fire] keeps per item: one
// gate on one thing has at most one pending row.
func (g *Gate) noSetPending(ctx context.Context, intentID string) error {
	pending, err := g.Pending(ctx)
	if err != nil {
		return err
	}
	for _, open := range pending {
		if open.Gate == Decomposition && open.Subject.IntentID == intentID {
			return fmt.Errorf("%w: %s over %s is open as %s",
				ErrRowPending, Decomposition, intentID, open.Row.ID)
		}
	}
	return nil
}

// subjectsFor is what the policy read at this row is performed against: the row,
// production's environment record, and the area of the first member that has
// one. A decomposition changes several services and may cross several areas, so
// there is no single service to read a safeguard on — what a safeguard on one of
// those areas does is reach the items in it at every gate below this one.
func subjectsFor(row Row, f SetFiring) policy.Subjects {
	subjects := policy.Subjects{GateRow: row.String(), EnvironmentID: f.EnvironmentID}
	for _, m := range f.Members {
		if m.AreaID != "" {
			subjects.AreaID = m.AreaID
			break
		}
	}
	return subjects
}

// servicesSpanned is how many services the set decomposition proposed changes,
// which is the reach the change group reads at this row. It is the set's and not
// the member's: what one item can affect at decomposition is the set it was
// proposed in, every member of which is approved by one verdict.
func servicesSpanned(f SetFiring) int {
	seen := map[string]bool{}
	for _, m := range f.Members {
		if m.ServiceID != "" {
			seen[m.ServiceID] = true
		}
	}
	return len(seen)
}
