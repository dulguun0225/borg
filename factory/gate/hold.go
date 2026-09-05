package gate

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

// The conditions the factory's own hold is computed from, in the words a caller
// reports one with. A hold of this kind is not a verdict and writes nothing: it
// is recomputed at the firing and at every re-evaluation of the open row while
// it stands, because a record for it would be a decision where the design says
// nothing is decided, and re-testing would append one every time.
//
// They are constants here and computed by whatever reads the records each one
// reads. What this package owns is the vocabulary, so a caller cannot report a
// hold under a name of its own, and an approve naming a hold names one of these.
const (
	// HoldDependencyNotLive is a declared dependency that is not live at all, at
	// the candidate deploy row: the environment is composed from it.
	HoldDependencyNotLive = "a declared dependency is not live"
	// HoldDependencyNotCurrent is a declared dependency that is not its
	// service's current release, at the production deploy row. Current means
	// marked complete on every production target, so a producer deployed to
	// three targets of four holds its consumer until the fourth lands.
	HoldDependencyNotCurrent = "a declared dependency is not its service's current release"
	// HoldContractMigrationNotShipped is the producing release of a contract
	// migration that has not shipped, at the candidate deploy row: a candidate
	// environment is composed from its dependencies' current releases and never
	// from another service's candidate.
	HoldContractMigrationNotShipped = "the producing release of a contract migration has not shipped"
	// HoldNoRoomOnThePlatform is the platform with no room for another
	// candidate environment. It is one of the two conditions at that row
	// decomposition could not have declared, and it lifts when an environment is
	// freed.
	HoldNoRoomOnThePlatform = "the platform has no room for another candidate environment"
	// HoldAtMaxConcurrentCandidateEnvironments is the count authored outright on
	// the production environment record, one per platform. It holds beside the
	// platform's own room, whichever of the two is reached first.
	HoldAtMaxConcurrentCandidateEnvironments = "the maximum concurrent candidate environments authored on the production environment record is reached"
	// HoldServiceNotProvisioned is a service its owner has not yet marked
	// provisioned: a repository nobody created, or a store missing from a
	// persistent environment. It holds at both deploy rows and lifts when the
	// owner writes the field.
	HoldServiceNotProvisioned = "the service is not marked provisioned"
	// HoldWindowLimitReached is the service already holding as many analysis
	// windows open as the window limit allows. It lifts itself when one of those
	// windows closes, and it is a wait on the factory and not on a human.
	HoldWindowLimitReached = "the service holds as many analysis windows open as the window limit allows"
	// HoldRollbackAwaitingRevert is a rollback whose revert has not shipped.
	// Master keeps the change that was rolled back and the next item was built
	// on master, so deploying it would redeliver the defect just removed. It is
	// the one hold with a routing of its own: its row waits on the duty that
	// undoes a shipped change.
	HoldRollbackAwaitingRevert = "a rollback's revert has not shipped, so deploying would redeliver the defect it removed"
	// HoldErrorBudgetExhausted is the service's error budget exhausted. It lifts
	// itself, and two items pass it rather than waiting: a revert, and an item
	// the health monitor raised on that service.
	HoldErrorBudgetExhausted = "the service's error budget is exhausted"
	// HoldAtMaxConcurrentKeptFleets is the service already at its maximum
	// concurrent kept fleets. Deploying further would tear down instances a
	// window could still call back, so the deploy waits rather than losing that
	// recovery, and the hold lifts as a kept fleet's window closes.
	HoldAtMaxConcurrentKeptFleets = "the service is at its maximum concurrent kept fleets"
	// HoldAdvisoryMatch is a package in the release's own resolved set matching
	// an advisory at or above the advisory severity. It lifts when the intent
	// the advisory raised ships its clearing version, and approving through it
	// accepts the vulnerability rather than a break.
	HoldAdvisoryMatch = "the release's resolved set holds a package matching an advisory at or above the advisory severity"
	// HoldChangeFreeze is a period an owner authored on the service within which
	// its production deploys are held. It lifts itself when the period passes
	// and takes the error budget hold's two exceptions.
	HoldChangeFreeze = "a change freeze the owner authored on this service is in force"
	// HoldHalt is the one authored record whose subject is the factory. It
	// holds every service's production deploys, and it is the one hold no
	// approve passes: approving through it would end it at a deploy gate rather
	// than at the row that decides it ends.
	HoldHalt = "a halt stands, and the factory is stopped"
	// HoldDriftMismatch is a record the drift detector found disagreeing with
	// what runs. It is the other kind of hold and the only one of it: no
	// evidence the factory can gather lifts it, because every remedy the factory
	// has reads the record in question. It pages and waits on a human, where the
	// conditions beside it lift themselves and page nobody.
	HoldDriftMismatch = "the drift detector found a record disagreeing with what runs"
)

// productionHolds is every hold that may stand at the production deploy row and
// at every further deploy row, which is fed from master the same way.
var productionHolds = []string{
	HoldDependencyNotCurrent, HoldServiceNotProvisioned, HoldWindowLimitReached,
	HoldRollbackAwaitingRevert, HoldErrorBudgetExhausted, HoldAtMaxConcurrentKeptFleets,
	HoldAdvisoryMatch, HoldChangeFreeze, HoldHalt, HoldDriftMismatch,
}

// candidateHolds is every hold that may stand at the candidate deploy row.
var candidateHolds = []string{
	HoldDependencyNotLive, HoldContractMigrationNotShipped, HoldServiceNotProvisioned,
	HoldNoRoomOnThePlatform, HoldAtMaxConcurrentCandidateEnvironments,
}

// HoldsAt is every hold that may stand at one row, and none at a row that is not
// a deploy: what a hold stops is an event that would otherwise happen, and only
// a deploy row has one.
func HoldsAt(row Row) []string {
	switch row.Kind {
	case KindDeployToCandidateEnvironment:
		return slices.Clone(candidateHolds)
	case KindDeployToProduction, KindDeployToEnvironment:
		return slices.Clone(productionHolds)
	default:
		return nil
	}
}

var (
	// ErrHoldUnknown is returned for a hold reported at a row whose vocabulary
	// does not hold it: a production hold named at the candidate deploy row, a
	// hold named at a row that is not a deploy, or a word this package does not
	// own.
	ErrHoldUnknown = errors.New("gate: that hold does not stand at this row")
	// ErrApproveNamesAHoldNotStanding is returned for an approve naming a hold
	// that is not standing at that re-evaluation.
	ErrApproveNamesAHoldNotStanding = errors.New("gate: the approve names a hold that is not standing")
	// ErrApproveLeavesAHoldOut is returned for an approve whose set leaves out a
	// hold that is standing, of which a bare approve while one stands is the
	// case with nothing named.
	ErrApproveLeavesAHoldOut = errors.New("gate: the approve leaves out a hold that is standing")
	// ErrApproveThroughAHalt is returned for an approve at a firing where a halt
	// stands. There is no verdict a halt admits, because approving through one
	// is ending it, and ending it is decided at A halt's withdrawal by the human
	// that row routes to.
	ErrApproveThroughAHalt = errors.New("gate: no approve passes a halt — it is ended at A halt's withdrawal and nowhere else")
)

// Subjects is what a hold is computed for: the row and the records the firing
// decides over. It is a value of its own because the holds are recomputed at
// every re-evaluation of a pending row, where what is in hand is the open event
// and not the firing that wrote it.
type Subjects struct {
	Row Row
	// IntentID is the intent the Decomposition row decided over, and is empty at
	// every row below it, which decides over one item.
	IntentID      string
	ItemID        string
	BuildID       string
	ServiceID     string
	AreaID        string
	EnvironmentID string
	// ReleaseID is the release the deploy would put on the environment, and is
	// empty at every row above the merge.
	ReleaseID string
}

// Holds is what the composition supplies for computing the factory's own holds:
// the conditions standing for one firing at the moment it is asked. It is an
// interface because what computes them reads the item's declared dependencies,
// the deploy records of their services, the open windows, the error budget, the
// advisories and the freeze, and a gate that imported all of those would import
// most of the module.
//
// It is asked at the firing and again at every re-evaluation of a pending row,
// which is the whole of what "recomputed at the firing" means. The halt is not
// among what it answers: this package reads that record itself, because the
// refusal it carries is the gate's.
type Holds interface {
	Standing(ctx context.Context, s Subjects) ([]string, error)
}

// NoHolds is the answer of a factory composed with nothing computing holds: no
// condition ever stands. It is a value rather than a nil interface so that a
// factory composed without one says so.
//
// What it costs is every hold but the halt and the drift mismatch: a deploy
// whose dependency is not current, whose service is not provisioned, or whose
// error budget is exhausted goes to a verdict rather than waiting.
type NoHolds struct{}

// Standing is never a hold.
func (NoHolds) Standing(context.Context, Subjects) ([]string, error) { return nil, nil }

// checkHolds refuses a hold reported at a row that does not have it, and returns
// the set in the order [HoldsAt] lists it so that two firings of one row that
// found the same conditions write the same set.
func checkHolds(row Row, standing []string) ([]string, error) {
	allowed := HoldsAt(row)
	for _, hold := range standing {
		if !slices.Contains(allowed, hold) {
			return nil, fmt.Errorf("%w: %q at %s", ErrHoldUnknown, hold, row)
		}
	}
	ordered := make([]string, 0, len(standing))
	for _, hold := range allowed {
		if slices.Contains(standing, hold) {
			ordered = append(ordered, hold)
		}
	}
	return ordered, nil
}

// checkApproveNamesTheSet refuses the three shapes the design names: an approve
// naming a hold not standing, an approve whose set leaves one out, and an
// approve at a firing where a halt stands.
//
// The order matters: the halt is refused first, so an approve that named every
// hold including the halt is told that no approve passes a halt rather than that
// its set was wrong.
func checkApproveNamesTheSet(standing, named []string) error {
	if slices.Contains(standing, HoldHalt) {
		return ErrApproveThroughAHalt
	}
	for _, name := range named {
		if !slices.Contains(standing, name) {
			return fmt.Errorf("%w: %q", ErrApproveNamesAHoldNotStanding, name)
		}
	}
	for _, hold := range standing {
		if !slices.Contains(named, hold) {
			return fmt.Errorf("%w: %q", ErrApproveLeavesAHoldOut, hold)
		}
	}
	return nil
}
