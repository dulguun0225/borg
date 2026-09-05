package score

import (
	"strconv"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/gatepolicy"
)

// TestFormulaStatesEveryBreakpoint is what keeps the published formula and the
// source from drifting apart. The formula is published so a human can disagree
// with it, and a published formula that has stopped matching the arithmetic is
// worse than none — so every breakpoint in the source is searched for in the
// text.
func TestFormulaStatesEveryBreakpoint(t *testing.T) {
	sets := []struct {
		name        string
		breakpoints []breakpoint
	}{
		{"change.size", sizeBreakpoints},
		{"change.reach", reachBreakpoints},
		{"change.area_churn", churnBreakpoints},
		{"change.test_coverage", criteriaBreakpoints},
		{"context.consumers", consumersBreakpoints},
	}
	for _, set := range sets {
		if !strings.Contains(Formula, set.name) {
			t.Errorf("the formula does not name %s", set.name)
		}
		for _, b := range set.breakpoints {
			for _, number := range []string{trim(b.upTo), trim(b.level)} {
				if !strings.Contains(Formula, number) {
					t.Errorf("the formula does not state %s of %s", number, set.name)
				}
			}
		}
	}
	for _, stated := range []string{
		"1 - 0.5 x (1 - change.reversibility)",
		"0.4 + 0.6 x likelihood",
	} {
		if !strings.Contains(Formula, stated) {
			t.Errorf("the formula does not state the last step's %q", stated)
		}
	}
	if reversibilityDiscount != 0.5 || likelihoodFloor != 0.4 {
		t.Errorf("the last step's constants are %v and %v, and the formula's text states 0.5 and 0.4",
			reversibilityDiscount, likelihoodFloor)
	}
}

// trim writes a number the way the formula's text writes it: the shortest form
// that reads back as the same value, with the top of the scale written as 1.0
// because that is how the text writes a level.
func trim(number float64) string {
	if number == 1 {
		return "1.0"
	}
	return strconv.FormatFloat(number, 'g', -1, 64)
}

// TestTheFactorSetIsWhatTheVersionPublishes: every factor, its group, its half,
// its weight, and what it reads — because a version names what a human reads.
func TestTheFactorSetIsWhatTheVersionPublishes(t *testing.T) {
	set := FactorSet()
	for _, d := range definitions {
		if !strings.Contains(set, d.name) {
			t.Errorf("the factor set does not name %s", d.name)
		}
		if !strings.Contains(set, d.reads) {
			t.Errorf("the factor set does not say what %s reads", d.name)
		}
		if d.read == nil {
			t.Errorf("%s is published and nothing computes it", d.name)
		}
	}
	if got := strings.Count(set, "\n"); got != len(definitions) {
		t.Errorf("the factor set has %d lines and there are %d factors", got, len(definitions))
	}
}

// TestTheWeightsSumToOneWithinEachHalf: the weights are the authored formula's,
// and a half whose weights did not sum to one would make its mean read as
// something other than a level.
func TestTheWeightsSumToOneWithinEachHalf(t *testing.T) {
	halves := map[Half]float64{}
	for _, d := range definitions {
		halves[d.half] += d.weight
	}
	for _, half := range []Half{HalfLikelihood, HalfImpact, HalfReversibility} {
		if weight := halves[half]; weight < 0.999 || weight > 1.001 {
			t.Errorf("the %s half's weights sum to %v, want 1", half, weight)
		}
	}
	if len(halves) != 3 {
		t.Errorf("the factors fall into %d halves, want likelihood, impact, and reversibility", len(halves))
	}
}

// TestEveryFactorSitsInAGroupTheDesignNames: three groups and no fourth.
func TestEveryFactorSitsInAGroupTheDesignNames(t *testing.T) {
	for _, d := range definitions {
		switch d.group {
		case GroupChange, GroupAuthor, GroupContext:
		default:
			t.Errorf("%s is in group %q, and the design names three", d.name, d.group)
		}
		if !strings.HasPrefix(d.name, string(d.group)+".") {
			t.Errorf("%s does not name its group %q", d.name, d.group)
		}
	}
}

// TestSuppliedCoversTenRowsAndNotTheCatalog: the score supplies a value for ten
// of gate policy's eleven rows and none for the list of allowed predicate kinds,
// which no outcome teaches.
func TestSuppliedCoversTenRowsAndNotTheCatalog(t *testing.T) {
	rows := map[string]bool{}
	for _, d := range gatepolicy.Definitions {
		value, supplied := Starting(d.Parameter)
		if d.Parameter == gatepolicy.AllowedPredicateKinds {
			if supplied {
				t.Errorf("the score supplies a list of allowed predicate kinds: %v", value)
			}
			continue
		}
		if !supplied {
			t.Errorf("the score supplies no value for %s", d.Parameter)
			continue
		}
		rows[d.Row] = true
		if !strings.Contains(StartingValues().Text(), string(d.Parameter)) {
			t.Errorf("the supplied text does not name %s", d.Parameter)
		}
	}
	if len(rows) != 10 {
		t.Errorf("the score supplies values across %d rows, want ten of the eleven", len(rows))
	}
	// Every supplied value publishes its reason: a default nobody chose is still
	// a decision, and it can stay invisible until it takes effect.
	for _, s := range starting {
		if s.Why == "" {
			t.Errorf("the supplied value for %s carries no reason", s.Parameter)
		}
	}
}

// TestTheSuppliedThresholdGatesAFirstReleaseAndNotTheNextOne is the calibration
// stated as arithmetic. The two vectors are what a fresh install produces for its
// first item and for the one after it, and the supplied threshold sits between
// them — which is what makes the milestone's demonstration reachable at all.
func TestTheSuppliedThresholdGatesAFirstReleaseAndNotTheNextOne(t *testing.T) {
	start, supplied := Starting(gatepolicy.RiskThreshold)
	if !supplied {
		t.Fatal("the score supplies no risk threshold")
	}
	threshold := start.Value

	first := []Factor{
		{Name: "change.size", Half: HalfLikelihood, Weight: 0.30, Level: 0.1},
		{Name: "change.area_churn", Half: HalfLikelihood, Weight: 0.20, Level: 0.0},
		{Name: "change.test_coverage", Half: HalfLikelihood, Weight: 0.20, Level: 0.5},
		{Name: "author.prior", Half: HalfLikelihood, Weight: 0.30, Level: 1.0},
		{Name: "change.reach", Half: HalfImpact, Weight: 0.50, Level: 1.0},
		{Name: "context.business_area", Half: HalfImpact, Weight: 0.30, Level: 1.0},
		{Name: "context.consumers", Half: HalfImpact, Weight: 0.20, Level: 0.0},
		{Name: "change.reversibility", Half: HalfReversibility, Weight: 1.00, Level: 1.0},
	}
	_, _, _, number := reduce(first)
	if number < threshold {
		t.Errorf("a service's first release reads %v against a threshold of %v, and a human is meant to decide it",
			number, threshold)
	}

	next := []Factor{
		{Name: "change.size", Half: HalfLikelihood, Weight: 0.30, Level: 0.1},
		{Name: "change.area_churn", Half: HalfLikelihood, Weight: 0.20, Level: 0.2},
		{Name: "change.test_coverage", Half: HalfLikelihood, Weight: 0.20, Level: 0.5},
		{Name: "author.prior", Half: HalfLikelihood, Weight: 0.30, Level: 0.5},
		{Name: "change.reach", Half: HalfImpact, Weight: 0.50, Level: 0.6},
		{Name: "context.business_area", Half: HalfImpact, Weight: 0.30, Level: 0.5},
		{Name: "context.consumers", Half: HalfImpact, Weight: 0.20, Level: 0.0},
		{Name: "change.reversibility", Half: HalfReversibility, Weight: 1.00, Level: 0.3},
	}
	_, _, _, number = reduce(next)
	if number >= threshold {
		t.Errorf("the item after it reads %v against a threshold of %v, and nobody is meant to decide it",
			number, threshold)
	}
}

// TestAnUnlikelyCatastropheIsStillGated: impact bounds the number and likelihood
// scales it inside that bound, never down to nothing — which is the design's
// requirement that unlikely but catastrophic is gated regardless.
func TestAnUnlikelyCatastropheIsStillGated(t *testing.T) {
	catastrophe := []Factor{
		{Half: HalfLikelihood, Weight: 1, Level: 0},
		{Half: HalfImpact, Weight: 1, Level: 1},
		{Half: HalfReversibility, Weight: 1, Level: 1},
	}
	likelihood, impact, exposure, number := reduce(catastrophe)
	if likelihood != 0 || impact != 1 || exposure != 1 {
		t.Fatalf("the halves read %v, %v, %v, want 0, 1, 1", likelihood, impact, exposure)
	}
	if number != likelihoodFloor {
		t.Errorf("the number is %v, want the likelihood floor %v", number, likelihoodFloor)
	}
	start, _ := Starting(gatepolicy.RiskThreshold)
	threshold := start.Value
	if number < threshold {
		t.Errorf("a catastrophe nothing is likely to have got wrong reads %v, under the supplied threshold %v",
			number, threshold)
	}
}

// TestReversibilityDiscountsImpactAndDoesNotJoinAHalf: the rollout strategy reads
// impact against reversibility, so reading them separately is what keeps the two
// from weakening together.
func TestReversibilityDiscountsImpactAndDoesNotJoinAHalf(t *testing.T) {
	irreversible := []Factor{
		{Half: HalfLikelihood, Weight: 1, Level: 1},
		{Half: HalfImpact, Weight: 1, Level: 0.8},
		{Half: HalfReversibility, Weight: 1, Level: 1},
	}
	_, impact, exposure, irreversibleNumber := reduce(irreversible)
	if impact != 0.8 || exposure != 0.8 {
		t.Fatalf("an irreversible change's impact and exposure are %v and %v, want both 0.8", impact, exposure)
	}

	reversible := irreversible
	reversible[2] = Factor{Half: HalfReversibility, Weight: 1, Level: 0}
	_, impact, exposure, reversibleNumber := reduce(reversible)
	if impact != 0.8 {
		t.Errorf("reversibility moved the impact half to %v, and it discounts rather than joining it", impact)
	}
	if exposure != 0.8*(1-reversibilityDiscount) {
		t.Errorf("a fully reversible change's exposure is %v, want %v", exposure, 0.8*(1-reversibilityDiscount))
	}
	if reversibleNumber >= irreversibleNumber {
		t.Errorf("a change that is cheap to undo reads %v against %v for one that is not",
			reversibleNumber, irreversibleNumber)
	}
}

// TestAnUnavailableFactorReducesToTheTop: a factor the score could not compute
// puts the number at the top of the scale, which is at or above every threshold
// an owner may author. The direction is the one thing that must not depend on a
// component being up.
func TestAnUnavailableFactorReducesToTheTop(t *testing.T) {
	vector := []Factor{
		{Name: "change.size", Half: HalfLikelihood, Weight: 1, Level: 1, Unavailable: "the diff could not be taken"},
		{Name: "change.reach", Half: HalfImpact, Weight: 1, Level: 0.1},
		{Name: "change.reversibility", Half: HalfReversibility, Weight: 1, Level: 0},
	}
	_, _, _, number := reduce(vector)
	if number != 1 {
		t.Errorf("a vector with an unavailable factor reduces to %v, want 1", number)
	}

	// Without the unavailable reason the same levels read low, which is the whole
	// point: what gates the change is the factor being uncomputed and not its
	// level.
	vector[0].Unavailable = ""
	if _, _, _, number = reduce(vector); number >= 1 {
		t.Errorf("the same levels with nothing unavailable reduce to %v", number)
	}
}

// TestEvidenceStartsWideAndNarrows: an author or an area the factory has not seen
// is at the top of the scale and narrows as human verdicts arrive, and a
// rejection counts against rather than merely failing to count for.
func TestEvidenceStartsWideAndNarrows(t *testing.T) {
	unseen := evidenceLevel(0, 0)
	if unseen != 1 {
		t.Errorf("an unseen author reads %v, want the top of the scale", unseen)
	}
	one := evidenceLevel(1, 0)
	four := evidenceLevel(4, 0)
	if !(one < unseen && four < one) {
		t.Errorf("the level does not narrow with evidence: %v, %v, %v", unseen, one, four)
	}
	if rejected := evidenceLevel(1, 1); rejected <= one {
		t.Errorf("a rejection reads %v, no worse than an author with one approval and no rejection %v", rejected, one)
	}

	// An author whose work has only ever been rejected reads the same as one the
	// factory has never seen: the scale has a top and both are at it. That is a
	// limit this test records rather than a defect it found — the level says how
	// far the score trusts an author and not why, and what tells the two apart is
	// the decisions themselves.
	if evidenceLevel(0, 3) != unseen {
		t.Errorf("an author with only rejections reads %v against an unseen author's %v",
			evidenceLevel(0, 3), unseen)
	}
}
