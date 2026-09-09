package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/screens"
	"github.com/dulguun0225/borg/factory/service"
)

// Ops is an acting screen and not watch-only: duty 10 in both its forms, a
// mitigation instructed and ended, the mark that a rollback was not caused by
// the release, and the one call no subcommand makes — a page a human fires on
// their own judgment.

// RollBack is duty 10's first form: the deployer returns production to the
// release below the one running, which is what "while the build it would
// return to is still running" comes to on this platform.
func (c *calls) RollBack(ctx context.Context, who principal.Principal, args screens.RollBackArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	if args.Reason == "" {
		return fmt.Errorf("%w: the record says what the undo was for, and this one says nothing", screens.ErrRefused)
	}
	svc, err := service.Get(ctx, c.p.d.pool, args.ServiceID)
	if err != nil {
		return fmt.Errorf("%w: %s", screens.ErrNotFound, args.ServiceID)
	}
	// The source on the rollback's own record names the human who asked, the
	// deploy being performed on their instruction and not on the health
	// monitor's reading.
	if err := c.p.rollBackNow(ctx, svc, actor.Key, args.Reason); err != nil {
		return err
	}
	c.changed("service", svc.ID)
	// The rollback is one of the two halves of the approve-and-undone pair at
	// Factory, which counts every rollback in the records.
	c.changed("factory", listAddressID)
	return nil
}

// RaiseRevert is duty 10's second form: the revert intent, naming the release
// that failed, which takes the whole path like any other item.
func (c *calls) RaiseRevert(ctx context.Context, who principal.Principal, args screens.RaiseRevertArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	if args.Reason == "" {
		return fmt.Errorf("%w: the record says what the undo was for, and this one says nothing", screens.ErrRefused)
	}
	svc, err := service.Get(ctx, c.p.d.pool, args.ServiceID)
	if err != nil {
		return fmt.Errorf("%w: %s", screens.ErrNotFound, args.ServiceID)
	}
	if err := c.p.revertIntent(ctx, actor, svc, args.ReleaseID, args.Reason); err != nil {
		return err
	}
	c.changed("work", listAddressID)
	// The badge counts what an intent's items leave waiting, which is why
	// [calls.SupplyIntent] announces the home view too: a revert is an intent
	// taken in and reaches it the same way.
	c.changed("home", listAddressID)
	c.changed("service", svc.ID)
	return nil
}

// StartMitigation instructs one of the mitigation class's two operations on a
// target. The factory performs neither on its own — a mitigation is a human's
// instruction and the record says which human.
func (c *calls) StartMitigation(ctx context.Context, who principal.Principal, args screens.StartMitigationArgs) (string, error) {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return "", err
	}
	performed, err := c.p.mitigate(ctx, actor, args.TargetID, deploy.Operation(args.Operation),
		args.Share, int(args.Count))
	if err != nil {
		return "", err
	}
	c.changed("service", performed.serviceID)
	return performed.id, nil
}

// EndMitigation ends a mitigation already standing; what it did to the target
// stands until a deploy replaces it.
//
// The service it stands on is read before it is ended, because a mitigation is
// keyed by the deploy it was performed against and one that has ended is no
// longer among the standing ones. Announcing it is what [calls.StartMitigation]
// already does: the mitigation renders on that service's own view, and
// [screens.Server.Changed] fans a service's change out to Ops and never Ops
// back to a service — so Ops alone would leave every open per-service view
// showing a mitigation that has ended.
func (c *calls) EndMitigation(ctx context.Context, who principal.Principal, args screens.EndMitigationArgs) error {
	ending, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	serviceID, err := c.serviceMitigated(ctx, args.MitigationID)
	if err != nil {
		return err
	}
	// A mitigation stands until a human ends it at Ops, and the record names
	// which human: the caller at the screen, who is the actor every call from
	// here is made as.
	if err := c.p.deploys.EndMitigation(ctx, ending, args.MitigationID); err != nil {
		return err
	}
	c.changed("ops", listAddressID)
	if serviceID != "" {
		c.changed("service", serviceID)
	}
	return nil
}

// serviceMitigated is the service one standing mitigation stands on, and empty
// where no standing mitigation carries that id — which [deploy.Writer.EndMitigation]
// is what refuses, so nothing is decided here.
func (c *calls) serviceMitigated(ctx context.Context, mitigationID string) (string, error) {
	standing, err := deploy.StandingMitigations(ctx, c.p.d.pool)
	if err != nil {
		return "", err
	}
	for _, one := range standing {
		if one.ID != mitigationID {
			continue
		}
		dep, err := deploy.Get(ctx, c.p.d.pool, one.DeployID)
		if err != nil {
			return "", err
		}
		return dep.ServiceID, nil
	}
	return "", nil
}

// MarkRollbackNotCaused is a named human at Ops saying that a rollback was not
// caused by the release it undid: the score and its learning pass exclude that
// release from then on.
func (c *calls) MarkRollbackNotCaused(ctx context.Context, who principal.Principal, args screens.MarkRollbackNotCausedArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	if args.Reason == "" {
		return fmt.Errorf("%w: the mark says what caused the rollback instead, and this one says nothing", screens.ErrRefused)
	}
	if err := markRollback(ctx, c.p.d.pool, c.p.d.token, actor, args.DeployID, args.Reason); err != nil {
		return err
	}
	dep, err := deploy.Get(ctx, c.p.d.pool, args.DeployID)
	if err != nil {
		return err
	}
	c.changed("service", dep.ServiceID)
	c.changed("work", listAddressID)
	return nil
}

// FirePage is the one action of the twelve duties no subcommand makes: a page
// a human fires on their own judgment. Nothing scores it and no bound applies
// to it; what limits it is that a page nobody needed makes its recipient
// slower to answer the next one.
//
// It routes the way every page about deployed software routes — to the holders
// of duty 12, taking over issues the factory cannot fix, widened once to the
// owner where nobody holds it — because a service is not a duty and the design
// gives a page no routing of its own.
func (c *calls) FirePage(ctx context.Context, who principal.Principal, args screens.FirePageArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	if args.Reason == "" {
		return fmt.Errorf("%w: a page a human fires says why, and this one says nothing", screens.ErrRefused)
	}
	svc, err := service.Get(ctx, c.p.d.pool, args.ServiceID)
	if err != nil {
		return fmt.Errorf("%w: %s", screens.ErrNotFound, args.ServiceID)
	}
	if c.p.notifier == nil {
		return errors.New("factory: this composition reaches no notifier, so a page cannot be fired")
	}
	if _, err := c.p.notifier.Notify(ctx, notifier.Wait{
		Row:  record.NewID(firedPagePrefix),
		Kind: notifier.KindOwnerFired,
		Waiting: fmt.Sprintf("%s fired a page about %s on their own judgment: %s",
			actor.Key, svc.Name, args.Reason),
		Holding: people.OfDuty(takeOverIssues),
		// The kind pages always, so the wait has to assert the condition:
		// package notifier refuses a caller that denies what the condition
		// already answers for the kind. What is worse until a human ends this
		// one is whatever the human who fired it saw, which is the whole of
		// what a page on a human's own judgment claims.
		Worse:     true,
		ServiceID: svc.ID,
	}); err != nil {
		return err
	}
	c.changed("service", svc.ID)
	return nil
}

// firedPagePrefix is what the row a human's own page names is prefixed with.
// The page channel's rows are keyed by what waits, and a page a human fired
// waits on nothing else — so it names a row of its own rather than a record,
// which is what keeps two pages about one service two rows.
const firedPagePrefix = "firedpage"

// mitigated is the mitigation one instruction performed: its id and the
// service it stands on, which is the address the screen it changed is at.
type mitigated struct {
	id        string
	serviceID string
}

// mitigate performs one mitigation on the target address given. It is its own
// function because two callers make the act — the mitigate subcommand and Ops
// — and the target the deployer reaches is read the same way for both: the
// address the service runs on, that being the set every reader of targets
// reads.
func (p *path) mitigate(ctx context.Context, actor record.Actor, deployID string,
	operation deploy.Operation, share float64, count int) (mitigated, error) {
	dep, err := deploy.Get(ctx, p.d.pool, deployID)
	if err != nil {
		return mitigated{}, err
	}
	svc, err := p.serviceOf(ctx, dep.ServiceID)
	if err != nil {
		return mitigated{}, err
	}
	address := p.d.dir
	if addresses := serviceAddresses(p.production, svc); len(addresses) > 0 {
		address = addresses[0]
	}
	performed, err := deploy.Mitigate(ctx, p.deploys, deploy.Mitigating{
		Actor:       actor,
		Principal:   deployerPrincipal,
		Operation:   operation,
		Address:     address,
		Target:      p.d.targets.at(address),
		DeployID:    deployID,
		ServiceName: svc.Name,
		Build:       dep.BuildID,
		Share:       share,
		Count:       count,
		Credential:  p.d.credential,
	})
	if err != nil {
		return mitigated{}, err
	}
	fmt.Fprintf(p.d.out, "Mitigation %s performed on %s by %s: %s\n",
		performed.ID, address, actor.Key, operation)
	fmt.Fprintln(p.d.out, "  it stands until a human ends it, and the drift detector reads the target against the deploy record meanwhile")
	return mitigated{id: performed.ID, serviceID: svc.ID}, nil
}
