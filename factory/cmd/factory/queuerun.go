package main

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/mergequeue"
	"github.com/dulguun0225/borg/factory/service"
)

// runQueue runs the merge queue once for the service and writes what it did onto
// each candidate. The queue reaches the repository and the candidate environments
// through this same value, which is the deployer: [path.Reverify] and
// [path.FastForward] are the two calls it makes.
//
// A merged candidate's environment is torn down here rather than inside the queue.
// Teardown is the deployer's — it stops the software and then writes the time —
// and the queue orders merges and reaches no deploy target.
func (p *path) runQueue(ctx context.Context, svc service.Service) ([]*candidate, error) {
	members, err := p.queue.Members(ctx, svc.ID)
	if err != nil {
		return nil, err
	}
	if len(members) == 0 {
		fmt.Fprintf(p.d.out, "The merge queue of %s is empty; nothing is merged\n", svc.Name)
		return nil, nil
	}
	named := make([]string, 0, len(members))
	for _, it := range members {
		named = append(named, fmt.Sprintf("%s (priority %d)", it.ID, it.Priority))
	}
	fmt.Fprintf(p.d.out, "The merge queue for %s, in order: %v\n", svc.Name, named)

	pass, err := p.queue.Run(ctx, svc.ID)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(p.d.out, "Master of %s is at %s; the newest release in the records names %s\n",
		svc.Name, pass.Master.Head, pass.Master.NewestCommit)
	if pass.Master.CompletedItemID != "" {
		fmt.Fprintf(p.d.out, "  master already held the commit of item %s, so the queue wrote the release its own fast-forward implied\n",
			pass.Master.CompletedItemID)
	}
	if pass.Stopped != "" {
		fmt.Fprintf(p.d.out, "The queue fast-forwarded nothing for %s: %s; wait row %s\n",
			svc.Name, pass.Stopped, pass.StopWaitRow)
	}

	// A member this run did not author is adopted: the queue's membership is the
	// service's, so a run that merges an item another run left queued has to tear its
	// environment down and deploy its release like any other. What is returned is
	// those, for the caller to report and to deploy beside its own.
	var adopted []*candidate
	for _, outcome := range pass.Outcomes {
		c, err := p.candidateFor(ctx, outcome.ItemID)
		if err != nil {
			return adopted, err
		}
		if !p.authored[outcome.ItemID] {
			adopted = append(adopted, c)
			fmt.Fprintf(p.d.out, "Item %s was left in the queue by an earlier run; this one finishes it\n", c.itemID)
		}
		c.reverifiedBuildID = outcome.BuildID
		c.reverifiedCommit = outcome.Commit

		if outcome.Stopped != "" {
			c.factoryHold = outcome.Stopped
			c.holdWaitRow = outcome.WaitRow
			fmt.Fprintf(p.d.out, "Item %s is held at the queue: %s; wait row %s\n",
				outcome.ItemID, outcome.Stopped, outcome.WaitRow)
			continue
		}
		if !outcome.Merged {
			c.queueRejected = true
			c.queueWhy = outcome.Why
			c.queueWaitRow = outcome.Rejection.Row
			// The item's own transition is the composition's write: the queue's row
			// in components.md names no dispatch, so what a rejection causes on the
			// item is written here and the rejection says what it is.
			if err := p.returnRejected(ctx, outcome); err != nil {
				return adopted, err
			}
			continue
		}
		c.merged = true
		c.releaseID = outcome.Release.ID
		c.releaseNumber = outcome.Release.Number
		c.published = outcome.Published
		if _, err := p.dispatch.End(ctx, mergequeue.Actor, outcome.ItemID); err != nil {
			return adopted, err
		}
		fmt.Fprintf(p.d.out, "Master fast-forwarded to %s; release %s minted, number %d; item %s is merged\n",
			outcome.Commit, outcome.Release.ID, outcome.Release.Number, outcome.ItemID)
		for _, skipped := range outcome.SkippedNumbers {
			fmt.Fprintf(p.d.out, "  number %d was passed over: the health monitor's store names it and the records do not\n", skipped)
		}
		for _, published := range outcome.Published {
			switch {
			case published.Created && published.Moved:
				fmt.Fprintf(p.d.out, "  contract %s created and published at %s: %s\n",
					published.Contract.Name, published.Version.Semver, published.Change.Describe())
			case published.Moved:
				fmt.Fprintf(p.d.out, "  contract %s published at %s: %s\n",
					published.Contract.Name, published.Version.Semver, published.Change.Describe())
			default:
				fmt.Fprintf(p.d.out, "  contract %s is unchanged, so this release publishes no version of it\n",
					published.Contract.Name)
			}
		}
		if err := p.tearDown(ctx, c); err != nil {
			return adopted, err
		}
	}
	return adopted, nil
}

// returnRejected is what a rejection causes on the item: it goes back to the
// stage the rejection names, with an attempt counted there. The queue writes
// neither — its row in components.md names the gate component, the build runner
// and the log and names no dispatch — so the transition is the composition's,
// and the actor is the queue, which is what the rejection row already says.
//
// The attempt is not incremented here either: an attempt is counted when a
// stage is entered to author, so what this does is send the item back to be
// entered again. The rejection's CountsAnAttempt is what says it will be, and
// it is on the row for a reader.
func (p *path) returnRejected(ctx context.Context, outcome mergequeue.Outcome) error {
	r := outcome.Rejection
	fmt.Fprintf(p.d.out, "The queue rejected item %s on its own merits: %s\n", outcome.ItemID, outcome.Why)
	fmt.Fprintf(p.d.out, "  it read the failure as %s, and learns from it as from a %s (the per-author prior moves: %v)\n",
		r.Reading, r.LearnsAs, r.PriorMoves)
	for _, moved := range r.Moved {
		fmt.Fprintf(p.d.out, "  %s moved from %s to %s\n", moved.What, moved.From, moved.To)
	}
	if r.ReturnsTo == "" {
		fmt.Fprintf(p.d.out, "  rejection row %s written as the queue; the item is sent back to nothing\n", r.Row)
		return nil
	}
	if _, err := p.dispatch.ReturnTo(ctx, mergequeue.Actor, outcome.ItemID, item.Stage(r.ReturnsTo)); err != nil {
		return err
	}
	fmt.Fprintf(p.d.out, "  rejection row %s written as the queue; the item is back at %s with an attempt counted there, and keeps its environment\n",
		r.Row, r.ReturnsTo)
	return nil
}

// candidateFor is the candidate of one item: the one this run authored where it
// authored it, and one read from the store where it did not. It is the one place
// that reads it, so the queue's outcome loop and the re-verification cannot disagree
// about whether an item is the run's.
//
// A candidate read from the store is what makes the queue the service's rather than
// the run's: a run that finds an item another one left queued finishes it, and
// everything finishing it needs is a record — the item for its branch, the
// environment for the directory its build ran in.
func (p *path) candidateFor(ctx context.Context, itemID string) (*candidate, error) {
	if c := p.byItem[itemID]; c != nil {
		return c, nil
	}
	it, err := item.Get(ctx, p.d.pool, itemID)
	if err != nil {
		return nil, err
	}
	svc, err := p.serviceOf(ctx, it.ServiceID)
	if err != nil {
		return nil, err
	}
	c := &candidate{itemID: itemID, intentID: it.IntentID, svc: svc, branch: it.Branch, waitsOn: it.WaitsOn}
	env, found, err := environment.ForItem(ctx, p.d.pool, itemID)
	if err != nil {
		return nil, err
	}
	if found && len(env.Targets) > 0 {
		c.environmentID = env.ID
		c.environmentDir = env.Targets[0].Address
		c.tornDown = !env.Live()
	}
	p.byItem[itemID] = c
	return c, nil
}

// tearDown stops the software on the candidate's environment and writes the time
// it was torn down. The row is kept: the deploy records naming it would otherwise
// point at nothing. Stopping comes first, so a record saying torn down never
// stands over a process still running.
func (p *path) tearDown(ctx context.Context, c *candidate) error {
	if c.environmentID == "" || c.tornDown {
		return nil
	}
	if err := p.d.targets.at(c.environmentDir).Stop(ctx, deployerPrincipal, c.svc.Name, p.d.credential); err != nil {
		return err
	}
	if err := p.candidates.TearDown(ctx, deployActor, c.environmentID, environment.ReasonMerged, environment.Rate{}); err != nil {
		return err
	}
	c.tornDown = true
	fmt.Fprintf(p.d.out, "Candidate environment %s torn down; the record is kept\n", c.environmentID)
	return nil
}
