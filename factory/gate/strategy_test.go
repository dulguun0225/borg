// The rollout strategy the production deploy row picks, read off the open
// event: which bound applied, where more than one could have.
package gate_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/score"
)

// TestTheGatesReasonsMirrorTheScoresAll: package gate copies the score's
// reasons onto the open event, so a reason the score names and this package
// does not would be stored under a word nothing here declares.
func TestTheGatesReasonsMirrorTheScoresAll(t *testing.T) {
	if !slices.Equal(gate.Whys, score.Whys) {
		t.Errorf("the gate names %q and the score names %q", gate.Whys, score.Whys)
	}
}

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

// TestAnIrreversibleAreaOnAPlatformServingNoSharePutsAHumanAtTheRow: where the
// platform serves no share there is no schedule to pick and every deploy there
// goes without a control, so an irreversible area's deploy to production is a
// human's whatever the formula returns.
func TestAnIrreversibleAreaOnAPlatformServingNoSharePutsAHumanAtTheRow(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.1)}, &fakePolicy{applied: applied(0.9)}
	ctx, pool, token, g := newGate(t, s, p)

	declared, err := area.NewWriter(pool, token).Declare(ctx, owner, "ledger",
		area.Inside{ProjectID: "prj_00000000000000000000000000000a"},
		area.Hazard{
			Grade: area.GradeIrreversible, Operation: "the ledger write",
			Bound: 10, BoundPeriodSeconds: 3600,
		})
	if err != nil {
		t.Fatalf("declaring the irreversible area: %v", err)
	}

	// The same firing on a platform that serves a share auto-passes: the number
	// is under the threshold and nothing else marks the row.
	shared := deployFiring(t, ctx, pool, token)
	shared.AreaID = declared.ID
	shared.ReplacesReleaseID = "rel_000000000000000000000000000000b"
	opened, err := g.Fire(ctx, shared)
	if err != nil {
		t.Fatalf("Fire on a platform that serves a share: %v", err)
	}
	if opened.HumanDecides {
		t.Fatalf("a controllable deploy under the threshold put a human at the row: %v", opened.Marks)
	}

	unshared := shared
	unshared.ItemID = "it_0000000000000000000000000000000b"
	unshared.EnvironmentID = unsharedEnvironment(t, ctx, pool, token)
	approvedAbove(t, ctx, pool, token, gate.MergeToMaster, unshared.ItemID)
	opened, err = g.Fire(ctx, unshared)
	if err != nil {
		t.Fatalf("Fire on a platform that serves no share: %v", err)
	}
	if !opened.IrreversibleWithoutAControl {
		t.Error("the open event does not say the area is irreversible and no control can run beside it")
	}
	if !opened.HumanDecides {
		t.Errorf("an irreversible deploy no control can run beside auto-passed: marks %v", opened.Marks)
	}
	if _, err := g.AutoPass(ctx, opened); !errors.Is(err, gate.ErrHumanDecides) {
		t.Errorf("AutoPass = %v, want ErrHumanDecides", err)
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
