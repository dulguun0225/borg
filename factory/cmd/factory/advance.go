package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/service"
)

// The run subcommand's whole pass, and the two halves it is made of. takeIn is
// the intake of the intents a run is given — each taken in, refined, decomposed,
// and its set ratified at Decomposition where it yielded more than one — and
// advance is everything below decomposition, performed over every live item read
// from the records rather than from a list this process holds.
//
// A pass performs what it can and stops each item where a human decides, so it
// is repeatable: what the next one does is read out of the records, per
// ../../../end-goal/one-process.md's rule that every component's restart is a
// read of its own records.

// run is the path's pass, driven to a standstill: the intents it is given taken
// in, and then [path.advance] repeated until nothing moves.
//
// It prompts for no verdict. A row a human decides is left pending in Work by
// the pass that fired it and the run reports the item waiting there, so an
// install with no screen open is not stuck but shown as stuck — which is what
// ../../../roadmap.md#m8--the-screens-and-the-fleet says of this subcommand.
// Where the composition supplies [deps.decide], that is what closes a pending
// row between two passes, in place of a human at Work; where it supplies none,
// the first pass that moves nothing is the last.
//
// Every candidate of one dependency layer reaches each step before any of them
// reaches the next, which is what makes two of them live at once — and the merge
// queue, which is the one step that is not per candidate, is where their order is
// decided. Layers exist because an item may wait on another: a consumer's candidate
// environment is composed from its producer's current release, so the producer has
// to have shipped before the consumer can be verified at all.
func run(ctx context.Context, d deps, statements []asked) (shipped, error) {
	var s shipped
	if len(statements) == 0 {
		return s, errors.New("factory: a run needs at least one intent")
	}

	p, err := compose(ctx, d)
	if err != nil {
		return s, err
	}
	s.environmentID = p.production.ID
	s.areaID = p.areaID

	// 1. Every intent taken in, refined, decomposed into its items, and
	// ratified at Decomposition where it yielded more than one.
	if err := p.takeIn(ctx, &s, statements); err != nil {
		return s, err
	}

	// 2. The path below decomposition, one pass at a time, until nothing moves.
	// The adopted candidates are reported whether or not the pass finished: an
	// item another run left queued is one this run touched, and a run that
	// stopped after touching it should still say so.
	deployed := ""
	for {
		pass, err := p.advance(ctx)
		s.candidates = append(s.candidates, pass.adopted...)
		s.candidates = append(s.candidates, pass.decomposed...)
		if pass.deployed != "" {
			deployed = pass.deployed
		}
		if err != nil {
			return s, err
		}
		if pass.moved {
			continue
		}
		closed, err := p.decideWaiting(ctx)
		if err != nil {
			return s, err
		}
		if closed == 0 {
			break
		}
	}

	// 3. The notifier's own two passes, once: the pages a service's authored
	// paging hours held back, and the drift detector's store. They are
	// per-factory rather than per-service and per-pass, so the path's own watch
	// leaves them to here.
	if err := p.notifierPasses(ctx); err != nil {
		return s, err
	}

	// 4. The acceptance round, per intent every item of which is live: the one
	// round that follows production, asked by intake and delivered by the
	// notifier, and the intent delivered where the factory raised it and there
	// is nobody to ask. It runs after the passes because what makes an intent
	// ready for it is its last item going live.
	if err := p.acceptanceRounds(ctx, s.decompositions); err != nil {
		return s, err
	}

	// 5. The detector: every deprecation-marked element whose derived consumer
	// contracts are gone gets a removal intent, so nobody has to remember step three
	// of a migration. It runs once at the end of a run rather than per pass, because
	// what empties a list is a release deploying and the passes above are where those
	// happened.
	if err := p.raiseRemovals(ctx); err != nil {
		return s, err
	}

	// 6. The walk, the demonstration's direction: from the last deploy back to
	// the intent, every step a field and none reconstructed. A run whose release was
	// failed walks the rollback's own deploy record, which is the deploy that is
	// live at the end of it.
	if deployed == "" {
		fmt.Fprintln(d.out, "Nothing reached production, so there is no deploy to walk back from")
		return s, nil
	}
	if c := p.byItem[itemOfDeploy(ctx, d.pool, deployed)]; c != nil && c.svc.ID != "" {
		if live, running, err := deploy.Current(ctx, d.pool, c.svc.ID, p.production.ID,
			serviceAddresses(p.production, c.svc)); err == nil && running {
			deployed = live.ID
		}
	}
	return s, walk(ctx, d.pool, d.out, d.token, asPrincipal(p.human), deployed)
}

// decideWaiting closes the rows pending in Work, where the composition supplies
// something that does. Nothing in this binary does: a verdict is given at a
// screen, and [deps.decide] is the seam a test drives that side of the gate
// component through. A composition supplying none reports the rows waiting and
// leaves them waiting.
func (p *path) decideWaiting(ctx context.Context) (int, error) {
	if p.d.decide == nil {
		return 0, nil
	}
	return p.d.decide(ctx, p)
}

// itemOfDeploy is the item a deploy's release was cut from, and empty where the
// deploy or the release cannot be read. It is used to find which service's current
// deploy the walk should start from, and a failure to read it leaves the walk
// starting where it already was.
func itemOfDeploy(ctx context.Context, pool *pgxpool.Pool, deployID string) string {
	dep, err := deploy.Get(ctx, pool, deployID)
	if err != nil || dep.ReleaseID == "" {
		return ""
	}
	rel, err := release.Get(ctx, pool, dep.ReleaseID)
	if err != nil {
		return ""
	}
	return rel.ItemID
}

// advanced is what one pass did: whether anything moved, the candidates it
// adopted from an earlier run's queue, and the last production deploy it wrote.
type advanced struct {
	// moved is whether the pass performed a step. A pass that moved nothing is
	// what says every live item is waiting, held, or done, which is where the
	// run's loop ends.
	moved bool
	// adopted is the candidates this run did not author and finished anyway,
	// which is what the merge queue's membership being the service's means.
	adopted []*candidate
	// decomposed is the candidates this pass's intent stage created, which is
	// what an intent taken in at Work reaches an item through.
	decomposed []*candidate
	// deployed is the last production deploy the pass wrote, which is where the
	// link walk starts.
	deployed string
}

// takeIn takes every intent the run was given as far as the Decomposition row:
// intake, the interview, decomposition into items, and that row's verdict where
// the decomposition yielded more than one. What each item does below it is
// [path.advance]'s.
func (p *path) takeIn(ctx context.Context, s *shipped, statements []asked) error {
	for n, one := range statements {
		set, candidates, err := p.authorIntent(ctx, one, fmt.Sprintf("%d of %d", n+1, len(statements)))
		if err != nil {
			return err
		}
		s.decompositions = append(s.decompositions, set)
		p.sets[set.intentID] = set
		s.candidates = append(s.candidates, candidates...)
		for _, c := range candidates {
			p.byItem[c.itemID] = c
			p.authored[c.itemID] = true
		}
	}
	for _, name := range p.d.serviceNames() {
		svc, found, err := service.ByName(ctx, p.d.pool, name)
		if err != nil {
			return err
		}
		if found {
			s.serviceIDs = append(s.serviceIDs, svc.ID)
		}
	}
	if len(s.serviceIDs) > 0 {
		s.serviceID = s.serviceIDs[0]
	}
	return nil
}

// advance is one pass of the path below decomposition: every set whose
// Decomposition row a human has since decided, then every live item read from
// the records, rehydrated and continued from the step those records say it
// stands at, taken one dependency layer at a time.
func (p *path) advance(ctx context.Context) (advanced, error) {
	var a advanced
	p.moved = false
	// The log this pass reads, taken once: what the pass reads it for is the
	// verdict on every row, and a read per item is quadratic in a log that grows
	// by a read event at every read.
	held, err := p.readLog(ctx)
	if err != nil {
		return a, err
	}
	p.logRead = held
	defer func() { p.logRead = nil }()
	if err := p.resumeSets(ctx); err != nil {
		return a, err
	}
	// The intent stage: every intent the records hold that has not reached its
	// items yet, interviewed as far as this composition can take it and then
	// decomposed. It runs before any step on an item, an item being what a step
	// is performed on.
	decomposed, err := p.intentStage(ctx)
	a.decomposed = append(a.decomposed, decomposed...)
	if err != nil {
		a.moved = p.moved
		return a, err
	}
	live, err := p.liveCandidates(ctx)
	if err != nil {
		a.moved = p.moved
		return a, err
	}
	// An item whose set is still being ratified performs no step: the
	// Decomposition row decides whether the set exists at all, so authoring
	// under a row a human has not decided would author a spec for an item the
	// verdict may supersede.
	ratifying := map[string]bool{}
	for _, open := range held.pending {
		if open.Gate.Kind == gate.KindDecomposition && open.Subject.IntentID != "" {
			ratifying[open.Subject.IntentID] = true
		}
	}
	var work []*candidate
	for _, c := range live {
		if c.from == stepEnded || c.superseded {
			continue
		}
		if ratifying[c.intentID] {
			fmt.Fprintf(p.d.out, "Item %s waits on the Decomposition row of intent %s, which is pending in Work\n",
				c.itemID, c.intentID)
			continue
		}
		work = append(work, c)
	}
	for _, one := range layers(work) {
		deployed, adopted, err := p.layer(ctx, one)
		a.adopted = append(a.adopted, adopted...)
		if deployed != "" {
			a.deployed = deployed
		}
		if err != nil {
			a.moved = p.moved
			return a, err
		}
	}
	a.moved = p.moved
	return a, nil
}

// resumeSets performs what a verdict at the Decomposition row causes, for every
// set this run took in whose row it left waiting in Work. The row is the one gate
// above a timeline, so what it causes is the intent's and not an item's:
// approving lets every item of the set proceed, and rejecting supersedes them
// all and counts a re-decomposition.
func (p *path) resumeSets(ctx context.Context) error {
	waiting := false
	for _, set := range p.sets {
		if set.waiting {
			waiting = true
		}
	}
	if !waiting {
		return nil
	}
	held, err := p.readLog(ctx)
	if err != nil {
		return err
	}
	closed := held.closed
	for intentID, set := range p.sets {
		if !set.waiting {
			continue
		}
		rows := closedOn(closed, func(o gate.Opened) bool { return o.Subject.IntentID == intentID })
		row, decided := rows[gate.KindDecomposition]
		if !decided {
			continue
		}
		set.waiting = false
		set.fired = recordFiring(row.opened, row.closing)
		in, err := intent.Get(ctx, p.d.pool, intentID)
		if err != nil {
			return err
		}
		if err := p.decompositionOutcome(ctx, in, set, row.verdict, row.reason); err != nil {
			return err
		}
		p.moved = true
	}
	return nil
}

// authorFrom performs the authoring stages of one item, from the step the
// records say it stands at: the reject a human left at a row sends it back to the
// stage that row names with an attempt counted there, and the stage authors
// again against what was found wrong.
//
// It reports whether it performed anything, which is what the run's loop reads
// to know a pass moved.
func (p *path) authorFrom(ctx context.Context, c *candidate) (bool, error) {
	switch c.from {
	case stepWaiting:
		if c.pending != nil {
			p.reportWaiting(*c.pending)
		}
		return false, nil
	case stepSentBack:
		fmt.Fprintf(p.d.out, "Item %s was sent back to %s by the merge queue: %s\n",
			c.itemID, item.StageImplementation, c.queueWhy)
		fmt.Fprintln(p.d.out, "  the attempt is counted there and building it again is not part of this milestone")
		return false, nil
	case stepHeld:
		fmt.Fprintf(p.d.out, "Item %s is held at %s by a human, and the row fires again when a holder releases it at Work\n",
			c.itemID, c.heldAt)
		return false, nil
	case stepQueue, stepEnded:
		return false, nil
	}

	if c.from <= stepSpec {
		returned, err := p.sendBack(ctx, c, gate.KindSpec, item.StageSpec, c.specArtifactID, c.spec)
		if err != nil {
			return true, err
		}
		if err := p.specStage(ctx, c, returned); err != nil {
			return true, err
		}
		if c.waiting != (gate.Row{}) {
			return true, nil
		}
		c.from = stepPlan
	}
	if c.from <= stepPlan {
		returned, err := p.sendBack(ctx, c, gate.KindImplementationPlan, item.StageImplementationPlan, c.planArtifactID, c.plan)
		if err != nil {
			return true, err
		}
		if err := p.planStage(ctx, c, returned); err != nil {
			return true, err
		}
		if c.waiting != (gate.Row{}) {
			return true, nil
		}
		c.from = stepTasks
	}
	if c.from <= stepTasks {
		returned, err := p.sendBack(ctx, c, gate.KindTasks, item.StageTasks, c.tasksArtifactID, c.tasks)
		if err != nil {
			return true, err
		}
		if err := p.tasksStage(ctx, c, returned); err != nil {
			return true, err
		}
		if c.waiting != (gate.Row{}) {
			return true, nil
		}
		c.from = stepImplementation
	}
	if c.from <= stepImplementation {
		if err := p.sentBackToImplementation(ctx, c); err != nil {
			return true, err
		}
		if err := p.implementationStage(ctx, c); err != nil {
			return true, err
		}
		if c.waiting != (gate.Row{}) {
			return true, nil
		}
		c.from = stepCandidateEnvironment
		return true, nil
	}
	return false, nil
}

// sendBack is what a reject a human left at one of the three document rows
// causes: the item returns to the stage that row names, with an attempt counted
// there, and the stage is handed the reason and the version that was rejected.
// It returns the zero value where nothing rejected, which is a stage entered for
// the first time.
//
// The rejection is dropped from what the pass read once it is acted on, so a row
// whose consequence has been performed is not performed again by a caller reading
// the same map.
func (p *path) sendBack(ctx context.Context, c *candidate, kind gate.Kind,
	stage item.Stage, subject, version string) (agent.Returned, error) {
	row, rejected := rejectedOver(c.rows, kind, subject)
	if !rejected {
		return agent.Returned{}, nil
	}
	delete(c.rows, kind)
	if _, err := p.items.ReturnTo(ctx, p.human, c.itemID, stage); err != nil {
		return agent.Returned{}, err
	}
	fmt.Fprintf(p.d.out, "Rejected at %s: %s\nItem %s is back at %s with an attempt counted there\n",
		row.opened.Gate, row.reason, c.itemID, stage)
	return agent.Returned{Reason: row.reason, Version: version}, nil
}

// sentBackToImplementation is the same thing for the three rows that send an item
// back to the implementation stage: that row's own reject, the candidate
// environment's, and Merge to master's. What the row found wrong is carried into
// the stage on [candidate.sentBack], which is the field the stage's own loop
// already reads.
func (p *path) sentBackToImplementation(ctx context.Context, c *candidate) error {
	for _, kind := range []gate.Kind{gate.KindImplementation, gate.KindDeployToCandidateEnvironment, gate.KindMergeToMaster} {
		subject := c.buildID
		if kind == gate.KindImplementation {
			subject = c.implArtifactID
		}
		row, rejected := rejectedOver(c.rows, kind, subject)
		if !rejected {
			continue
		}
		delete(c.rows, kind)
		if _, err := p.items.ReturnTo(ctx, p.human, c.itemID, item.StageImplementation); err != nil {
			return err
		}
		fmt.Fprintf(p.d.out, "Item %s goes back to implementation against what the %s row found wrong: %s\n",
			c.itemID, row.opened.Gate, row.reason)
		commit := c.commit
		c.resetForRebuild()
		c.sentBack = agent.Returned{Reason: row.reason, Version: commit}
		return nil
	}
	return nil
}
