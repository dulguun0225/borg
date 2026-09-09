package gate

import (
	"errors"
	"fmt"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/screenstatemachine"
)

// The Implementation row's own mechanical rejections: what the transition check
// and the drivers found over the build. The state machine the spec authored is
// enforced here, mechanically rather than on taste, which is the whole reason
// for authoring a screen as a machine.

// The checks that reject at the Implementation row, in the words a close event
// names one by. They are constants here for the reason the merge row's and the
// Spec row's are: a caller cannot report a rejection under a name of its own.
const (
	// AutoRejectedByForbiddenTransition is a transition the check can show the
	// implementation admits that the screen's machine does not declare from that
	// state on that event. The machine is closed, so every undeclared transition
	// is forbidden.
	AutoRejectedByForbiddenTransition = "an implementation that admits a transition the screen's state machine forbids"
	// AutoRejectedByADriver is the drivers in either direction: a state in force
	// for the build that nothing drives, and a driver naming a state no machine
	// in force declares.
	AutoRejectedByADriver = "a state in force that nothing drives, or a driver naming a state no machine in force declares"
	// AutoRejectedByCompile is a build the build runner refused outright: the
	// commit does not compile. It is a fact about the build and not a judgment,
	// so the row rejects on it before a verdict is asked for and the compiler's
	// own words are the feedback — computed by the caller from the build
	// runner's own error, the way the caller computes it for the two checks
	// above from what it derived over the checkout.
	AutoRejectedByCompile = "a build that does not compile"
	// AutoRejectedByACriterionTheBuildDecided is an acceptance criterion the
	// build's own process decided against. An encoding declares which of two
	// places decides it, and one deciding a criterion over the code alone is
	// run by the build runner in the build's own process, its result written
	// onto the build. So this row rejects on it as it rejects a build that
	// does not compile, before any environment exists, and a human reading the
	// diff here reads those results beside it.
	AutoRejectedByACriterionTheBuildDecided = "an acceptance criterion the build's own process decided against"
)

// ImplementationChecks is every check that rejects on its own terms at the
// Implementation row, in the order the design names them.
var ImplementationChecks = []string{
	AutoRejectedByForbiddenTransition, AutoRejectedByADriver, AutoRejectedByCompile,
	AutoRejectedByACriterionTheBuildDecided,
}

// CriterionRejection is the Implementation row's rejection over what the build
// decided: a criterion whose encoding declared the build as its place and whose
// result stops the build. The candidate environment decides only the encodings
// declaring it, so a result from there is passed over here and read at
// [_Merge to master_] instead.
//
// An undecided criterion is read the way a failure is and one the service's own
// unreliable bound marks unreliable reads as absent, which is what
// [criterion.Outcome.Blocks] already decides — the same reading the merge row
// makes of the same results.
func CriterionRejection(criteria []CriterionResult) (check, found string, rejects bool) {
	for _, c := range criteria {
		if c.Place != criterion.PlaceBuild || !c.Outcome.Blocks(c.Unreliable) {
			continue
		}
		return AutoRejectedByACriterionTheBuildDecided,
			fmt.Sprintf("criterion %s was decided %s in the build's own process", c.CriterionID, c.Outcome),
			true
	}
	return "", "", false
}

// ScreenRejection is that rejection, over what the caller derived from the
// build: the transition check's derivation, the drivers', and the machines in
// force for the build. It returns which of [ImplementationChecks] rejects and
// what it found, and false where neither does.
//
// The transition check is reported first, a forbidden transition being what the
// machine was authored to forbid; a firing that fails both carries the one this
// returns and the other is found by the next attempt. Neither direction rejects
// on what could not be derived: a screen the check could not read resolves a
// factor instead, which is [screenstatemachine.Derivation.Unavailable] and the
// firing's own [Firing.Screens], and a build whose drivers no extractor could
// read is the same absence one step over.
//
// What derives the two is the caller: the derivations run where the checkout is,
// and this package reaches no repository.
func ScreenRejection(derived screenstatemachine.Derivation, drivers screenstatemachine.DriverDerivation,
	inForce []screenstatemachine.Machine) (check, found string, rejects bool) {

	if err := screenstatemachine.CheckTransitions(derived, inForce); err != nil {
		return AutoRejectedByForbiddenTransition, err.Error(), true
	}
	err := screenstatemachine.CheckDrivers(drivers, inForce)
	if err == nil {
		return "", "", false
	}
	var couldNotDerive *screenstatemachine.DriversCouldNotDeriveError
	if errors.As(err, &couldNotDerive) {
		return "", "", false
	}
	return AutoRejectedByADriver, err.Error(), true
}

// screensNotDerived is every screen this firing's derivation could not derive,
// in the words the check describes one by, which is what the score reads as an
// unavailable input. A firing at any other row derived none.
func screensNotDerived(f Firing) []string {
	var could []string
	for _, s := range f.Screens.Unavailable() {
		could = append(could, s.Describe())
	}
	return could
}
