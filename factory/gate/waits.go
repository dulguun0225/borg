package gate

import (
	"context"
	"fmt"
	"slices"

	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/score"
)

// Who a row waits on: the duty the design names for the row, the named human a
// safeguard's routing field gives, and the owner where nobody holds it.

// The duties the design names for a gate row, and no others. Every row it names
// none for widens to the owner, which is where every unheld row goes.
const (
	// DutyConfirmTheCriteria is duty 6, confirming that the acceptance criteria
	// are the right ones, which is what a human at the Spec row is doing.
	DutyConfirmTheCriteria people.Duty = 6
	// DutyUAT is duty 7, user acceptance testing, which is what a human at the
	// Merge to master row is doing.
	DutyUAT people.Duty = 7
	// DutyUndoAShippedChange is duty 10. It is not a row's duty but a hold's:
	// where a rollback whose revert has not shipped is in the set holding a
	// deploy row, that row waits on this duty, because approving through it
	// redelivers the defect the factory has just removed.
	DutyUndoAShippedChange people.Duty = 10
)

// RoutedTo is what a row whose routing is a record's rather than the design's
// carries: a safeguard's withdrawal routes to the duty or named human that
// safeguard's own routing field gives, a halt's and a legal hold's withdrawal
// route to the owner and away from the human who wrote the withdrawal, and the
// shortening of decision-log retention routes away from whoever authored the
// shorter value. It is supplied by the caller, the record being one this package
// does not read.
type RoutedTo struct {
	// Duty is the duty the record routes to, and is zero where it names none.
	Duty people.Duty
	// Human is the per-person key of the named human the record routes to, and
	// is empty where it names none.
	Human string
	// NotHuman is the per-person key of a human this row may not route to and
	// may not be closed by:
	// the actor on a withdrawal is never the human its row waits on, and the
	// human who authored a shorter retention value is not the one who decides
	// it.
	NotHuman string
}

// Waits is who an open event waits on, read at the firing and written onto it so
// that a reader of a pending decision knows who the verdict is waited on from.
// It is empty on a firing that put no human at the row, because nothing is
// waited on.
type Waits struct {
	// Duty is the duty a human at the row is performing, and is zero where the
	// design names none for it.
	Duty people.Duty
	// Human is the per-person key of the named human a safeguard's routing field
	// gives, and is empty where none is named.
	Human string
	// Holders is who the People declaration recorded as holding the duty at the
	// firing, by the per-person key that declaration holds. It is empty where the row
	// names no duty and where nobody holds the one it names, and an empty list
	// is what widens the row to the owner.
	Holders []string
	// NotHuman is the per-person key of the human this row may not be closed by,
	// carried from
	// [RoutedTo] so that a reader of the row sees the separation the record
	// makes between the actor and the decider.
	NotHuman string
}

// TheOwner reports whether the row widens to the owner, which is what an unheld
// row does everywhere in the design.
func (w Waits) TheOwner() bool { return len(w.Holders) == 0 && w.Human == "" }

// dutyOf is the duty the design names for one row and the holds standing at it.
// A deploy row holding on a rollback whose revert has not shipped waits on duty
// 10 whatever else stands, that hold being the one with a routing of its own; a
// deploy row otherwise names no duty, a human at one deciding whether the deploy
// happens rather than verifying an artifact.
func dutyOf(row Row, holds []string) people.Duty {
	if row.Deploys() && slices.Contains(holds, HoldRollbackAwaitingRevert) {
		return DutyUndoAShippedChange
	}
	switch row.Kind {
	case KindSpec:
		return DutyConfirmTheCriteria
	case KindMergeToMaster:
		return DutyUAT
	default:
		return 0
	}
}

// waitsOn is who the row waits on, read from the People declaration at the
// firing. A row whose routing is a record's takes that record's duty or named
// human; every other row takes the duty the design names, and widens to the
// owner where nobody holds it.
func (g *Gate) waitsOn(ctx context.Context, row Row, holds []string, routed RoutedTo) (Waits, error) {
	waits := Waits{Duty: dutyOf(row, holds), Human: routed.Human, NotHuman: routed.NotHuman}
	if routed.Duty != 0 {
		waits.Duty = routed.Duty
	}
	if waits.Duty == 0 || waits.Human != "" {
		return waits, nil
	}
	holders, err := g.holdersOf(ctx, waits.Duty)
	if err != nil {
		return Waits{}, err
	}
	waits.Holders = holders
	return waits, nil
}

// routedByAResolution is the human a resolution's own provenance names, and is
// empty where none does — a firing that resolved nothing, and one whose
// resolutions name nobody the factory can resolve, both leave the row on the
// duty the design names for it.
//
// It is what routes the Spec row to the human a withdrawn protection's
// provenance names rather than to the owner by default. The first is taken: a
// version withdrawing two protections is one decision and goes to one human, and
// the vector names every one of them beside the factor.
func routedByAResolution(resolved []score.Resolution) string {
	for _, r := range resolved {
		if r.RoutedTo != "" {
			return r.RoutedTo
		}
	}
	return ""
}

// holdersOf is who the People declaration records as holding one duty, by the
// per-person key that declaration holds. It is read at the firing for what the open event
// says and again at the close for the two refusals that turn on who holds the
// row's duty.
func (g *Gate) holdersOf(ctx context.Context, duty people.Duty) ([]string, error) {
	holders, err := people.Holders(ctx, g.pool, people.OfDuty(duty))
	if err != nil {
		return nil, fmt.Errorf("gate: reading who holds duty %d: %w", duty, err)
	}
	return holders, nil
}
