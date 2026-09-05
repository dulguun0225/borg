// Tests of the analysis window opened over a production deploy: the
// first release, which has nothing below it to be compared against.
package main

import (
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/boundary"
	"github.com/dulguun0225/borg/factory/healthmonitor"
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
