package score

// FormulaVersion names the published formula. It is authored and stays authored:
// the breakpoints and the last step were written by hand and calibrated against a
// factory that has just been installed, and what learning moves is the values the
// score supplies rather than any of those — an owner authors what the number is
// compared against and never how it is computed. What moves the weights is a
// recalibration, which writes a version differing in the weights and in nothing
// else.
//
// It moves when a line of [Formula] changes, and a version that changes it does
// not decide a gate an authored threshold binds until the owner has confirmed or
// re-authored that threshold against it, which is [InForceAt].
const FormulaVersion = "authored-4"

// Formula is the published formula, in the words the score version stores and a
// human disagreeing with a number reads. It states every breakpoint, and
// TestFormulaStatesEveryBreakpoint is what keeps the text and the source from
// drifting apart. What it does not state is the weights: those are a field of the
// version, fitted apart per factor set, and the version publishes them beside
// this text.
const Formula = `Each factor resolves to a level between 0 and 1, where 1 is the risky end.

  change.size            lines the diff changes: <=50 -> 0.1, <=200 -> 0.3, <=600 -> 0.6, above -> 1.0
                         at Decomposition, the requirements the proposed set answers: <=2 -> 0.1, <=5 -> 0.3,
                         <=12 -> 0.6, above -> 1.0
  change.reach           share of the service's files touched: <=0.1 -> 0.1, <=0.3 -> 0.3, <=0.6 -> 0.6, above -> 1.0
  change.area_churn      releases in this area lately: <=0 -> 0.0, <=2 -> 0.2, <=9 -> 0.5, above -> 1.0
  change.reversibility   no earlier release to return to -> 1.0; an earlier release -> 0.3; a diff that
                         destroys stored data is resolved rather than valued
  author.prior           1 - good / (good + bad + 1), over every outcome on this author's work: a human
                         approving one of its versions and a window closing passed over a release of its
                         work are good, a human rejecting one, a window closing failed, and a human's undo
                         are bad. timed out and skipped are neither, and the prior may not narrow past the
                         width its own count of passed and failed closes supports
  exposure.reach         what the diff and the build's resolved set reach that the service did not reach
                         before, counted over outbound calls added, credentials named or read,
                         authorization checks removed or weakened, and dependency changes:
                         <=0 -> 0.0, <=2 -> 0.4, <=5 -> 0.7, above -> 1.0. It only ever raises the number,
                         it is inapplicable above a build, and above the exposure bound it is resolved
  context.hazard_severity  the hazard severity in force on the item's area: negligible -> 0.1,
                         recoverable -> 0.5, irreversible -> resolved at Implementation rather than weighed
  context.intent_source  an owner's request -> 0.2, something the factory found itself -> 0.4, reports ->
                         resolved at Spec rather than weighed
  context.consumers      sibling services declaring they consume this one: <=0 -> 0.0, <=2 -> 0.4, <=9 -> 0.7,
                         above -> 1.0
  fleet.share_working_from_it  the share of the factory working from the version in force this one replaces
  fleet.departure        how far this version differs from the version in force, as the share of its lines
                         that differ
  fleet.reversibility    0.3 for every version: withdrawal is a second record and nothing was deployed

  likelihood       = sum(weight x level) / sum(weight) over the likelihood term, resolved factors left out
  impact           = sum(weight x level) / sum(weight) over the impact term less exposure.reach, resolved factors
                      left out, plus exposure.reach's own weight x level, capped at 1
  discountedImpact = impact x (1 - 0.5 x (1 - change.reversibility))
  number           = discountedImpact x (0.4 + 0.6 x likelihood)

A factor the score resolves is left out of the weighted means and never valued. The formula still runs on
the factors that were computable, and the number is recorded beside the resolution so that calibration
keeps the reading it would otherwise lose. What decides such a gate is the resolution and not the number:
a human decides whatever the formula returns, and the held-out sample may not select past it.

Impact bounds the number and likelihood scales it inside that bound, never down to nothing: a change that
is unlikely to be wrong and catastrophic if it is keeps four tenths of its discounted impact and is gated
whatever its likelihood, and one that is likely wrong and cheap to undo is not. Reversibility discounts
impact by half at most, because undoing a release here is a redeploy nobody watched — a fuller discount
needs the control and the analysis window.

One threshold is read against three factor sets, so every set returns a number on one scale: the number
estimates the share of held-out windows that failed among decisions taken at that number on that set.
Each set's weights are fitted apart on the held-out decisions taken on that set alone, and a set with too
few keeps the weights the product shipped for it, with the count published beside its bands.
`

// The last step's two constants, named because the formula's text states them
// and this is where they are applied.
const (
	// reversibilityDiscount is how much of the impact a fully reversible change
	// gives back.
	reversibilityDiscount = 0.5
	// likelihoodFloor is the share of the discounted impact that stands however
	// low the likelihood reads, which is what gates an unlikely catastrophe.
	likelihoodFloor = 0.4
)

// Assessment is the score's answer for one change: the vector a human argues
// with, the two terms kept apart, and the number the formula reduced them to.
type Assessment struct {
	// Version is the score version the assessment was computed under, which is
	// the id of the log row that appended it. The open event names it, so
	// recomputing the vector later under a moved version cannot pass for the
	// vector the decision was made on.
	Version string
	// FormulaVersion is what that version names as its formula, carried here so
	// a reader of an assessment does not have to read the version to know which
	// formula produced the number.
	FormulaVersion string
	// FactorSet is the set the vector was computed on, which is what says which
	// weights the number was reduced by and which bands it is read against.
	FactorSet FactorSet
	Vector    []Factor
	// Resolved is every factor this firing resolved. A firing with any
	// resolution is a human's whatever the number reads, and the sample may not
	// select past it.
	Resolved         []Resolution
	Likelihood       float64
	Impact           float64
	DiscountedImpact float64
	Number           float64
}

// ResolvedFactors is the names of the factors this firing resolved, in vector
// order, and empty where it resolved none. A gate prints them beside the number,
// because a fault in whatever supplies a factor otherwise reads as the score
// having changed its mind.
func (a Assessment) ResolvedFactors() []string {
	var names []string
	for _, r := range a.Resolved {
		names = append(names, r.Factor)
	}
	return names
}

// reduce applies the published formula to a vector under one set's weights: the
// weighted mean of each term over the factors that were computable, exposure's
// own contribution added to impact and the sum capped at 1, impact discounted
// by reversibility, and the last step. A resolved factor is left out of every
// mean, of the addition, and of the discount.
//
// Exposure is added rather than folded into the impact mean because a mean
// cannot only ever raise: a zero level pulls a mean down like any other factor,
// and the design states that exposure's absence is not evidence of safety. It is
// added after the mean over the other impact factors instead, so a diff adding
// none of it leaves that mean untouched and a diff adding some of it can only
// raise the sum, which the cap then holds to the scale every other factor
// shares.
func reduce(vector []Factor) (likelihood, impact, discountedImpact, number float64) {
	var likelihoodWeight, impactWeight float64
	var exposureWeight, exposureLevel float64
	reversibility := 1.0
	for _, f := range vector {
		if f.Resolved != "" {
			continue
		}
		if f.Group == GroupExposure {
			exposureWeight, exposureLevel = f.Weight, f.Level
			continue
		}
		switch f.Term {
		case TermLikelihood:
			likelihood += f.Weight * f.Level
			likelihoodWeight += f.Weight
		case TermImpact:
			impact += f.Weight * f.Level
			impactWeight += f.Weight
		case TermReversibility:
			reversibility = f.Level
		}
	}
	if likelihoodWeight > 0 {
		likelihood /= likelihoodWeight
	}
	if impactWeight > 0 {
		impact /= impactWeight
	}
	impact += exposureWeight * exposureLevel
	if impact > 1 {
		impact = 1
	}
	discountedImpact = impact * (1 - reversibilityDiscount*(1-reversibility))
	number = discountedImpact * (likelihoodFloor + (1-likelihoodFloor)*likelihood)
	return likelihood, impact, discountedImpact, number
}
