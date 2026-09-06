// The rollout strategy the production deploy row picks, read off the open
// event: which bound applied, where more than one could have.
package gate_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/score"
)

// TestTheHeldOutSampleKeepsItsOwnBoundInAnIrreversibleArea: Why names what
// bounded the pick, and a bound already named is not overwritten — an
// irreversible area holds a controlled rollout to the widening schedule, which
// is the schedule every controlled rollout already takes, so the reading a
// human gets beside the strategy stays the sample's.
func TestTheHeldOutSampleKeepsItsOwnBoundInAnIrreversibleArea(t *testing.T) {
	s := &fakeScore{assessment: assessed(0.2), selection: score.Selection{HeldOut: true, Why: "the sample"}}
	p := &fakePolicy{applied: applied(0.1), heldOutRate: 1}
	ctx, pool, token, g := newGate(t, s, p)

	declared, err := area.NewWriter(pool, token).Declare(ctx, owner, "payouts",
		area.Inside{ProjectID: "prj_00000000000000000000000000000a"},
		area.Hazard{
			Grade: area.GradeIrreversible, Operation: "the payout",
			Bound: 100, BoundPeriodSeconds: 86400,
		})
	if err != nil {
		t.Fatalf("declaring the irreversible area: %v", err)
	}

	firing := deployFiring(t, ctx, pool, token)
	firing.AreaID = declared.ID
	// A release being replaced is what makes a control possible at all; without
	// one the pick is bounded by the first release before anything else is read.
	firing.ReplacesReleaseID = "rel_000000000000000000000000000000b"
	opened, err := g.Fire(ctx, firing)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}

	if opened.Strategy.Strategy != gate.StrategyWithControl {
		t.Fatalf("the pick is %+v, want the row with a control", opened.Strategy)
	}
	if opened.Strategy.Schedule != gate.ScheduleWidened {
		t.Errorf("the schedule is %q, want the widening one", opened.Strategy.Schedule)
	}
	if opened.Strategy.Why != gate.WhyHeldOut {
		t.Errorf("the pick's bound reads %q, want the held-out sample's, which is what applied",
			opened.Strategy.Why)
	}
}

// TestAnIrreversibleAreaIsTheBoundWhereNothingElseWasOne: with no sample and no
// first release, the area is what bounded the pick and Why says so.
func TestAnIrreversibleAreaIsTheBoundWhereNothingElseWasOne(t *testing.T) {
	s := &fakeScore{assessment: assessed(0.2)}
	p := &fakePolicy{applied: applied(0.5)}
	ctx, pool, token, g := newGate(t, s, p)

	// The impact discounted by reversibility at or above the bound the score
	// version names is what picks the row with a control where no sample did.
	s.assessment.ControlBound = score.ShippedControlBound
	s.assessment.DiscountedImpact = score.ShippedControlBound

	declared, err := area.NewWriter(pool, token).Declare(ctx, owner, "erasures",
		area.Inside{ProjectID: "prj_00000000000000000000000000000a"},
		area.Hazard{
			Grade: area.GradeIrreversible, Operation: "the erasure",
			Bound: 10, BoundPeriodSeconds: 3600,
		})
	if err != nil {
		t.Fatalf("declaring the irreversible area: %v", err)
	}

	firing := deployFiring(t, ctx, pool, token)
	firing.AreaID = declared.ID
	// A release being replaced is what makes a control possible at all; without
	// one the pick is bounded by the first release before anything else is read.
	firing.ReplacesReleaseID = "rel_000000000000000000000000000000b"
	opened, err := g.Fire(ctx, firing)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if opened.Strategy.Why != gate.WhyIrreversible {
		t.Errorf("the pick's bound reads %q, want the irreversible area's", opened.Strategy.Why)
	}
}
