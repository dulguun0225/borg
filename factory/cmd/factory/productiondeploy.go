package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/incident"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/lastcheck"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/service"
)

// productionDeploy is the Deploy to production row: the last row before a release
// takes traffic, and the one that offers hold and no reject.
//
// The factory's own holds are computed first. Four of the five lift themselves — a
// dependency becomes current, a window closes, a revert ships — so the deploy waits,
// nothing is written, and the next firing recomputes; a gate fired for one of them
// would ask a human to approve through something the factory is about to clear on
// its own. Approving through them all the same is `factory approve`, which is the
// emergency action the design keeps at this row.
//
// The fifth is the drift detector's mismatch, and it is not computed here:
// the gate reads that store itself at the firing, puts a human at the row, and
// carries what disagreed on the open event.
func (p *path) productionDeploy(ctx context.Context, c *candidate) error {
	d := p.d
	it, err := item.Get(ctx, d.pool, c.itemID)
	if err != nil {
		return err
	}
	held, err := p.factoryHolds(ctx, c.svc, it)
	if err != nil {
		return err
	}
	if held != "" {
		c.factoryHold = held
		fmt.Fprintf(d.out, "Release %s waits at %s: %s\n", c.releaseID, gate.DeployToProduction, held)
		fmt.Fprintln(d.out, "  the factory set this hold over records that already exist, so nothing is written and it lifts itself")
		fmt.Fprintf(d.out, "  a human may approve through it: `factory approve %s`\n", c.itemID)
		return nil
	}

	opened, firing, err := p.fireProduction(ctx, c)
	if err != nil {
		return err
	}
	report(d.out, opened, c.criteria)
	verdict, _, closing, err := p.settle(ctx, opened, firing)
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
	return p.putOnProduction(ctx, c, opened.Strategy)
}

// fireProduction fires the production deploy row over one candidate. It is its own
// function because two callers fire it: the path, and a human approving through a
// factory hold.
func (p *path) fireProduction(ctx context.Context, c *candidate) (gate.Opened, gate.Firing, error) {
	reached, err := p.exposureOf(ctx, c.reverifiedBuildID)
	if err != nil {
		return gate.Opened{}, gate.Firing{}, err
	}
	it, err := item.Get(ctx, p.d.pool, c.itemID)
	if err != nil {
		return gate.Opened{}, gate.Firing{}, err
	}
	// The revert's own row is the one this reading is for: the rollback hold
	// stands on every other item of the service and not on this one, so the row
	// fires, and where a human decides it the wait is one the page's condition
	// meets. It is read at this row because this is where the revert ships.
	revert, err := p.revertWhileRollbackHolds(ctx, c.svc, it)
	if err != nil {
		return gate.Opened{}, gate.Firing{}, err
	}
	firing := gate.Firing{
		Row:                      gate.DeployToProduction,
		ItemID:                   c.itemID,
		BuildID:                  c.reverifiedBuildID,
		ServiceID:                c.svc.ID,
		AreaID:                   p.areaID,
		EnvironmentID:            p.production.ID,
		CriteriaInForce:          len(c.criteria),
		Criteria:                 c.criteria,
		Measurement:              c.measurement,
		Exposure:                 reached,
		RevertWhileRollbackHolds: revert,
	}
	opened, err := p.gate.Fire(ctx, firing)
	return opened, firing, err
}

// putOnProduction is what an approval at that row performs: the verified build put
// on production's target, and the analysis window opened over the deploy record that
// results.
//
// The window is opened after the deploy record is written, which is what the
// design says of it — and the deploy having completed first is what makes the
// release the window watches one that is actually running. Nothing here closes
// it: the health monitor evaluates every exit, so what this leaves is a window
// for the watch to finish.
func (p *path) putOnProduction(ctx context.Context, c *candidate, pick gate.Pick) error {
	d := p.d
	// The binary the re-verification built is where it ran, which is the candidate
	// environment's directory. Copying it is what puts the verified build on
	// production rather than compiling the same commit a second time. A release whose
	// candidate environment is gone — one deployed a second time, or a revert whose
	// binary is already here — is deployed from what is already in production's
	// directory, which nothing removes.
	from := filepath.Join(c.environmentDir, c.reverifiedBuildID)
	to := filepath.Join(d.dir, c.reverifiedBuildID)
	if c.environmentDir != "" {
		if _, err := os.Stat(from); err == nil {
			if err := copyFile(from, to); err != nil {
				return err
			}
		}
	}
	if _, err := os.Stat(to); err != nil {
		return fmt.Errorf("factory: build %s is not in production's directory and its candidate environment has none: %w",
			c.reverifiedBuildID, err)
	}

	dep, err := p.intoProduction(ctx, c, pick)
	if err != nil {
		return err
	}
	c.deployID = dep.ID
	fmt.Fprintf(d.out, "Deploy %s complete: release %s runs in production under the strategy %s\n",
		dep.ID, c.releaseID, dep.StrategyPerformed)

	// The deployer's own last check over each production target, and its four
	// fields on the service record. The last check is what says the deployer
	// reached that target and when, which is what a drift-detection exemption
	// standing on a rollout that is not advancing is refused against.
	if err := p.recordTargetChecks(ctx, dep); err != nil {
		return err
	}
	if err := p.recordPlatformCheck(ctx); err != nil {
		return err
	}
	if err := p.adopt(ctx, c.svc, dep); err != nil {
		return err
	}

	// Whether the score held this item out is read off the decisions on it rather
	// than carried down from the firing, because a window is opened at the deploy
	// and the selection may have been made at any row above it.
	heldOut, err := score.HeldOut(ctx, d.pool, d.token, c.itemID)
	if err != nil {
		return err
	}
	opened, isNew, err := p.healthMonitor.Open(ctx, healthmonitor.Watching{
		ID: c.svc.ID, Name: c.svc.Name, EnvironmentID: p.production.ID,
	}, dep.ID, c.releaseID, p.scoreVersion, heldOut)
	if err != nil {
		return err
	}
	if !isNew {
		fmt.Fprintf(d.out, "No window opens: release %s was watched already, by window %s\n", c.releaseID, opened.ID)
		return nil
	}
	c.windowID = opened.ID
	passed := "the passed exit is available to it"
	switch {
	case opened.HeldOut:
		passed = "the passed exit is not available to it: the score held this item out of the gate it would have gated, so its window runs to the cap — the longest watch there is"
	case !opened.PassedAvailable:
		passed = "the passed exit is not available to it, nothing below it being there to compare against — so it can end only at its cap"
	}
	fmt.Fprintf(d.out, "Analysis window %s opened over deploy %s: size %v, confidence %v, cap %vs; %s\n",
		opened.ID, dep.ID, opened.Size, opened.Confidence, opened.CapSeconds, passed)

	// Which release is a brownout is package contractcheck's to answer, and the
	// health monitor is not told it: the window just opened is an ordinary one.
	// So the run reports the reading and says what the window is, rather than
	// what a brownout's window would be — a line claiming the cap and the
	// reading over other services would describe behaviour nothing here
	// performs. That package's doc.go names what the health monitor still needs.
	of, isBrownout, err := p.contracts.IsBrownout(ctx, c.releaseID)
	if err != nil {
		return err
	}
	if isBrownout {
		fmt.Fprintf(d.out, "Release %s is the brownout of %s, and window %s over it is an ordinary one: the health monitor is not told which release is a brownout, so this window can still end at the boundary and reads this service's numbers alone\n",
			c.releaseID, of.Element, opened.ID)
	}
	return nil
}

// recordTargetChecks is the deployer's own last check over each target this
// deploy record names, written after the deploy has been performed. Whether a
// further pass is owed is read off that target's own row: a target the rollout
// has reached is one it is finished with, so this pass is the last one owed
// there and the record says so; a target it has not reached is one the rollout
// still owes a pass, and the interval it promises that pass within is the
// watch's own, the longest thing a run does after a deploy.
//
// The two directions are what makes the record readable. A record past its
// interval with a further pass owed is always something that stopped, so a
// rollout the deployer has finished with that promised a further pass it will
// never make would raise a stale-component mismatch after every run, holding
// every service on that target and paging. A rollout that stopped part way still
// leaves the targets it never reached owing one, which is what the drift
// detector's rollout exemption is bounded by.
//
// It is the deploy record's targets and not the service's whole set, because
// this is a record of a pass the deployer made: a target the deploy did not
// reach at all is one it made no pass over.
func (p *path) recordTargetChecks(ctx context.Context, dep deploy.Deploy) error {
	targets, err := deploy.Targets(ctx, p.d.pool, dep.ID)
	if err != nil {
		return err
	}
	for _, target := range targets {
		furtherPassOwed := target.Completion == deploy.CompletionNotReached
		payload := fmt.Sprintf(`{"deploy_id":%q,"build_id":%q,"completion":%q}`,
			dep.ID, dep.BuildID, target.Completion)
		if err := deploy.RecordTargetCheck(ctx, p.checks, deployActor,
			target.Address, atLeastASecond(p.d.watchFor), !furtherPassOwed, payload); err != nil {
			return err
		}
	}
	return nil
}

// recordPlatformCheck is the deployer's own last check over the platform this
// production environment declares, written through
// [lastcheck.Writer.RecordPlatformPass], the one writer of that record. It runs
// beside [path.recordTargetChecks] so the record is exercised on every
// production deploy rather than left uncalled.
//
// Seam 4 has no operation that answers how many candidate environments the
// platform holds or what room it reports, so this pass reads only the half it
// can: how many candidate environments the factory's own records hold as
// standing for the project. It reports that count as held by the platform too
// and no room figure, which is the honest reading where nothing on the other
// side of the seam answers either question — not a modelled guess at what the
// platform would say.
func (p *path) recordPlatformCheck(ctx context.Context) error {
	if p.production.Platform.Name == "" {
		return nil
	}
	standing, err := environment.CountLiveCandidates(ctx, p.d.pool, p.production.ID)
	if err != nil {
		return err
	}
	_, err = p.checks.RecordPlatformPass(ctx, deployActor, p.production.Platform.Name,
		atLeastASecond(p.d.watchFor), lastcheck.PlatformPass{
			StandingByTheRecords: standing,
			HeldByThePlatform:    standing,
		})
	return err
}

// factoryHolds is every hold the factory sets at the production deploy row that
// lifts itself, in the order it is worth reporting them: a declared dependency that
// is not live still, the service already holding as many analysis windows open as the
// window limit
// allows, a rollback whose revert has not shipped, and the service's error budget
// exhausted. It returns the words the first
// one found is reported with, and nothing where none holds.
//
// None of the four is written anywhere. Each is computed from records that already
// exist — the deploy records of the dependencies' services, the open windows, the
// newest rollback, the emission the objective is read over — and the design gives such
// a hold no row: a record for it would be
// a decision where nothing is decided, and re-testing would append one every time the
// gate re-fired. What that costs is that how long the factory has been holding is
// answerable for the platform's ceiling alone, which is the one wait at a deploy row
// that is written.
func (p *path) factoryHolds(ctx context.Context, svc service.Service, it item.Item) (string, error) {
	held, err := p.dependencyHold(ctx, it)
	if err != nil || held != "" {
		return held, err
	}
	if held, err := p.windowHold(ctx, svc); err != nil || held != "" {
		return held, err
	}
	if held, err := p.rollbackHold(ctx, svc, it); err != nil || held != "" {
		return held, err
	}
	return p.objectiveHold(ctx, svc, it)
}

// objectiveHold is the two things a service level objective does, read from one
// budget: the hold an exhausted budget sets on that service's production
// deploys, and the intent the objective raises. The hold lifts itself when the
// period rolls forward far enough to restore the budget, nothing is decided and
// no page fires — the shape the hold a dependency that is not current sets
// already has. A budget the store does not cover is uncomputed and holds the way
// an exhausted one does, a budget taken as intact over records that are not
// there being an absent input read as evidence.
//
// The raise is on the same reading because the two are one mechanism: the fix
// for whatever exhausted the budget is itself a production deploy, and the item
// that passes the hold on a service that crossed nothing is the one this raise
// takes in. A budget read as exhausted with nothing raised on it would be a hold
// no item could lift.
//
// Where an owner authored no objective there is no budget, nothing is held and
// nothing is raised: that reading and the window are the whole of what protects
// the service.
func (p *path) objectiveHold(ctx context.Context, svc service.Service, it item.Item) (string, error) {
	w := healthmonitor.Watching{ID: svc.ID, Name: svc.Name, EnvironmentID: p.production.ID}
	budget, err := p.healthMonitor.ErrorBudget(ctx, w)
	if err != nil {
		return "", err
	}
	if _, err := p.healthMonitor.RaiseObjectiveIntent(ctx, w, budget); err != nil {
		return "", err
	}
	if !budget.Holds() {
		return "", nil
	}
	passes, err := p.passesTheBudgetHold(ctx, svc, it)
	if err != nil || passes {
		return "", err
	}
	if !budget.Covered {
		return fmt.Sprintf("%s — the store does not cover the objective's period of %.0f seconds, so the budget is uncomputed and holds the way a spent one does",
			gate.HoldErrorBudgetExhausted, budget.PeriodSeconds), nil
	}
	return fmt.Sprintf("%s — %.0f%% of the allowance is left over a period of %.0f seconds",
		gate.HoldErrorBudgetExhausted, budget.Remaining*100, budget.PeriodSeconds), nil
}

// passesTheBudgetHold is the two items the design lets past it: a revert, which
// passes the hold a rollback leaves for the same reason, and an item whose intent
// a detector raised on that service — the health monitor's at a crossing, or the
// objective's own. Without the second the hold would stand hardest exactly where
// production is worst, no item on a service that crossed nothing being able to
// lift it.
//
// A request an owner raised on that service does not pass; the route is the
// objective's intent, which exists whenever the budget is exhausted.
func (p *path) passesTheBudgetHold(ctx context.Context, svc service.Service, it item.Item) (bool, error) {
	if it.IntentID == "" {
		return false, nil
	}
	_, revertIntentID, outstanding, err := p.outstandingRevert(ctx, svc)
	if err != nil {
		return false, err
	}
	if outstanding && it.IntentID == revertIntentID {
		return true, nil
	}
	raised, err := intent.Get(ctx, p.d.pool, it.IntentID)
	if err != nil {
		return false, err
	}
	if raised.Source != intent.SourceDetector || raised.Evidence == "" {
		return false, nil
	}
	// The evidence is stored as the key package intent composes, and the service
	// it names is what says the detector raised this on this service and not on
	// another.
	var evidence intent.Evidence
	if err := json.Unmarshal([]byte(raised.Evidence), &evidence); err != nil {
		return false, fmt.Errorf("factory: reading the evidence on intent %s: %w", raised.ID, err)
	}
	return evidence.ServiceID == svc.ID, nil
}

// windowHold is the window limit: an open window blocks nothing until the service
// holds as many as it allows, and then the next production deploy waits. It is a wait on the factory
// rather than on a human, so it does not page — it shows only to a reader who asks,
// which on this interface is this line.
func (p *path) windowHold(ctx context.Context, svc service.Service) (string, error) {
	room, open, limit, err := p.healthMonitor.Room(ctx, svc.ID)
	if err != nil || room {
		return "", err
	}
	return fmt.Sprintf("%s — %d open against a window limit of %d, and this is a wait on the factory rather than on anybody",
		gate.HoldWindowLimitReached, open, limit), nil
}

// rollbackHold is the hold a rollback leaves: master keeps the change that was
// rolled back and the next item was built on master, so deploying it would redeliver
// the defect just removed.
//
// It does not hold the revert — a dependency hold that blocked its own dependency
// would never lift — and what says which item is the revert is the intent the
// rollback's own deploy record names. That link is the one stored fact connecting the
// two, nothing on the item saying it is a revert.
func (p *path) rollbackHold(ctx context.Context, svc service.Service, it item.Item) (string, error) {
	rollback, revertIntentID, outstanding, err := p.outstandingRevert(ctx, svc)
	if err != nil || !outstanding {
		return "", err
	}
	if it.IntentID != "" && it.IntentID == revertIntentID {
		return "", nil
	}
	return fmt.Sprintf("%s — rollback %s failed release %s and its revert, intent %s, has not shipped",
		gate.HoldRollbackAwaitingRevert, rollback.ID, rollback.Undoing.FailedReleaseID,
		revertIntentID), nil
}

// outstandingRevert is the rollback this service is waiting for the revert of,
// the intent of that revert, and whether anything is outstanding at all: no
// rollback, no incident still open behind it, a revert already shipped, or a
// mark against the rollback is nothing outstanding.
//
// The mark is read first because it is what ends the wait before the revert
// ships: a named human at Ops saying the rollback was not caused by the release
// leaves no defect on master for the hold to keep off production, so the next
// release from master carries the change and is measured again.
//
// The walk from the rollback to the revert's intent is
// [healthmonitor.RevertOfRollback] and not a copy of it here, so the hold and
// the mark's own command read one predicate.
func (p *path) outstandingRevert(ctx context.Context, svc service.Service) (deploy.Deploy, string, bool, error) {
	rollback, found, err := deploy.NewestRollback(ctx, p.d.pool, svc.ID, p.production.ID)
	if err != nil || !found {
		return deploy.Deploy{}, "", false, err
	}
	marked, err := healthmonitor.MarkStands(ctx, p.d.pool, rollback.ID)
	if err != nil || marked {
		return deploy.Deploy{}, "", false, err
	}
	revertIntentID, _, outstanding, err := healthmonitor.RevertOfRollback(ctx, p.d.pool, p.production.ID, rollback)
	if err != nil || !outstanding {
		return deploy.Deploy{}, "", false, err
	}
	return rollback, revertIntentID, true, nil
}

// revertWhileRollbackHolds is the one branch [path.rollbackHold] answers with no
// hold: this item is the revert of a rollback that has not shipped. The service
// runs the build the rollback restored, master still contains the defect, and
// nothing ships past a human at this row — which is what the gate carries onto
// the open event and what fires a page where a human decides it.
func (p *path) revertWhileRollbackHolds(ctx context.Context, svc service.Service, it item.Item) (bool, error) {
	_, revertIntentID, outstanding, err := p.outstandingRevert(ctx, svc)
	if err != nil || !outstanding {
		return false, err
	}
	return it.IntentID != "" && it.IntentID == revertIntentID, nil
}

// revertIntentOf is the intent whose revert a rollback is waiting for, and empty
// where nothing is waiting. The rollback's own deploy record names the release it
// failed and not the intent it raised: the intent is on the incident the health
// monitor raised at the same crossing, so the link between the two is the failed
// release, and that is the walk this makes.
//
// An incident that has resolved is a revert that shipped and a crossing that
// stopped, which is why only an open one is read: [incident.Open] answers with
// the open incident on that service and release, and its absence is a rollback
// with nothing outstanding behind it.
func revertIntentOf(ctx context.Context, p *path, svc service.Service, rollback deploy.Deploy) (string, error) {
	if rollback.Undoing.FailedReleaseID == "" {
		return "", nil
	}
	open, found, err := incident.Open(ctx, p.d.pool, svc.ID, rollback.Undoing.FailedReleaseID)
	if err != nil || !found {
		return "", err
	}
	return open.IntentID, nil
}
