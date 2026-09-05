package score

import "fmt"

// Cause is why a factor was resolved rather than weighed. The design names
// five, and every one of them is a fact recorded on the decision rather than a
// low number a reader has to interpret.
type Cause string

const (
	// CauseUnavailable is a factor the score could not compute: an input it
	// could not read, a build whose diff it could not take, a toolchain with no
	// extractor behind the exposure list, or an artifact version authored from a
	// truncated read.
	CauseUnavailable Cause = "unavailable"
	// CauseDrifted is a factor calibration found drifted, or a per-author prior
	// its own held-out reading found drifted. It takes the treatment an
	// unavailable factor takes until a recalibration is in force at the gate.
	CauseDrifted Cause = "drifted"
	// CauseIrreversibleHazard is the hazard severity in force on the item's area
	// being irreversible. It resolves at Implementation and at no gate above it.
	CauseIrreversibleHazard Cause = "irreversible hazard severity"
	// CauseReportSourcedIntent is an intent whose evidence carries text the
	// factory did not author, which resolves the source value toward the Spec
	// gate.
	CauseReportSourcedIntent Cause = "the intent came from reports"
	// CauseDestroysStoredData is a diff that destroys stored data, which
	// resolves the reversibility factor at Implementation.
	CauseDestroysStoredData Cause = "the diff destroys stored data"
	// CauseExposureOverTheBound is an exposure factor above the bound gate
	// policy sets, which resolves at Implementation.
	CauseExposureOverTheBound Cause = "exposure over the bound"
)

// Resolution is one factor resolved at one firing: which factor, why, and the
// cause in the design's own words. A vector recording one is a decision a human
// takes whatever the formula returns, and it is the fact [Score.HoldOut] reads
// before it may select — read off the decision rather than inferred from where
// the number landed.
//
// It binds that firing and no other, a vector being computed at one firing and
// never recomputed: a supplier still down, a factor still drifted and a
// toolchain still without an extractor are found again by the next firing's own
// vector.
type Resolution struct {
	Factor string `json:"factor"`
	Cause  Cause  `json:"cause"`
	Why    string `json:"why"`
}

func (r Resolution) String() string {
	return fmt.Sprintf("%s: %s (%s)", r.Factor, r.Why, r.Cause)
}

// resolve marks a factor resolved and records the resolution beside it. The
// factor keeps the level it was read at, where it was read at all, because a
// resolved factor is left out of the weighted means rather than valued at the
// top of the scale: valuing it would make the number say what the resolution
// says, and the number is recorded beside the resolution so that calibration
// keeps the reading it would otherwise lose on every resolved decision.
func resolve(f *Factor, resolutions *[]Resolution, cause Cause, why string) {
	f.Resolved = why
	*resolutions = append(*resolutions, Resolution{Factor: f.Name, Cause: cause, Why: why})
}
