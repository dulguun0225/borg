// The health monitor's own tests, and they are about the two questions no
// end-to-end run can isolate: what a rollback returns to, and whether what an
// incident raised has shipped. Both are queries over records a run writes in
// passing, so a run that got them wrong would fail somewhere else and say
// something else.
//
// The failed exit, the four window exits, and the rollback itself are
// demonstrated through the command-line interface in cmd/factory, where there
// is a target to deploy against and a process emitting the quantity. What is
// here is the arithmetic of the graph.
//
// A silent release arm beside a serving control failing on the request rate is
// [boundary.TestASilentReleaseArmBesideAServingControlFails]'s, package
// boundary being the one this package asks that arithmetic of; and a
// rollback's deploy record naming the failed release apart from the skipped
// ones, and its dedup against a release named both, are deploy's own — its own
// database tests are where that writer's rules belong.
//
// db_test.go is the two queries over the graph; watch_test.go is the pass and
// the exits; fixtures_test.go is what both share. The three are one external
// test package split by subject, each file held to 500 lines.
//
// These tests do not skip when the database is unreachable — the milestone is
// demonstrated by them running, so an unreachable database fails the run.
package healthmonitor_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/targetseam"
	"github.com/dulguun0225/borg/factory/window"
)

// TestTheTargetIsTheNewestReleaseBelowWhoseWindowCountsIt is the whole of what a
// rollback returns to. It descends past failed, past skipped, and past any
// window still open, and it descends from the release being rolled back rather
// than from the top — stated per service alone the query would return a
// release above the failed one and the factory would restore the change it had
// just failed.
func TestTheTargetIsTheNewestReleaseBelowWhoseWindowCountsIt(t *testing.T) {
	ctx, g := newGraph(t)

	// Five releases, one per exit the query has to reason about.
	one := shipOne(t, ctx, g, "in_1", window.ExitTimedOut) // counts: never failed
	two := shipOne(t, ctx, g, "in_2", window.ExitPassed)   // counts
	three := shipOne(t, ctx, g, "in_3", window.ExitFailed) // failed
	four := shipOne(t, ctx, g, "in_4", window.ExitSkipped) // nothing left running its build
	five := shipOne(t, ctx, g, "in_5", "")                 // still open

	// A rollback of the topmost release returns to the newest one below it that
	// counts, which is the passed close and not the failed one above it or the open one.
	target, found, err := g.monitor.TargetBelow(ctx, g.watching(), five.Number)
	if err != nil || !found {
		t.Fatalf("TargetBelow(%d) = found %v, %v", five.Number, found, err)
	}
	if target.ID != two.ID {
		t.Errorf("a rollback of release %d returns to %d, want %d — it descends past failed and past skipped",
			five.Number, target.Number, two.Number)
	}

	// Asked below the passed one, it descends to the cap: closing at the cap counts,
	// because a release that was never failed is one the factory can return to and
	// requiring a passed close would leave a quiet service with no target at all.
	target, found, err = g.monitor.TargetBelow(ctx, g.watching(), two.Number)
	if err != nil || !found {
		t.Fatalf("TargetBelow(%d) = found %v, %v", two.Number, found, err)
	}
	if target.ID != one.ID {
		t.Errorf("a rollback of release %d returns to %d, want the cap close %d", two.Number, target.Number, one.Number)
	}

	// A service's first release has no target at all: nothing below it closed without
	// failing it, and there is no earlier build to redeploy.
	if _, found, err := g.monitor.TargetBelow(ctx, g.watching(), one.Number); err != nil || found {
		t.Errorf("TargetBelow(%d) = found %v, %v; a first release has no target", one.Number, found, err)
	}

	// The releases a rollback of the passed one undoes: every release above it,
	// whatever its own window closed at. Master is linear, so returning to a
	// target below them undoes all of them and that is not a choice.
	above, err := release.Above(ctx, g.pool, g.serviceID, two.Number)
	if err != nil {
		t.Fatalf("Above: %v", err)
	}
	if len(above) != 3 {
		t.Fatalf("%d releases are above %d, want three: %+v", len(above), two.Number, above)
	}
	for n, want := range []release.Release{three, four, five} {
		if above[n].ID != want.ID {
			t.Errorf("the release above %d at position %d is %d, want %d", two.Number, n, above[n].Number, want.Number)
		}
	}
	// And nothing is above the topmost one.
	if above, err := release.Above(ctx, g.pool, g.serviceID, five.Number); err != nil || len(above) != 0 {
		t.Errorf("%d releases are above the newest, %v", len(above), err)
	}
}

// TestAWindowThatFailedToCloseLeavesTheTargetOlderThanItShouldBe is the cost the
// design states for computing the target rather than storing it: the rollback goes
// further back and undoes releases nothing failed, which is the safe direction and
// still a real loss.
func TestAWindowThatFailedToCloseLeavesTheTargetOlderThanItShouldBe(t *testing.T) {
	ctx, g := newGraph(t)

	one := shipOne(t, ctx, g, "in_1", window.ExitTimedOut)
	two := shipOne(t, ctx, g, "in_2", "") // a window nothing closed
	three := shipOne(t, ctx, g, "in_3", "")

	target, found, err := g.monitor.TargetBelow(ctx, g.watching(), three.Number)
	if err != nil || !found {
		t.Fatalf("TargetBelow = found %v, %v", found, err)
	}
	if target.ID != one.ID {
		t.Errorf("the target is release %d, want %d: the window over %d never closed, so it does not count",
			target.Number, one.Number, two.Number)
	}
	// Which is what makes the loss real: rolling back the third release undoes the
	// second as well, and nothing failed it.
	above, err := release.Above(ctx, g.pool, g.serviceID, one.Number)
	if err != nil {
		t.Fatalf("Above: %v", err)
	}
	if len(above) != 2 {
		t.Errorf("a rollback to release %d undoes %d releases, want both above it", one.Number, len(above))
	}
}

// TestShippedIsAReleaseDeployedAndNotJustMinted is the predicate two mechanisms ask of
// one intent: whether the incident it raised has finished, and whether the hold a
// rollback leaves may lift. A numbered release that has never run anywhere is normal,
// so a minted number is not a shipped item.
func TestShippedIsAReleaseDeployedAndNotJustMinted(t *testing.T) {
	ctx, g := newGraph(t)

	// An intent nothing has been decomposed from has not shipped: the factory has not worked
	// it yet, which is not the same as its having finished.
	if shipped, err := healthmonitor.Shipped(ctx, g.pool, theEnvironment, "in_untouched"); err != nil || shipped {
		t.Errorf("Shipped for an intent with no items = %v, %v", shipped, err)
	}
	// And an intent nobody named is not shipped either, rather than trivially so.
	if shipped, err := healthmonitor.Shipped(ctx, g.pool, theEnvironment, ""); err != nil || shipped {
		t.Errorf("Shipped for no intent at all = %v, %v", shipped, err)
	}

	// An item decomposed and not built: not shipped.
	it, err := g.items.Create(ctx, theActor, item.New{
		IntentID: "in_working", ServiceID: g.serviceID, Branch: "item/working",
	}, "", "", nil)
	if err != nil {
		t.Fatalf("decomposing the item: %v", err)
	}
	if shipped, err := healthmonitor.Shipped(ctx, g.pool, theEnvironment, "in_working"); err != nil || shipped {
		t.Errorf("Shipped for an item with no release = %v, %v", shipped, err)
	}

	// Minted and never deployed: still not shipped. The number records that a change
	// was accepted and not that it is live.
	bl, err := g.builds.Create(ctx, theActor, build.Draft{
		ItemID: it.ID, ServiceID: g.serviceID, CommitHash: "commit-working", ArtifactDigest: "digest-working",
		ShippedBundleIdentity: "bundle-test",
	})
	if err != nil {
		t.Fatalf("writing the build: %v", err)
	}
	rel, err := g.releases.Mint(ctx, theActor, release.Minting{
		ServiceID: g.serviceID, BuildID: bl.ID, Commit: bl.CommitHash, ItemID: it.ID,
	})
	if err != nil {
		t.Fatalf("minting the release: %v", err)
	}
	if shipped, err := healthmonitor.Shipped(ctx, g.pool, theEnvironment, "in_working"); err != nil || shipped {
		t.Errorf("Shipped for a release minted and never deployed = %v, %v", shipped, err)
	}

	// A deploy started and not completed: still not shipped, which is the same rule
	// current release keeps.
	dep, err := g.deploys.Start(ctx, theActor, deploy.Beginning{
		ServiceID: g.serviceID, EnvironmentID: theEnvironment,
		What: deploy.OfRelease(rel.ID, bl.ID), Targets: []deploy.Reaching{{Address: theTarget, KeptInstances: 1}},
	})
	if err != nil {
		t.Fatalf("starting the deploy: %v", err)
	}
	if shipped, err := healthmonitor.Shipped(ctx, g.pool, theEnvironment, "in_working"); err != nil || shipped {
		t.Errorf("Shipped for a deploy that has not completed = %v, %v", shipped, err)
	}

	if err := g.deploys.ReachTarget(ctx, dep.ID, theTarget); err != nil {
		t.Fatalf("reaching the target: %v", err)
	}
	if err := g.deploys.CompleteTarget(ctx, dep.ID, theTarget, targetseam.ReplacementDrained); err != nil {
		t.Fatalf("completing the target: %v", err)
	}
	if err := g.deploys.Complete(ctx, dep.ID); err != nil {
		t.Fatalf("completing the deploy: %v", err)
	}
	if shipped, err := healthmonitor.Shipped(ctx, g.pool, theEnvironment, "in_working"); err != nil || !shipped {
		t.Errorf("Shipped for a release deployed and complete = %v, %v", shipped, err)
	}

	// An intent decomposed into two items has shipped only when both have: decomposition may divide
	// the work, and half a revert is not a revert.
	second, err := g.items.Create(ctx, theActor, item.New{
		IntentID: "in_working", ServiceID: g.serviceID, Branch: "item/working-2",
	}, "", "", nil)
	if err != nil {
		t.Fatalf("decomposing the second item: %v", err)
	}
	if shipped, err := healthmonitor.Shipped(ctx, g.pool, theEnvironment, "in_working"); err != nil || shipped {
		t.Errorf("Shipped with a second item that has no release = %v, %v", shipped, err)
	}
	_ = second
}

// closedOn is the read a test closes a window on: a pair of counts with a
// baseline in it, which is what an exit other than skipped always has. The
// numbers are not what any of these tests assert over — what they assert is
// the exit — but a close with no read is refused, and rightly: an exit nobody
