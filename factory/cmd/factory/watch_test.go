// Tests of a bad deploy caught by its window and rolled back: an
// incident, the release failed, the target's build put back, and a
// revert intent at the start of the pipeline.
package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/incident"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/window"
)

// TestABadDeployIsCaughtByItsWindowAndRolledBack is the milestone's demonstration. The
// second release fails a share of the work it does, in no criterion's path — so every
// criterion in force passes and the change ships. Its window opens with the first
// release as its baseline, the boundary crosses, and the exit is harm: an incident, the
// release failed, the target's build put back, and a revert intent at the start of
// the pipeline.
func TestABadDeployIsCaughtByItsWindowAndRolledBack(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)

	first, err := run(ctx, d, of(theStatement))
	if err != nil {
		t.Fatalf("the first run stopped: %v\noutput so far:\n%s", err, out)
	}
	good := only(t, first)

	// The deliberately bad one: every other unit of work fails, and no criterion says
	// anything about how often the work succeeds.
	d.in = strings.NewReader(approvals)
	d.model = interviewed(2)
	res, err := run(ctx, d, of(theSecondStatement))
	if err != nil {
		t.Fatalf("the bad run stopped, and a rollback is not an error: %v\noutput so far:\n%s", err, out)
	}
	bad := only(t, res)

	// It shipped. Every criterion in force passed, which is the point: the defect is
	// in no criterion's path, so nothing before production had anything to say about it.
	if bad.releaseID == "" || bad.deployID == "" {
		t.Fatalf("the bad change did not ship, and the window is what was meant to catch it:\n%s", out)
	}
	for _, result := range bad.criteria {
		if result.Outcome.Blocks(false) {
			t.Errorf("criterion %s is %s, and a change the criteria catch is not what a window is for",
				result.CriterionID, result.Outcome)
		}
	}
	if bad.deployGate.humanDecided {
		t.Errorf("a human decided at the production deploy row (number %v against %v), and the window's authority is what has no human in it",
			bad.deployGate.number, bad.deployGate.threshold)
	}

	// The window closed failed, and it read the first release as its baseline.
	w, err := window.Get(ctx, d.pool, bad.windowID)
	if err != nil {
		t.Fatalf("reading the window: %v", err)
	}
	if w.Exit != window.ExitFailed {
		t.Fatalf("the window closed %q, want harm:\n%s", w.Exit, out)
	}
	if !w.PassedAvailable {
		t.Error("clean was unavailable to the second release, and the first one's window closed at the cap")
	}

	// The incident: on production, naming the release and the deploy that was running,
	// written by the health monitor and by no human.
	incidents, err := incident.ForService(ctx, d.pool, res.serviceID)
	if err != nil {
		t.Fatalf("reading the incidents: %v", err)
	}
	if len(incidents) != 1 {
		t.Fatalf("%d incidents were raised, want the one crossing: %+v", len(incidents), incidents)
	}
	raised := incidents[0]
	if raised.ReleaseID != bad.releaseID || raised.DeployID != bad.deployID {
		t.Errorf("the incident names release %s and deploy %s, the crossing was against %s and %s",
			raised.ReleaseID, raised.DeployID, bad.releaseID, bad.deployID)
	}
	if raised.EnvironmentID != res.environmentID {
		t.Errorf("the incident is on environment %s, and an incident is a record on production", raised.EnvironmentID)
	}
	if raised.Actor != healthmonitor.Actor || raised.Reading != incident.ReadingComparison {
		t.Errorf("the incident was written by %+v on the %s reading, want the health monitor on the comparison",
			raised.Actor, raised.Reading)
	}
	if !raised.Open() {
		t.Error("the incident is resolved, and a rollback does not resolve one — production is still worse")
	}

	// The revert: an intent at the start of the pipeline, from the detector, and
	// nothing on any item says it is a revert.
	revert, err := intent.Get(ctx, d.pool, raised.IntentID)
	if err != nil {
		t.Fatalf("reading the revert intent: %v", err)
	}
	if revert.Source != intent.SourceDetector {
		t.Errorf("the revert intent's source is %s, want the detector", revert.Source)
	}
	if revert.State != intent.StateUnrefined {
		t.Errorf("the revert intent is %s, and it takes the same stages and gates as any other", revert.State)
	}
	if items, err := item.ForIntent(ctx, d.pool, revert.ID); err != nil || len(items) != 0 {
		t.Errorf("the revert intent has %d items, %v; decomposition has not run over it yet", len(items), err)
	}

	// The rollback: a deploy record of the release returned to, naming what it
	// failed, the source that called for it, and the intent it raised.
	rollback, found, err := deploy.NewestRollback(ctx, d.pool, res.serviceID, res.environmentID)
	if err != nil || !found {
		t.Fatalf("NewestRollback = found %v, %v", found, err)
	}
	if rollback.ReleaseID != good.releaseID || rollback.BuildID != good.reverifiedBuildID {
		t.Errorf("the rollback returned to release %s build %s, want the first release %s build %s",
			rollback.ReleaseID, rollback.BuildID, good.releaseID, good.reverifiedBuildID)
	}
	if rollback.Undoing.FailedReleaseID != bad.releaseID {
		t.Errorf("the rollback failed %s, the window failed %s", rollback.Undoing.FailedReleaseID, bad.releaseID)
	}
	if len(rollback.Undoing.SkippedReleaseIDs) != 0 {
		t.Errorf("the rollback skipped %v, and nothing was above the failed release", rollback.Undoing.SkippedReleaseIDs)
	}
	if rollback.Undoing.Source != deploy.SourceHealthMonitorAtFailed {
		t.Errorf("the rollback's source is %q, want the health monitor at the failed exit", rollback.Undoing.Source)
	}
	// The rollback's record names the release it failed and not the intent the
	// crossing raised: that link is on the incident, which is where a reader
	// follows a rollback to what it asked for.
	if raised.IntentID != revert.ID {
		t.Errorf("the incident names revert intent %s, the health monitor raised %s", raised.IntentID, revert.ID)
	}
	if rollback.Status != deploy.StatusComplete {
		t.Errorf("the rollback's own status is %s, and it is a completed deploy of the release it returned to", rollback.Status)
	}

	// The failed release's own deploy is rolled back, and the release keeps its
	// number.
	// Rolled back is a completion per target and not a status of the record:
	// a rollback advances the deploys it undoes target by target as it completes
	// on each.
	undone, err := deploy.Targets(ctx, d.pool, bad.deployID)
	if err != nil {
		t.Fatalf("reading the failed deploy's targets: %v", err)
	}
	for _, target := range undone {
		if target.Completion != deploy.CompletionRolledBack {
			t.Errorf("target %s of the failed deploy is %s, want rolled back", target.Address, target.Completion)
		}
	}
	rel, err := release.Get(ctx, d.pool, bad.releaseID)
	if err != nil || rel.Number != 2 {
		t.Errorf("the failed release is number %d, %v; a rolled-back release keeps its number", rel.Number, err)
	}

	// What is running is the first release again, both in the store and on the target.
	current, running, err := deploy.Current(ctx, d.pool, res.serviceID, res.environmentID, []string{d.dir})
	if err != nil || !running {
		t.Fatalf("Current = running %v, %v", running, err)
	}
	// The release is the first one again. Which deploy record answers as current
	// is not asserted: the rollback and the deploy that first shipped that release
	// both name it and both are complete on every target, so the read orders them
	// by release number and has no tie to break.
	if current.ReleaseID != good.releaseID {
		t.Errorf("the current deploy is %s of release %s, want the first release %s back",
			current.ID, current.ReleaseID, good.releaseID)
	}
	onTarget, err := d.targets.at(d.dir).ReadRunning(ctx, deployerPrincipal, "demo", d.credential)
	if err != nil {
		t.Fatalf("reading what the target runs: %v", err)
	}
	if onTarget.Build != good.reverifiedBuildID {
		t.Errorf("the target runs %q, the rollback put build %s back", onTarget.Build, good.reverifiedBuildID)
	}

	// The rollback is reported and not requested, and reporting is not paging: mail and
	// chat carried it and the log holds no page event.
	if !strings.Contains(out.String(), "mail to owner") || !strings.Contains(out.String(), "chat to owner") {
		t.Errorf("the rollback was not reported on mail and chat:\n%s", out)
	}
	if events, err := p(ctx, t, d).notifier.EventsFor(ctx, raised.ID); err != nil || len(events) != 0 {
		t.Errorf("the rollback fired %d page event(s), %v; the factory does not page to inform", len(events), err)
	}

	// And the whole episode is readable as links: the walk from what is live now
	// reaches the intent behind the release that is serving.
	var walked bytes.Buffer
	if err := walk(ctx, d.pool, &walked, d.token, asPrincipal(owner(t, ctx, d.pool, d.token, d.human)), current.ID); err != nil {
		t.Fatalf("the walk from the rollback stopped: %v\noutput so far:\n%s", err, walked.String())
	}
	if !strings.Contains(walked.String(), theStatement) {
		t.Errorf("the walk from the rollback does not reach the restored release's statement:\n%s", walked.String())
	}
	if err := verifyLog(t, ctx, d); err != nil {
		t.Errorf("the chain does not verify after a rollback: %v", err)
	}
}
