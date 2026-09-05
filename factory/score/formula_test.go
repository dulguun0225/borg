package score

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/gatepolicy"
)

// TestFormulaStatesEveryBreakpoint holds the published text and the source
// together: a formula an owner reads that does not state the number the code
// applies is worse than no published formula at all.
func TestFormulaStatesEveryBreakpoint(t *testing.T) {
	for _, breakpoints := range [][]breakpoint{
		sizeBreakpoints, requirementsBreakpoints, reachBreakpoints,
		churnBreakpoints, consumersBreakpoints, exposureBreakpoints,
	} {
		for _, b := range breakpoints {
			if !strings.Contains(Formula, fmt.Sprintf("%v", b.upTo)) {
				t.Errorf("the published formula does not state the breakpoint at %v", b.upTo)
			}
		}
	}
	for _, stated := range []string{"0.4 + 0.6 x likelihood", "1 - 0.5 x (1 - change.reversibility)", "discountedImpact"} {
		if !strings.Contains(Formula, stated) {
			t.Errorf("the published formula does not state %q", stated)
		}
	}
}

// TestTestCoverageIsNoFactor: the design refuses it by name, because it would be
// computed from the encodings the scored agent authored in the same stage as the
// code, so the scored author would set the factor.
func TestTestCoverageIsNoFactor(t *testing.T) {
	if strings.Contains(Formula, "test_coverage") || strings.Contains(FactorSetsText(ShippedWeightsBySet()), "test_coverage") {
		t.Error("the published formula or a factor set names test coverage, which the design refuses by name")
	}
	for _, set := range FactorSets {
		for _, d := range definitionsOf(set) {
			if strings.Contains(d.name, "coverage") {
				t.Errorf("%s weighs %s", set, d.name)
			}
		}
	}
}

// TestTheFourGroupsAndWhereExposureIsInapplicable: the four rows below a build
// weigh four groups; the four above weigh three, exposure being inapplicable
// there rather than unavailable — treating a factor the gate was never going to
// have as missing would put a human at every one of those gates forever.
func TestTheFourGroupsAndWhereExposureIsInapplicable(t *testing.T) {
	groups := map[Group]bool{}
	for _, d := range definitionsOf(SetWithABuild) {
		groups[d.group] = true
	}
	for _, want := range []Group{GroupChange, GroupAuthor, GroupExposure, GroupContext} {
		if !groups[want] {
			t.Errorf("the set with a build weighs no %s factor", want)
		}
	}
	for _, set := range []FactorSet{SetAboveABuild, SetRolePromptOrSkill} {
		for _, d := range definitionsOf(set) {
			if d.group == GroupExposure {
				t.Errorf("%s weighs %s, and exposure is read from a diff and a build record", set, d.name)
			}
		}
	}
	for _, d := range definitionsOf(SetRolePromptOrSkill) {
		if d.name == changeSize.name || d.name == changeAreaChurn.name {
			t.Errorf("the role prompt's set weighs %s, and a role prompt has no code to have sized and no area to have churned", d.name)
		}
	}
}

// TestEverySetsWeightsSumToOneWithinEachTerm: one threshold is read against
// three sets, so the number every set returns has to be on one scale.
func TestEverySetsWeightsSumToOneWithinEachTerm(t *testing.T) {
	for _, set := range FactorSets {
		weights := ShippedWeights(set)
		for _, term := range []Term{TermLikelihood, TermImpact, TermReversibility} {
			total := 0.0
			for _, name := range factorsOf(set, term) {
				total += weights.Of(name)
			}
			if !near(total, 1) {
				t.Errorf("%s's %s weights sum to %v", set, term, total)
			}
		}
	}
}

// TestSuppliedCoversTenOfElevenRows: a supplied value for ten of the eleven rows
// and none for the list of allowed predicate kinds, which no outcome teaches.
func TestSuppliedCoversTenOfElevenRows(t *testing.T) {
	rows := map[string]bool{}
	for _, d := range gatepolicy.Definitions {
		if _, supplied := Starting(d.Parameter); supplied {
			rows[d.Row] = true
			continue
		}
		if d.Parameter != gatepolicy.AllowedPredicateKinds {
			t.Errorf("the score supplies no value for %s", d.Parameter)
		}
	}
	if len(rows) != 10 {
		t.Errorf("the score supplies a value for %d of gate policy's rows, want ten of the eleven", len(rows))
	}
	if _, supplied := Starting(gatepolicy.AllowedPredicateKinds); supplied {
		t.Error("the score supplies a list of allowed predicate kinds, which no outcome teaches")
	}
}

// TestAResolvedFactorIsLeftOutOfTheMeansAndTheNumberIsRecorded: the formula
// still runs on the factors that were computable, so calibration keeps the
// reading it would otherwise lose on every resolved decision, and what decides
// the gate is the resolution rather than the number.
func TestAResolvedFactorIsLeftOutOfTheMeansAndTheNumberIsRecorded(t *testing.T) {
	whole := []Factor{
		{Name: "a", Term: TermLikelihood, Level: 0.2, Weight: 0.5},
		{Name: "b", Term: TermLikelihood, Level: 0.2, Weight: 0.5},
		{Name: "c", Term: TermImpact, Level: 0.4, Weight: 1},
		{Name: "d", Term: TermReversibility, Level: 0.3, Weight: 1},
	}
	resolved := append([]Factor{}, whole...)
	resolved[1] = Factor{Name: "b", Term: TermLikelihood, Level: 1, Weight: 0.5, Resolved: "the supplier is down"}

	_, _, _, wholeNumber := reduce(whole)
	likelihood, _, _, resolvedNumber := reduce(resolved)
	if !near(likelihood, 0.2) {
		t.Errorf("the likelihood reads %v with one factor resolved, want the mean over the computable one", likelihood)
	}
	if !near(wholeNumber, resolvedNumber) {
		t.Errorf("a resolved factor moved the number from %v to %v, and it is left out rather than valued",
			wholeNumber, resolvedNumber)
	}
	if resolvedNumber >= 1 {
		t.Errorf("the number reads %v, and a resolution is a recorded fact and not a top-of-scale number", resolvedNumber)
	}
}
