package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/incident"
	"github.com/dulguun0225/borg/factory/item"
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
	firing := gate.Firing{
		Row:             gate.DeployToProduction,
		ItemID:          c.itemID,
		BuildID:         c.reverifiedBuildID,
		ServiceID:       c.svc.ID,
		AreaID:          p.areaID,
		EnvironmentID:   p.production.ID,
		CriteriaInForce: len(c.criteria),
		Criteria:        c.criteria,
		Measurement:     c.measurement,
		Exposure:        reached,
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
	return nil
}

// recordTargetChecks is the deployer's own last check over each target the
// deploy reached, written after the deploy completed. There is one per target of
// a persistent environment, and the interval it promises the next pass within is
// how often this interface deploys — which is once per run, so the interval is
// the watch's own, the longest thing a run does after a deploy.
//
// What it costs is that the interval is the run's and not a period the deployer
// keeps: this interface deploys when a run reaches the row rather than on a pass
// of its own, so a target whose check is older than the interval means the run
// ended and not that the deployer stopped.
func (p *path) recordTargetChecks(ctx context.Context, dep deploy.Deploy) error {
	for _, target := range p.production.Targets {
		payload := fmt.Sprintf(`{"deploy_id":%q,"build_id":%q}`, dep.ID, dep.BuildID)
		if err := deploy.RecordTargetCheck(ctx, p.checks, deployActor,
			target.Address, atLeastASecond(p.d.watchFor), false, payload); err != nil {
			return err
		}
	}
	return nil
}

// factoryHolds is every hold the factory sets at the production deploy row that
// lifts itself, in the order it is worth reporting them: a declared dependency that
// is not live still, the service already holding as many analysis windows open as the
// window limit
// allows, and a rollback whose revert has not shipped. It returns the words the first
// one found is reported with, and nothing where none holds.
//
// None of the three is written anywhere. Each is computed from records that already
// exist — the deploy records of the dependencies' services, the open windows, the
// newest rollback — and the design gives such a hold no row: a record for it would be
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
	return p.rollbackHold(ctx, svc, it)
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
	rollback, found, err := deploy.NewestRollback(ctx, p.d.pool, svc.ID, p.production.ID)
	if err != nil || !found {
		return "", err
	}
	revertIntentID, err := revertIntentOf(ctx, p, svc, rollback)
	if err != nil || revertIntentID == "" {
		return "", err
	}
	shipped, err := healthmonitor.Shipped(ctx, p.d.pool, p.production.ID, revertIntentID)
	if err != nil || shipped {
		return "", err
	}
	if it.IntentID != "" && it.IntentID == revertIntentID {
		return "", nil
	}
	return fmt.Sprintf("%s — rollback %s failed release %s and its revert, intent %s, has not shipped",
		gate.HoldRollbackAwaitingRevert, rollback.ID, rollback.Undoing.FailedReleaseID,
		revertIntentID), nil
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
