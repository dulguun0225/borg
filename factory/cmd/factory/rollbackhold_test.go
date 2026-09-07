// Tests of the hold a rollback leaves: it lifts once the revert ships,
// and approving through it redelivers the defect the rollback removed.
package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/screens"
	"github.com/dulguun0225/borg/factory/window"
)

// TestTheRollbackHoldsUntilTheRevertShips is the hold a rollback leaves and its two
// exceptions. Master keeps the change that was rolled back and the next item was built
// on master, so deploying it would redeliver the defect just removed — and the revert
// itself is not held, and deploys ahead of every release the hold is holding.
func TestTheRollbackHoldsUntilTheRevertShips(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	// The window limit at two, so that what holds the release behind the revert is the
	// rollback and not the revert's own open window. At the limit of one the score
	// supplies, both holds
	// stand at once and a test could not tell which of them stopped the deploy.
	installWindow(t, ctx, d, 2)
	rolled := rollBackABadRelease(ctx, t, d, out)

	// A fresh change while the revert is outstanding: it merges, it is minted a
	// number, and its deploy is held.
	// The runs after the rollback are given verdicts to type. The score has learned
	// from the episode this test just drove — a change auto-passed on the number and
	// then failed by its window lowers the threshold that row supplies — so rows
	// that auto-passed before it are decided by a human after it, which is the loop
	// working rather than the test fighting it.
	d.decide = scriptedAtWork(approvals).decide
	d.model = interviewed(0)
	held, err := run(ctx, d, of(theThirdStatement))
	if err != nil {
		t.Fatalf("the run stopped, and a hold is not an error: %v\noutput so far:\n%s", err, out)
	}
	stuck := only(t, held)
	if stuck.releaseID == "" {
		t.Fatalf("the change did not merge, and the hold is at the deploy:\n%s", out)
	}
	if stuck.deployID != "" {
		t.Errorf("the change deployed %s while the revert had not shipped", stuck.deployID)
	}
	if !strings.Contains(stuck.factoryHold, gate.HoldRollbackAwaitingRevert) {
		t.Errorf("the hold is %q, want the rollback's", stuck.factoryHold)
	}

	// The revert and one more change, in one run. The revert is authored from the
	// intent the health monitor already took in — named directly, package intent's
	// rewrite offering no statement-keyed lookup [path.take] could resume it
	// through otherwise — it is not held, and it deploys ahead of the release the
	// hold is holding, which is the one place the number does not order deploys.
	d.decide = scriptedAtWork(approvals).decide
	res, err := run(ctx, d, []asked{
		{statement: theFourthStatement, services: []string{theService}},
		{statement: rolled.revertStatement, services: []string{theService}, resumeIntentID: rolled.revertIntentID},
	})
	if err != nil {
		t.Fatalf("the revert run stopped: %v\noutput so far:\n%s", err, out)
	}
	var revert, other *candidate
	for _, c := range res.candidates {
		if c.intentID == rolled.revertIntentID {
			revert = c
			continue
		}
		other = c
	}
	if revert == nil {
		t.Fatalf("no candidate was authored from the revert intent %s; the run authored %d",
			rolled.revertIntentID, len(res.candidates))
	}
	if revert.intentID != rolled.revertIntentID {
		t.Errorf("the revert candidate names intent %s, want the one the rollback raised, %s", revert.intentID, rolled.revertIntentID)
	}
	if revert.deployID == "" {
		t.Fatalf("the revert did not deploy, and the hold does not hold its own revert:\n%s", out)
	}
	if !strings.Contains(out.String(), "deploys ahead of") {
		t.Errorf("the run does not report the revert deploying ahead of what the hold is holding:\n%s", out)
	}

	// The hold lifted once the revert shipped, so the other change in the same run
	// deployed behind it.
	if other == nil || other.deployID == "" {
		t.Errorf("the change behind the revert did not deploy after it shipped:\n%s", out)
	}
	shipped, err := healthmonitor.Shipped(ctx, d.pool, res.environmentID, rolled.revertIntentID)
	if err != nil || !shipped {
		t.Errorf("Shipped(the revert intent) = %v, %v", shipped, err)
	}
	path := p(ctx, t, d)
	stuckItem, err := item.Get(ctx, d.pool, stuck.itemID)
	if err != nil {
		t.Fatalf("reading the held item: %v", err)
	}
	if hold, err := path.rollbackHold(ctx, theServiceRecord(t, ctx, path), stuckItem); err != nil || hold != "" {
		t.Errorf("the rollback still holds %q, %v; its revert has shipped", hold, err)
	}
}

// TestApprovingThroughARollbackHoldRedeliversTheDefect is the emergency action the
// design keeps at the production deploy row — approve now, not skip — and the most
// damaging thing in the factory to approve through: what it accepts is the defect that
// was just removed.
func TestApprovingThroughARollbackHoldRedeliversTheDefect(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	rollBackABadRelease(ctx, t, d, out)

	// The change the hold stops carries the defect. On a real factory it carries it by
	// having been built on the master the failed release left; here the fake's
	// implementer rewrites every file whole, so it carries it by writing the same failing
	// emitter again. The two are indistinguishable to everything downstream, which is what
	// makes this the consequence the design names.
	d.decide = scriptedAtWork(approvals).decide
	d.model = interviewed(2)
	held, err := run(ctx, d, of(theThirdStatement))
	if err != nil {
		t.Fatalf("the run stopped: %v\noutput so far:\n%s", err, out)
	}
	stuck := only(t, held)
	if stuck.deployID != "" || stuck.factoryHold == "" {
		t.Fatalf("the change was not held: deploy %q hold %q\n%s", stuck.deployID, stuck.factoryHold, out)
	}

	// The action is at Work, on the item: the hold stops the production deploy
	// row being fired, so what a screen offers is firing it and approving it in
	// one call rather than a verdict against a row already open.
	s := newScreens(t, ctx, d, out)
	var view screens.Item
	s.get(t, "/api/item/"+stuck.itemID, &view)
	if !strings.Contains(view.FactoryHold, gate.HoldRollbackAwaitingRevert) {
		t.Fatalf("the item view reads the hold as %q, want the rollback's", view.FactoryHold)
	}
	if !view.ApprovableThrough {
		t.Error("the item view offers no way through a hold the design keeps an emergency action at")
	}
	s.mustCall(t, "approveThroughHold", screens.ApproveThroughHoldArgs{
		ItemID:         stuck.itemID,
		Reason:         "the incident is worse than the defect; ship it",
		OpenedInWorkAt: theOpenedInWorkAt,
	})
	if !strings.Contains(out.String(), "approving through accepts what the hold was preventing") {
		t.Errorf("the run does not say what approving through accepts:\n%s", out)
	}

	// The close event carries the reason and when the actor opened the item in
	// Work, which is the field no terminal could fill.
	approving := productionDeployClose(t, ctx, d, stuck.itemID)
	if approving.OpenedInWorkAt != theOpenedInWorkAt {
		t.Errorf("the close event says the row was opened in Work at %q, want %q",
			approving.OpenedInWorkAt, theOpenedInWorkAt)
	}
	if closing := closingPayload(t, approving); closing.Reason == "" {
		t.Error("the close event carries no reason, and approving through a hold says why")
	}

	// The deploy happened and a window opened over it. The reads and the watch
	// below are made on the composition behind the screens rather than on one
	// composed here: composing a path restarts the health monitor, which
	// evaluates every open window again — and the window this deploy just
	// opened would be failed by that restart before the deploy is read.
	current, running, err := deploy.Current(ctx, d.pool, held.serviceID, held.environmentID, []string{d.dir})
	if err != nil || !running {
		t.Fatalf("Current = running %v, %v", running, err)
	}
	if current.ReleaseID != stuck.releaseID {
		t.Fatalf("what runs is release %s, the human approved %s through", current.ReleaseID, stuck.releaseID)
	}

	// And the defect is back, which is what "redelivers" means: the change was built
	// on the master that still holds the failed release's code, so the very next
	// reading of its window fails it again.
	if err := s.p.watchTo(ctx, theServiceRecord(t, ctx, s.p), time.Now().Add(theWatchFor), theWatchEvery); err != nil {
		t.Fatalf("the watch stopped: %v\noutput so far:\n%s", err, out)
	}
	w, watched, err := window.ForRelease(ctx, d.pool, stuck.releaseID)
	if err != nil || !watched {
		t.Fatalf("ForRelease = watched %v, %v", watched, err)
	}
	if w.Exit != window.ExitFailed {
		t.Errorf("the window over the approved-through release closed %q, and what was approved through was the defect itself",
			w.Exit)
	}
}

// productionDeployClose is the close event at one item's production deploy
// row, read out of the log. The row is fired by the call that approves through
// the hold and by no pass, so this is where what that call wrote is read.
func productionDeployClose(t *testing.T, ctx context.Context, d deps, itemID string) decisionlog.Row {
	t.Helper()
	rows := readLog(t, ctx, d)
	openingID := ""
	for _, row := range rows {
		if row.Shape != decisionlog.ShapeDecision || row.Part != decisionlog.PartOpen {
			continue
		}
		opening := openingPayload(t, row)
		if opening.Gate == gate.DeployToProduction.String() && opening.ItemID == itemID {
			openingID = row.ID
		}
	}
	if openingID == "" {
		t.Fatalf("the log holds no production deploy row for item %s", itemID)
	}
	for _, row := range rows {
		if row.Part == decisionlog.PartClose && row.Closes == openingID {
			return row
		}
	}
	t.Fatalf("the production deploy row of item %s was never closed", itemID)
	return decisionlog.Row{}
}
