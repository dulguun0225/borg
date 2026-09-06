// The mark a named human at Ops writes against a rollback, and the two things it
// does to work already in flight: it ends the revert item and it lifts the hold
// the rollback set.
package main

import (
	"context"
	"testing"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/window"
)

// TestTheMarkEndsTheRevertItemAndLiftsTheHold is what the mark does before the
// revert ships. The comparison was confounded — a difference between the arms
// that was not the change — so there is no defect on master for the hold to keep
// off production: the revert item is dropped, with Ops as the caller, and the
// hold lifts, the next release from master carrying the change and being
// measured again.
//
// The rollback and the incident stand: production was worse, whatever made it
// so, and nothing the factory did on its own is undone by the mark.
func TestTheMarkEndsTheRevertItemAndLiftsTheHold(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	rolled := rollBackABadRelease(ctx, t, d, out)

	path := p(ctx, t, d)
	svc := theServiceRecord(t, ctx, path)
	rollback, found, err := deploy.NewestRollback(ctx, d.pool, svc.ID, path.production.ID)
	if err != nil || !found {
		t.Fatalf("NewestRollback = found %v, %v", found, err)
	}

	// The revert item as decomposition would write it: the intent the health
	// monitor raised at the crossing, decomposed and not yet shipped.
	revert, err := path.decomposition.Create(ctx, decompositionActor, item.New{
		IntentID: rolled.revertIntentID, ServiceID: svc.ID, Branch: "revert/" + rolled.revertIntentID,
	}, "", "", nil)
	if err != nil {
		t.Fatalf("writing the revert item: %v", err)
	}
	// And one change of the service's own, which is what the hold holds.
	next := anotherItem(ctx, t, path, svc.ID)

	if held, err := path.rollbackHold(ctx, svc, next); err != nil || held == "" {
		t.Fatalf("the rollback holds %q, %v before the mark; its revert has not shipped", held, err)
	}

	// The mark itself and not the subcommand around it: the subcommand opens the
	// pool and takes the lease this test already holds.
	if err := markRollback(ctx, d.pool, d.token, owner(t, ctx, d.pool, d.token, d.human),
		rollback.ID, "a zone lost its network under the release's instances"); err != nil {
		t.Fatalf("marking the rollback: %v\noutput so far:\n%s", err, out)
	}

	// The mark stands, so nothing is outstanding and the hold is lifted.
	if marked, err := healthmonitor.MarkStands(ctx, d.pool, rollback.ID); err != nil || !marked {
		t.Fatalf("MarkStands = %v, %v", marked, err)
	}
	if held, err := path.rollbackHold(ctx, svc, next); err != nil || held != "" {
		t.Errorf("the rollback still holds %q, %v; the mark says there is no defect on master", held, err)
	}

	// The revert item is ended, and the change the hold was holding is not: the
	// mark drops what the rollback raised and nothing else.
	if ended := mustItem(ctx, t, d, revert.ID); ended.Stage != item.StageDropped {
		t.Errorf("the revert item is at %s, want dropped", ended.Stage)
	}
	if standing := mustItem(ctx, t, d, next.ID); standing.Stage == item.StageDropped {
		t.Error("the change the hold was holding was dropped with the revert")
	}

	// Nothing the factory did on its own is undone: the rollback's own record
	// stands and so does the window that failed the release.
	if _, stillThere, err := deploy.NewestRollback(ctx, d.pool, svc.ID, path.production.ID); err != nil || !stillThere {
		t.Errorf("NewestRollback after the mark = %v, %v; the rollback stands", stillThere, err)
	}
	failed, watched, err := window.ForRelease(ctx, d.pool, rollback.Undoing.FailedReleaseID)
	if err != nil || !watched {
		t.Fatalf("ForRelease(the failed release) = watched %v, %v", watched, err)
	}
	if failed.Exit != window.ExitFailed {
		t.Errorf("the window over the failed release reads %q after the mark, want failed", failed.Exit)
	}
}

// anotherItem is one change of the service's own, on an intent of its own: what
// the hold a rollback leaves holds, the revert being the one item it does not.
func anotherItem(ctx context.Context, t *testing.T, path *path, serviceID string) item.Item {
	t.Helper()
	in, err := path.intake.TakeIn(ctx, path.human, intent.Arrival{
		Source: intent.SourceOwner, Statement: "the next change to " + theService,
		ProjectID: path.projectID,
	})
	if err != nil {
		t.Fatalf("taking the next change in: %v", err)
	}
	it, err := path.decomposition.Create(ctx, decompositionActor, item.New{
		IntentID: in.ID, ServiceID: serviceID, Branch: "the-next-change",
	}, "", "", nil)
	if err != nil {
		t.Fatalf("writing the next change's item: %v", err)
	}
	return it
}

// mustItem reads one item back, a test that asserts on its stage having no use
// for a second error path at each call.
func mustItem(ctx context.Context, t *testing.T, d deps, id string) item.Item {
	t.Helper()
	it, err := item.Get(ctx, d.pool, id)
	if err != nil {
		t.Fatalf("reading item %s: %v", id, err)
	}
	return it
}
