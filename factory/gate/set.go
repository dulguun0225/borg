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

// The Decomposition row, which is the one row that decides over a set rather than
// over one item's build. Everything below it is per item, so everything else in
// this package takes one [Firing]; this file is what the set costs.

// ErrSetIncomplete is returned by [Gate.FireSet] for a firing missing the intent
// or naming fewer than two items. Fewer than two is not an error of shape but of
// occasion: the row fires where the cut yielded more than one item, and putting it
// in front of a single-item cut would be a decision about nothing.
var ErrSetIncomplete = errors.New("gate: the set firing is missing something every one has")

// SetMember is one item of a cut as the row decides over it: the item, the
// service it changes, the area it is in, and what it waits on. There is no build
// and no artifact — the cut writes items and the gate decides over records that
// already exist, so what is decided is the shape of the set.
type SetMember struct {
	ItemID    string
	ServiceID string
	AreaID    string
	WaitsOn   []string
}

// SetFiring is what fires the Decomposition row: the intent the cut was over,
// production's environment — whose record holds the threshold every row reads —
// and every item the cut produced.
type SetFiring struct {
	IntentID      string
	EnvironmentID string
	Members       []SetMember
}

// SetMemberPayload is one member as the opening row stores it.
type SetMemberPayload struct {
	ItemID    string   `json:"item_id"`
	ServiceID string   `json:"service_id"`
	AreaID    string   `json:"area_id"`
	WaitsOn   []string `json:"waits_on"`
}

// SetOpeningPayload is what the Decomposition row's opening row says: the intent,
// the whole set, the vector of the member the number came from, and the values
// applied.
//
// It does not embed [score.Subject], which every other opening row does, and the
// omission is the point: the cut proposes a set rather than an artifact, so a
// verdict here is an outcome on no author's work and on no one item. NumberFrom
// names the member whose number was applied so a reader can see what drove it,
// and it is a field of its own rather than the subject for exactly that reason —
// the score reads the subject back when it counts outcomes, and this decision is
// not one of them.
type SetOpeningPayload struct {
	Gate     string             `json:"gate"`
	IntentID string             `json:"intent_id"`
	Set      []SetMemberPayload `json:"set"`
	// NumberFrom is the item whose assessment the row applied, which is the
	// highest of the set's.
	NumberFrom     string         `json:"number_applied_from_item"`
	Vector         []score.Factor `json:"vector"`
	FormulaVersion string         `json:"formula_version"`
	Likelihood     float64        `json:"likelihood"`
	Impact         float64        `json:"impact"`
	Exposure       float64        `json:"exposure"`
	Number         float64        `json:"number"`
	Threshold      float64        `json:"threshold"`
	ThresholdFrom  string         `json:"threshold_from"`
	Pins           []string       `json:"pins"`
	Unavailable    []string       `json:"unavailable_factors"`
	HumanDecides   bool           `json:"human_decides"`
	WhyHuman       string         `json:"why_human"`
	WaitsOn        string         `json:"waits_on"`
}

// NoBuildAtTheCut is why the diff could not be measured at this row, written onto
// every member's measurement. The change factors computed from a diff are
// unavailable at the cut, because the cut happens before anything is built — and
// an unavailable factor resolves to the top of the scale, so the formula puts a
// human at this row.
//
// That is the design's own rule for an unavailable factor rather than a decision
// this row takes, and it costs a human at every Decomposition until there is a
// factor set that can be computed over a proposed set rather than over a build.
// What it buys is that the row is scored like every other, with the vector on the
// opening row for the human to argue with, instead of a row that auto-passes on a
// number computed from nothing.
const NoBuildAtTheCut = "no build exists at the cut, and the diff is measured from one"

// FireSet fires the Decomposition row over one cut. It asks the score about each
// member and applies the highest of their numbers, because approving the set
// approves every item in it — a set is as risky as its riskiest item. What that
// costs is that a set of ten small items and one large one is decided at the large
// one's number, which is the safe direction and is a real loss of throughput.
//
// The whole set's members are on the opening row whichever one the number came
// from, so a human reading it sees what they are approving and not only what drove
// the number.
func (g *Gate) FireSet(ctx context.Context, f SetFiring) (Opened, error) {
	if f.IntentID == "" {
		return Opened{}, fmt.Errorf("%w: it names no intent", ErrSetIncomplete)
	}
	if f.EnvironmentID == "" {
		return Opened{}, fmt.Errorf("%w: it names no environment to read the threshold from", ErrSetIncomplete)
	}
	if len(f.Members) < 2 {
		return Opened{}, fmt.Errorf("%w: it names %d item(s), and this row fires where the cut yielded more than one",
			ErrSetIncomplete, len(f.Members))
	}

	var applied score.Assessment
	from := ""
	members := make([]SetMemberPayload, 0, len(f.Members))
	for _, m := range f.Members {
		if m.ItemID == "" || m.ServiceID == "" {
			return Opened{}, fmt.Errorf("%w: a member names item %q and service %q",
				ErrSetIncomplete, m.ItemID, m.ServiceID)
		}
		assessment, err := g.score.Assess(ctx, score.Change{
			ItemID:      m.ItemID,
			ServiceID:   m.ServiceID,
			AreaID:      m.AreaID,
			Measurement: score.Measurement{Unavailable: NoBuildAtTheCut},
		})
		if err != nil {
			return Opened{}, fmt.Errorf("gate: assessing item %s of the cut: %w", m.ItemID, err)
		}
		if from == "" || assessment.Number > applied.Number {
			applied, from = assessment, m.ItemID
		}
		members = append(members, SetMemberPayload{
			ItemID: m.ItemID, ServiceID: m.ServiceID, AreaID: m.AreaID, WaitsOn: m.WaitsOn,
		})
	}

	// The policy is read against the row and production's environment, which is
	// what every row reads its threshold from. It is not read against a service:
	// one cut changes several, and a threshold per service would make one row read
	// two.
	policyApplied, err := g.policy.AtGate(ctx, subjectsFor(Decomposition, f))
	if err != nil {
		return Opened{}, fmt.Errorf("gate: reading what applies at %s: %w", Decomposition, err)
	}

	overThreshold := applied.Number >= policyApplied.Threshold
	opened := Opened{
		Gate:         Decomposition,
		Assessment:   applied,
		Applied:      policyApplied,
		HumanDecides: overThreshold || policyApplied.HumanPinned,
		WhyHuman:     why(overThreshold, policyApplied.HumanPinned, false),
	}
	waitsOn := ""
	if opened.HumanDecides {
		waitsOn = WaitsOn(Decomposition)
	}
	payload, err := json.Marshal(SetOpeningPayload{
		Gate:           string(Decomposition),
		IntentID:       f.IntentID,
		Set:            members,
		NumberFrom:     from,
		Vector:         applied.Vector,
		FormulaVersion: applied.FormulaVersion,
		Likelihood:     applied.Likelihood,
		Impact:         applied.Impact,
		Exposure:       applied.Exposure,
		Number:         applied.Number,
		Threshold:      policyApplied.Threshold,
		ThresholdFrom:  string(policyApplied.ThresholdFrom),
		Pins:           policyApplied.Pins,
		Unavailable:    applied.UnavailableFactors(),
		HumanDecides:   opened.HumanDecides,
		WhyHuman:       opened.WhyHuman,
		WaitsOn:        waitsOn,
	})
	if err != nil {
		return Opened{}, fmt.Errorf("gate: marshalling the opening payload of the cut: %w", err)
	}
	row, err := g.log.AppendDecisionOpening(ctx, decisionlog.Entry{
		Actor:         component(Decomposition),
		Payload:       string(payload),
		PolicyVersion: policyApplied.PolicyVersion,
		ScoreVersion:  applied.Version,
	})
	if err != nil {
		return Opened{}, err
	}
	opened.Row = row
	return opened, nil
}

// subjectsFor is what the policy read at this row is performed against: the row,
// production's environment record, and the area of the first member that has one.
// A cut changes several services and may cross several areas, so there is no
// single service to read a pin on — what a pin on one of those areas does is reach
// the items in it at every gate below this one, which is where the design puts a
// holder who wants to decide at the cut for their own service.
func subjectsFor(row Row, f SetFiring) policy.Subjects {
	subjects := policy.Subjects{GateRow: string(row), EnvironmentID: f.EnvironmentID}
	for _, m := range f.Members {
		if m.AreaID != "" {
			subjects.AreaID = m.AreaID
			break
		}
	}
	return subjects
}
