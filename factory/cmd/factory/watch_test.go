// Roadmap M4's demonstration: everything downstream of a deploy, driven through
// the same run function the run subcommand calls. The milestone's own claim is
// a deliberately bad deploy — shipped, caught by its window, rolled back, and
// the whole episode readable as links — and the rest of this file is the parts
// of that no single run reaches: the window limit, the hold a rollback leaves,
// approving through it, a crossing found after the window closed, and a
// mismatch the drift detector raised.
//
// The helpers every one of these shares are in main_test.go, including the watch
// window these tests author and why the values the score supplies are unreachable
// here.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/boundary"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/driftdetector"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/incident"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/window"
)

// TestAWindowOpensOverEveryProductionDeploy is the analysis window at its weakest, which
// is where every service starts: a first release has nothing below it to be compared
// against, so the passed exit is not available to it, nothing about it is discovered by
// watching, and its window ends at the cap.
func TestAWindowOpensOverEveryProductionDeploy(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)

	res, err := run(ctx, d, of(theStatement))
	if err != nil {
		t.Fatalf("the path stopped: %v\noutput so far:\n%s", err, out)
	}
	c := only(t, res)

	if c.windowID == "" {
		t.Fatalf("no window opened over the production deploy:\n%s", out)
	}
	w, err := window.Get(ctx, d.pool, c.windowID)
	if err != nil {
		t.Fatalf("reading the window: %v", err)
	}
	if w.DeployID != c.deployID || w.ReleaseID != c.releaseID {
		t.Errorf("the window names deploy %s and release %s, the run deployed %s of %s",
			w.DeployID, w.ReleaseID, c.deployID, c.releaseID)
	}
	if w.Actor != healthmonitor.Actor {
		t.Errorf("the window's actor is %+v, and the health monitor is what writes one", w.Actor)
	}

	// The parameters are copied onto the record at the open, which is what makes a
	// reading at an exit interpretable: an owner re-authoring the size afterwards does
	// not change what a window already closed is read to have meant.
	if w.Size != theWindowSize || w.Confidence != theWindowConfidence || w.CapSeconds != theWindowCap {
		t.Errorf("the window carries size %v, confidence %v, cap %v; the owner authored %v, %v, %v",
			w.Size, w.Confidence, w.CapSeconds, theWindowSize, theWindowConfidence, theWindowCap)
	}
	if w.Formula != boundary.Formula {
		t.Errorf("the window names formula %q, want %q — the size and the confidence alone do not say what was done with them",
			w.Formula, boundary.Formula)
	}
	if w.PolicyVersion == "" || w.ScoreVersion == "" {
		t.Errorf("the window names policy version %q and score version %q", w.PolicyVersion, w.ScoreVersion)
	}

	// The passed exit is not available and the window timed out, which is weak
	// protection reported as weak rather than a comparison that ran out of time.
	if w.PassedAvailable {
		t.Error("the window says clean was available to a service's first release, and there is nothing below it to compare against")
	}
	if w.Exit != window.ExitTimedOut {
		t.Errorf("the window closed %q, want the cap: nothing can clear a first release early", w.Exit)
	}
	if !strings.Contains(out.String(), boundary.NoBaseline) {
		t.Errorf("the run does not report that neither exit was reachable:\n%s", out)
	}

	// CloseEvent at the cap counts as a release the factory can return to, which is what
	// makes the second release measurable at all.
	if !w.Exit.Counts() {
		t.Error("a window closed at the cap does not count as a release to return to, and a release nothing failed is one")
	}
	// A rollback of it has no target all the same, there being nothing below it.
	if _, found, err := healthmonitor.TargetBelow(ctx, d.pool, res.serviceID, 1); err != nil || found {
		t.Errorf("TargetBelow(1) = found %v, %v; a service's first release has no target at all", found, err)
	}

	// One window per release watched: a second deploy of the same release opens none.
	if _, isNew, err := p(ctx, t, d).healthMonitor.Open(ctx, watching(res, "demo"),
		c.deployID, c.releaseID, "score-again", false); err != nil || isNew {
		t.Errorf("a second Open over release %s = new %v, %v; one release is watched once", c.releaseID, isNew, err)
	}
}

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
	d.in = strings.NewReader("")
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
		if result.Outcome.Blocks() {
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
	if raised.Actor != healthmonitor.Actor || raised.Crossing != healthmonitor.Crossing {
		t.Errorf("the incident was written by %+v saying %q", raised.Actor, raised.Crossing)
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
	if len(rollback.Undoing.SweptReleaseIDs) != 0 {
		t.Errorf("the rollback swept %v, and nothing was above the failed release", rollback.Undoing.SweptReleaseIDs)
	}
	if rollback.Undoing.Source != deploy.SourceHealthMonitorAtFailed {
		t.Errorf("the rollback's source is %q, want the health monitor at the failed exit", rollback.Undoing.Source)
	}
	if rollback.Undoing.RevertIntentID != revert.ID {
		t.Errorf("the rollback names revert intent %s, the health monitor raised %s", rollback.Undoing.RevertIntentID, revert.ID)
	}
	if rollback.Status != deploy.StatusComplete {
		t.Errorf("the rollback's own status is %s, and it is a completed deploy of the release it returned to", rollback.Status)
	}

	// The failed release's own deploy is rolled back, and the release keeps its
	// number.
	failed, err := deploy.Get(ctx, d.pool, bad.deployID)
	if err != nil {
		t.Fatalf("reading the failed deploy: %v", err)
	}
	if failed.Status != deploy.StatusRolledBack {
		t.Errorf("the failed deploy is %s, want rolled back", failed.Status)
	}
	rel, err := release.Get(ctx, d.pool, bad.releaseID)
	if err != nil || rel.Number != 2 {
		t.Errorf("the failed release is number %d, %v; a rolled-back release keeps its number", rel.Number, err)
	}

	// What is running is the first release again, both in the store and on the target.
	current, running, err := deploy.Current(ctx, d.pool, res.serviceID, res.environmentID)
	if err != nil || !running {
		t.Fatalf("Current = running %v, %v", running, err)
	}
	if current.ID != rollback.ID || current.ReleaseID != good.releaseID {
		t.Errorf("the current deploy is %s of release %s, want the rollback %s of %s",
			current.ID, current.ReleaseID, rollback.ID, good.releaseID)
	}
	onTarget, err := d.targets.at(d.dir).ReadRunning(ctx, "demo", d.credential)
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
	if events, err := notifier.EventsFor(ctx, d.pool, raised.ID); err != nil || len(events) != 0 {
		t.Errorf("the rollback fired %d page event(s), %v; the factory does not page to inform", len(events), err)
	}

	// And the whole episode is readable as links: the walk from what is live now
	// reaches the intent behind the release that is serving.
	var walked bytes.Buffer
	if err := walk(ctx, d.pool, &walked, current.ID); err != nil {
		t.Fatalf("the walk from the rollback stopped: %v\noutput so far:\n%s", err, walked.String())
	}
	if !strings.Contains(walked.String(), theStatement) {
		t.Errorf("the walk from the rollback does not reach the restored release's statement:\n%s", walked.String())
	}
	if err := decisionlog.Verify(ctx, d.pool); err != nil {
		t.Errorf("the chain does not verify after a rollback: %v", err)
	}
}

// TestTheWindowLimitHoldsTheNextProductionDeploy is the window limit at the value the
// score supplies, which is the serial factory: one window open per service, so the second release of a run waits
// behind the first. It is a wait on the factory rather than on anybody, so it writes
// nothing and does not page.
func TestTheWindowLimitHoldsTheNextProductionDeploy(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)

	if _, err := run(ctx, d, of(theStatement)); err != nil {
		t.Fatalf("the first run stopped: %v\noutput so far:\n%s", err, out)
	}

	d.in = strings.NewReader("")
	res, err := run(ctx, d, of(theSecondStatement, theThirdStatement))
	if err != nil {
		t.Fatalf("the run stopped, and a hold is not an error: %v\noutput so far:\n%s", err, out)
	}
	a, b := res.candidates[0], res.candidates[1]

	// Both merged and both were minted a number: an open window blocks nothing up to
	// the window limit, and what it blocks is the deploy and nothing above it.
	for _, c := range res.candidates {
		if !c.merged || c.releaseID == "" {
			t.Fatalf("item %s merged=%v release=%q, and the window limit holds no merge:\n%s", c.itemID, c.merged, c.releaseID, out)
		}
	}
	if a.deployID == "" {
		t.Fatalf("the first release of the run did not deploy with no window open:\n%s", out)
	}
	if b.deployID != "" {
		t.Errorf("the second release deployed %s with the window limit at one and a window already open", b.deployID)
	}
	if !strings.Contains(b.factoryHold, gate.HoldWindowLimitReached) {
		t.Errorf("the second release's hold is %q, want the window limit's", b.factoryHold)
	}
	if b.deployGate.opening != "" {
		t.Error("the production deploy row fired for the held release, and a hold that lifts itself opens no decision")
	}
	if b.windowID != "" {
		t.Errorf("a window %s opened over a deploy that did not happen", b.windowID)
	}

	// A numbered release that has never run anywhere is normal and not an anomaly.
	if _, watched, err := window.ForRelease(ctx, d.pool, b.releaseID); err != nil || watched {
		t.Errorf("ForRelease on the undeployed release = watched %v, %v", watched, err)
	}

	// Nothing was written for the hold: it is computed from records that already exist,
	// so a record for it would be a decision where nothing is decided.
	rows, err := decisionlog.Read(ctx, d.pool)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	for _, row := range rows {
		if row.Shape == decisionlog.ShapeWait {
			t.Errorf("the log holds a wait row %s, and the window limit's hold writes nothing", row.ID)
		}
		if row.Shape == decisionlog.ShapePageEvent {
			t.Errorf("the log holds a page event %s, and a deploy queued behind the window limit waits on the factory", row.ID)
		}
	}
}

// TestARollbackSweepsTheReleaseAboveItsTarget is the window limit above one and what
// it costs. Master
// is linear, so returning to a target below a failed release undoes every release
// above it — the failed one failed, the rest skipped.
//
// The steps are driven one at a time rather than through a run, because this substrate
// replaces the process rather than shifting traffic: the lower release stops emitting
// the moment the upper one deploys, so a run that deploys both back to back leaves the
// lower one with nothing for its comparison to read. The pause between the two deploys
// is what a substrate keeping a control would not need, and it is where a window limit
// above one is
// weakest here.
func TestARollbackSweepsTheReleaseAboveItsTarget(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	installWindow(t, ctx, d, 2)

	if _, err := run(ctx, d, of(theStatement)); err != nil {
		t.Fatalf("the first run stopped: %v\noutput so far:\n%s", err, out)
	}
	firstRelease, found, err := release.Highest(ctx, d.pool, serviceOf(ctx, t, d))
	if err != nil || !found {
		t.Fatalf("reading the first release: found %v, %v", found, err)
	}

	// Two bad candidates, both merged, and then deployed one at a time.
	d.in = strings.NewReader("")
	d.model = interviewed(2)
	path := p(ctx, t, d)
	var candidates []*candidate
	for _, statement := range []string{theSecondStatement, theThirdStatement} {
		candidates = append(candidates, authorOne(t, ctx, path, statement, out))
	}
	for _, c := range candidates {
		if err := path.candidateEnvironment(ctx, c); err != nil {
			t.Fatalf("the candidate environment of %s: %v\noutput so far:\n%s", c.itemID, err, out)
		}
		if err := path.mergeGate(ctx, c); err != nil {
			t.Fatalf("the Merge to master gate of %s: %v\noutput so far:\n%s", c.itemID, err, out)
		}
	}
	if _, err := path.runQueue(ctx, theServiceRecord(t, ctx, path)); err != nil {
		t.Fatalf("the queue stopped: %v\noutput so far:\n%s", err, out)
	}

	lower, upper := candidates[0], candidates[1]
	if err := path.productionDeploy(ctx, lower); err != nil {
		t.Fatalf("deploying the lower release: %v\noutput so far:\n%s", err, out)
	}
	// Long enough for the lower release to emit something for its own comparison to
	// read, which is what a substrate keeping a control would give it for free.
	time.Sleep(200 * time.Millisecond)
	if err := path.productionDeploy(ctx, upper); err != nil {
		t.Fatalf("deploying the upper release: %v\noutput so far:\n%s", err, out)
	}
	if lower.windowID == "" || upper.windowID == "" {
		t.Fatalf("windows opened %q and %q, and the window limit is two:\n%s", lower.windowID, upper.windowID, out)
	}

	if err := path.watchTo(ctx, theServiceRecord(t, ctx, path), time.Now().Add(theWatchFor), theWatchEvery); err != nil {
		t.Fatalf("the watch stopped: %v\noutput so far:\n%s", err, out)
	}

	// The lower one is failed and the upper one is skipped: its health monitor
	// simply stopped, master being linear and the release being above the target.
	lowerWindow, err := window.Get(ctx, d.pool, lower.windowID)
	if err != nil {
		t.Fatalf("reading the lower window: %v", err)
	}
	upperWindow, err := window.Get(ctx, d.pool, upper.windowID)
	if err != nil {
		t.Fatalf("reading the upper window: %v", err)
	}
	if lowerWindow.Exit != window.ExitFailed {
		t.Fatalf("the lower window closed %q, want harm:\n%s", lowerWindow.Exit, out)
	}
	if upperWindow.Exit != window.ExitSkipped {
		t.Errorf("the upper window closed %q, want swept", upperWindow.Exit)
	}
	if upperWindow.Exit.Counts() {
		t.Error("a swept window counts as a release to return to, and nothing is left running a swept release's build")
	}

	// One rollback undid both, and the two are named apart: one failed release is
	// one revert item, and the swept one was never failed.
	rollback, found, err := deploy.NewestRollback(ctx, d.pool, theServiceRecord(t, ctx, path).ID, path.production.ID)
	if err != nil || !found {
		t.Fatalf("NewestRollback = found %v, %v", found, err)
	}
	if rollback.ReleaseID != firstRelease.ID {
		t.Errorf("the rollback returned to release %s, want the first one %s — the newest below the failed one whose window closed without failing a release",
			rollback.ReleaseID, firstRelease.ID)
	}
	if rollback.Undoing.FailedReleaseID != lower.releaseID {
		t.Errorf("the rollback failed %s, the lower release is %s", rollback.Undoing.FailedReleaseID, lower.releaseID)
	}
	if len(rollback.Undoing.SweptReleaseIDs) != 1 || rollback.Undoing.SweptReleaseIDs[0] != upper.releaseID {
		t.Errorf("the rollback swept %v, want the one release above it, %s",
			rollback.Undoing.SweptReleaseIDs, upper.releaseID)
	}
	for _, undone := range []*candidate{lower, upper} {
		dep, err := deploy.Get(ctx, d.pool, undone.deployID)
		if err != nil {
			t.Fatalf("reading the deploy of %s: %v", undone.releaseID, err)
		}
		if dep.Status != deploy.StatusRolledBack {
			t.Errorf("the deploy of release %s is %s, and one rollback undid both", undone.releaseID, dep.Status)
		}
	}
	if err := decisionlog.Verify(ctx, d.pool); err != nil {
		t.Errorf("the chain does not verify after a rollback that swept: %v", err)
	}
}

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
	d.in = strings.NewReader(approvals)
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
	// intent the health monitor already took in, it is not held, and it deploys
	// ahead of the release the hold is holding — which is the one place the number
	// does not order deploys.
	d.in = strings.NewReader(approvals)
	res, err := run(ctx, d, of(theFourthStatement, rolled.revertStatement))
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
	if !strings.Contains(out.String(), "is already waiting with this statement") {
		t.Errorf("the run took in a second intent rather than working the one the rollback raised:\n%s", out)
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
	d.in = strings.NewReader(approvals)
	d.model = interviewed(2)
	held, err := run(ctx, d, of(theThirdStatement))
	if err != nil {
		t.Fatalf("the run stopped: %v\noutput so far:\n%s", err, out)
	}
	stuck := only(t, held)
	if stuck.deployID != "" || stuck.factoryHold == "" {
		t.Fatalf("the change was not held: deploy %q hold %q\n%s", stuck.deployID, stuck.factoryHold, out)
	}

	path := p(ctx, t, d)
	if err := path.approveThrough(ctx, stuck.itemID, gate.VerdictApprove,
		"the incident is worse than the defect; ship it"); err != nil {
		t.Fatalf("approving through the hold: %v\noutput so far:\n%s", err, out)
	}
	if !strings.Contains(out.String(), "approving through accepts what the hold was preventing") {
		t.Errorf("the run does not say what approving through accepts:\n%s", out)
	}

	// The deploy happened and a window opened over it.
	current, running, err := deploy.Current(ctx, d.pool, held.serviceID, held.environmentID)
	if err != nil || !running {
		t.Fatalf("Current = running %v, %v", running, err)
	}
	if current.ReleaseID != stuck.releaseID {
		t.Fatalf("what runs is release %s, the human approved %s through", current.ReleaseID, stuck.releaseID)
	}

	// And the defect is back, which is what "redelivers" means: the change was built
	// on the master that still holds the failed release's code, so the very next
	// reading of its window fails it again.
	if err := path.watchTo(ctx, theServiceRecord(t, ctx, path), time.Now().Add(theWatchFor), theWatchEvery); err != nil {
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

// TestACrossingAfterTheWindowClosedRaisesAnIntent is the other side of the
// window's authority. The health monitor keeps running after the window closes;
// what it finds then is not a rollback candidate, because the change has been
// live for a week and the window's authority ended long before. It is an
// incident and an unrefined intent at the start of the pipeline.
func TestACrossingAfterTheWindowClosedRaisesAnIntent(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)

	if _, err := run(ctx, d, of(theStatement)); err != nil {
		t.Fatalf("the first run stopped: %v\noutput so far:\n%s", err, out)
	}
	d.in = strings.NewReader("")
	res, err := run(ctx, d, of(theSecondStatement))
	if err != nil {
		t.Fatalf("the second run stopped: %v\noutput so far:\n%s", err, out)
	}
	c := only(t, res)
	w, err := window.Get(ctx, d.pool, c.windowID)
	if err != nil {
		t.Fatalf("reading the window: %v", err)
	}
	if w.Open() {
		t.Fatalf("the window is still open, and this test is about what happens after it closes:\n%s", out)
	}

	// The software starts failing after its window has closed. A test writes what the
	// running program would have emitted, which is the one thing here that is not the
	// factory's own doing — the quantity is the build's, and this stands in for a build
	// that got worse.
	signal := localtarget.SignalFile(d.dir, c.reverifiedBuildID)
	if err := os.WriteFile(signal, []byte(strings.Repeat("error\n", 400)), 0o644); err != nil {
		t.Fatalf("writing what the running build emits: %v", err)
	}

	path := p(ctx, t, d)
	if err := path.watchPass(ctx, theServiceRecord(t, ctx, path)); err != nil {
		t.Fatalf("the pass stopped: %v\noutput so far:\n%s", err, out)
	}

	incidents, err := incident.ForService(ctx, d.pool, res.serviceID)
	if err != nil {
		t.Fatalf("reading the incidents: %v", err)
	}
	if len(incidents) != 1 {
		t.Fatalf("%d incidents were raised, want one: %+v", len(incidents), incidents)
	}
	raised := incidents[0]
	if raised.ReleaseID != c.releaseID {
		t.Errorf("the incident names release %s, the crossing was against %s", raised.ReleaseID, c.releaseID)
	}
	if raised.Observations != 0 {
		t.Errorf("the first crossing recorded %d observations", raised.Observations)
	}

	// An intent and no rollback: the window's authority ended when it closed.
	found, err := intent.Get(ctx, d.pool, raised.IntentID)
	if err != nil {
		t.Fatalf("reading the intent the crossing raised: %v", err)
	}
	if found.Source != intent.SourceDetector || found.State != intent.StateUnrefined {
		t.Errorf("the intent is %s from %s, want an unrefined one from the detector", found.State, found.Source)
	}
	if _, rolled, err := deploy.NewestRollback(ctx, d.pool, res.serviceID, res.environmentID); err != nil || rolled {
		t.Errorf("NewestRollback = %v, %v; nothing rolls back after the window has closed", rolled, err)
	}
	if !strings.Contains(out.String(), "a crossing after the window closed") {
		t.Errorf("the pass does not say the crossing was after the window closed:\n%s", out)
	}

	// A second crossing on the same service and release is an observation on the
	// incident already open, and never a second intent.
	if err := path.watchPass(ctx, theServiceRecord(t, ctx, path)); err != nil {
		t.Fatalf("the second pass stopped: %v", err)
	}
	again, err := incident.ForService(ctx, d.pool, res.serviceID)
	if err != nil {
		t.Fatalf("reading the incidents again: %v", err)
	}
	if len(again) != 1 {
		t.Fatalf("%d incidents after a second crossing, want the one deduplicated onto: %+v", len(again), again)
	}
	if again[0].Observations != 1 {
		t.Errorf("the incident records %d observations after a second crossing, want one", again[0].Observations)
	}
	var intents int
	if err := d.pool.QueryRow(ctx, `select count(*) from `+intent.Table+` where source = $1`,
		string(intent.SourceDetector)).Scan(&intents); err != nil {
		t.Fatalf("counting the detector's intents: %v", err)
	}
	if intents != 1 {
		t.Errorf("%d intents came from the detector, and a further crossing is an observation and never a second intent", intents)
	}
}

// TestADriftMismatchHoldsTheProductionDeployAndPages is the one hold the factory
// cannot lift by gathering evidence, and so the one that fires the row and pages. What
// the factory recorded about the service is not what is running, so nothing here can be
// decided on the record.
func TestADriftMismatchHoldsTheProductionDeployAndPages(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	d.driftdetector = newDriftDetectorStore(t, ctx)

	if _, err := run(ctx, d, of(theStatement)); err != nil {
		t.Fatalf("the first run stopped: %v\noutput so far:\n%s", err, out)
	}
	if !strings.Contains(out.String(), "An drift detector is installed") {
		t.Errorf("the run does not report an drift detector installed:\n%s", out)
	}

	// Installing the drift detector is substrate outside the twelve duties,
	// so the page a mismatch fires reaches whoever the declaration says installed
	// it.
	installer := "sre"
	if _, err := people.NewWriter(d.pool).Declare(ctx, owner(d.human), installer,
		people.OfObligation(people.ObligationDriftDetector)); err != nil {
		t.Fatalf("declaring who installed the drift detector: %v", err)
	}

	// A target changed underneath: the drift detector's own store now holds a
	// mismatch, written by the drift detector and by nothing in the factory.
	raised, err := driftdetector.NewWriter(d.driftdetector).Record(ctx, driftdetector.Pass{
		ServiceID: serviceOf(ctx, t, d), Target: d.dir, Reached: true,
		RunningBuild: "bl_somebodyelses", RecordedBuildID: "bl_thefactorys",
		RecordedReleaseID: "rel_thefactorys",
	})
	if err != nil {
		t.Fatalf("recording the pass: %v", err)
	}
	if raised.Raised == "" {
		t.Fatal("the pass raised no mismatch, and the target ran something the factory did not record")
	}

	// The next change: the production deploy row fires with the mismatch on its
	// open event and a human at it, and the human holds.
	// One verdict is asked for and not three: by the second change the score
	// auto-passes the two rows above production, and the mismatch is what puts a human
	// at that one.
	d.in = strings.NewReader("hold the record is wrong and I am checking the target\n")
	d.model = interviewed(0)
	res, err := run(ctx, d, of(theSecondStatement))
	if err != nil {
		t.Fatalf("the run stopped, and a hold is not an error: %v\noutput so far:\n%s", err, out)
	}
	c := only(t, res)
	if !c.held || c.deployID != "" {
		t.Fatalf("the change was not held at the production deploy row: held=%v deploy=%q\n%s", c.held, c.deployID, out)
	}
	if !c.deployGate.humanDecided {
		t.Error("no human decided at the row, and a mismatch puts one there whatever the number reads")
	}
	if !strings.Contains(c.deployGate.whyHuman, gate.WhyMismatch) {
		t.Errorf("the row says a human decided because %q, want the mismatch among the reasons", c.deployGate.whyHuman)
	}
	rows, err := decisionlog.Read(ctx, d.pool)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	var opening decisionlog.Row
	for _, row := range rows {
		if row.ID == c.deployGate.opening {
			opening = row
		}
	}
	payload := openingPayload(t, opening)
	if payload.Mismatch == "" || !strings.Contains(payload.Mismatch, driftdetector.HoldWords) {
		t.Errorf("the open event's mismatch reads %q, and a human approving through has to read what disagreed",
			payload.Mismatch)
	}

	// The page: reached, to whoever installed the drift detector, because a mismatch
	// belongs to no duty of the twelve.
	events, err := notifier.EventsFor(ctx, d.pool, raised.Raised)
	if err != nil {
		t.Fatalf("reading the page events: %v", err)
	}
	if len(events) != 1 || notifier.Event(events[0].Event) != notifier.EventReached {
		t.Fatalf("the page's events are %+v, want one reached", events)
	}
	if events[0].Reached != installer {
		t.Errorf("the page reached %q, and %q is who the declaration says installed the drift detector",
			events[0].Reached, installer)
	}
	if !strings.Contains(out.String(), "PAGE reached to "+installer) {
		t.Errorf("the page was not delivered:\n%s", out)
	}

	// Unanswered, it widens exactly once, to the owner. There is no second widening.
	path := p(ctx, t, d)
	for range 3 {
		if err := path.watchPass(ctx, theServiceRecord(t, ctx, path)); err != nil {
			t.Fatalf("a pass stopped: %v", err)
		}
	}
	events, err = notifier.EventsFor(ctx, d.pool, raised.Raised)
	if err != nil {
		t.Fatalf("reading the page events: %v", err)
	}
	var widened int
	for _, e := range events {
		if notifier.Event(e.Event) == notifier.EventWidened {
			widened++
			if e.Reached != d.human {
				t.Errorf("the page widened to %q, want the owner %q", e.Reached, d.human)
			}
		}
	}
	if widened != 1 {
		t.Errorf("the page widened %d times, and unanswered it widens exactly once", widened)
	}

	// Cleared at the drift detector and nowhere else, and the answered event
	// is written by the pass that finds it cleared — because that store calls
	// nothing.
	if _, err := driftdetector.NewWriter(d.driftdetector).Clear(ctx, raised.Raised, installer); err != nil {
		t.Fatalf("clearing the mismatch: %v", err)
	}
	if err := path.watchPass(ctx, theServiceRecord(t, ctx, path)); err != nil {
		t.Fatalf("the pass after the clearing stopped: %v", err)
	}
	events, err = notifier.EventsFor(ctx, d.pool, raised.Raised)
	if err != nil {
		t.Fatalf("reading the page events: %v", err)
	}
	last := events[len(events)-1]
	if notifier.Event(last.Event) != notifier.EventAnswered || last.Reached != installer {
		t.Errorf("the page's last event is %+v, want answered by %q", last, installer)
	}

	// And with the mismatch cleared, the row is the score's again.
	stillHeld, why, err := driftdetector.NewStore(d.driftdetector).Mismatch(ctx, serviceOf(ctx, t, d))
	if err != nil || stillHeld {
		t.Errorf("Mismatch = %v %q, %v; a cleared one holds nothing", stillHeld, why, err)
	}
	if err := decisionlog.Verify(ctx, d.pool); err != nil {
		t.Errorf("the chain does not verify over a log holding page events beside the decisions: %v", err)
	}
}

// TestAnEscalationPagesOnlyWhereSomethingLiveIsWorse is the page's condition read off
// a record rather than off a list. The factory giving up on a defect that is live is
// production staying worse until a human takes it over; giving up on a feature nobody is
// running is not, and that one waits in Work.
func TestAnEscalationPagesOnlyWhereSomethingLiveIsWorse(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)

	// An owner's request the factory cannot do: no page.
	d.model = &refusingModel{inner: &fakeModel{}, refusals: attemptLimit + 5}
	if _, err := run(ctx, d, of(theStatement)); err == nil {
		t.Fatalf("the run finished, and every implementer reply was refused:\n%s", out)
	}
	rows, err := decisionlog.Read(ctx, d.pool)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	for _, row := range rows {
		if row.Shape == decisionlog.ShapePageEvent {
			t.Errorf("an escalation on an owner's feature fired page event %s, and nothing live is worse for it", row.ID)
		}
	}
	if !strings.Contains(out.String(), "mail to owner") {
		t.Errorf("the escalation was not delivered on mail:\n%s", out)
	}

	// A detector's intent the factory cannot fix: the same escalation, and a page,
	// because the defect it describes is live.
	// The statement is one this fake can author a spec for, because what makes this
	// page is where the intent came from and not the words in it.
	detected, err := intent.NewIntake(d.pool).TakeIn(ctx, healthmonitor.Actor, intent.SourceDetector, theSecondStatement)
	if err != nil {
		t.Fatalf("taking in the detector's intent: %v", err)
	}
	if detected.Source != intent.SourceDetector {
		t.Fatalf("the intent's source is %s", detected.Source)
	}
	d.in = strings.NewReader(theAnswer + "\n" + approvals)
	if _, err := run(ctx, d, of(detected.Statement)); err == nil {
		t.Fatalf("the run finished, and every implementer reply was refused:\n%s", out)
	}

	var paged int
	rows, err = decisionlog.Read(ctx, d.pool)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	for _, row := range rows {
		if row.Shape != decisionlog.ShapePageEvent {
			continue
		}
		paged++
		var payload notifier.Payload
		if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
			t.Fatalf("reading the page event: %v", err)
		}
		if payload.WaitKind != string(notifier.KindItemEscalated) {
			t.Errorf("the page is about a %q, want an item escalated", payload.WaitKind)
		}
		if !strings.Contains(payload.Waiting, string(intent.SourceDetector)) {
			t.Errorf("the page says %q, and what makes it one is where the intent came from", payload.Waiting)
		}
		if payload.Holding != people.OfDuty(takeOverIssues).String() {
			t.Errorf("the page routes by %q, want the duty of taking over issues the factory cannot fix", payload.Holding)
		}
	}
	if paged != 1 {
		t.Errorf("%d page events were written, want the one on the detector's item", paged)
	}
	if err := decisionlog.Verify(ctx, d.pool); err != nil {
		t.Errorf("the chain does not verify: %v", err)
	}
}

// rolledBack is what [rollBackABadRelease] leaves behind: the revert intent the
// health monitor raised and the statement a later run works it through.
type rolledBack struct {
	revertIntentID  string
	revertStatement string
}

// rollBackABadRelease ships a good release and then a bad one, and returns once the
// bad one has been failed and rolled back. It is the state three of the tests here
// start from, so it is written once — and it asserts its own outcome, because a test
// that began from a state it did not reach would report the wrong thing.
func rollBackABadRelease(ctx context.Context, t *testing.T, d deps, out *bytes.Buffer) rolledBack {
	t.Helper()
	if _, err := run(ctx, d, of(theStatement)); err != nil {
		t.Fatalf("the first run stopped: %v\noutput so far:\n%s", err, out)
	}

	d.in = strings.NewReader("")
	d.model = interviewed(2)
	res, err := run(ctx, d, of(theSecondStatement))
	if err != nil {
		t.Fatalf("the bad run stopped: %v\noutput so far:\n%s", err, out)
	}
	bad := only(t, res)
	w, err := window.Get(ctx, d.pool, bad.windowID)
	if err != nil {
		t.Fatalf("reading the bad release's window: %v", err)
	}
	if w.Exit != window.ExitFailed {
		t.Fatalf("the bad release's window closed %q, want harm:\n%s", w.Exit, out)
	}

	rollback, found, err := deploy.NewestRollback(ctx, d.pool, res.serviceID, res.environmentID)
	if err != nil || !found {
		t.Fatalf("NewestRollback = found %v, %v", found, err)
	}
	revert, err := intent.Get(ctx, d.pool, rollback.Undoing.RevertIntentID)
	if err != nil {
		t.Fatalf("reading the revert intent: %v", err)
	}
	return rolledBack{revertIntentID: revert.ID, revertStatement: revert.Statement}
}

// p composes the path over the same deps a run uses, for a test that drives one step
// rather than the whole thing.
func p(ctx context.Context, t *testing.T, d deps) *path {
	t.Helper()
	composed, err := compose(ctx, d)
	if err != nil {
		t.Fatalf("composing the path: %v", err)
	}
	return composed
}

// watching is the service one call of the health monitor is about.
func watching(s shipped, name string) healthmonitor.Watching {
	return healthmonitor.Watching{ID: s.serviceID, Name: name, EnvironmentID: s.environmentID}
}

// serviceOf is the id of the service these tests run against.
func serviceOf(ctx context.Context, t *testing.T, d deps) string {
	t.Helper()
	var id string
	if err := d.pool.QueryRow(ctx, `select id from service where name = $1`, theService).Scan(&id); err != nil {
		t.Fatalf("reading the service's id: %v", err)
	}
	return id
}

// newDriftDetectorStore is the drift detector's own store for one test: a schema
// of its own, its own schema applied by its own applier, and nothing of the
// factory's in it. The factory reads it and never writes it, which is what a
// pool handed to the path as its driftdetector is.
//
// It is opened on the same server the factory's tests use, with a schema of its own,
// rather than on [driftdetector.DefaultURL]. What makes this store independent is that no
// factory component writes it and that it is reached through a URL of its own — not
// which machine it is on — so a test naming a second server would be checking the
// deployment rather than the code, and would fail wherever the factory's database is
// not where that default says.
func newDriftDetectorStore(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the drift detector's schema: %v", err)
	}
	schema := "driftdetector_" + hex.EncodeToString(suffix[:])

	pool, err := driftdetector.Open(ctx, inSchema(t, postgres.URL(), schema))
	if err != nil {
		t.Fatalf("the drift detector's store is not reachable, and these tests do not skip: %v", err)
	}
	t.Cleanup(func() {
		drop, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := pool.Exec(drop, `drop schema if exists `+pgx.Identifier{schema}.Sanitize()+` cascade`); err != nil {
			t.Errorf("dropping schema %s: %v", schema, err)
		}
		pool.Close()
	})
	if _, err := pool.Exec(ctx, `create schema `+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("creating schema %s: %v", schema, err)
	}
	if err := driftdetector.Apply(ctx, pool); err != nil {
		t.Fatalf("applying the drift detector's schema: %v", err)
	}
	return pool
}
