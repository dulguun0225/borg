package main

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/incident"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/service"
)

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

// rollbackHolds is every rollback hold standing, in the form package item
// computes the graph's edges from: the service the hold stands on and the
// intent the revert was decomposed from, which is [path.rollbackHold]'s
// reading. The hold is held by no record, so decomposition reads it at every
// write from what this gate reads it from, through [rollbackHoldsSeam], and
// what it imposes — every unmerged item of the service other than the revert
// waiting on the revert item — is computed where the graph is.
//
// It reads every service and not the one being written: the cycle the union
// can hold runs through the hold of a service the write does not name — two
// reverts each declaring a dependency on an item the other's hold holds — and
// a read of one service's hold would pass it. What it costs is the hold's
// three reads per service at every write of an item, which is the cost of
// reading another component's state at a write.
func (p *path) rollbackHolds(ctx context.Context) ([]item.Hold, error) {
	services, err := service.All(ctx, p.d.pool)
	if err != nil {
		return nil, err
	}
	var holds []item.Hold
	for _, svc := range services {
		_, revertIntentID, outstanding, err := p.outstandingRevert(ctx, svc)
		if err != nil {
			return nil, err
		}
		if outstanding {
			holds = append(holds, item.Hold{ServiceID: svc.ID, RevertIntentID: revertIntentID})
		}
	}
	return holds, nil
}

// rollbackHoldsSeam is [item.RollbackHolds] over [path.rollbackHolds], wired
// onto [item.Decomposition.Holds] at composition so decomposition reads every
// rollback hold standing itself, at each write, from the same reading the
// production deploy gate makes. It is a type of its own and not a method
// named Standing on *path directly, because *path already answers
// [gate.Holds] with a method of that name and a different signature.
type rollbackHoldsSeam struct{ p *path }

func (s rollbackHoldsSeam) Standing(ctx context.Context) ([]item.Hold, error) {
	return s.p.rollbackHolds(ctx)
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
