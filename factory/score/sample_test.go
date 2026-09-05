package score

import (
	"testing"
)

// alwaysDraw selects at every rate above nothing, which is what makes the
// refusals below readable: whatever the draw says, the rules above it decide.
type alwaysDraw struct{}

func (alwaysDraw) Fraction() float64 { return 0 }

// TestTheSampleRefusesASafeguardAndAResolvedVector: a human a safeguard added at
// a gate is a human an owner added, and the score is in no position to auto-pass
// a gate its own number never decided. Both refusals return before the log is
// read, which is what lets this test hold no pool.
func TestTheSampleRefusesASafeguardAndAResolvedVector(t *testing.T) {
	s := &Score{draw: alwaysDraw{}, marks: NoMarks{}}
	ctx := t.Context()

	selection, err := s.HoldOut(ctx, "it_a", 1, true, true, nil)
	if err != nil {
		t.Fatalf("HoldOut past a safeguard: %v", err)
	}
	if selection.HeldOut {
		t.Error("the sample selected an item a safeguard put a human at")
	}

	resolved := []Resolution{{Factor: "context.hazard_severity", Cause: CauseIrreversibleHazard, Why: "irreversible"}}
	selection, err = s.HoldOut(ctx, "it_a", 1, true, false, resolved)
	if err != nil {
		t.Fatalf("HoldOut past a resolution: %v", err)
	}
	if selection.HeldOut {
		t.Error("the sample selected a firing whose vector resolved a factor")
	}

	// A rate of nothing selects nothing, which is what a safeguard clamping the
	// rate to zero on a service leaves. This is the rule [Score.decide] holds
	// once the log has said the item was not selected earlier, so it is asked of
	// decide directly and holds no pool either.
	if selection := s.decide(0, true, false); selection.HeldOut {
		t.Error("the sample selected at a rate of nothing")
	}
}

// TestTheSelectionCarriesTheRateInForce: a held-out outcome is weighted by the
// probability that selected it, so the decision carries the rate at the
// selection and not the rate a later reader would compute.
func TestTheSelectionCarriesTheRateInForce(t *testing.T) {
	s := &Score{draw: alwaysDraw{}, marks: NoMarks{}}
	selection := s.decide(0.25, true, false)
	if !selection.HeldOut || selection.Why != SelectedHere {
		t.Fatalf("the draw selected nothing: %+v", selection)
	}
	if selection.RateInForce != 0.25 {
		t.Errorf("the selection carries a rate of %v, want the 0.25 in force at the selection", selection.RateInForce)
	}
}

// TestTheSampleDrawsAgainstTheRateInForceAndNotAConstant: the rate is authored
// with the rest of gate policy, so a factory whose owner narrowed it selects
// less and one that authored nothing selects at what the score supplies.
func TestTheSampleDrawsAgainstTheRateInForceAndNotAConstant(t *testing.T) {
	s := &Score{draw: halfDraw{}, marks: NoMarks{}}

	if selection := s.decide(0.9, true, false); !selection.HeldOut {
		t.Errorf("a rate of 0.9 selected nothing at a draw of 0.5: %+v", selection)
	}
	if selection := s.decide(0.1, true, false); selection.HeldOut {
		t.Errorf("a rate of 0.1 selected at a draw of 0.5: %+v", selection)
	}
	if selection := s.decide(0.9, false, false); selection.HeldOut {
		t.Errorf("the sample selected a firing the score would have passed anyway: %+v", selection)
	}
	if selection := s.decide(0.1, false, true); !selection.HeldOut || selection.Why != SelectedEarlier {
		t.Errorf("an item selected at an earlier gate stopped being selected: %+v", selection)
	}
}

type halfDraw struct{}

func (halfDraw) Fraction() float64 { return 0.5 }
