package mergequeue

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/build"
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

	m, err := q.membership(ctx, serviceID)
	if err != nil {
		return Master{}, nil, err
	}
	read, completed, _, err := q.readMaster(ctx, serviceID, m.Members)
	return read, completed, err
}

// readMaster is that reading. Master's head being the commit the service's
// newest release names is the ordinary case. The two readings that stop the
// service are a commit master does not hold and a commit the queue did not put
// there, and between them is the one the queue completes: a commit past the
// newest release's that a build of a candidate approved at Merge to master
// names, which is the queue's own unfinished merge.
//
// A service with no release yet is compared against nothing: what master holds
// before the first merge is whatever created the repository, and no record says
// otherwise.
func (q *Queue) readMaster(ctx context.Context, serviceID string, members []item.Item) (Master, []Outcome, []subject, error) {
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
	// the records hold. It is told from a commit the queue did not make by the
	// records and not by the queue's memory of its work.
	for _, it := range members {
		made, found, err := build.ForCommit(ctx, q.pool, it.ID, serviceID, head)
		if err != nil {
			return read, nil, nil, err
		}
		if !found {
			continue
		}
		completed, err := q.complete(ctx, it, made, head)
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
// the point anything may be sent back from.
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

// beforeMint is the same reading again, made before every mint rather than only
// at the start: master's head being the commit the service's newest release names
// is what a mint is made against, and a commit that arrived on master since this
// pass began is one the queue did not make. It answers with the wait kind that
// holds the service and the payload that wait stands as, and with an empty kind
// where the reading found master and the records agreeing.
//
// A pass performs its own fast-forwards and mints each one before it reaches the
// next candidate, so inside one pass this reading disagrees only where something
// outside the queue moved master while the pass was running.
func (q *Queue) beforeMint(ctx context.Context, serviceID string) (WaitKind, WaitPayload, error) {
	newest, found, err := release.Highest(ctx, q.pool, serviceID)
	if err != nil {
		return "", WaitPayload{}, err
	}
	if !found {
		return "", WaitPayload{}, nil
	}
	holds, err := q.repo.Holds(ctx, serviceID, newest.Commit)
	if err != nil {
		return "", WaitPayload{}, fmt.Errorf("mergequeue: reading whether master of %s holds %s: %w",
			serviceID, newest.Commit, err)
	}
	if !holds {
		return WaitAReleaseNamesACommitMasterDoesNotHold, WaitPayload{
			Kind: WaitAReleaseNamesACommitMasterDoesNotHold, ServiceID: serviceID,
			ReleaseID: newest.ID, Commit: newest.Commit,
		}, nil
	}
	head, err := q.repo.Head(ctx, serviceID)
	if err != nil {
		return "", WaitPayload{}, fmt.Errorf("mergequeue: reading master of %s: %w", serviceID, err)
	}
	if head != newest.Commit {
		return WaitMasterHoldsACommitTheQueueDidNotMake, WaitPayload{
			Kind: WaitMasterHoldsACommitTheQueueDidNotMake, ServiceID: serviceID, Commit: head,
		}, nil
	}
	return "", WaitPayload{}, nil
}
