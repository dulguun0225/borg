package score

import (
	"context"
	"fmt"
)

// exposure reads what the change reaches that the service did not reach before:
// an outbound call added, a credential named or read, an authorization check
// removed or weakened, and a dependency change, each entry naming the file and
// line or the package with its version and its declared licence.
//
// Three things separate it from the change group and all three are here. It only
// ever raises the number: a diff adding none of this reads as nothing, its
// absence not being evidence of safety. It learns from no outcome — a change
// that posts credentials outward moves neither error rate nor latency, so a
// window closing passed on it says nothing about it — so nothing in this package
// moves it on an exit. And it reads the diff and never the intent, which is what
// makes it the one factor a stranger's sentence in a report cannot talk its way
// past.
//
// It is in the set only where there is a build to read it from. Above a build
// the group is inapplicable and not unavailable, which is why the four rows
// above one weigh a set without it rather than resolving it: treating a factor
// the gate was never going to have as missing would put a human at every one of
// those gates forever.
//
// Where no extractor runs for the toolchain the factor is unavailable and never
// nothing, and above the bound gate policy sets it is resolved: a human at
// Implementation, with the list beside the diff, and the held-out sample barred
// from selecting past it.
func (s *Score) exposure(_ context.Context, c Change) (reading, error) {
	if c.Exposure.Unavailable != "" {
		return reading{unavailable: c.Exposure.Unavailable}, nil
	}
	if !c.Exposure.Derived {
		return reading{unavailable: "nothing derived an exposure list for this change, and a list nobody derived is never read as no dependency change"}, nil
	}
	list := c.Exposure.List()
	value := level(float64(len(list)), exposureBreakpoints, 1.0)
	words := fmt.Sprintf("%d outbound call(s) added, %d credential(s) named or read, %d authorization check(s) removed or weakened, and %d dependency change(s)",
		len(c.Exposure.OutboundCalls), len(c.Exposure.Credentials),
		len(c.Exposure.AuthorizationChecks), len(c.Exposure.DependencyChanges))
	if c.AtImplementation && c.ExposureBound > 0 && value > c.ExposureBound {
		return reading{
			level:    value,
			words:    words,
			evidence: list,
			resolved: fmt.Sprintf("the exposure factor reads %.2f, above the bound of %.2f in force on this service, so a human decides at Implementation with the list beside the diff", value, c.ExposureBound),
			cause:    CauseExposureOverTheBound,
		}, nil
	}
	return reading{level: value, words: words, evidence: list}, nil
}
