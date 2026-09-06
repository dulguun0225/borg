// [gate.Gate.FireSet] over the Decomposition row: the riskiest member's number
// applied to the whole set, its reject naming no stage, and edit in place
// refused.
package gate_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/score"
)

// varyingScore answers a different number per item, which is what a set firing needs:
// the row applies the riskiest member's.
type varyingScore struct {
	by           map[string]float64
	asked        score.Change
	heldOutAsked []string
}

func (v *varyingScore) AssessUnder(_ context.Context, version score.Version, c score.Change) (score.Assessment, error) {
	v.asked = c
	assessment := assessed(v.by[c.ItemID])
	assessment.Version = version.ID
	return assessment, nil
}

// HoldOut selects nothing. The Decomposition row does not ask, and a test that
// drives one asserts that: the sample selects an item and one draw over a set
// would select several on a number that is none of theirs.
func (v *varyingScore) HoldOut(_ context.Context, itemID string, _ float64, _, _ bool, _ []score.Resolution) (score.Selection, error) {
	v.heldOutAsked = append(v.heldOutAsked, itemID)
	return score.Selection{}, nil
}

// Version is the score version every decision this varying score's gate opens
// names.
func (v *varyingScore) Version() score.Version {
	return score.Version{ID: testScoreVersion, FormulaVersion: score.FormulaVersion}
}

// TestTheDecompositionRowDecidesOverASetAndAppliesItsRiskiestMember: the one row
// where approving admits several timelines at once, fired over the items decomposition wrote.
func TestTheDecompositionRowDecidesOverASetAndAppliesItsRiskiestMember(t *testing.T) {
	// Two members and two answers: the score is asked per member and the row
	// applies the higher of the numbers, because approving the set approves every
	// item in it.
	s, p := &varyingScore{by: map[string]float64{"it_a": 0.2, "it_b": 0.7}}, &fakePolicy{applied: applied(0.5)}
	ctx, pool, token, g := newGate(t, s, p)

	opened, err := g.FireSet(ctx, gate.SetFiring{
		IntentID:      "in_0000000000000000000000000000000a",
		EnvironmentID: "env_000000000000000000000000000000a",
		Members: []gate.SetMember{
			{ItemID: "it_a", ServiceID: "svc_a", AreaID: "ar_a", Requirements: 2},
			{ItemID: "it_b", ServiceID: "svc_b", AreaID: "ar_a", Requirements: 5, WaitsOn: []string{"it_a"}},
		},
	})
	if err != nil {
		t.Fatalf("FireSet: %v", err)
	}
	if opened.Gate != gate.Decomposition {
		t.Fatalf("the row is %s", opened.Gate)
	}
	if opened.Assessment.Number != 0.7 || !opened.HumanDecides {
		t.Fatalf("the row applied %v and human=%v, want the riskiest member's number over the threshold",
			opened.Assessment.Number, opened.HumanDecides)
	}

	var payload gate.SetOpeningPayload
	if err := json.Unmarshal([]byte(opened.Row.Payload), &payload); err != nil {
		t.Fatalf("reading the opening payload: %v", err)
	}
	if payload.IntentID != "in_0000000000000000000000000000000a" {
		t.Errorf("the open event names intent %q", payload.IntentID)
	}
	if len(payload.Set) != 2 {
		t.Fatalf("the open event carries %d members, want the whole set whichever one drove the number", len(payload.Set))
	}
	if payload.NumberFrom != "it_b" {
		t.Errorf("the number came from %q, want the riskier member", payload.NumberFrom)
	}
	if len(payload.Set[1].WaitsOn) != 1 {
		t.Errorf("the row does not say what waits on what: %+v", payload.Set)
	}
	// The subject a decision names is what the score reads back when it counts
	// outcomes, and this row names none: decomposition proposes a set rather than an
	// artifact, so a verdict here is an outcome on no author's work.
	var opening score.OpenEvent
	if err := json.Unmarshal([]byte(opened.Row.Payload), &opening); err != nil {
		t.Fatalf("reading the payload as an opening: %v", err)
	}
	if opening.ItemID != "" || opening.ArtifactID != "" {
		t.Errorf("the Decomposition row names an item %+v, and decomposition is not an artifact", opening)
	}
	if opening.HeldOut {
		t.Error("the Decomposition row says the score held something out, and the sample does not reach a set")
	}
	// The change group here is computed from the set decomposition proposed: the
	// requirements the member answers and the services the set spans. Nothing is
	// unavailable — a factor this row was never going to have, treated as
	// missing, would put a human at every decomposition forever.
	if s.asked.Measurement.Unavailable != "" {
		t.Errorf("the score was asked with measurement %+v", s.asked.Measurement)
	}
	if !s.asked.Measurement.FromProposedSet() {
		t.Errorf("the measurement is not the set proposed: %+v", s.asked.Measurement)
	}
	if s.asked.Measurement.RequirementsProposed != 5 || s.asked.Measurement.ServicesProposed != 2 {
		t.Errorf("the last member was assessed at %d requirement(s) over %d service(s), want 5 over 2",
			s.asked.Measurement.RequirementsProposed, s.asked.Measurement.ServicesProposed)
	}
	if payload.Set[1].Requirements != 5 {
		t.Errorf("the open event records %d requirement(s) for the riskier member, want 5",
			payload.Set[1].Requirements)
	}

	closing, err := g.Decide(ctx, opened, gate.Given{Actor: owner, Verdict: gate.VerdictApprove})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if err := decisionlog.NewReader(pool, token).Verify(ctx, owner); err != nil {
		t.Fatalf("the chain does not verify after a set decision: %v", err)
	}
	if closing.Closes != opened.Row.ID {
		t.Errorf("the close event closes %q", closing.Closes)
	}
}

// TestARejectAtDecompositionNamesNoStage: its reject re-decomposes the set rather than
// sending an item anywhere, so the field its close event would carry stays unwritten.
func TestARejectAtDecompositionNamesNoStage(t *testing.T) {
	s, p := &varyingScore{by: map[string]float64{"it_a": 0.7, "it_b": 0.7}}, &fakePolicy{applied: applied(0.5)}
	ctx, _, _, g := newGate(t, s, p)

	opened, err := g.FireSet(ctx, gate.SetFiring{
		IntentID:      "in_0000000000000000000000000000000a",
		EnvironmentID: "env_000000000000000000000000000000a",
		Members: []gate.SetMember{
			{ItemID: "it_a", ServiceID: "svc_a", Requirements: 1},
			{ItemID: "it_b", ServiceID: "svc_b", Requirements: 1},
		},
	})
	if err != nil {
		t.Fatalf("FireSet: %v", err)
	}
	closing, err := g.Decide(ctx, opened, gate.Given{Actor: owner, Verdict: gate.VerdictReject, Reason: "this should have been three items"})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	var payload gate.ClosingPayload
	if err := json.Unmarshal([]byte(closing.Payload), &payload); err != nil {
		t.Fatalf("reading the closing payload: %v", err)
	}
	if payload.ReturnsTo != "" {
		t.Errorf("the reject returns the item to %q, and Decomposition names nothing at all", payload.ReturnsTo)
	}
}

// TestASetFiringMissingSomethingIsRefused: the row fires where decomposition yielded more
// than one item, and a firing of one is not an error of shape but of occasion.
func TestASetFiringMissingSomethingIsRefused(t *testing.T) {
	s, p := &varyingScore{by: map[string]float64{}}, &fakePolicy{applied: applied(0.5)}
	ctx, _, _, g := newGate(t, s, p)

	two := []gate.SetMember{
		{ItemID: "it_a", ServiceID: "svc_a", Requirements: 1},
		{ItemID: "it_b", ServiceID: "svc_b", Requirements: 1},
	}
	for name, firing := range map[string]gate.SetFiring{
		"no intent":      {EnvironmentID: "env_a", Members: two},
		"no environment": {IntentID: "in_a", Members: two},
		"one member":     {IntentID: "in_a", EnvironmentID: "env_a", Members: two[:1]},
		"a member with no service": {IntentID: "in_a", EnvironmentID: "env_a",
			Members: []gate.SetMember{{ItemID: "it_a", Requirements: 1}, {ItemID: "it_b", ServiceID: "svc_b", Requirements: 1}}},
		"a member answering no requirement": {IntentID: "in_a", EnvironmentID: "env_a",
			Members: []gate.SetMember{{ItemID: "it_a", ServiceID: "svc_a"}, {ItemID: "it_b", ServiceID: "svc_b", Requirements: 1}}},
	} {
		if _, err := g.FireSet(ctx, firing); !errors.Is(err, gate.ErrSetIncomplete) {
			t.Errorf("a set firing with %s = %v, want ErrSetIncomplete", name, err)
		}
	}
	// And a Decomposition firing given as an ordinary one is refused: that row
	// decides over a set and not over one item's build.
	f := mergeFiring
	f.Row = gate.Decomposition
	if _, err := g.Fire(ctx, f); !errors.Is(err, gate.ErrFiringIncomplete) {
		t.Errorf("firing Decomposition through Fire = %v, want ErrFiringIncomplete", err)
	}
}

// TestEditInPlaceAtDecompositionIsRefusedWithItsReason: re-decomposing is not
// built, so a bad decomposition is rejected rather than repaired, and edit in
// place — a human authoring a new version while the row waits — is refused with
// its own reason. Decomposition still offers approve, reject and refer: refer
// is on every row, and it is not edit in place.
func TestEditInPlaceAtDecompositionIsRefusedWithItsReason(t *testing.T) {
	actions, err := gate.Actions(gate.Decomposition)
	if err != nil {
		t.Fatalf("Actions: %v", err)
	}
	if len(actions) != 3 || actions[0] != gate.VerdictApprove ||
		!slices.Contains(actions, gate.VerdictReject) || !slices.Contains(actions, gate.VerdictRefer) {
		t.Fatalf("Decomposition offers %v, want approve, reject and refer", actions)
	}

	s, p := &varyingScore{by: map[string]float64{"it_a": 0.2, "it_b": 0.3}}, &fakePolicy{applied: applied(0.5)}
	ctx, _, _, g := newGate(t, s, p)
	opened, err := g.FireSet(ctx, gate.SetFiring{
		IntentID:      "in_0000000000000000000000000000000a",
		EnvironmentID: "env_000000000000000000000000000000a",
		Members: []gate.SetMember{
			{ItemID: "it_a", ServiceID: "svc_a", Requirements: 1},
			{ItemID: "it_b", ServiceID: "svc_b", Requirements: 1},
		},
	})
	if err != nil {
		t.Fatalf("FireSet: %v", err)
	}
	if _, err := g.EditInPlace(ctx, opened, gate.Firing{}); !errors.Is(err, gate.ErrEditInPlaceRefused) {
		t.Errorf("EditInPlace at Decomposition = %v, want ErrEditInPlaceRefused", err)
	}
}
