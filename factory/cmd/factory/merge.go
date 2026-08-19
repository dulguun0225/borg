package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/mergequeue"
)

// mergeGate is the Merge to master row: where the verdict on the candidate is
// given. What it reads is the candidate's own run — the acceptance criteria
// decided against the candidate environment, undecided read the way a failure is.
// Approving admits the candidate to the merge queue, which is the stage the item
// advances to; rejecting sends the item back with an attempt counted where it goes.
func (p *path) mergeGate(ctx context.Context, c *candidate) error {
	if c.environmentID == "" {
		fmt.Fprintf(p.d.out, "Item %s reached no candidate environment, so its merge gate does not fire\n", c.itemID)
		return nil
	}

	opened, err := p.gate.Fire(ctx, gate.Firing{
		Row:             gate.MergeToMaster,
		ItemID:          c.itemID,
		BuildID:         c.buildID,
		ArtifactID:      c.implArtifactID,
		ServiceID:       p.svc.ID,
		AreaID:          p.areaID,
		EnvironmentID:   p.production.ID,
		CriteriaInForce: len(c.criteria),
		Criteria:        c.criteria,
		Measurement:     c.measurement,
	})
	if err != nil {
		return err
	}
	report(p.d.out, opened, c.criteria)
	verdict, feedback, closing, err := p.settle(ctx, opened)
	if err != nil {
		return err
	}
	c.mergeGate = recordFiring(opened, closing)
	if verdict == gate.VerdictReject {
		c.rejected = true
		if _, err := p.dispatch.SendBack(ctx, p.human, c.itemID, item.StageImplementation); err != nil {
			return err
		}
		fmt.Fprintf(p.d.out, "Rejected: %s\nItem %s goes back to %s with an attempt counted there, and keeps its environment\n",
			feedback, c.itemID, gate.ReturnsTo)
		return nil
	}

	if _, err := p.dispatch.Advance(ctx, dispatchActor, c.itemID, item.StageQueued); err != nil {
		return err
	}
	c.queued = true
	fmt.Fprintf(p.d.out, "Approved; item %s is in the merge queue\n", c.itemID)
	return nil
}

// runQueue runs the merge queue once for the service and writes what it did onto
// each candidate. The queue reaches the repository and the candidate environments
// through this same value, which is the deploy agent: [path.Reverify] and
// [path.FastForward] are the two calls it makes.
//
// A merged candidate's environment is torn down here rather than inside the queue.
// Teardown is the deploy agent's — it stops the software and then writes the time —
// and the queue orders merges and reaches no deploy target.
func (p *path) runQueue(ctx context.Context) ([]*candidate, error) {
	members, err := p.queue.Members(ctx, p.svc.ID)
	if err != nil {
		return nil, err
	}
	if len(members) == 0 {
		fmt.Fprintln(p.d.out, "The merge queue is empty; nothing is merged")
		return nil, nil
	}
	named := make([]string, 0, len(members))
	for _, it := range members {
		named = append(named, fmt.Sprintf("%s (priority %d)", it.ID, it.Priority))
	}
	fmt.Fprintf(p.d.out, "The merge queue for %s, in order: %v\n", p.svc.ID, named)

	outcomes, err := p.queue.Run(ctx, p.svc.ID)
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
		fmt.Fprintf(p.d.out, "Master fast-forwarded to %s; release %s minted, number %d\n",
			outcome.Commit, outcome.Release.ID, outcome.Release.Number)
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
	c := &candidate{itemID: itemID, intentID: it.IntentID, branch: it.Branch}
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
	if err := p.d.targets.at(c.environmentDir).Stop(ctx, p.svc.Name, p.d.credential); err != nil {
		return err
	}
	if err := p.candidates.TearDown(ctx, c.environmentID); err != nil {
		return err
	}
	c.tornDown = true
	fmt.Fprintf(p.d.out, "Candidate environment %s torn down; the record is kept\n", c.environmentID)
	return nil
}

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
	// after one merge gate approved leaves an item there, and the next run's queue
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

	if _, err := git(p.d.repo, "switch", it.Branch); err != nil {
		return mergequeue.Verified{}, err
	}

	head, err := p.masterHead(ctx)
	if err != nil {
		return mergequeue.Verified{}, err
	}
	if head != "" {
		// Unrelated histories are allowed rather than special-cased. A candidate cut
		// before the first release has no base, so two of them share no commit and
		// this merge has two roots; the flag does nothing where the histories are
		// related. Where the two trees then conflict, the candidate failed its own
		// re-verification, which is the right answer and not a workaround.
		out, err := git(p.d.repo, "merge", "--allow-unrelated-histories",
			"-m", "merge master into "+it.Branch+" for re-verification", "master")
		if err != nil {
			// The abort is what leaves the branch where it was. A merge that failed
			// before it started nothing to abort, so its error is discarded.
			_, _ = git(p.d.repo, "merge", "--abort")
			return mergequeue.Verified{Why: "merging master into the candidate branch failed: " + firstLines(out)}, nil
		}
	}
	commit, err := git(p.d.repo, "rev-parse", "HEAD")
	if err != nil {
		return mergequeue.Verified{}, err
	}

	// A rebuild is a new build, and a re-verification that changed nothing rebuilt
	// nothing: where the commit is the one already built for this item, that build
	// is the one the release will name.
	bl, found, err := build.ForCommit(ctx, p.d.pool, it.ID, commit)
	if err != nil {
		return mergequeue.Verified{}, err
	}
	if !found {
		bl, err = p.builds.Create(ctx, buildActor, it.ID, commit)
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
	if err := p.candidates.Recompose(ctx, c.environmentID, composed); err != nil {
		return mergequeue.Verified{}, err
	}
	c.composedFrom = composed

	if err := buildInto(p.d.repo, c.environmentDir, bl.ID); err != nil {
		return mergequeue.Verified{Commit: commit, BuildID: bl.ID,
			Why: "the tree does not compile with master merged into it: " + firstLines(err.Error())}, nil
	}
	dep, err := deploy.Straight(ctx, p.deploys, p.d.targets.at(c.environmentDir), deployActor,
		p.svc.ID, p.svc.Name, c.environmentID, deploy.OfBuild(bl.ID), p.d.credential)
	if err != nil {
		return mergequeue.Verified{}, err
	}
	c.candidateDeployID = dep.ID

	inForce, err := p.inForceFor(ctx, it.ID)
	if err != nil {
		return mergequeue.Verified{}, err
	}
	if err := criterion.CheckEncodings(p.d.repo, inForce); err != nil {
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
	c.measurement = measure(p.d.repo, head != "")

	for _, result := range results {
		if result.Outcome.Blocks() {
			return mergequeue.Verified{Commit: commit, BuildID: bl.ID,
				Why: fmt.Sprintf("criterion %s is %s against build %s", result.CriterionID, result.Outcome, bl.ID)}, nil
		}
	}
	return mergequeue.Verified{Commit: commit, BuildID: bl.ID, Passed: true}, nil
}

// FastForward moves master to the commit the re-verification produced. It is one
// command that creates master at that commit or fast-forwards it to there, and
// refuses anything else — which is what makes the commit that merges the commit
// that was verified.
func (p *path) FastForward(ctx context.Context, _ item.Item, commit string) error {
	_, err := git(p.d.repo, "fetch", ".", commit+":master")
	return err
}

// productionDeploy is the Deploy to production row: the last row before a release
// takes traffic, and the one that offers hold and no reject. Its own hold is
// computed first and writes nothing, the same dependency check the candidate
// deploy row made, asked now about whether the dependency is live still.
func (p *path) productionDeploy(ctx context.Context, c *candidate) error {
	d := p.d
	it, err := item.Get(ctx, d.pool, c.itemID)
	if err != nil {
		return err
	}
	held, err := p.dependencyHold(ctx, it)
	if err != nil {
		return err
	}
	if held != "" {
		c.factoryHold = held
		fmt.Fprintf(d.out, "Release %s waits at %s: %s\n", c.releaseID, gate.DeployToProduction, held)
		return nil
	}

	opened, err := p.gate.Fire(ctx, gate.Firing{
		Row:             gate.DeployToProduction,
		ItemID:          c.itemID,
		BuildID:         c.reverifiedBuildID,
		ServiceID:       p.svc.ID,
		AreaID:          p.areaID,
		EnvironmentID:   p.production.ID,
		CriteriaInForce: len(c.criteria),
		Criteria:        c.criteria,
		Measurement:     c.measurement,
	})
	if err != nil {
		return err
	}
	report(d.out, opened, c.criteria)
	verdict, _, closing, err := p.settle(ctx, opened)
	if err != nil {
		return err
	}
	c.deployGate = recordFiring(opened, closing)
	if verdict == gate.VerdictHold {
		c.held = true
		fmt.Fprintf(d.out, "Held; release %s is minted and is not deployed, and the event stays queued\n", c.releaseID)
		fmt.Fprintf(d.out, "No attempt is counted and the score learns nothing from a hold; item %s stays where it is\n", c.itemID)
		return nil
	}

	// The binary the re-verification built is where it ran, which is the candidate
	// environment's directory. Copying it is what puts the verified build on
	// production rather than compiling the same commit a second time.
	if err := copyFile(filepath.Join(c.environmentDir, c.reverifiedBuildID),
		filepath.Join(d.dir, c.reverifiedBuildID)); err != nil {
		return err
	}
	dep, err := deploy.Straight(ctx, p.deploys, d.targets.at(d.dir), deployActor,
		p.svc.ID, p.svc.Name, p.production.ID,
		deploy.OfRelease(c.releaseID, c.reverifiedBuildID), d.credential)
	if err != nil {
		return err
	}
	c.deployID = dep.ID
	fmt.Fprintf(d.out, "Deploy %s complete: release %s runs in production\n", dep.ID, c.releaseID)
	return nil
}
