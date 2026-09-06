package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/mergequeue"
)

// Reverify is the queue's re-verification, and it is everything the deployer
// does for one: master merged into the candidate branch, a build written for the
// commit that produced, the environment recomposed, that build put on it, and the
// criteria decided there.
//
// A candidate that failed on its merits comes back as a [mergequeue.Verified] that
// did not pass, with the reason on it — a merge that conflicts, a tree that no
// longer compiles, an encoding that is missing after the merge, and a criterion
// that failed are all that. What comes back as an error is infrastructure: a
// repository that cannot be read, a record that cannot be written.
func (p *path) Reverify(ctx context.Context, it item.Item, ahead []item.Item) (mergequeue.Verified, error) {
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

	// Master as the repository has it, not as the records say: the queue read the
	// two against each other before it asked for this and stopped the service
	// where they disagreed, so a second comparison here would refuse a
	// re-verification the queue has already admitted.
	head, err := masterCommit(c.svc.Repository)
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
	// The speculation: every candidate ahead of this one in the queue's order,
	// merged into this branch after master, so what is verified is this candidate
	// against the master that would result if those merged first. A conflict with
	// one of them is this candidate failing its own re-verification, the same
	// disposition a conflict with master is.
	for _, one := range ahead {
		out, err := git(repo, "merge", "--allow-unrelated-histories",
			"-m", "merge "+one.Branch+" into "+it.Branch+" for the speculation", one.Branch)
		if err != nil {
			_, _ = git(repo, "merge", "--abort")
			return mergequeue.Verified{Why: "merging " + one.Branch +
				", which is ahead of this candidate in the queue, failed: " + firstLines(out)}, nil
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
	dep, err := p.intoCandidate(ctx, c, bl.ID)
	if err != nil {
		return mergequeue.Verified{}, err
	}
	c.candidateDeployID = dep.ID

	// The criteria the tree is decided against are this item's and every item
	// ahead of it in the queue: the speculation merged their branches in, so
	// their encodings are in the build and their criteria are in force for it.
	// A build holding an encoding for a criterion the set leaves out is what the
	// encoding check refuses.
	inForce, err := p.inForceFor(ctx, c.svc, withAhead(it, ahead))
	if err != nil {
		return mergequeue.Verified{}, err
	}
	if err := p.checkEncodings(ctx, c, repo, c.svc.ID, withAhead(it, ahead), inForce); err != nil {
		return mergequeue.Verified{}, err
	}
	if c.encodingDefect != "" {
		return mergequeue.Verified{Commit: commit, BuildID: bl.ID,
			Why: "the criteria and the encodings do not match with master merged in: " + c.encodingDefect}, nil
	}
	if c.encodingCouldNotDerive {
		return mergequeue.Verified{Commit: commit, BuildID: bl.ID,
			Why: "the criteria and the encodings do not match with master merged in: the encodings could not be derived"}, nil
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
				Why:                 fmt.Sprintf("criterion %s is %s against build %s", result.CriterionID, result.Outcome, bl.ID),
				FailedCriteria:      failedCriteria(results),
				Composition:         environment.Composition{From: c.composedFrom},
				ApprovedComposition: environment.Composition{From: c.approvedComposition},
			}, nil
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
			Why:                 check + " with master merged in: " + checked.Why(),
			Composition:         environment.Composition{From: c.composedFrom},
			ApprovedComposition: environment.Composition{From: c.approvedComposition},
		}, nil
	}
	return mergequeue.Verified{
		Commit: commit, BuildID: bl.ID, Passed: true, Forms: checked.Publishes,
		Composition:         environment.Composition{From: c.composedFrom},
		ApprovedComposition: environment.Composition{From: c.approvedComposition},
	}, nil
}

// withAhead is the items one re-verified tree carries: this one, and every
// candidate ahead of it in the queue's order that the speculation merged in.
func withAhead(it item.Item, ahead []item.Item) []string {
	ids := make([]string, 0, len(ahead)+1)
	ids = append(ids, it.ID)
	for _, one := range ahead {
		ids = append(ids, one.ID)
	}
	return ids
}

// failedCriteria is the criteria in force this run did not pass, which is what
// the confirming run is over. A failure no criterion decided — a merge conflict,
// a breaking contract diff — leaves it empty, and there the queue has no
// confirming run to make.
func failedCriteria(results []gate.CriterionResult) []string {
	var failed []string
	for _, result := range results {
		if result.Outcome.Blocks(false) {
			failed = append(failed, result.CriterionID)
		}
	}
	return failed
}

// Head is [mergequeue.Repository]: master's head as the version control system
// holds it, read from the repository and never derived from a record. The queue
// reads it at every start and before every mint, and it compares it against the
// records itself — so this answers what git says and makes no comparison of its
// own.
func (p *path) Head(ctx context.Context, serviceID string) (string, error) {
	svc, err := p.serviceOf(ctx, serviceID)
	if err != nil {
		return "", err
	}
	return masterCommit(svc.Repository)
}

// Holds is the other direction of the same reading: whether master holds one
// commit. A release record naming a commit master does not hold is git restored
// behind the graph, and the queue stops the service on it.
//
// A commit the repository does not have at all reads as not held rather than as
// an error: that is the same fact — master does not hold it — and a restore that
// removed the object is exactly the case this reading exists for.
func (p *path) Holds(ctx context.Context, serviceID, commit string) (bool, error) {
	svc, err := p.serviceOf(ctx, serviceID)
	if err != nil {
		return false, err
	}
	if _, err := git(svc.Repository, "merge-base", "--is-ancestor", commit, "refs/heads/master"); err != nil {
		return false, nil
	}
	return true, nil
}

// Confirm is [mergequeue.Repository]: the confirming run. The criteria the
// re-verification failed are decided once more over that same build and that
// same composition — once, and never until green — and which of them answered
// differently is what tells the queue's second reading of a failure from its
// third.
//
// It is the same run the re-verification made, made again: the encodings on the
// candidate's own environment, over the build the re-verification produced. A
// criterion that answered a failure and then a pass over one build decided
// nothing, which is what makes it undecided for the build.
func (p *path) Confirm(ctx context.Context, it item.Item, verified mergequeue.Verified) (mergequeue.Confirmation, error) {
	if len(verified.FailedCriteria) == 0 {
		return mergequeue.Confirmation{}, nil
	}
	c, err := p.candidateFor(ctx, it.ID)
	if err != nil {
		return mergequeue.Confirmation{}, err
	}
	inForce, err := p.inForceFor(ctx, c.svc, []string{it.ID})
	if err != nil {
		return mergequeue.Confirmation{}, err
	}
	results, err := p.decideCriteria(ctx, c, verified.BuildID, inForce)
	if err != nil {
		return mergequeue.Confirmation{}, err
	}

	failed := map[string]bool{}
	for _, result := range results {
		failed[result.CriterionID] = result.Outcome.Blocks(false)
	}
	var confirmation mergequeue.Confirmation
	for _, id := range verified.FailedCriteria {
		if failed[id] {
			confirmation.Repeated = append(confirmation.Repeated, id)
			continue
		}
		confirmation.Disagreed = append(confirmation.Disagreed, id)
	}
	if len(confirmation.Repeated) > 0 {
		confirmation.Why = fmt.Sprintf("%d criterion(s) failed again over the re-verification's own build %s",
			len(confirmation.Repeated), verified.BuildID)
	}
	return confirmation, nil
}

// VerifyCommit is [mergequeue.Repository]: a commit a human accepted at Work,
// built and re-verified the way a candidate is. It names no item, there being
// none — the commit reached master by another path — so there is no candidate
// environment to run the criteria on and no contract check to make against one.
//
// What it verifies is that the commit builds. That is less than a candidate's
// re-verification and it is all this interface has: the criteria and the
// contract checks are decided against a candidate environment's own run, and a
// commit with no item has no candidate environment. The queue mints a number for
// what this passes, so what it costs is a release numbered over a build nothing
// exercised.
//
// The repository is switched onto commit before createBuild compiles it, which
// is what makes the build record's digest the digest of that commit's own
// binary and not of whatever the repository last held. A commit that does not
// compile is [ErrDoesNotCompile] from createBuild, told apart here from an
// infrastructure failure and reported as the soft outcome this interface's
// caller reads rather than an error — no build record exists for it, there
// being no digest to write one with.
func (p *path) VerifyCommit(ctx context.Context, serviceID, commit string) (mergequeue.Verified, error) {
	svc, err := p.serviceOf(ctx, serviceID)
	if err != nil {
		return mergequeue.Verified{}, err
	}
	if _, err := git(svc.Repository, "switch", "--detach", commit); err != nil {
		return mergequeue.Verified{}, err
	}
	bl, err := p.createBuild(ctx, svc.Repository, "", svc.ID, commit)
	if err != nil {
		if errors.Is(err, ErrDoesNotCompile) {
			return mergequeue.Verified{Commit: commit,
				Why: "the commit a human accepted does not compile: " + firstLines(err.Error())}, nil
		}
		return mergequeue.Verified{}, err
	}
	return mergequeue.Verified{Commit: commit, BuildID: bl.ID, Passed: true}, nil
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
