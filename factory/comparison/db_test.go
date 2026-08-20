// The comparison's own tests, and they are about the two questions no end-to-end run
// can isolate: what a rollback returns to, and whether what an incident raised has
// shipped. Both are queries over records a run writes in passing, so a run that got
// them wrong would fail somewhere else and say something else.
//
// The harm exit, the four window exits, and the rollback itself are demonstrated
// through the crude interface in cmd/factory, where there is a target to deploy against
// and a process emitting the quantity. What is here is the arithmetic of the graph.
//
// These tests do not skip when the database is unreachable — the milestone is
// demonstrated by them running, so an unreachable database fails the run.
package comparison_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/boundary"
	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/comparison"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/window"
)

// The two ids this test names records against. Neither points at a record: an id field
// is checked for being present and not for pointing at anything, which record's doc.go
// states once, and what these tests are about is the releases and the windows.
const (
	theService     = "svc_underwatch"
	theEnvironment = "env_production"
)

var theActor = record.Actor{Kind: record.KindComponent, Name: "test"}

// graph is the records one test writes and the writers it writes them through.
type graph struct {
	pool     *pgxpool.Pool
	builds   *build.Writer
	releases *release.Writer
	deploys  *deploy.Writer
	windows  *window.Writer
	items    *item.Cut
}

// newGraph gives a test a schema of its own with the whole factory schema applied.
func newGraph(t *testing.T) (context.Context, graph) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "comparison_" + hex.EncodeToString(suffix[:])

	pool, err := postgres.Open(ctx, inSchema(t, postgres.URL(), schema))
	if err != nil {
		t.Fatalf("the database at %s is not reachable, and these tests do not skip: %v", postgres.URL(), err)
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
	if err := postgres.Apply(ctx, pool); err != nil {
		t.Fatalf("applying the schema: %v", err)
	}

	return ctx, graph{
		pool:     pool,
		builds:   build.NewWriter(pool),
		releases: release.NewWriter(pool),
		deploys:  deploy.NewWriter(pool),
		windows:  window.NewWriter(pool),
		items:    item.NewCut(pool),
	}
}

func inSchema(t *testing.T, base, schema string) string {
	t.Helper()
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parsing %s: %v", base, err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// shipOne writes the records one release leaves behind: an item, a build, the release,
// a completed production deploy of it, and a window over that deploy at the exit named.
// An empty exit leaves the window open.
//
// It is what a run does, written directly. The point of writing it here rather than
// running the path is that a test can put the windows in an order a run cannot easily
// produce, which is exactly the case the rollback's target exists for.
func shipOne(t *testing.T, ctx context.Context, g graph, intentID string, exit window.Exit) release.Release {
	t.Helper()
	it, err := g.items.Create(ctx, theActor, item.New{
		IntentID: intentID, ServiceID: theService, Branch: "item/" + intentID,
	})
	if err != nil {
		t.Fatalf("cutting the item: %v", err)
	}
	bl, err := g.builds.Create(ctx, theActor, it.ID, "commit-"+intentID)
	if err != nil {
		t.Fatalf("writing the build: %v", err)
	}
	rel, err := g.releases.Mint(ctx, theActor, theService, bl.ID, it.ID)
	if err != nil {
		t.Fatalf("minting the release: %v", err)
	}
	dep, err := g.deploys.Start(ctx, theActor, theService, theEnvironment, deploy.OfRelease(rel.ID, bl.ID))
	if err != nil {
		t.Fatalf("starting the deploy: %v", err)
	}
	if err := g.deploys.Complete(ctx, dep.ID); err != nil {
		t.Fatalf("completing the deploy: %v", err)
	}
	w, err := g.windows.Open(ctx, comparison.Actor, window.Opening{
		DeployID: dep.ID, ReleaseID: rel.ID, ServiceID: theService,
		CleanAvailable: rel.Number > 1,
		Size:           0.1, Confidence: 0.95, CapSeconds: 60,
		Formula: boundary.Formula, PolicyVersion: "pv_test", ScoreVersion: "scv_test",
	})
	if err != nil {
		t.Fatalf("opening the window: %v", err)
	}
	if exit != "" {
		if _, err := g.windows.Close(ctx, w.ID, exit, readFor(exit)); err != nil {
			t.Fatalf("closing the window at %s: %v", exit, err)
		}
	}
	return rel
}

// TestTheTargetIsTheNewestReleaseBelowWhoseWindowCountsIt is the whole of what a
// rollback returns to. It descends past harm, past swept, and past any window still
// open, and it descends from the release being rolled back rather than from the top —
// stated per service alone the query would return a release above the condemned one and
// the factory would restore the change it had just condemned.
func TestTheTargetIsTheNewestReleaseBelowWhoseWindowCountsIt(t *testing.T) {
	ctx, g := newGraph(t)

	// Five releases, one per exit the query has to reason about.
	one := shipOne(t, ctx, g, "in_1", window.ExitCap)    // counts: never condemned
	two := shipOne(t, ctx, g, "in_2", window.ExitClean)  // counts
	three := shipOne(t, ctx, g, "in_3", window.ExitHarm) // condemned
	four := shipOne(t, ctx, g, "in_4", window.ExitSwept) // nothing left running its build
	five := shipOne(t, ctx, g, "in_5", "")               // still open

	// A rollback of the topmost release returns to the newest one below it that
	// counts, which is the clean close and not the harm above it or the open one.
	target, found, err := comparison.TargetBelow(ctx, g.pool, theService, five.Number)
	if err != nil || !found {
		t.Fatalf("TargetBelow(%d) = found %v, %v", five.Number, found, err)
	}
	if target.ID != two.ID {
		t.Errorf("a rollback of release %d returns to %d, want %d — it descends past harm and past swept",
			five.Number, target.Number, two.Number)
	}

	// Asked below the clean one, it descends to the cap: closing at the cap counts,
	// because a release that was never condemned is one the factory can return to and
	// requiring a clean close would leave a quiet service with no target at all.
	target, found, err = comparison.TargetBelow(ctx, g.pool, theService, two.Number)
	if err != nil || !found {
		t.Fatalf("TargetBelow(%d) = found %v, %v", two.Number, found, err)
	}
	if target.ID != one.ID {
		t.Errorf("a rollback of release %d returns to %d, want the cap close %d", two.Number, target.Number, one.Number)
	}

	// A service's first release has no target at all: nothing below it closed without
	// condemning it, and there is no earlier build to redeploy.
	if _, found, err := comparison.TargetBelow(ctx, g.pool, theService, one.Number); err != nil || found {
		t.Errorf("TargetBelow(%d) = found %v, %v; a first release has no target", one.Number, found, err)
	}

	// The releases a rollback of the clean one sweeps: every release above it, whatever
	// its own window closed at. Master is linear, so returning to a target below them
	// undoes all of them and that is not a choice.
	above, err := release.Above(ctx, g.pool, theService, two.Number)
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
	if above, err := release.Above(ctx, g.pool, theService, five.Number); err != nil || len(above) != 0 {
		t.Errorf("%d releases are above the newest, %v", len(above), err)
	}
}

// TestAWindowThatFailedToCloseLeavesTheTargetOlderThanItShouldBe is the cost the
// design states for computing the target rather than storing it: the rollback goes
// further back and undoes releases nothing condemned, which is the safe direction and
// still a real loss.
func TestAWindowThatFailedToCloseLeavesTheTargetOlderThanItShouldBe(t *testing.T) {
	ctx, g := newGraph(t)

	one := shipOne(t, ctx, g, "in_1", window.ExitCap)
	two := shipOne(t, ctx, g, "in_2", "") // a window nothing closed
	three := shipOne(t, ctx, g, "in_3", "")

	target, found, err := comparison.TargetBelow(ctx, g.pool, theService, three.Number)
	if err != nil || !found {
		t.Fatalf("TargetBelow = found %v, %v", found, err)
	}
	if target.ID != one.ID {
		t.Errorf("the target is release %d, want %d: the window over %d never closed, so it does not count",
			target.Number, one.Number, two.Number)
	}
	// Which is what makes the loss real: rolling back the third release undoes the
	// second as well, and nothing condemned it.
	above, err := release.Above(ctx, g.pool, theService, one.Number)
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

	// An intent nothing has been cut from has not shipped: the factory has not worked
	// it yet, which is not the same as its having finished.
	if shipped, err := comparison.Shipped(ctx, g.pool, theEnvironment, "in_untouched"); err != nil || shipped {
		t.Errorf("Shipped for an intent with no items = %v, %v", shipped, err)
	}
	// And an intent nobody named is not shipped either, rather than trivially so.
	if shipped, err := comparison.Shipped(ctx, g.pool, theEnvironment, ""); err != nil || shipped {
		t.Errorf("Shipped for no intent at all = %v, %v", shipped, err)
	}

	// An item cut and not built: not shipped.
	it, err := g.items.Create(ctx, theActor, item.New{
		IntentID: "in_working", ServiceID: theService, Branch: "item/working",
	})
	if err != nil {
		t.Fatalf("cutting the item: %v", err)
	}
	if shipped, err := comparison.Shipped(ctx, g.pool, theEnvironment, "in_working"); err != nil || shipped {
		t.Errorf("Shipped for an item with no release = %v, %v", shipped, err)
	}

	// Minted and never deployed: still not shipped. The number records that a change
	// was accepted and not that it is live.
	bl, err := g.builds.Create(ctx, theActor, it.ID, "commit-working")
	if err != nil {
		t.Fatalf("writing the build: %v", err)
	}
	rel, err := g.releases.Mint(ctx, theActor, theService, bl.ID, it.ID)
	if err != nil {
		t.Fatalf("minting the release: %v", err)
	}
	if shipped, err := comparison.Shipped(ctx, g.pool, theEnvironment, "in_working"); err != nil || shipped {
		t.Errorf("Shipped for a release minted and never deployed = %v, %v", shipped, err)
	}

	// A deploy started and not completed: still not shipped, which is the same rule
	// current release keeps.
	dep, err := g.deploys.Start(ctx, theActor, theService, theEnvironment, deploy.OfRelease(rel.ID, bl.ID))
	if err != nil {
		t.Fatalf("starting the deploy: %v", err)
	}
	if shipped, err := comparison.Shipped(ctx, g.pool, theEnvironment, "in_working"); err != nil || shipped {
		t.Errorf("Shipped for a deploy that has not completed = %v, %v", shipped, err)
	}

	if err := g.deploys.Complete(ctx, dep.ID); err != nil {
		t.Fatalf("completing the deploy: %v", err)
	}
	if shipped, err := comparison.Shipped(ctx, g.pool, theEnvironment, "in_working"); err != nil || !shipped {
		t.Errorf("Shipped for a release deployed and complete = %v, %v", shipped, err)
	}

	// An intent cut into two items has shipped only when both have: the cut may divide
	// the work, and half a revert is not a revert.
	second, err := g.items.Create(ctx, theActor, item.New{
		IntentID: "in_working", ServiceID: theService, Branch: "item/working-2",
	})
	if err != nil {
		t.Fatalf("cutting the second item: %v", err)
	}
	if shipped, err := comparison.Shipped(ctx, g.pool, theEnvironment, "in_working"); err != nil || shipped {
		t.Errorf("Shipped with a second item that has no release = %v, %v", shipped, err)
	}
	_ = second
}

// TestARollbackNamesTheCondemnedReleaseApartFromTheSwept is why the two are separate
// fields: one condemned release is one revert item, and the swept ones were never
// condemned — their code is still on master and the revert redelivers them.
func TestARollbackNamesTheCondemnedReleaseApartFromTheSwept(t *testing.T) {
	ctx, g := newGraph(t)

	one := shipOne(t, ctx, g, "in_1", window.ExitCap)
	two := shipOne(t, ctx, g, "in_2", window.ExitHarm)
	three := shipOne(t, ctx, g, "in_3", window.ExitSwept)

	rollback, err := g.deploys.StartUndoing(ctx, theActor, theService, theEnvironment,
		deploy.OfRelease(one.ID, one.BuildID), deploy.Undoing{
			CondemnedReleaseID: two.ID,
			SweptReleaseIDs:    []string{three.ID},
			Source:             deploy.SourceComparisonAtHarm,
			RevertIntentID:     "in_revert",
		})
	if err != nil {
		t.Fatalf("StartUndoing: %v", err)
	}
	if err := g.deploys.Complete(ctx, rollback.ID); err != nil {
		t.Fatalf("completing the rollback: %v", err)
	}

	read, found, err := deploy.NewestRollback(ctx, g.pool, theService, theEnvironment)
	if err != nil || !found {
		t.Fatalf("NewestRollback = found %v, %v", found, err)
	}
	if read.ID != rollback.ID {
		t.Errorf("the newest rollback is %s, want %s", read.ID, rollback.ID)
	}
	if !read.Undoing.Any() {
		t.Error("the rollback's record does not read as a rollback's, and the condemned release is what says so")
	}
	if read.Undoing.CondemnedReleaseID != two.ID {
		t.Errorf("it condemned %s, want %s", read.Undoing.CondemnedReleaseID, two.ID)
	}
	if len(read.Undoing.SweptReleaseIDs) != 1 || read.Undoing.SweptReleaseIDs[0] != three.ID {
		t.Errorf("it swept %v, want the one release above the condemned one", read.Undoing.SweptReleaseIDs)
	}
	if read.Undoing.RevertIntentID != "in_revert" {
		t.Errorf("it names revert intent %q", read.Undoing.RevertIntentID)
	}

	// One release cannot be both. The two are apart so the revert rule can name one,
	// and a rollback saying otherwise is refused where it is written.
	if _, err := g.deploys.StartUndoing(ctx, theActor, theService, theEnvironment,
		deploy.OfRelease(one.ID, one.BuildID), deploy.Undoing{
			CondemnedReleaseID: two.ID, SweptReleaseIDs: []string{two.ID},
			Source: deploy.SourceComparisonAtHarm,
		}); err == nil {
		t.Error("a rollback naming one release as both condemned and swept was written")
	}
	// And an ordinary deploy is not a rollback's record: it names neither.
	ordinary, err := g.deploys.Start(ctx, theActor, theService, theEnvironment, deploy.OfRelease(one.ID, one.BuildID))
	if err != nil {
		t.Fatalf("starting an ordinary deploy: %v", err)
	}
	if ordinary.Undoing.Any() {
		t.Errorf("an ordinary deploy reads as a rollback's: %+v", ordinary.Undoing)
	}
	// The source is what a rollback carries beside its actor, and the actor stays the
	// agent that performed it.
	if read.Actor != theActor {
		t.Errorf("the rollback's actor is %+v, and the source is what says who called for it", read.Actor)
	}
	if read.Undoing.Source != deploy.SourceComparisonAtHarm {
		t.Errorf("the rollback's source is %q", read.Undoing.Source)
	}
	human := deploy.SourceOfHuman("ada", "the dashboards look wrong")
	if !strings.Contains(human, "ada") || !strings.Contains(human, "the dashboards look wrong") {
		t.Errorf("a human's source reads %q, and it names them and their reason", human)
	}
}

// TestASignalWithNothingToReadLeavesTheWindowOpen is what a release just deployed
// looks like, and what an implementation that emits nothing looks like too: no units,
// so neither exit is reachable and the cap is what ends the window. Nothing here can
// tell the two apart.
func TestASignalWithNothingToReadLeavesTheWindowOpen(t *testing.T) {
	b := boundary.Boundary{Size: 0.1, Confidence: 0.95}
	reading, err := b.Evaluate(boundary.Observed{BaselineUnits: 1000, BaselineFailures: 1})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if reading.Harm || reading.Clean {
		t.Errorf("harm=%v clean=%v with nothing emitted yet", reading.Harm, reading.Clean)
	}
	if reading.Unavailable != "" {
		t.Errorf("the reading is unavailable (%s), and the baseline is there — it is the release that has said nothing",
			reading.Unavailable)
	}
}

// closedOn is the read a test closes a window on: a pair of counts with a
// baseline in it, which is what an exit other than swept always has. The numbers
// are not what any of these tests assert over — what they assert is the exit —
// but a close with no read is refused, and rightly: an exit nobody can recompute
// is one nobody can argue with.
func closedOn() boundary.Observed {
	return boundary.Observed{Units: 200, Failures: 2, BaselineUnits: 200, BaselineFailures: 2}
}

// readFor is [closedOn] for every exit but swept, which takes none. A loop closing
// windows at each of the four exits needs the read to follow the exit.
func readFor(exit window.Exit) boundary.Observed {
	if exit == window.ExitSwept {
		return boundary.Observed{}
	}
	return closedOn()
}
