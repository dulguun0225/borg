package main

import (
	"context"
	"fmt"
	"slices"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/intent"
)

// A criterion's outcome history, read against the service's own unreliable
// bound at the moment the candidate run's results are recorded, and the
// intent becoming unreliable raises. [criterion.Unreliable] resolves the bound
// itself; what was missing was a caller at the gate, which this is — the
// candidate run's own recording, read again unchanged wherever the criterion
// result travels afterward, Merge to master among them.

// markUnreliable reads every criterion this run just decided against its own
// outcome history and marks the ones above the service's unreliable bound,
// setting [gate.CriterionResult.Unreliable] in place so Merge to master reads
// it the same way this run did: while unreliable, a criterion's failure blocks
// nothing, per [criterion.Outcome.Blocks].
//
// history is every build this item's own criteria have been decided against
// so far, [candidate.buildHistory] appended with buildID once: the two cuts
// the design puts on that history — one seed version, a diff reaching the
// requirement — are not derived anywhere yet, so this reads the wider set
// [criterion.Unreliable]'s own doc names that choice as the caller's to make.
func (p *path) markUnreliable(ctx context.Context, c *candidate, buildID string, results []gate.CriterionResult) error {
	if !slices.Contains(c.buildHistory, buildID) {
		c.buildHistory = append(c.buildHistory, buildID)
	}
	for i, result := range results {
		reliability, err := criterion.Unreliable(ctx, p.d.pool, result.CriterionID, c.buildHistory, c.svc.UnreliableBound)
		if err != nil {
			return err
		}
		results[i].Unreliable = reliability.Unreliable
		if !reliability.Unreliable {
			continue
		}
		if err := p.raiseUnreliable(ctx, result.CriterionID); err != nil {
			return err
		}
	}
	return nil
}

// raiseUnreliable is the intent becoming unreliable raises, keyed by the
// criterion the way a detector's own condition is: the oldest one already
// open on it, or a fresh one where there is none, so a second crossing while
// that intent is open joins it rather than raising a second one — the same
// dedup the health monitor's own raises make over their own evidence.
func (p *path) raiseUnreliable(ctx context.Context, criterionID string) error {
	evidence := intent.Evidence{CriterionID: criterionID}
	_, found, err := intent.OnEvidence(ctx, p.d.pool, evidence)
	if err != nil {
		return err
	}
	if found || p.intake == nil {
		return nil
	}
	_, err = p.intake.TakeIn(ctx, deployActor, intent.Arrival{
		Source: intent.SourceDetector,
		Statement: fmt.Sprintf(
			"Criterion %s disagrees across too much of its outcome history to trust; its encoding is re-authored through the pipeline.",
			criterionID),
		Evidence: evidence,
	})
	return err
}
