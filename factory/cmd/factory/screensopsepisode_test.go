// Episode four of the demonstration, over HTTP: Ops. The deliberately bad
// release deployed, its window failing and the rollback performed by the watch
// pass, and then duty 10 in both its forms beside the mitigation, the mark and
// the page a human fires on their own judgment.
package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/screens"
	"github.com/dulguun0225/borg/factory/window"
)

// TestEpisodeFourRollsBackAtOpsAndFiresAPage is duty 10's first form and the
// three actions beside it, each through the call Ops makes: the release below
// the one running put back while its build is still on the target, a
// mitigation instructed and ended, the rollback marked as not caused by the
// release, and a page fired on a human's own judgment.
//
// It starts from two good releases rather than from the factory's own
// rollback, because a rollback returns production to the release below the one
// running and there is none below a service's first.
func TestEpisodeFourRollsBackAtOpsAndFiresAPage(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	// Two windows open at once, so the second release is not held behind the
	// first's own window and both reach production.
	installWindow(t, ctx, d, 2)

	if _, err := run(ctx, d, of(theStatement)); err != nil {
		t.Fatalf("the first run stopped: %v\noutput so far:\n%s", err, out)
	}
	d.decide = scriptedAtWork(approvals).decide
	res, err := run(ctx, d, of(theSecondStatement))
	if err != nil {
		t.Fatalf("the second run stopped: %v\noutput so far:\n%s", err, out)
	}
	second := only(t, res)

	s := newScreens(t, ctx, d, out)
	svc := theServiceRecord(t, ctx, s.p)
	address := "/api/service/" + svc.ID + "/on/" + s.p.production.ID

	// What Ops reads before the undo: the running release per target.
	var view screens.Service
	s.get(t, address, &view)
	if running := runningOnTheTarget(t, view); running.ReleaseNumber != 2 {
		t.Fatalf("the target runs release %d, the second run shipped 2\n%s", running.ReleaseNumber, out)
	}

	// Duty 10's first form: the deployer returns production to the release
	// below the one running, on a human's instruction and not on the health
	// monitor's reading.
	const why = "the checkout page is slower under it and the incident is open"
	s.mustCall(t, "rollBack", screens.RollBackArgs{ServiceID: svc.ID, Reason: why})

	s.get(t, address, &view)
	if running := runningOnTheTarget(t, view); running.ReleaseNumber != 1 {
		t.Errorf("the target runs release %d after the rollback, want the one below", running.ReleaseNumber)
	}
	rollback, found, err := deploy.NewestRollback(ctx, d.pool, svc.ID, s.p.production.ID)
	if err != nil || !found {
		t.Fatalf("NewestRollback = found %v, %v", found, err)
	}
	if rollback.Undoing.FailedReleaseID != second.releaseID {
		t.Errorf("the rollback undid release %s, the human asked for %s undone",
			rollback.Undoing.FailedReleaseID, second.releaseID)
	}
	if rollback.Undoing.Source != deploy.SourceOfHuman(s.p.human.Key, why) {
		t.Errorf("the rollback's source is %q, want the human who asked and their reason", rollback.Undoing.Source)
	}

	// A mitigation is a human's instruction and the record says which human.
	// It stands on the target until a human ends it, and Ops shows it while it
	// does.
	// The instance count and not a traffic shift: this platform moves a
	// process rather than traffic, so it serves no share. The count is one,
	// which is the one this platform runs — a mitigation the target refuses
	// outright is a refusal and not a mitigation standing.
	mitigationID := s.mustCall(t, "startMitigation", screens.StartMitigationArgs{
		TargetID:  rollback.ID,
		Operation: string(deploy.OperationSetInstanceCount),
		Count:     1,
	})
	if mitigationID == "" {
		t.Fatal("startMitigation answered with no id")
	}
	s.get(t, address, &view)
	if view.Mitigation == nil || view.Mitigation.ID != mitigationID {
		t.Errorf("Ops shows mitigation %+v while one stands, want %s", view.Mitigation, mitigationID)
	}
	s.mustCall(t, "endMitigation", screens.EndMitigationArgs{MitigationID: mitigationID})
	s.get(t, address, &view)
	if view.Mitigation != nil {
		t.Errorf("Ops shows a mitigation after it was ended: %+v", view.Mitigation)
	}

	// The mark: a named human at Ops saying the rollback was not caused by the
	// release it undid, so the score and its learning pass exclude it.
	s.mustCall(t, "markRollbackNotCaused", screens.MarkRollbackNotCausedArgs{
		DeployID: rollback.ID, Reason: "a zone lost its network under the release's instances",
	})
	marked, err := healthmonitor.MarkStands(ctx, d.pool, rollback.ID)
	if err != nil || !marked {
		t.Fatalf("MarkStands = %v, %v", marked, err)
	}

	// The one action of the twelve duties no subcommand ever made.
	s.mustCall(t, "firePage", screens.FirePageArgs{
		ServiceID: svc.ID, Reason: "the incident is not what the window measured",
	})
	if fired := firedPagesInTheLog(t, ctx, d); fired != 1 {
		t.Errorf("the log holds %d page(s) a human fired, want the one", fired)
	}

	var factory screens.Factory
	s.get(t, "/api/factory", &factory)
	if factory.PageChannel.HumanFiredPages != 1 {
		t.Errorf("Factory reports %d page(s) fired at Ops on a human's own judgment, want one",
			factory.PageChannel.HumanFiredPages)
	}
	if len(factory.PageChannel.PagesPerHuman) == 0 {
		t.Errorf("Factory reports no page per human, and one page reached one: %+v", factory.PageChannel)
	}

	if err := verifyLog(t, ctx, d); err != nil {
		t.Errorf("the chain does not verify after the episode: %v", err)
	}
}

// TestEpisodeFourRaisesTheRevertAfterTheFactorysOwnRollback is duty 10's
// second form and what precedes it: M4's deliberately bad release deployed,
// its window failing, and the rollback performed by the watch pass rather than
// by a human — after which the build the rollback would return to is gone from
// master, and the undo is a revert raised at Ops.
func TestEpisodeFourRaisesTheRevertAfterTheFactorysOwnRollback(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	rolled := rollBackABadRelease(ctx, t, d, out)

	s := newScreens(t, ctx, d, out)
	svc := theServiceRecord(t, ctx, s.p)
	rollback, found, err := deploy.NewestRollback(ctx, d.pool, svc.ID, s.p.production.ID)
	if err != nil || !found {
		t.Fatalf("NewestRollback = found %v, %v", found, err)
	}

	// The window over the release the factory rolled back failed, which is
	// what called for the rollback: the watch pass is the one thing that
	// closes a window.
	failed, watched, err := window.ForRelease(ctx, d.pool, rollback.Undoing.FailedReleaseID)
	if err != nil || !watched {
		t.Fatalf("ForRelease(the failed release) = watched %v, %v", watched, err)
	}
	if failed.Exit != window.ExitFailed {
		t.Fatalf("the window over the rolled-back release reads %q, want failed", failed.Exit)
	}
	if rollback.Undoing.Source != deploy.SourceHealthMonitorAtFailed {
		t.Errorf("the rollback's source is %q, want the health monitor's reading at the window's failed exit",
			rollback.Undoing.Source)
	}

	// The revert the crossing raised is an intent the factory took in. A human
	// at Ops raising one names the failed release themselves, and intake writes
	// that link as the intent's evidence either way.
	raised, err := intent.Get(ctx, d.pool, rolled.revertIntentID)
	if err != nil {
		t.Fatalf("reading the revert the factory raised: %v", err)
	}
	if raised.Source != intent.SourceDetector {
		t.Errorf("the revert the factory raised has source %s, want the detector's", raised.Source)
	}
	rel, err := release.Get(ctx, d.pool, rollback.Undoing.FailedReleaseID)
	if err != nil {
		t.Fatalf("reading the failed release: %v", err)
	}
	s.mustCall(t, "raiseRevert", screens.RaiseRevertArgs{
		ServiceID: svc.ID, ReleaseID: rel.ID,
		Reason: "the window failed it and master still holds the change",
	})

	// Ops reads the running release per target, which after the rollback is the
	// release below the one that failed.
	var view screens.Service
	s.get(t, "/api/service/"+svc.ID+"/on/"+s.p.production.ID, &view)
	if running := runningOnTheTarget(t, view); running.ReleaseNumber >= rel.Number {
		t.Errorf("the target runs release %d, want one below the failed %d", running.ReleaseNumber, rel.Number)
	}
	if len(view.OpenIncidents) == 0 {
		t.Errorf("Ops shows no open incident, and the crossing opened one: %+v", view)
	}

	if err := verifyLog(t, ctx, d); err != nil {
		t.Errorf("the chain does not verify after the episode: %v", err)
	}
}

// runningOnTheTarget is the release running on the one target these tests'
// production environment holds: Ops lists one row per target of the
// environment, so a second row would be a second target and not a second
// deploy.
func runningOnTheTarget(t *testing.T, view screens.Service) screens.TargetRelease {
	t.Helper()
	if len(view.Targets) != 1 {
		t.Fatalf("the service on this environment shows %d target(s), want the one production holds: %+v",
			len(view.Targets), view.Targets)
	}
	return view.Targets[0]
}

// firedPagesInTheLog is how many page events name a page a human fired on
// their own judgment, which is the one wait kind no component writes.
func firedPagesInTheLog(t *testing.T, ctx context.Context, d deps) int {
	t.Helper()
	fired := 0
	for _, row := range readLog(t, ctx, d) {
		if row.Shape != decisionlog.ShapePageEvent {
			continue
		}
		var payload notifier.Payload
		if json.Unmarshal([]byte(row.Payload), &payload) != nil {
			continue
		}
		if payload.WaitKind == string(notifier.KindOwnerFired) {
			fired++
		}
	}
	return fired
}
