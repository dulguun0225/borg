package mergequeue

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/release"
)

// Master is what one reading of master against the service's release records
// found. The queue makes it at every start and before every mint, because the
// two systems move at one event with nothing spanning them: a queue that stops
// between the fast-forward and the record write leaves the records one commit
// behind master, and a restore of the records behind git leaves them one release
// ahead of it.
type Master struct {
	// Head is master's head, read from the version control system that holds it.
	Head string
	// NewestReleaseID and NewestCommit are the service's newest release and the
	// commit it names, and are empty where the service has no release yet.
	NewestReleaseID string
	NewestCommit    string
	// Stopped is the wait kind that holds the whole service, and is empty where
	// the reading found master and the records agreeing. WaitRow is the log row
	// that stop stands as.
	Stopped string
	WaitRow string
	// CompletedItemID is the item whose unfinished merge this reading completed:
	// master holds the commit and the release record did not, so the queue wrote
	// what the fast-forward already implied. It is empty at every other pass.
	CompletedItemID string
}

// Restart is the queue's restart: it reads master against the service's
// release records and writes the release record its own unfinished merge left
// owing. It is the read [Queue.Run] makes before it mints, performed on its
// own so that a start makes it without fast-forwarding anything — a start is a
// read of the queue's own records and not a pass of the queue.
//
// It takes the same advisory lock a run does, so a start and a run of one
// service cannot interleave.
func (q *Queue) Restart(ctx context.Context, serviceID string) (Master, []Outcome, error) {
	if serviceID == "" {
		return Master{}, nil, ErrServiceIDEmpty
	}
	unlock, err := q.lock(ctx, serviceID)
	if err != nil {
		return Master{}, nil, err
	}
	defer unlock()

	read, completed, _, err := q.readMaster(ctx, serviceID)
	return read, completed, err
}

// readMaster is that reading, made at the start of a run, before every mint —
// [beforeMint's former, separate implementation is gone; this is now the one
// function both call — and at a restart, so the two never drift apart. Master's
// head being the commit the service's newest release names is the ordinary
// case. The two readings that stop the service are a commit master does not
// hold and a commit the queue did not put there, and between them is the one
// the queue completes: a commit past the newest release's that a build of a
// candidate approved at Merge to master names, which is the queue's own
// unfinished merge.
//
// The builds compared are every build the records hold naming this service and
// this commit, through [build.ForServiceCommit] — not the ordered and
// intent-filtered membership a pass computed for itself, and not only the
// builds of an item still at [item.StageQueued]: an item the queue itself sent
// back, or one a caller has already advanced past queued, still explains the
// commit if its build names it and it was once approved at Merge to master.
// That approval is read through [gate.ApprovalTimes] over [gate.MergeToMaster]
// — the log's own record of the approval, which stands whatever stage the item
// is at now; the item's own stage row does not, because [item.Dispatch.Advance]
// counts an attempt only at an authoring stage and queued is not one, so no
// per-stage row survives an item sent back past it.
//
// An item the intent's state stops is not completed even where its build
// names the commit: its unfinished merge stands as a wait instead, the way the
// halt's does — nothing is decided, and the record write is left owing until
// the state clears.
//
// A service with no release yet is compared against nothing: what master holds
// before the first merge is whatever created the repository, and no record says
// otherwise.
func (q *Queue) readMaster(ctx context.Context, serviceID string) (Master, []Outcome, []subject, error) {
	head, err := q.repo.Head(ctx, serviceID)
	if err != nil {
		return Master{}, nil, nil, fmt.Errorf("mergequeue: reading master of %s: %w", serviceID, err)
	}
	read := Master{Head: head}

	newest, found, err := release.Highest(ctx, q.pool, serviceID)
	if err != nil {
		return read, nil, nil, err
	}
	if !found {
		return read, nil, nil, nil
	}
	read.NewestReleaseID, read.NewestCommit = newest.ID, newest.Commit

	holds, err := q.repo.Holds(ctx, serviceID, newest.Commit)
	if err != nil {
		return read, nil, nil, fmt.Errorf("mergequeue: reading whether master of %s holds %s: %w",
			serviceID, newest.Commit, err)
	}
	if !holds {
		// Git restored behind the graph: the members of the recovery unit landed
		// apart. The wait ends only when master holds the commit again, from the
		// repository restored to a later point or from any clone that holds it,
		// because a release is written once and cannot be unwritten to match.
		payload := WaitPayload{
			Kind: WaitAReleaseNamesACommitMasterDoesNotHold, ServiceID: serviceID,
			ReleaseID: newest.ID, Commit: newest.Commit,
		}
		row, err := q.openWait(ctx, payload)
		if err != nil {
			return read, nil, nil, err
		}
		read.Stopped, read.WaitRow = string(payload.Kind), row.ID
		return read, nil, []subject{payload.subject()}, nil
	}
	if head == newest.Commit {
		return read, nil, nil, nil
	}

	// A commit on master past the newest release's, compared against the builds
	// the records hold — every build naming this service and this commit,
	// whatever item it names and whatever stage that item is at now.
	made, err := build.ForServiceCommit(ctx, q.pool, serviceID, head)
	if err != nil {
		return read, nil, nil, err
	}
	// approved is read at most once per reading, and only where a build names an
	// item at all: [gate.ApprovalTimes] reads the whole log, and a service with no
	// item-bound build at this commit has no need of it.
	var approved map[string]string
	for _, b := range made {
		if b.ItemID == "" {
			// A search build names a service and no item, and decides nothing at
			// Merge to master.
			continue
		}
		if approved == nil {
			approved, err = gate.ApprovalTimes(ctx, q.pool, q.token, componentPrincipal, gate.MergeToMaster)
			if err != nil {
				return read, nil, nil, err
			}
		}
		if _, ok := approved[b.ItemID]; !ok {
			continue
		}
		it, err := item.Get(ctx, q.pool, b.ItemID)
		if err != nil {
			return read, nil, nil, err
		}
		in, err := intent.Get(ctx, q.pool, it.IntentID)
		if err != nil {
			return read, nil, nil, fmt.Errorf("mergequeue: reading the intent of %s: %w", it.ID, err)
		}
		if stops(in.State) {
			// Its unfinished merge stands as a wait, the way the halt's does:
			// nothing is decided, and the record write this commit already
			// implies is left owing until the state clears.
			payload := WaitPayload{
				Kind: WaitIntentStops, ServiceID: serviceID, ItemID: it.ID, IntentState: string(in.State),
			}
			row, err := q.openWait(ctx, payload)
			if err != nil {
				return read, nil, nil, err
			}
			read.Stopped, read.WaitRow = string(payload.Kind), row.ID
			return read, nil, []subject{payload.subject()}, nil
		}
		completed, err := q.complete(ctx, it, b, head)
		if err != nil {
			return read, nil, nil, err
		}
		read.CompletedItemID = it.ID
		return read, []Outcome{completed}, nil, nil
	}

	// A commit the queue did not put there: a human's push, an agent's, or a
	// merge whose records a restore lost. The queue mints nothing for the service
	// and writes the wait, which has no holder, so it widens to the owner and
	// pages nobody — production is no worse for it, and what it stops is work.
	payload := WaitPayload{Kind: WaitMasterHoldsACommitTheQueueDidNotMake, ServiceID: serviceID, Commit: head}
	row, err := q.openWait(ctx, payload)
	if err != nil {
		return read, nil, nil, err
	}
	read.Stopped, read.WaitRow = string(payload.Kind), row.ID
	return read, nil, []subject{payload.subject()}, nil
}

// complete is the write the fast-forward already implied: the commit is on
// master and no release names it, so the queue mints one in master's order.
//
// The re-verification is asked again for the contract versions the release
// publishes — master is already an ancestor of the candidate, so it names the
// build already in force and rebuilds nothing — and it is asked for the forms
// and not for a verdict: the commit is on master, so the item is merged and past
// the point anything may be sent back from. Nothing here decides pass or fail,
// so [ErrReverificationRepeats] is never asked of it.
func (q *Queue) complete(ctx context.Context, it item.Item, made build.Build, head string) (Outcome, error) {
	verified, err := q.repo.Reverify(ctx, it, nil)
	if err != nil {
		return Outcome{}, fmt.Errorf("mergequeue: completing the merge of %s: %w", it.ID, err)
	}
	if verified.Commit != head || verified.BuildID != made.ID {
		return Outcome{}, fmt.Errorf(
			"mergequeue: completing the merge of %s: master holds %s built as %s, and the re-verification names %s built as %s",
			it.ID, head, made.ID, verified.Commit, verified.BuildID)
	}
	return q.mint(ctx, it.ServiceID, it.ID, verified)
}
