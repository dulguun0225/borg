package main

import (
	"context"
	"errors"
	"flag"
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

// rollbackCommand is `factory rollback <service>`. It returns production to the
// release below the one running, where the build that release was cut from is
// still on the target — which is what "while the build it would return to is
// still running" comes to on this platform, the binary being a file the deploy
// left in production's directory. Where it is not, the undo is a revert and
// -revert is what asks for one.
func rollbackCommand(args []string) error {
	flags := flag.NewFlagSet("rollback", flag.ContinueOnError)
	secrets := flags.String("secrets", "", "path of the secrets file (required)")
	targets := flags.String("targets", "", "the directory the local target runs releases from (required)")
	human := flags.String("human", "owner", "the named human at Ops asking for the undo")
	revert := flags.Bool("revert", false, "raise a revert intent instead, which is the undo after the build is gone")
	reason := flags.String("reason", "", "why the change is being undone, which the record carries")

	name := ""
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		name, args = args[0], args[1:]
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if name == "" || flags.NArg() != 0 {
		return errors.New("factory rollback: one argument, the service, and then any flags")
	}
	if *reason == "" {
		return errors.New("factory rollback: -reason is what the record says the undo was for")
	}

	return withPath(pathFlags{secrets: *secrets, targets: *targets, human: *human},
		func(ctx context.Context, p *path) error {
			svc, found, err := service.ByName(ctx, p.d.pool, name)
			if err != nil {
				return err
			}
			if !found {
				return fmt.Errorf("factory rollback: no service named %q", name)
			}
			if *revert {
				return p.revertIntent(ctx, svc, *reason)
			}
			return p.rollBackNow(ctx, svc, *reason)
		})
}

// rollBackNow performs the rollback: the release below the one production is
// running is the target, and the deployer puts its build back. The actor on
// every record is the human who asked, the deploy being performed on their
// instruction and not on the health monitor's reading.
func (p *path) rollBackNow(ctx context.Context, svc service.Service, reason string) error {
	live, running, err := deploy.Current(ctx, p.d.pool, svc.ID, p.production.ID,
		serviceAddresses(p.production, svc))
	if err != nil {
		return err
	}
	if !running {
		return fmt.Errorf("factory rollback: nothing of %s is running on production, so there is nothing to undo", svc.Name)
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
			"factory rollback: %s has no release below %d to return to, so the undo is a revert: `factory rollback %s -revert`",
			svc.Name, failed.Number, svc.Name)
	}
	if err := p.RollBack(ctx, healthmonitor.Rollback{
		ServiceID: svc.ID, ServiceName: svc.Name, EnvironmentID: p.production.ID,
		ToReleaseID: target.ID, ToBuildID: target.BuildID,
		FailedReleaseID: failed.ID,
		Source:          deploy.SourceOfHuman(p.d.human, reason),
	}); err != nil {
		return err
	}
	fmt.Fprintf(p.d.out, "Rolled back %s to release %d, on %s's instruction: %s\n",
		svc.Name, target.Number, p.d.human, reason)
	fmt.Fprintln(p.d.out, "  every deploy above the target is undone with it, master being linear")
	fmt.Fprintln(p.d.out, "  the rollback holds every deploy of this service but the revert until that revert ships")
	return nil
}

// revertIntent is the other form of the undo: an intent through intake, taken
// in as the named human at Ops, which decomposes into an item and takes the
// whole path. It is what duty 10 comes to once the build the rollback would
// return to is gone.
func (p *path) revertIntent(ctx context.Context, svc service.Service, reason string) error {
	live, running, err := deploy.Current(ctx, p.d.pool, svc.ID, p.production.ID,
		serviceAddresses(p.production, svc))
	if err != nil {
		return err
	}
	statement := fmt.Sprintf("%s: revert what release %s shipped — %s", svc.Name, live.ReleaseID, reason)
	if !running {
		statement = fmt.Sprintf("%s: revert what shipped — %s", svc.Name, reason)
	}
	in, err := p.intake.TakeIn(ctx, p.human, intent.Arrival{
		Source: intent.SourceOwner, Statement: statement, ProjectID: p.projectID,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(p.d.out, "Revert intent %s taken in as %s: %s\n", in.ID, p.d.human, statement)
	fmt.Fprintf(p.d.out, "  it takes the whole path like any other item: `factory run -intent %q`\n", statement)
	return nil
}

// markRollbackCommand is `factory mark-rollback <deploy-id>`: a named human at
// Ops saying that a rollback was not caused by the release it undid. The score
// and its learning pass exclude the release the mark points at, so a rollback
// performed for a reason outside the change teaches the score nothing.
//
// The mark points at the rollback's own deploy record, which is where every
// field of the rollback is.
//
// It does two further things where the revert has not shipped: it ends the
// revert item — dropped, with Ops as the caller — and it lifts the hold the
// rollback set, which the production deploy row lifts by reading the mark. There
// is no defect on master for the hold to keep off production, so the next
// release from master carries the change, opens a window of its own and is
// measured again; until one comes the service stays on the release the rollback
// returned to. After the revert has shipped the change is gone from master and
// the mark corrects the evidence only.
func markRollbackCommand(args []string) error {
	flags := flag.NewFlagSet("mark-rollback", flag.ContinueOnError)
	human := flags.String("human", "owner", "the named human at Ops writing the mark")
	reason := flags.String("reason", "", "what caused the rollback, if not the release")

	deployID := ""
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		deployID, args = args[0], args[1:]
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if deployID == "" || flags.NArg() != 0 {
		return errors.New("factory mark-rollback: one argument, the rollback's deploy id, and then any flags")
	}
	if *reason == "" {
		return errors.New("factory mark-rollback: -reason is what the mark says caused the rollback instead")
	}

	return withPool(func(ctx context.Context, pool *pgxpool.Pool, token lease.Token) error {
		actor, err := humanNamed(ctx, pool, token, *human)
		if err != nil {
			return err
		}
		return markRollback(ctx, pool, token, actor, deployID, *reason)
	})
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
		return fmt.Errorf("factory mark-rollback: deploy %s undid no release, so it is no rollback to mark", deployID)
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
