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
// a gate its own number never decided. Neither is selected, so both are asked of
// [Score.decide] over a log that said nothing.
func TestTheSampleRefusesASafeguardAndAResolvedVector(t *testing.T) {
	s := &Score{draw: alwaysDraw{}, marks: NoMarks{}}

	if selection := s.decide(1, true, false, true); selection.HeldOut {
		t.Error("the sample selected a firing a safeguard or a resolved factor put a human at")
	}

	// A rate of nothing selects nothing, which is what a safeguard clamping the
	// rate to zero on a service leaves.
	if selection := s.decide(0, true, false, false); selection.HeldOut {
		t.Error("the sample selected at a rate of nothing")
	}
}

// TestASelectedItemKeepsItsSelectionAtAResolvedFiring: held out is written on
// every decision on the item from the selection onward, so a firing that
// resolved a factor or met a safeguard carries the selection forward and
// withholds the auto-pass alone. Losing it there would take the control off the
// held-out release's production deploy, which is what the sample exists to
// produce evidence with.
func TestASelectedItemKeepsItsSelectionAtAResolvedFiring(t *testing.T) {
	s := &Score{draw: alwaysDraw{}, marks: NoMarks{}}

	for _, humanStays := range []bool{true, false} {
		selection := s.decide(0.5, true, true, humanStays)
		if !selection.HeldOut || selection.Why != SelectedEarlier {
			t.Fatalf("an item selected earlier stopped being selected with humanStays %v: %+v",
				humanStays, selection)
		}
		if selection.AutoPasses == humanStays {
			t.Errorf("the selection auto-passes %v with humanStays %v", selection.AutoPasses, humanStays)
		}
	}
}

// TestTheSelectionCarriesTheRateInForce: a held-out outcome is weighted by the
// probability that selected it, so the decision carries the rate at the
// selection and not the rate a later reader would compute.
func TestTheSelectionCarriesTheRateInForce(t *testing.T) {
	s := &Score{draw: alwaysDraw{}, marks: NoMarks{}}
	selection := s.decide(0.25, true, false, false)
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

	if selection := s.decide(0.9, true, false, false); !selection.HeldOut {
		t.Errorf("a rate of 0.9 selected nothing at a draw of 0.5: %+v", selection)
	}
	if selection := s.decide(0.1, true, false, false); selection.HeldOut {
		t.Errorf("a rate of 0.1 selected at a draw of 0.5: %+v", selection)
	}
	if selection := s.decide(0.9, false, false, false); selection.HeldOut {
		t.Errorf("the sample selected a firing the score would have passed anyway: %+v", selection)
	}
	if selection := s.decide(0.1, false, true, false); !selection.HeldOut || selection.Why != SelectedEarlier {
		t.Errorf("an item selected at an earlier gate stopped being selected: %+v", selection)
	}
}

type halfDraw struct{}

func (halfDraw) Fraction() float64 { return 0.5 }
