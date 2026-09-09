// The per-author prior's restart: what a truncation of the log that removes
// every held-out decision behind a drifted prior does, split out at the
// length a file is held to rather than grown onto learn_test.go, which already
// stood over it.
package score

import (
	"testing"

	"github.com/dulguun0225/borg/factory/window"
)

// TestADriftedAuthorWithNoSurvivingHeldOutDecisionsRestarts: a truncation of
// the log that removes every held-out decision behind a drifted prior removes
// what a recalibration would have read, and the prior restarts as an unseen
// author's rather than standing drifted on evidence that can no longer
// arrive. Where some of it survives, nothing restarts: a recalibration is
// what reads what is left.
func TestADriftedAuthorWithNoSurvivingHeldOutDecisionsRestarts(t *testing.T) {
	prior := func(level float64) []Factor {
		return []Factor{{Name: authorPrior.name, Term: TermLikelihood, Level: level}}
	}
	drifted := heldOutEvidence([]Firing{
		heldOutFiring("it_a", SetWithABuild, 0.2, "model-1", prior(0.1)),
		heldOutFiring("it_b", SetWithABuild, 0.2, "model-1", prior(0.1)),
		heldOutFiring("it_c", SetWithABuild, 0.2, "model-1", prior(0.1)),
		heldOutFiring("it_d", SetWithABuild, 0.2, "model-1", prior(0.9)),
		heldOutFiring("it_e", SetWithABuild, 0.2, "model-1", prior(0.9)),
		heldOutFiring("it_f", SetWithABuild, 0.2, "model-1", prior(0.9)),
	}, []window.Exit{
		window.ExitFailed, window.ExitFailed, window.ExitFailed,
		window.ExitPassed, window.ExitPassed, window.ExitPassed,
	})
	if found := drifted.drift(""); len(found) != 1 || found[0].Author != "model-1" {
		t.Fatalf("the pass found %+v, want the prior on model-1 drifted", found)
	}
	under := Under{DriftedPriors: []string{"model-1"}}

	// Every held-out decision behind the finding is gone, which is what a
	// truncation of the log leaves.
	truncated := heldOutEvidence(nil, nil)
	restarts := truncated.restartedPriors(under)
	if restarts["model-1"] == "" {
		t.Fatalf("a drifted author with no surviving held-out decisions did not restart: %v", restarts)
	}
	if found := truncated.drift(""); len(found) != 0 {
		t.Errorf("the truncated author reads drifted again by the same pass: %+v, and a restart is a second exit from a drift a recalibration is not the only one of", found)
	}

	// Carried forward: what the version below already restarted stays as it
	// was, an author restarted once staying restarted.
	already := Under{DriftedPriors: []string{"model-1"}, PriorRestarts: map[string]string{"model-1": "2026-01-01T00:00:00.000000000Z"}}
	if again := truncated.restartedPriors(already); again["model-1"] != "2026-01-01T00:00:00.000000000Z" {
		t.Errorf("a restart already on record reads %v, want it carried forward unchanged", again)
	}

	// Where some held-out decisions on the author survive the cut, nothing
	// restarts.
	partial := heldOutEvidence([]Firing{
		heldOutFiring("it_d", SetWithABuild, 0.2, "model-1", prior(0.9)),
	}, []window.Exit{window.ExitPassed})
	if restarts := partial.restartedPriors(under); restarts["model-1"] != "" {
		t.Errorf("an author with a surviving held-out decision restarted at %q, want nothing: recalibration is what reads it", restarts["model-1"])
	}
}
