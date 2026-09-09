package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/healthmonitor"
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
// its own. Approving through them all the same is [path.approveThrough], which is the
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
		fmt.Fprintln(d.out, "  a human may approve through it, which is the emergency action the design keeps at this row")
		return nil
	}

	// The verdict a human left at Work on a row an earlier pass fired, which is
	// the ordinary case at this row: nothing here decides a row a human decides,
	// so the approval that ships a release arrives between two passes.
	if already, decided := c.rows[gate.KindDeployToProduction]; decided && already.subject() == c.reverifiedBuildID {
		fmt.Fprintf(d.out, "%s of item %s was decided at Work as %s; row %s closed by %s %s\n",
			gate.DeployToProduction, c.itemID, already.verdict, already.opened.Row.ID,
			already.closing.Actor.Kind, already.closing.Actor.Key)
		p.moved = true
		c.deployGate = recordFiring(already.opened, already.closing)
		if already.verdict == gate.VerdictHold {
			c.held = true
			c.heldAt = gate.DeployToProduction
			fmt.Fprintf(d.out, "Held; release %s is minted and is not deployed, and the event stays queued\n", c.releaseID)
			return nil
		}
		return p.putOnProduction(ctx, c, already.opened.Strategy)
	}

	opened, _, err := p.fireProduction(ctx, c)
	if err != nil {
		return err
	}
	p.moved = true
	report(d.out, opened, c.criteria)
	done, err := p.settle(ctx, opened)
	if err != nil {
		return err
	}
	c.deployGate = recordFiring(opened, done.closing)
	if done.waiting {
		c.waiting = gate.DeployToProduction
		return nil
	}
	if done.verdict == gate.VerdictHold {
		c.held = true
		c.heldAt = gate.DeployToProduction
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

	// The deployer's own last check for the production environment, and its
	// four fields on the service record. The last check is what says the
	// deployer reached the environment and when, which is what a
	// drift-detection exemption standing on a rollout that is not advancing
	// is refused against.
	if err := p.recordEnvironmentCheck(ctx, dep); err != nil {
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
	// health monitor asks it through [path.IsBrownout] at every evaluation. What
	// the run reports here is the half of a brownout's window that is built: it
	// reads every service against its own recent history while it is open, any
	// crossing failing it. The other half — such a window running to the cap
	// rather than stopping where the boundary would allow — is not, and the line
	// says so rather than claiming it.
	of, isBrownout, err := p.contracts.IsBrownout(ctx, c.releaseID)
	if err != nil {
		return err
	}
	if isBrownout {
		fmt.Fprintf(d.out, "Release %s is the brownout of %s, and window %s over it reads every service against its own recent history: any of them crossing fails this window, an element restored that nobody read. It can still end at the boundary, a brownout's window running to the cap not being built\n",
			c.releaseID, of.Element, opened.ID)
	}
	return nil
}

// IsBrownout is [healthmonitor.Brownouts]: whether a release is the brownout of
// a marked contract element. It is this value and not the enforcement component
// handed over, because the health monitor is composed before enforcement is and
// a component composed with a nil interface would read every release as no
// brownout for the life of the process.
//
// One bit and not the element: what the health monitor does with it is read
// every service against its own recent history while that window is open, which
// the element's name does not enter.
func (p *path) IsBrownout(ctx context.Context, releaseID string) (bool, error) {
	if p.contracts == nil {
		return false, nil
	}
	_, is, err := p.contracts.IsBrownout(ctx, releaseID)
	return is, err
}

// recordEnvironmentCheck is the deployer's own last check for the production
// environment this deploy record names, written once after the deploy has
// been performed, and not per target: the deployer's last check is keyed by
// the production environment record, the way the maximum concurrent candidate
// environments already is, so an install whose projects run on two platforms
// adds neither count across them. Whether a further pass is owed is read off
// the deploy record's own targets: a target the rollout has reached is one it
// is finished with; a target it has not reached is one the rollout still owes
// a pass over, and the interval that pass is promised within is the watch's
// own, the longest thing a run does after a deploy. The environment's check
// names a further pass owed where any target of it does.
//
// The two directions are what makes the record readable. A record past its
// interval with a further pass owed is always something that stopped, so a
// rollout the deployer has finished with that promised a further pass it will
// never make would raise a stale-component mismatch after every run, holding
// every service on that environment and paging. A rollout that stopped part
// way still leaves a further pass owed, which is what the drift detector's
// rollout exemption is bounded by.
//
// It is the deploy record's targets and not the service's whole set, because
// this is a record of a pass the deployer made: a target the deploy did not
// reach at all is one it made no pass over, and one the service does not run
// on is not counted either way.
func (p *path) recordEnvironmentCheck(ctx context.Context, dep deploy.Deploy) error {
	targets, err := deploy.Targets(ctx, p.d.pool, dep.ID)
	if err != nil {
		return err
	}
	furtherPassOwed := false
	complete, owed := 0, 0
	for _, target := range targets {
		if target.NotRunHere {
			continue
		}
		if target.Completion == deploy.CompletionNotReached {
			furtherPassOwed = true
			owed++
			continue
		}
		complete++
	}
	payload := fmt.Sprintf(`{"deploy_id":%q,"build_id":%q,"targets_complete":%d,"targets_owed":%d}`,
		dep.ID, dep.BuildID, complete, owed)
	return deploy.RecordEnvironmentCheck(ctx, p.checks, deployActor,
		dep.EnvironmentID, atLeastASecond(p.d.watchFor), !furtherPassOwed, payload)
}

// recordPlatformCheck is the deployer's own last check over the platform this
// production environment declares, written through
// [lastcheck.Writer.RecordPlatformPass], the one writer of that record. It runs
// beside [path.recordEnvironmentCheck] so the record is exercised on every
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

// factoryHoldsAsRead is [path.factoryHolds] for a caller that writes nothing:
// the same four holds in the same order, with the error budget read and the
// intent an exhausted budget calls for left unraised. Its caller is the item
// view, which shows the hold standing at the row and offers approving through
// it, and [views] writes nothing at all — so the chain is written out again
// rather than shared, one call in the middle of it being what differs.
func (p *path) factoryHoldsAsRead(ctx context.Context, svc service.Service, it item.Item) (string, error) {
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
	budget, err := p.healthMonitor.ErrorBudget(ctx, healthmonitor.Watching{
		ID: svc.ID, Name: svc.Name, EnvironmentID: p.production.ID,
	})
	if err != nil {
		return "", err
	}
	return p.budgetHold(ctx, svc, it, budget)
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
