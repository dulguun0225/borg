package gate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/dulguun0225/borg/factory/decisionlog"
)

// What an event gate's firing waits on. An artifact gate is fired by the
// artifact store writing the version the stage submits; an event gate is fired
// by the event it decides becoming next, which needs the row above approved
// and, at a merge, the candidate's run ended.
//
// An approval is read for whether it still stands, because a reject naming a
// target leaves nothing below that target standing as approved: a plan authored
// from a spec version since superseded is not this item's plan, and leaving it
// approved would have the Implementation row decide a build against it.

var (
	// ErrRowAboveNotApproved is returned by [Gate.Fire] at an event gate whose
	// row above holds no approval that still stands. The event has not become
	// next, so there is nothing to decide and no open event is appended.
	ErrRowAboveNotApproved = errors.New("gate: the row above this one is not approved, so this event is not next")
	// ErrCandidateRunNotEnded is returned by [Gate.Fire] at the merge row for a
	// firing whose candidate's run has not ended. What that row decides is the
	// candidate's own run, so a firing before it ended would decide over
	// nothing.
	ErrCandidateRunNotEnded = errors.New("gate: the merge row fires when the candidate's run has ended")
)

// RowAbove is the row an event gate waits on the approval of, and false at
// every row that is not an event gate on an item's path. The candidate deploy
// row waits on Implementation, the merge row on the candidate deploy, and the
// production deploy row on the merge — as does every further deploy row, which
// is fed from master the same way.
func RowAbove(row Row) (Row, bool) {
	switch row.Kind {
	case KindDeployToCandidateEnvironment:
		return Implementation, true
	case KindMergeToMaster:
		return DeployToCandidateEnvironment, true
	case KindDeployToProduction, KindDeployToEnvironment:
		return MergeToMaster, true
	default:
		return Row{}, false
	}
}

// depthOf is how far down an item's path a row is, which is the one scale a
// reject's target and an approval are compared on. The five rows outside every
// item have no path to be at a depth of.
func depthOf(row Row) (int, bool) {
	switch row.Kind {
	case KindDecomposition:
		return 1, true
	case KindSpec:
		return 2, true
	case KindImplementationPlan:
		return 3, true
	case KindTasks:
		return 4, true
	case KindImplementation:
		return 5, true
	case KindDeployToCandidateEnvironment:
		return 6, true
	case KindMergeToMaster:
		return 7, true
	case KindDeployToProduction, KindDeployToEnvironment:
		return 8, true
	default:
		return 0, false
	}
}

// depthOfTarget is the same scale for the stage a reject names, so that "below
// the target" is one comparison. The interview is the earliest thing reachable
// and decomposition the next, both above an item's own stages.
func depthOfTarget(target ReturnsTo) (int, bool) {
	switch target {
	case ReturnsToTheInterview:
		return 0, true
	case ReturnsToDecomposition:
		return 1, true
	case ReturnsToSpec:
		return 2, true
	case ReturnsToImplementationPlan:
		return 3, true
	case ReturnsToTasks:
		return 4, true
	case ReturnsToImplementation:
		return 5, true
	default:
		return 0, false
	}
}

// ApprovalStands reports whether one item's decision at one row stands as
// approved. It is the log read the design's "nothing below the target stands as
// approved" comes to: an approval is superseded by any later reject of that
// item naming a target above the row, so the gates below fire again on what is
// re-authored rather than deciding against work that has been sent back.
//
// It is a read and not a row of its own. A chained record cannot be rewritten,
// and an abandonment is reserved for a decision that will never receive a
// verdict — the approval received one. What says the approval no longer stands
// is the reject that named the target, which is already in the chain, so an
// auditor reads the same fact this does.
//
// The comparison is strictly below the target: the target's own row re-authors
// and fires again over the new version, which is a fresh approval either way.
// A reject at the Decomposition row names no target at all, so it supersedes
// nothing here; what it supersedes is the items themselves.
func (g *Gate) ApprovalStands(ctx context.Context, itemID string, row Row) (bool, error) {
	depth, onThePath := depthOf(row)
	if !onThePath {
		return false, fmt.Errorf("%w: %s is on no item's path", ErrRowUnknown, row)
	}
	closed, err := decisionlog.NewReader(g.pool, g.token).ClosedDecisions(ctx, componentPrincipal(row))
	if err != nil {
		return false, err
	}

	// One entry per close event that moves the answer, ordered by the close and
	// not by the opening: a row opened first can be closed last, and what
	// stands is decided by the order the verdicts were given in.
	type ending struct {
		seq      int64
		approves bool
	}
	var endings []ending
	for _, c := range closed {
		var opening OpeningPayload
		if json.Unmarshal([]byte(c.OpenEvent.Payload), &opening) != nil {
			continue
		}
		if opening.ItemID != itemID {
			continue
		}
		at, err := RowFrom(opening.Gate)
		if err != nil {
			continue
		}
		switch Verdict(c.CloseEvent.Verdict) {
		case VerdictApprove:
			if at == row {
				endings = append(endings, ending{seq: c.CloseEvent.Seq, approves: true})
			}
		case VerdictReject:
			var closing ClosingPayload
			if json.Unmarshal([]byte(c.CloseEvent.Payload), &closing) != nil {
				continue
			}
			target, named := depthOfTarget(closing.ReturnsTo)
			if named && depth > target {
				endings = append(endings, ending{seq: c.CloseEvent.Seq})
			}
		}
	}
	slices.SortFunc(endings, func(a, b ending) int { return int(a.seq - b.seq) })

	stands := false
	for _, e := range endings {
		stands = e.approves
	}
	return stands, nil
}

// eventIsNext refuses an event gate's firing before what fires it has happened:
// the row above approved, and at a merge the candidate's run ended. A firing on
// no item reaches no read: the five rows outside every item have no row above,
// and neither does an artifact gate.
//
// It is checked after the firing's own shape rather than with it, because these
// are facts about the item's progress and not about what the firing named — a
// merge firing missing its build is told that before it is told the run it
// decides has not ended.
func (g *Gate) eventIsNext(ctx context.Context, f Firing) error {
	if f.Row.Kind == KindMergeToMaster && !f.CandidateRunEnded {
		return fmt.Errorf("%w: %s of %s", ErrCandidateRunNotEnded, f.Row, f.ItemID)
	}
	above, waits := RowAbove(f.Row)
	if !waits || f.ItemID == "" {
		return nil
	}
	stands, err := g.ApprovalStands(ctx, f.ItemID, above)
	if err != nil {
		return err
	}
	if !stands {
		return fmt.Errorf("%w: %s of %s", ErrRowAboveNotApproved, above, f.ItemID)
	}
	return nil
}
