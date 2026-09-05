package main

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/service"
)

// runQueue runs the merge queue once for the service and writes what it did onto
// each candidate. The queue reaches the repository and the candidate environments
// through this same value, which is the deploy agent: [path.Reverify] and
// [path.FastForward] are the two calls it makes.
//
// A merged candidate's environment is torn down here rather than inside the queue.
// Teardown is the deploy agent's — it stops the software and then writes the time —
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

	outcomes, err := p.queue.Run(ctx, svc.ID)
	if err != nil {
		return nil, err
	}
	// A member this run did not author is adopted: the queue's membership is the
	// service's, so a run that merges an item another run left queued has to tear its
	// environment down and deploy its release like any other. What is returned is
	// those, for the caller to report and to deploy beside its own.
	var adopted []*candidate
	for _, outcome := range outcomes {
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
		if !outcome.Merged {
			c.queueRejected = true
			c.queueWhy = outcome.Why
			c.queueWaitRow = outcome.WaitRow
			fmt.Fprintf(p.d.out, "The queue rejected item %s on its own merits: %s\n", outcome.ItemID, outcome.Why)
			fmt.Fprintf(p.d.out, "  wait row %s written as the queue; the item is back at %s with an attempt counted there, and keeps its environment\n",
				outcome.WaitRow, gate.ReturnsTo)
			continue
		}
		c.merged = true
		c.releaseID = outcome.Release.ID
		c.releaseNumber = outcome.Release.Number
		c.published = outcome.Published
		fmt.Fprintf(p.d.out, "Master fast-forwarded to %s; release %s minted, number %d\n",
			outcome.Commit, outcome.Release.ID, outcome.Release.Number)
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
		c.environmentDir = env.Targets[0]
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
	if err := p.d.targets.at(c.environmentDir).Stop(ctx, c.svc.Name, p.d.credential); err != nil {
		return err
	}
	if err := p.candidates.TearDown(ctx, c.environmentID); err != nil {
		return err
	}
	c.tornDown = true
	fmt.Fprintf(p.d.out, "Candidate environment %s torn down; the record is kept\n", c.environmentID)
	return nil
}
