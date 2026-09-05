package mergequeue

import (
	"context"
	"fmt"
	"slices"

	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/release"
)

// candidate is one member of a pass with everything read about it before any
// fast-forward happened: the build the Merge to master approval was given over,
// the members the speculation put ahead of it, and what re-verifying it against
// that speculation produced.
type candidate struct {
	it item.Item
	// approved is the build in force at the approval, read before the
	// re-verification writes another one. It is what the two comparisons no
	// criterion reads are made against, and approvedFound is false where the item
	// has no build record — which is not a state the path produces, and which
	// leaves both comparisons with nothing to compare.
	approved      build.Build
	approvedFound bool
	speculation   []item.Item
	verified      Verified
}

// Run takes each member of the service's queue in order and fast-forwards the
// ones that pass. Before anything is minted it reads master against the service's
// release records, both directions, and where that reading finds a commit the
// queue did not make or a commit master no longer holds it mints nothing for the
// service and writes a wait. A standing halt and a reached backlog cap each stop
// the fast-forwarding the same way, each with the exceptions it takes.
//
// Every pending member is re-verified against master plus the members ahead of it
// before any of them fast-forwards, which is the speculation. A failure
// invalidates the speculation behind it, and those candidates re-verify against
// the master that actually resulted, counting nothing — they were redone because
// of somebody else. What that costs is compute on speculative runs a failure
// ahead of them throws away.
//
// The whole run holds one advisory lock per service, so two runs of one service
// cannot interleave. It is session-level and taken on one connection of its own,
// because a run performs git work and several transactions and a transaction held
// across all of it would hold row locks nothing needs held.
func (q *Queue) Run(ctx context.Context, serviceID string) (Pass, error) {
	if serviceID == "" {
		return Pass{}, ErrServiceIDEmpty
	}
	pass := Pass{ServiceID: serviceID}
	unlock, err := q.lock(ctx, serviceID)
	if err != nil {
		return pass, err
	}
	defer unlock()

	// The membership is read under the lock, so an approval landing mid-run joins
	// the next run rather than a run that has already decided its order.
	m, err := q.membership(ctx, serviceID)
	if err != nil {
		return pass, err
	}
	stood, err := q.intentWaits(ctx, m)
	if err != nil {
		return pass, err
	}

	read, completed, held, err := q.readMaster(ctx, serviceID, m.Members)
	if err != nil {
		return pass, err
	}
	pass.Master = read
	pass.Outcomes = append(pass.Outcomes, completed...)
	stood = append(stood, held...)
	if read.Stopped != "" {
		pass.Stopped, pass.StopWaitRow = read.Stopped, read.WaitRow
		// The service is held, so no candidate was reached and neither the halt
		// nor the backlog cap was read: the waits those two stand as are left
		// where they are.
		return pass, q.closeStale(ctx, serviceID, stood, []WaitKind{
			WaitIntentStops, WaitAReleaseNamesACommitMasterDoesNotHold,
			WaitMasterHoldsACommitTheQueueDidNotMake,
		})
	}

	halted, err := q.halted(ctx)
	if err != nil {
		return pass, err
	}
	waiting, err := q.backlog.Behind(ctx, serviceID)
	if err != nil {
		return pass, fmt.Errorf("mergequeue: reading what waits behind a rollback hold on %s: %w", serviceID, err)
	}
	capped := waiting.Standing && waiting.Cap > 0 && waiting.Releases >= waiting.Cap

	// What is left to merge, and what each of them was approved over. A member
	// that already has a release is a fast-forward and a mint that landed and an
	// advance that did not: master holds its commit, so it is finished here rather
	// than re-verified — re-verifying it would mint a second number for one merge
	// — and it is in nobody's speculation.
	var pending []*candidate
	for _, it := range m.Members {
		if it.ID == read.CompletedItemID {
			// The reading of master already completed this one's merge and
			// answered with its outcome, so it is not walked a second time.
			continue
		}
		minted, found, err := release.ForItem(ctx, q.pool, it.ID)
		if err != nil {
			return pass, err
		}
		if found {
			pass.Outcomes = append(pass.Outcomes, Outcome{
				ItemID: it.ID, Merged: true, Release: minted,
				BuildID: minted.BuildID, Commit: minted.Commit,
			})
			continue
		}
		if kind := stopFor(it, m.Intents[it.ID], halted, capped, waiting); kind != "" {
			outcome, subject, err := q.stop(ctx, it, kind, waiting)
			if err != nil {
				return pass, err
			}
			pass.Outcomes = append(pass.Outcomes, outcome)
			stood = append(stood, subject)
			continue
		}
		approved, approvedFound, err := build.Newest(ctx, q.pool, it.ID)
		if err != nil {
			return pass, err
		}
		pending = append(pending, &candidate{it: it, approved: approved, approvedFound: approvedFound})
	}

	// The speculation: every pending member re-verified against master plus every
	// candidate ahead of it, before any of them fast-forwards.
	ahead := make([]item.Item, 0, len(pending))
	for _, c := range pending {
		c.speculation = slices.Clone(ahead)
		c.verified, err = q.repo.Reverify(ctx, c.it, c.speculation)
		if err != nil {
			return pass, fmt.Errorf("mergequeue: re-verifying %s: %w", c.it.ID, err)
		}
		ahead = append(ahead, c.it)
	}

	// The merges, in the queue's order. A candidate whose speculation a failure
	// ahead of it invalidated re-verifies against the master that resulted, and
	// what the discarded run said about it is not read at all.
	merged := make([]item.Item, 0, len(pending))
	for _, c := range pending {
		// Master is read again before every mint, not only at the start: a commit
		// that arrived while this pass was running is one the queue did not make,
		// and the service is held on it rather than merged onto it.
		kind, payload, err := q.beforeMint(ctx, serviceID)
		if err != nil {
			return pass, err
		}
		if kind != "" {
			row, err := q.openWait(ctx, payload)
			if err != nil {
				return pass, err
			}
			pass.Stopped, pass.StopWaitRow = string(kind), row.ID
			stood = append(stood, payload.subject())
			break
		}
		if !sameItems(c.speculation, merged) {
			c.verified, err = q.repo.Reverify(ctx, c.it, slices.Clone(merged))
			if err != nil {
				return pass, fmt.Errorf("mergequeue: re-verifying %s against the master that resulted: %w",
					c.it.ID, err)
			}
		}
		outcome, err := q.mergeOne(ctx, c)
		if err != nil {
			return pass, err
		}
		pass.Outcomes = append(pass.Outcomes, outcome)
		if outcome.Merged {
			merged = append(merged, c.it)
		}
	}
	return pass, q.closeStale(ctx, serviceID, stood, WaitKinds)
}

// sameItems reports whether the speculation a candidate was re-verified against
// is what actually merged ahead of it, which is the one thing that says a
// speculative run may be used as it stands.
func sameItems(speculation, merged []item.Item) bool {
	if len(speculation) != len(merged) {
		return false
	}
	for i := range speculation {
		if speculation[i].ID != merged[i].ID {
			return false
		}
	}
	return true
}

// mergeOne is the queue's whole treatment of one candidate whose re-verification
// has been performed against the master it will actually merge into: the two
// comparisons no criterion reads, then the fast-forward, then the mint with the
// contract versions the release publishes inside its transaction.
func (q *Queue) mergeOne(ctx context.Context, c *candidate) (Outcome, error) {
	replaced, err := q.designSystemMoved(ctx, c)
	if err != nil {
		return Outcome{}, err
	}
	if replaced != "" {
		return q.rejectDesignSystemMove(ctx, c, replaced)
	}
	if !c.verified.Passed {
		return q.reject(ctx, c)
	}
	if c.verified.Commit == "" || c.verified.BuildID == "" {
		return Outcome{}, fmt.Errorf("mergequeue: the re-verification of %s passed and names commit %q, build %q",
			c.it.ID, c.verified.Commit, c.verified.BuildID)
	}
	differs, err := q.resolvedSetDiffers(ctx, c)
	if err != nil {
		return Outcome{}, err
	}
	if differs != "" {
		return q.rejectResolvedSet(ctx, c, differs)
	}

	if err := q.repo.FastForward(ctx, c.it, c.verified.Commit); err != nil {
		return Outcome{}, fmt.Errorf("mergequeue: fast-forwarding master of %s to %s: %w",
			c.it.ServiceID, c.verified.Commit, err)
	}
	return q.mint(ctx, c.it.ServiceID, c.it.ID, c.verified)
}
