package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/window"
)

// Duty 10, which is undoing a change after it shipped — one the factory
// auto-approved. The design gives it two forms and this gives both: a rollback
// while the build it would return to is still running, and a revert after.
//
// A rollback is the deployer's work with the human as the actor: no role
// reaches a deploy target, and a human at Ops asking for one is asking the
// deployer. A revert is an intent through intake, taken in as the named human,
// and it takes the whole path like any other item.

// rollBackNow performs the rollback: the release below the one production is
// running is the target, and the deployer puts its build back. The source on
// every record names the human who asked, the deploy being performed on their
// instruction and not on the health monitor's reading — by is that human's own
// per-person key, which is what a screen and a terminal both hand it.
func (p *path) rollBackNow(ctx context.Context, svc service.Service, by, reason string) error {
	live, running, err := deploy.Current(ctx, p.d.pool, svc.ID, p.production.ID,
		serviceAddresses(p.production, svc))
	if err != nil {
		return err
	}
	if !running {
		return fmt.Errorf("factory: nothing of %s is running on production, so there is nothing to undo", svc.Name)
	}
	failed, err := release.Get(ctx, p.d.pool, live.ReleaseID)
	if err != nil {
		return err
	}
	target, found, err := p.healthMonitor.TargetBelow(ctx, healthmonitor.Watching{
		ID: svc.ID, Name: svc.Name, EnvironmentID: p.production.ID,
	}, failed.Number)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf(
			"factory: %s has no release below %d to return to, so the undo is a revert, raised at Ops",
			svc.Name, failed.Number)
	}
	if err := p.RollBack(ctx, healthmonitor.Rollback{
		ServiceID: svc.ID, ServiceName: svc.Name, EnvironmentID: p.production.ID,
		ToReleaseID: target.ID, ToBuildID: target.BuildID,
		FailedReleaseID: failed.ID,
		Source:          deploy.SourceOfHuman(by, reason),
	}); err != nil {
		return err
	}
	fmt.Fprintf(p.d.out, "Rolled back %s to release %d, on %s's instruction: %s\n",
		svc.Name, target.Number, by, reason)
	fmt.Fprintln(p.d.out, "  every deploy above the target is undone with it, master being linear")
	fmt.Fprintln(p.d.out, "  the rollback holds every deploy of this service but the revert until that revert ships")
	return nil
}

// revertIntent is the other form of the undo: an intent through intake, taken
// in as the named human at Ops, which decomposes into an item and takes the
// whole path. It is what duty 10 comes to once the build the rollback would
// return to is gone.
//
// The named human names the failed release, and intake writes it as the
// intent's evidence — the same link a detector's own revert carries, written
// the same way regardless of which of the two raised it. A halt or freeze
// passes a revert by reading that link and never by reading the source, which
// is what makes the link required here rather than optional.
func (p *path) revertIntent(ctx context.Context, actor record.Actor, svc service.Service, releaseID, reason string) error {
	failed, err := release.Get(ctx, p.d.pool, releaseID)
	if err != nil {
		return err
	}
	if failed.ServiceID != svc.ID {
		return fmt.Errorf("factory: release %s is not a release of %s", releaseID, svc.Name)
	}
	statement := fmt.Sprintf("%s: revert what release %d shipped — %s", svc.Name, failed.Number, reason)
	in, err := p.intake.TakeIn(ctx, actor, intent.Arrival{
		Source: intent.SourceOwner, Statement: statement, ProjectID: p.projectID,
		Evidence: intent.Evidence{ServiceID: svc.ID, ReleaseID: failed.ID},
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(p.d.out, "Revert intent %s taken in as %s: %s\n", in.ID, actor.Key, statement)
	fmt.Fprintf(p.d.out, "  it takes the whole path like any other item: `factory run -intent %q`\n", statement)
	return nil
}

// markRollback writes the mark and does what the mark does to the work in
// flight. It is its own function because two callers make the act: the
// subcommand above, and a test driving the mark without the process's own lease.
func markRollback(ctx context.Context, pool *pgxpool.Pool, token lease.Token,
	actor record.Actor, deployID, reason string) error {
	dep, err := deploy.Get(ctx, pool, deployID)
	if err != nil {
		return err
	}
	if dep.Undoing.FailedReleaseID == "" {
		return fmt.Errorf("factory: deploy %s undid no release, so it is no rollback to mark", deployID)
	}
	// What the mark ends is read before it is written, so the items it drops are
	// the ones outstanding at the moment the human marked it.
	revertIntentID, standing, outstanding, err := healthmonitor.RevertOfRollback(
		ctx, pool, dep.EnvironmentID, dep)
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	mark, err := window.WriteMark(ctx, tx, token, actor, deployID, dep.ServiceID, reason)
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	fmt.Printf("Mark %s written: rollback %s was not caused by release %s — %s\n",
		mark.ID, deployID, dep.Undoing.FailedReleaseID, reason)
	fmt.Println("The score and its learning pass exclude that release from here on")
	if !outstanding {
		return nil
	}
	// The drop is its own transaction, package item's write being one: the mark
	// is written first, so a drop that fails leaves the hold lifted and the item
	// standing rather than the item ended and the hold in force.
	items := item.NewDispatch(pool, token)
	for _, id := range standing {
		dropped, err := items.Drop(ctx, actor, id)
		if err != nil {
			return err
		}
		fmt.Printf("Revert item %s is dropped: the mark says there is no defect on master to revert\n", dropped.ID)
	}
	fmt.Printf("The hold rollback %s set is lifted: intent %s needed no revert, and the next release from master is measured again\n",
		deployID, revertIntentID)
	return nil
}
