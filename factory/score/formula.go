package score

// FormulaVersion names the published formula. It is authored and stays authored:
// the weights, the breakpoints, and the last step were written by hand and
// calibrated against a factory that has just been installed, and what learning
// moves is the seven values the score supplies rather than any of those — an owner
// authors what the number is compared against and never how it is computed.
//
// It moves when a line of [Formula] changes, which is the second thing it has
// named: authored-1 read the authorship prior over human verdicts alone, and
// authored-2 reads it over every outcome on that author's work, which is a
// different number from the same arithmetic. What moves a supplied value instead
// is [Rules], and [LearningVersion] is that text's own name.
const FormulaVersion = "authored-2"

// Formula is the published formula, in the words the score version stores and a
// human disagreeing with a number reads. It states every breakpoint, and
// TestFormulaStatesEveryBreakpoint is what keeps the text and the source from
// drifting apart.
const Formula = `Each factor resolves to a level between 0 and 1, where 1 is the risky end.

  change.size            lines the diff changes: <=50 -> 0.1, <=200 -> 0.3, <=600 -> 0.6, above -> 1.0
  change.reach           share of the service's files touched: <=0.1 -> 0.1, <=0.3 -> 0.3, <=0.6 -> 0.6, above -> 1.0
  change.area_churn      releases in this area lately: <=0 -> 0.0, <=2 -> 0.2, <=9 -> 0.5, above -> 1.0
  change.test_coverage   any criterion failed -> 1.0; otherwise criteria in force: <=0 -> 1.0, <=2 -> 0.5, <=9 -> 0.3, above -> 0.1
  change.reversibility   no earlier release to return to -> 1.0; an earlier release -> 0.3
  authorship.prior       1 - good / (good + bad + 1), over every outcome on this author's work: a human
                         approving one of its versions and a release of its watched to a close without
                         harm are good, a human rejecting one, a window condemning a release at harm,
                         and a human vetoing one are bad, and a swept window is neither
  context.business_area  1 - approved / (approved + rejected + 1), over human verdicts on items in this area
  context.consumers      sibling services declaring they consume this one: <=0 -> 0.0, <=2 -> 0.4, <=9 -> 0.7, above -> 1.0

  likelihood = sum(weight x level) / sum(weight) over the likelihood half
  impact     = sum(weight x level) / sum(weight) over the impact half
  exposure   = impact x (1 - 0.5 x (1 - change.reversibility))
  number     = exposure x (0.4 + 0.6 x likelihood)

A factor the score could not compute resolves to 1.0 and the number is 1.0, which is at or above
every threshold an owner may author — so one absent input puts a human at the gate however low the
rest of the vector reads. The direction is the one thing that must not depend on a component being
up; how far it moves the number is this formula's, published like the rest of it.

Impact bounds the number and likelihood scales it inside that bound, never down to nothing: a
change that is unlikely to be wrong and catastrophic if it is keeps four tenths of its exposure and
is gated whatever its likelihood, and one that is likely wrong and cheap to undo is not.
Reversibility discounts impact by half at most, because undoing a release here is a redeploy nobody
watched — a fuller discount needs the control and the watch window.
`

// The last step's two constants, named because the formula's text states them
// and this is where they are applied.
const (
	// reversibilityDiscount is how much of the impact a fully reversible change
	// gives back.
	reversibilityDiscount = 0.5
	// likelihoodFloor is the share of the exposure that stands however low the
	// likelihood reads, which is what gates an unlikely catastrophe.
	likelihoodFloor = 0.4
)

// Assessment is the score's answer for one change: the vector a human argues
// with, the two halves kept apart, and the number the formula reduced them to.
type Assessment struct {
	// Version is the score version the assessment was computed under, which is
	// the id of a row of this package's table. The opening row names it, so
	// recomputing the vector later under a moved version cannot pass for the
	// vector the decision was made on.
	Version string
	// FormulaVersion is what that version names as its formula, carried here so
	// a reader of an assessment does not have to read the version record to
	// know which formula produced the number.
	FormulaVersion string
	Vector         []Factor
	Likelihood     float64
	Impact         float64
	Exposure       float64
	Number         float64
}

// UnavailableFactors is the names of the factors the score could not compute,
// in vector order, and empty where it computed all of them. A gate prints them
// beside the number, because a fault in whatever supplies a factor otherwise
// reads as the score having changed its mind.
func (a Assessment) UnavailableFactors() []string {
	var names []string
	for _, f := range a.Vector {
		if f.Unavailable != "" {
			names = append(names, f.Name)
		}
	}
	return names
}

// reduce applies the published formula to a vector: the weighted mean of each
// half, impact discounted by reversibility, and the last step. A vector with any
// factor unavailable reduces to the top of the scale.
func reduce(vector []Factor) (likelihood, impact, exposure, number float64) {
	var likelihoodWeight, impactWeight float64
	reversibility := 1.0
	unavailable := false
	for _, f := range vector {
		if f.Unavailable != "" {
			unavailable = true
		}
		switch f.Half {
		case HalfLikelihood:
			likelihood += f.Weight * f.Level
			likelihoodWeight += f.Weight
		case HalfImpact:
			impact += f.Weight * f.Level
			impactWeight += f.Weight
		case HalfReversibility:
			reversibility = f.Level
		}
	}
	if likelihoodWeight > 0 {
		likelihood /= likelihoodWeight
	}
	if impactWeight > 0 {
		impact /= impactWeight
	}
	exposure = impact * (1 - reversibilityDiscount*(1-reversibility))
	number = exposure * (likelihoodFloor + (1-likelihoodFloor)*likelihood)
	if unavailable {
		return likelihood, impact, exposure, 1
	}
	return likelihood, impact, exposure, number
}
