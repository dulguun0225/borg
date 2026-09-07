// Tests of the window limit: one window open per service holds the next
// production deploy, and a rollback above one sweeps every release above
// its target.
package main

import (
	"strings"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/window"
)

// TestTheWindowLimitHoldsTheNextProductionDeploy is the window limit at the value the
// score supplies, which is the serial factory: one window open per service, so the second release of a run waits
// behind the first. It is a wait on the factory rather than on anybody, so it writes
// nothing and does not page.
func TestTheWindowLimitHoldsTheNextProductionDeploy(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)

	if _, err := run(ctx, d, of(theStatement)); err != nil {
		t.Fatalf("the first run stopped: %v\noutput so far:\n%s", err, out)
	}

	d.in = strings.NewReader(approvals)
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
	rows := readLog(t, ctx, d)
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
// The steps are driven one at a time rather than through a run, because this platform
// replaces the process rather than shifting traffic: the lower release stops emitting
// the moment the upper one deploys, so a run that deploys both back to back leaves the
// lower one with nothing for its comparison to read. The pause between the two deploys
// is what a platform keeping a control would not need, and it is where a window limit
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
	d.in = strings.NewReader(approvals)
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
	// read, which is what a platform keeping a control would give it for free.
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
		t.Fatalf("the lower window closed %q, want failed:\n%s", lowerWindow.Exit, out)
	}
	if upperWindow.Exit != window.ExitSkipped {
		t.Errorf("the upper window closed %q, want swept", upperWindow.Exit)
	}
	if upperWindow.Exit.PassedOrTimedOut() {
		t.Error("a skipped window counts as a release to return to, and nothing is left running a skipped release's build")
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
	if len(rollback.Undoing.SkippedReleaseIDs) != 1 || rollback.Undoing.SkippedReleaseIDs[0] != upper.releaseID {
		t.Errorf("the rollback swept %v, want the one release above it, %s",
			rollback.Undoing.SkippedReleaseIDs, upper.releaseID)
	}
	for _, undone := range []*candidate{lower, upper} {
		dep, err := deploy.Get(ctx, d.pool, undone.deployID)
		if err != nil {
			t.Fatalf("reading the deploy of %s: %v", undone.releaseID, err)
		}
		_ = dep
		targets, err := deploy.Targets(ctx, d.pool, undone.deployID)
		if err != nil {
			t.Fatalf("reading the targets of the deploy of %s: %v", undone.releaseID, err)
		}
		for _, target := range targets {
			if target.Completion != deploy.CompletionRolledBack {
				t.Errorf("target %s of the deploy of release %s is %s, and one rollback undid both",
					target.Address, undone.releaseID, target.Completion)
			}
		}
	}
	if err := verifyLog(t, ctx, d); err != nil {
		t.Errorf("the chain does not verify after a rollback that swept: %v", err)
	}
}
