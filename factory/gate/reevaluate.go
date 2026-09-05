package gate

import (
	"context"

	"github.com/dulguun0225/borg/factory/decisionlog"
)

// A hold the factory sets holds the open row rather than firing the gate again.
// The row stays open while such a hold stands, one open event and never one per
// re-test, and the gate re-evaluates the pending row whenever a record a hold of
// this kind reads changes, and on a fixed interval at the least.

// Reevaluated is what one re-evaluation found: the holds still standing, whether
// the row closed, and the close event where it did.
type Reevaluated struct {
	// Holds is what still stands, in the order [HoldsAt] lists them, and is
	// empty where every hold has lifted.
	Holds []string
	// Closed is the close event this re-evaluation appended, and is the zero row
	// where the row goes on waiting: a hold still standing, or a human the row
	// names who has not decided.
	Closed decisionlog.Row
	// WaitsOnAHuman is why the row is still open with no hold standing: the mark
	// on it sends a human, or the number is at or above the threshold, so the
	// row goes on waiting on the human it names.
	WaitsOnAHuman bool
}

// Reevaluate re-tests the holds standing on one pending row. When none stands
// the row closes as it would have at a firing with no hold: where the row
// carries no mark sending a human and the number on the open event is under the
// threshold, that is an auto-approve with the gate component as actor; otherwise
// the row goes on waiting on the human it names, who may already have approved
// through the hold by naming it.
//
// It appends nothing while a hold stands, which is what keeps a pending row to
// one open event however long the hold stood, and what keeps the score's join of
// open row to close row free of unpaired rows.
func (g *Gate) Reevaluate(ctx context.Context, opened Opened) (Reevaluated, error) {
	standing, err := g.standingHolds(ctx, opened.Subject)
	if err != nil {
		return Reevaluated{}, err
	}
	if len(standing) > 0 {
		return Reevaluated{Holds: standing}, nil
	}
	if opened.HumanDecides {
		return Reevaluated{WaitsOnAHuman: true}, nil
	}
	// The number on the open event and not a recomputed one: a vector is
	// computed at one firing and never recomputed, and the row was opened under
	// the score version its own row names.
	opened.Holds = nil
	closed, err := g.AutoPass(ctx, opened)
	if err != nil {
		return Reevaluated{}, err
	}
	return Reevaluated{Closed: closed}, nil
}

// ReevaluatePending re-evaluates every pending row a hold stands on, which is
// what the fixed interval calls. A row with no hold on it is left alone: nothing
// about it changed, and re-evaluating it would append a verdict on a row nobody
// asked about.
func (g *Gate) ReevaluatePending(ctx context.Context) ([]Reevaluated, error) {
	pending, err := g.Pending(ctx)
	if err != nil {
		return nil, err
	}
	var found []Reevaluated
	for _, open := range pending {
		if !open.Holding() {
			continue
		}
		result, err := g.Reevaluate(ctx, open)
		if err != nil {
			return found, err
		}
		found = append(found, result)
	}
	return found, nil
}
