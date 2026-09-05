package main

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/mergequeue"
)

// Reverify is the queue's re-verification, and it is everything the deploy agent
// does for one: master merged into the candidate branch, a build written for the
// commit that produced, the environment recomposed, that build put on it, and the
// criteria decided there.
//
// A candidate that failed on its merits comes back as a [mergequeue.Verified] that
// did not pass, with the reason on it — a merge that conflicts, a tree that no
// longer compiles, an encoding that is missing after the merge, and a criterion
// that failed are all that. What comes back as an error is infrastructure: a
// repository that cannot be read, a record that cannot be written.
func (p *path) Reverify(ctx context.Context, it item.Item) (mergequeue.Verified, error) {
	// The queue's membership is every item of the service at the queued stage, which
	// is not the same set as the candidates this run authored: a run that stopped
	// after one Merge to master gate approved leaves an item there, and the next run's queue
	// has it. Everything a re-verification needs is a record, so a candidate the run
	// does not hold is read from the store rather than refused.
	c, err := p.candidateFor(ctx, it.ID)
	if err != nil {
		return mergequeue.Verified{}, err
	}
	if c.environmentID == "" || c.tornDown {
		return mergequeue.Verified{}, fmt.Errorf(
			"factory: item %s is in the queue and has no live candidate environment to re-verify on", it.ID)
	}

	repo := c.svc.Repository
	if _, err := git(repo, "switch", it.Branch); err != nil {
		return mergequeue.Verified{}, err
	}

	head, err := p.masterHead(ctx, c.svc)
	if err != nil {
		return mergequeue.Verified{}, err
	}
	if head != "" {
		// Unrelated histories are allowed rather than special-cased. A candidate decomposed
		// before the first release has no base, so two of them share no commit and
		// this merge has two roots; the flag does nothing where the histories are
		// related. Where the two trees then conflict, the candidate failed its own
		// re-verification, which is the right answer and not a workaround.
		out, err := git(repo, "merge", "--allow-unrelated-histories",
			"-m", "merge master into "+it.Branch+" for re-verification", "master")
		if err != nil {
			// The abort is what leaves the branch where it was. A merge that failed
			// before it started nothing to abort, so its error is discarded.
			_, _ = git(repo, "merge", "--abort")
			return mergequeue.Verified{Why: "merging master into the candidate branch failed: " + firstLines(out)}, nil
		}
	}
	commit, err := git(repo, "rev-parse", "HEAD")
	if err != nil {
		return mergequeue.Verified{}, err
	}

	// A rebuild is a new build, and a re-verification that changed nothing rebuilt
	// nothing: where the commit is the one already built for this item, that build
	// is the one the release will name.
	bl, found, err := build.ForCommit(ctx, p.d.pool, it.ID, c.svc.ID, commit)
	if err != nil {
		return mergequeue.Verified{}, err
	}
	if !found {
		bl, err = p.createBuild(ctx, repo, it.ID, c.svc.ID, commit)
		if err != nil {
			return mergequeue.Verified{}, err
		}
		fmt.Fprintf(p.d.out, "Re-verification of item %s: build %s made from commit %s\n", it.ID, bl.ID, commit)
	} else {
		fmt.Fprintf(p.d.out, "Re-verification of item %s: master was already in the branch, so build %s stands\n", it.ID, bl.ID)
	}

	composed, err := p.compositionFor(ctx, it)
	if err != nil {
		return mergequeue.Verified{}, err
	}
	if err := p.candidates.Recompose(ctx, deployActor, c.environmentID, environment.Composition{From: composed}); err != nil {
		return mergequeue.Verified{}, err
	}
	c.composedFrom = composed

	if err := buildInto(repo, c.environmentDir, bl.ID); err != nil {
		return mergequeue.Verified{Commit: commit, BuildID: bl.ID,
			Why: "the tree does not compile with master merged into it: " + firstLines(err.Error())}, nil
	}
	dep, err := deploy.WithoutControl(ctx, p.deploys, p.d.targets.at(c.environmentDir), deployActor,
		c.svc.ID, c.svc.Name, c.environmentID, deploy.OfBuild(bl.ID), p.d.credential)
	if err != nil {
		return mergequeue.Verified{}, err
	}
	c.candidateDeployID = dep.ID

	inForce, err := p.inForceFor(ctx, c.svc, it.ID)
	if err != nil {
		return mergequeue.Verified{}, err
	}
	if err := p.checkEncodings(ctx, repo, c.svc.ID, it.ID, inForce); err != nil {
		return mergequeue.Verified{Commit: commit, BuildID: bl.ID,
			Why: "the criteria and the encodings do not match with master merged in: " + firstLines(err.Error())}, nil
	}
	results, err := p.decideCriteria(ctx, c, bl.ID, inForce)
	if err != nil {
		return mergequeue.Verified{}, err
	}
	c.criteria = results

	// The measurement is re-taken, because this is a different build against a
	// master that has moved: the production deploy row fires over it, and a vector
	// computed from the earlier diff would not be this build's. What is not
	// overwritten is the build and the commit the implementation stage made — a
	// rebuild is a new build, and the item has as many builds as it was built.
	c.measurement = measure(repo, head != "")

	for _, result := range results {
		if result.Outcome.Blocks(false) {
			return mergequeue.Verified{Commit: commit, BuildID: bl.ID,
				Why: fmt.Sprintf("criterion %s is %s against build %s", result.CriterionID, result.Outcome, bl.ID)}, nil
		}
	}

	// The contract checks again, against the master that actually resulted. Both
	// baselines are computed at the moment they are read and neither is recorded, so
	// the queue rejects on the same terms with itself as the actor — which is how a
	// baseline that moved while the candidate waited is caught rather than passed.
	//
	// The forms come back on the verified value: the queue writes the contract and
	// its version inside the mint's transaction, and it reaches no checkout, so what
	// the derivation read here is what it writes.
	checked, err := p.enforceContracts(ctx, c, bl.ID)
	if err != nil {
		return mergequeue.Verified{}, err
	}
	if check := checked.Check(); check != "" {
		return mergequeue.Verified{Commit: commit, BuildID: bl.ID,
			Why: check + " with master merged in: " + checked.Why()}, nil
	}
	return mergequeue.Verified{Commit: commit, BuildID: bl.ID, Passed: true, Forms: checked.Publishes}, nil
}

// FastForward moves master to the commit the re-verification produced. It is one
// command that creates master at that commit or fast-forwards it to there, and
// refuses anything else — which is what makes the commit that merges the commit
// that was verified.
func (p *path) FastForward(ctx context.Context, it item.Item, commit string) error {
	svc, err := p.serviceOf(ctx, it.ServiceID)
	if err != nil {
		return err
	}
	_, err = git(svc.Repository, "fetch", ".", commit+":master")
	return err
}
