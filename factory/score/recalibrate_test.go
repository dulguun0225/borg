package score

import (
	"reflect"
	"testing"
)

// TestARecalibrationLeavesEveryOtherFieldEqual: a recalibration writes a
// version differing in the weights and in nothing else — the scale fitted
// with them, the branch, the drift findings it ends and the point it read to
// are what change; every other field of the version below it survives
// untouched.
func TestARecalibrationLeavesEveryOtherFieldEqual(t *testing.T) {
	below := Version{
		ID:                    "dl_below",
		FormulaVersion:        FormulaVersion,
		Formula:               Formula,
		Weights:               ShippedWeightsBySet(),
		FactorSets:            FactorSetsText(ShippedWeightsBySet()),
		ControlBound:          ShippedControlBound,
		Rules:                 Rules,
		LearningVersion:       LearningVersion,
		BandWidth:             ShippedBandWidth,
		ShippedPriors:         map[string]float64{"model-1": 0.4},
		Scale:                 map[FactorSet]Scale{SetWithABuild: {Slope: 1, Intercept: 0}},
		RecalibratedThrough:   "2026-01-01T00:00:00.000000000Z",
		PriorRestarts:         map[string]string{"model-2": "2026-02-01T00:00:00.000000000Z"},
		Supplied:              StartingValues(),
		Bands:                 []Band{{FactorSet: SetWithABuild, Service: "svc_a", From: 0, To: 0.1}},
		Drift:                 []Drift{{Factor: "change.size", Why: "drifted"}},
		FalseAlarms:           []FalseAlarm{{Human: "person:reviewer", Count: 1}},
		Branch:                BranchSupplied,
		ShippedBundleIdentity: "",
		Supersedes:            "dl_earlier",
	}

	fitted := map[FactorSet]Weights{SetWithABuild: {"change.size": 1}}
	scaled := map[FactorSet]Scale{SetWithABuild: {Slope: 2, Intercept: 0.5}}
	next := recalibrated(below, true, fitted, scaled, "2026-03-01T00:00:00.000000000Z")

	// What moves: the weights, the scale fitted with them, the text they
	// produce, the branch, the drift a recalibration ends, and the point it
	// read to.
	if !reflect.DeepEqual(next.Weights, fitted) {
		t.Errorf("Weights = %+v, want %+v", next.Weights, fitted)
	}
	if !reflect.DeepEqual(next.Scale, scaled) {
		t.Errorf("Scale = %+v, want %+v: it is fitted with the weights and moves with them", next.Scale, scaled)
	}
	if want := FactorSetsText(fitted); next.FactorSets != want {
		t.Errorf("FactorSets = %q, want %q", next.FactorSets, want)
	}
	if next.Branch != BranchRecalibration {
		t.Errorf("Branch = %q, want %q", next.Branch, BranchRecalibration)
	}
	if next.Drift != nil {
		t.Errorf("Drift = %+v, want nil: a recalibration is the exit every drift under the version below takes", next.Drift)
	}
	if next.RecalibratedThrough != "2026-03-01T00:00:00.000000000Z" {
		t.Errorf("RecalibratedThrough = %q, want the point this pass read to", next.RecalibratedThrough)
	}

	// What does not: everything else, field for field against the version
	// below.
	if next.FormulaVersion != below.FormulaVersion {
		t.Error("FormulaVersion moved")
	}
	if next.Formula != below.Formula {
		t.Error("Formula moved")
	}
	if next.ControlBound != below.ControlBound {
		t.Error("ControlBound moved")
	}
	if next.Rules != below.Rules {
		t.Error("Rules moved")
	}
	if next.LearningVersion != below.LearningVersion {
		t.Error("LearningVersion moved")
	}
	if next.BandWidth != below.BandWidth {
		t.Error("BandWidth moved")
	}
	if !reflect.DeepEqual(next.ShippedPriors, below.ShippedPriors) {
		t.Error("ShippedPriors moved")
	}
	if !reflect.DeepEqual(next.PriorRestarts, below.PriorRestarts) {
		t.Error("PriorRestarts moved, and only a restart or a further pass carries it forward")
	}
	if !reflect.DeepEqual(next.Supplied, below.Supplied) {
		t.Error("Supplied moved, and a recalibration reads none of the outcomes an ordinary pass does")
	}
	if !reflect.DeepEqual(next.Bands, below.Bands) {
		t.Error("Bands moved")
	}
	if !reflect.DeepEqual(next.FalseAlarms, below.FalseAlarms) {
		t.Error("FalseAlarms moved")
	}
	if next.ShippedBundleIdentity != below.ShippedBundleIdentity {
		t.Error("ShippedBundleIdentity moved")
	}

	// The not-found case is the one exception the design states: with no
	// version below it, a recalibration still has to publish a starting table.
	first := recalibrated(Version{}, false, fitted, scaled, "2026-03-01T00:00:00.000000000Z")
	if !reflect.DeepEqual(first.Supplied, StartingValues()) {
		t.Errorf("a recalibration with nothing below it supplies %+v, want the starting values", first.Supplied)
	}
}
