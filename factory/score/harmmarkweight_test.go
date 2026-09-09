// The harm mark's weight, held at nothing through every refit. It is a second
// file beside harmmark_test.go because the two reach different things: this
// one reaches [Fit] and the evidence it is computed over, which are this
// package's own, and that one needs a database and so is in score_test, where
// nothing unexported is reachable.
package score

import (
	"fmt"
	"testing"

	"github.com/dulguun0225/borg/factory/window"
)

// TestTheHarmMarkIsNeverWeighedHoweverWellItSeparates: the mark adds no gate a
// report did not already meet, so its weight is nothing and stays nothing — a
// weight above it would be the gate, a marked report raising the number and the
// number being what decides whether a human is asked.
//
// The evidence here is the fit's best case for it: the mark reads one on every
// held-out release whose window failed and nothing on every one that passed, so
// it separates perfectly and a fit that weighed it would give it the whole
// share of its term. The other factor of that term separates nothing, so
// weighing the mark is the only thing the fit could do with this population —
// and it does not.
func TestTheHarmMarkIsNeverWeighedHoweverWellItSeparates(t *testing.T) {
	var firings []Firing
	var exits []window.Exit
	for i := range fitEvidence {
		failed := i%2 == 0
		mark, hazard := 0.0, 0.5
		if failed {
			mark = 1.0
		}
		firings = append(firings, heldOutFiring(fmt.Sprintf("it_%d", i), SetAboveABuild, 0.5, "model-1", []Factor{
			{Name: contextHarmMarkedReport.name, Term: TermImpact, Level: mark},
			{Name: contextHazardSeverity.name, Term: TermImpact, Level: hazard},
		}))
		if failed {
			exits = append(exits, window.ExitFailed)
		} else {
			exits = append(exits, window.ExitPassed)
		}
	}
	fitted := Fit(heldOutEvidence(firings, exits))[SetAboveABuild]

	if got := fitted.Of(contextHarmMarkedReport.name); got != 0 {
		t.Errorf("a refit gave the harm mark a weight of %v, and it adds no gate a report did not already meet", got)
	}
	// The mark took no share and added nothing to the total the shares come out
	// of, so the term was fitted as though it were not there: nothing else
	// separated either, and the term keeps what the product shipped.
	if got := fitted.Of(contextHazardSeverity.name); !near(got, ShippedWeights(SetAboveABuild).Of(contextHazardSeverity.name)) {
		t.Errorf("the term's other factor was fitted to %v, want what the product shipped", got)
	}

	// Away from Spec the mark is a level on the vector and not a resolution, so
	// what keeps it from moving the number is the weight alone. At nothing it
	// moves it by nothing, however high the level reads.
	vector := []Factor{
		{Name: contextHazardSeverity.name, Group: GroupContext, Term: TermImpact, Weight: 0.3, Level: 0.5},
		{Name: contextHarmMarkedReport.name, Group: GroupContext, Term: TermImpact,
			Weight: fitted.Of(contextHarmMarkedReport.name), Level: 1.0},
	}
	_, marked, _, _ := reduce(vector, Scale{})
	_, unmarked, _, _ := reduce(vector[:1], Scale{})
	if !near(marked, unmarked) {
		t.Errorf("a marked report moved the impact from %v to %v, and the mark moves the number by nothing",
			unmarked, marked)
	}
}

// TestOnlyTheHarmMarkIsNeverWeighed: the withdrawal ships at nothing too and is
// not held there — its own comment says a recalibration fits it from the
// held-out decisions like every other, so what it ships at is a starting value
// and not a bound. Naming the list here is what keeps a later factor from being
// added to it without the design saying so.
func TestOnlyTheHarmMarkIsNeverWeighed(t *testing.T) {
	if len(neverWeighed) != 1 || !neverWeighed[contextHarmMarkedReport.name] {
		t.Errorf("the factors a refit never weighs are %v, want the harm mark alone", neverWeighed)
	}
	if neverWeighed[contextProtectionWithdrawn.name] {
		t.Error("the withdrawal is held at its shipped weight, and a recalibration fits it like every other")
	}
}
