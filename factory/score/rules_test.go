// The published rules against the code that applies them. learn_test.go holds
// the numbers; this holds the one condition the text has to state in words,
// because a rule an owner argues with that omits a condition the code applies is
// a published rule the factory does not follow.
package score

import (
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/window"
)

// TestThePublishedRuleStatesWhatStopsTheThresholdsRise: the code stops the rise
// on a held-out firing at that row whose release reached a window that closed
// failed, and says so in the reason it writes per decision. The published text
// is what an owner disagreeing with a moved value argues with, so it states the
// same condition.
func TestThePublishedRuleStatesWhatStopsTheThresholdsRise(t *testing.T) {
	start, _ := Starting(gatepolicy.RiskThreshold)
	const row = "merge_to_master"

	held := []Firing{
		autoPassed(row, 0.9, AutoPassSample, "it_a"),
		autoPassed(row, 0.9, AutoPassSample, "it_b"),
		autoPassed(row, 0.9, AutoPassSample, "it_c"),
	}
	rose := firingEvidence(t, held)
	if got := valueOf(t, rose, gatepolicy.RiskThreshold, row); !near(got, start.Value+thresholdBand) {
		t.Fatalf("three held-out releases whose windows passed supply %v, want %v", got, start.Value+thresholdBand)
	}
	stopped := fail(firingEvidence(t, append(append([]Firing{}, held...),
		autoPassed(row, 0.9, AutoPassSample, "it_d"))), "it_d")
	if got := valueOf(t, stopped, gatepolicy.RiskThreshold, row); got != start.Value {
		t.Fatalf("a held-out release that failed at the row left %v, want the starting %v", got, start.Value)
	}

	for _, stated := range []string{
		"no held-out",
		"closed failed",
		"stops the rise",
	} {
		if !strings.Contains(Rules, stated) {
			t.Errorf("the published rules do not state %q, and the code stops the rise on it", stated)
		}
	}
}

// TestTheRiseIsStillOnThePassedWindowsAlone: the condition the text now states
// is the one the code applies and no wider — a held-out release whose window
// timed out neither raises the threshold nor stops the rise, the exit that
// reports no outcome reporting none either way.
func TestTheRiseIsStillOnThePassedWindowsAlone(t *testing.T) {
	start, _ := Starting(gatepolicy.RiskThreshold)
	const row = "merge_to_master"

	held := []Firing{
		autoPassed(row, 0.9, AutoPassSample, "it_a"),
		autoPassed(row, 0.9, AutoPassSample, "it_b"),
		autoPassed(row, 0.9, AutoPassSample, "it_c"),
		autoPassed(row, 0.9, AutoPassSample, "it_d"),
	}
	e := exitOf(firingEvidence(t, held), "it_d", window.ExitTimedOut)
	if got := valueOf(t, e, gatepolicy.RiskThreshold, row); !near(got, start.Value+thresholdBand) {
		t.Errorf("a fourth held-out release whose window timed out leaves %v, want the one band the other three raised", got)
	}
}
