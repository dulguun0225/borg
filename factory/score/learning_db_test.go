package score_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/boundary"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/factorypolicy"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/window"
)

// This is the milestone's own demonstration at the level of the score: a supplied
// value moving because outcomes moved it, read out of a real graph rather than out
// of an evidence a test assembled — that one is learn_test.go's, in this package's
// own internal test.

// alwaysDraw selects every firing the score would have gated. The sample is one in
// ten and a test on the runtime's own generator would pass by chance.
type alwaysDraw struct{}

func (alwaysDraw) Fraction() float64 { return 0 }

// TestASuppliedValueMovesBecauseOutcomesMovedIt: three windows of one service
// close without harm, and the K the score supplies for that service rises — with
// the movement readable as a version naming the one it superseded and every
// decision after it naming the new one.
func TestASuppliedValueMovesBecauseOutcomesMovedIt(t *testing.T) {
	ctx, pool, s := newScore(t)
	start, _ := score.Starting(gatepolicy.K)

	// A real service record, because what package policy reads in force is the
	// authored value on that record and the supplied one where the field is empty —
	// and a service nobody declared has no field to be empty.
	svc, err := service.NewWriter(pool).Create(ctx, cutActor, "checkout", "/repos/checkout")
	if err != nil {
		t.Fatalf("creating the service: %v", err)
	}
	// The factory policy record too: every read of what is in force asks which pins
	// are placed, and a pin may name that record, so a factory nobody installed has
	// no record for the question to be asked against.
	if _, err := factorypolicy.NewWriter(pool).Ensure(ctx, owner); err != nil {
		t.Fatalf("ensuring the factory policy record: %v", err)
	}

	before, found := s.Version().Value(gatepolicy.K, svc.ID)
	if !found || before.Value != start.Value || before.Moved() {
		t.Fatalf("K on a fresh factory reads %+v, want the starting value for every subject", before)
	}

	// Two windows closed at the cap move nothing: the rise is per three, and a
	// service that rose on two would be one taking throughput it has not earned.
	closeWindows(t, ctx, pool, svc.ID, 2)
	twoClosed, err := score.Learn(ctx, pool)
	if err != nil {
		t.Fatalf("Learn: %v", err)
	}
	if k, _ := twoClosed.Value(gatepolicy.K, svc.ID); k.Value != start.Value {
		t.Errorf("two windows without harm supply K = %v, want the starting %v", k.Value, start.Value)
	}

	// The third moves it, and the row says what moved it.
	closeWindows(t, ctx, pool, svc.ID, 1)
	learned, err := score.Learn(ctx, pool)
	if err != nil {
		t.Fatalf("Learn: %v", err)
	}
	k, found := learned.Value(gatepolicy.K, svc.ID)
	if !found || k.Value != start.Value+1 {
		t.Fatalf("three windows without harm supply K = %+v, want %v", k, start.Value+1)
	}
	if !k.Moved() || k.Subject != svc.ID {
		t.Errorf("the moved value names subject %q, want the service it was learned about", k.Subject)
	}
	if k.Why == "" {
		t.Error("the moved value carries no evidence, and a learned number nobody can argue with is one nobody will trust")
	}

	// The version moves by the ordinary path, and the one it superseded still says
	// what it said: a decision taken before the movement is readable against the
	// value it was decided under.
	moved, err := score.NewWriter(pool).Ensure(ctx, scoreActor)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if moved.ID == s.Version().ID {
		t.Fatal("the version did not move, and a supplied value did")
	}
	if moved.Supersedes != s.Version().ID {
		t.Errorf("the moved version supersedes %q, want %q", moved.Supersedes, s.Version().ID)
	}
	if now, _ := moved.Value(gatepolicy.K, svc.ID); now.Value != start.Value+1 {
		t.Errorf("the appended version supplies K = %v", now.Value)
	}
	if was, err := score.Get(ctx, pool, s.Version().ID); err != nil {
		t.Fatalf("Get the superseded version: %v", err)
	} else if k, _ := was.Value(gatepolicy.K, svc.ID); k.Value != start.Value {
		t.Errorf("the superseded version now supplies K = %v, and an append-only record does not change", k.Value)
	}

	// A second ensure over the same store appends nothing: the rules are a function
	// of the graph, so a pass that runs twice moves nothing twice.
	again, err := score.NewWriter(pool).Ensure(ctx, scoreActor)
	if err != nil {
		t.Fatalf("Ensure again: %v", err)
	}
	if again.ID != moved.ID {
		t.Errorf("a second ensure appended %s beside %s", again.ID, moved.ID)
	}

	// And what policy reads in force is the moved value, from the score, with the
	// evidence on it — which is the whole point of the movement.
	effective, err := policy.NewReader(pool, moved).WindowParameters(ctx, svc.ID)
	if err != nil {
		t.Fatalf("WindowParameters: %v", err)
	}
	if effective.K.Source != policy.FromSupplied || effective.K.Number != start.Value+1 {
		t.Errorf("K in force reads %v from %s, want the moved %v", effective.K.Number, effective.K.Source, start.Value+1)
	}
	if effective.K.Supplied.Why != k.Why {
		t.Errorf("K in force carries the evidence %q, want the score's own", effective.K.Supplied.Why)
	}
}

// TestTheThresholdFallsWhereTheScorePassedSomethingThatWentWrong is the
// calibration over a real graph: a change the score auto-passed on the number and
// a window that condemned it lower the threshold that row supplies below the
// number it passed.
func TestTheThresholdFallsWhereTheScorePassedSomethingThatWentWrong(t *testing.T) {
	ctx, pool, s := newScore(t)
	g := gate.New(decisionlog.NewWriter(pool), s, fakePolicy{threshold: 0.9}, gate.NoReconciler{})

	// A threshold of nine tenths auto-passes anything, which is the state the
	// calibration is evidence against.
	it, implementation := cutItem(t, ctx, pool, "item/passed")
	opened, err := g.Fire(ctx, firing(it, implementation,
		score.Measurement{LinesChanged: 20, FilesChanged: 1, FilesInTree: 10}))
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if opened.HumanDecides {
		t.Fatalf("the firing put a human at the row at a threshold of 0.9: %s", opened.WhyHuman)
	}
	if _, err := g.AutoPass(ctx, opened); err != nil {
		t.Fatalf("AutoPass: %v", err)
	}

	// It ships, and its window condemns it.
	rel := mint(t, ctx, pool, serviceID, it.ID, 1)
	openWindow(t, ctx, pool, serviceID, rel.ID, window.ExitHarm, false)
	rollBack(t, ctx, pool, rel.ID)

	learned, err := score.Learn(ctx, pool)
	if err != nil {
		t.Fatalf("Learn: %v", err)
	}
	threshold, found := learned.Value(gatepolicy.RiskThreshold, string(gate.MergeToMaster))
	if !found {
		t.Fatal("the score supplies no threshold for the merge row")
	}
	if threshold.Value >= opened.Assessment.Number {
		t.Errorf("the threshold reads %v after passing a change at %v that was condemned, and the next change like it would pass again",
			threshold.Value, opened.Assessment.Number)
	}
	if !threshold.Moved() || threshold.Subject != string(gate.MergeToMaster) {
		t.Errorf("the moved threshold names subject %q, want the gate row", threshold.Subject)
	}
}

// TestTheSampleRemovesTheNumbersHumanAndTheSelectionSticks: the one mechanism in
// the tree that takes a human off a row, and the two fields it is recorded in.
func TestTheSampleRemovesTheNumbersHumanAndTheSelectionSticks(t *testing.T) {
	ctx, pool, _ := newScore(t)
	version, found, err := score.Newest(ctx, pool)
	if err != nil || !found {
		t.Fatalf("Newest: %v", err)
	}
	s := score.New(pool, version, alwaysDraw{})
	g := gate.New(decisionlog.NewWriter(pool), s, fakePolicy{threshold: 0.1}, gate.NoReconciler{})

	it, implementation := cutItem(t, ctx, pool, "item/sampled")
	opened, err := g.Fire(ctx, firing(it, implementation,
		score.Measurement{LinesChanged: 900, FilesChanged: 10, FilesInTree: 10}))
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if opened.Assessment.Number < 0.1 {
		t.Fatalf("the firing reads %v, and this test needs a number the score would have gated", opened.Assessment.Number)
	}
	if opened.HumanDecides {
		t.Fatalf("a held-out firing put a human at the row: %s", opened.WhyHuman)
	}
	if !opened.HeldOut || opened.WhyHeldOut != score.SelectedHere {
		t.Errorf("the firing reads held out %v because %q", opened.HeldOut, opened.WhyHeldOut)
	}

	closing, err := g.AutoPass(ctx, opened)
	if err != nil {
		t.Fatalf("AutoPass: %v", err)
	}
	var payload score.Closing
	if err := json.Unmarshal([]byte(closing.Payload), &payload); err != nil {
		t.Fatalf("reading the closing payload: %v", err)
	}
	if payload.AutoPassedBy != score.AutoPassedBySample {
		t.Errorf("the closing row says it was auto-passed by %q, want the sample", payload.AutoPassedBy)
	}

	// The selection is the item's and not the firing's: a second row on the same
	// item is held out whatever its number reads, and where the number would have
	// passed anyway the closing row says the threshold.
	held, err := score.HeldOut(ctx, pool, it.ID)
	if err != nil || !held {
		t.Fatalf("HeldOut = %v, %v; the selection is written on the decisions", held, err)
	}
	small, err := g.Fire(ctx, firing(it, implementation,
		score.Measurement{LinesChanged: 1, FilesChanged: 1, FilesInTree: 1000}))
	if err != nil {
		t.Fatalf("Fire again: %v", err)
	}
	if !small.HeldOut || small.WhyHeldOut != score.SelectedEarlier {
		t.Errorf("a second firing on a selected item reads held out %v because %q", small.HeldOut, small.WhyHeldOut)
	}

	// A pin is never passed. The sample is asked with the pin's answer, so a gate
	// pinned always-on keeps its human however the draw falls.
	pinned := gate.New(decisionlog.NewWriter(pool), s,
		fakePolicy{threshold: 0.1, pinned: true}, gate.NoReconciler{})
	other, otherImplementation := cutItem(t, ctx, pool, "item/pinned")
	opened, err = pinned.Fire(ctx, firing(other, otherImplementation,
		score.Measurement{LinesChanged: 900, FilesChanged: 10, FilesInTree: 10}))
	if err != nil {
		t.Fatalf("Fire over the pinned row: %v", err)
	}
	if !opened.HumanDecides {
		t.Error("the sample passed a pinned gate, which is the one thing a pin exists to prevent")
	}
	if opened.HeldOut {
		t.Error("the score selected an item at a pinned row")
	}
}

// TestAWindowClosingWithoutHarmNarrowsThePrior: the prior moves on every outcome
// on that author's work, so it keeps moving on a factory that has stopped putting
// humans at gates. Until the windows were built it could only move on a verdict.
func TestAWindowClosingWithoutHarmNarrowsThePrior(t *testing.T) {
	ctx, pool, s := newScore(t)

	first, _ := cutItem(t, ctx, pool, "item/one")
	wide, err := s.Assess(ctx, score.Change{ItemID: first.ID, ServiceID: serviceID, AreaID: areaID})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	before := levelOf(t, wide, "authorship.prior")

	// The first item ships and its window closes at the cap, which counts: a
	// release that was never condemned is one the factory can return to.
	rel := mint(t, ctx, pool, serviceID, first.ID, 1)
	openWindow(t, ctx, pool, serviceID, rel.ID, window.ExitCap, false)

	second, _ := cutItem(t, ctx, pool, "item/two")
	narrowed, err := s.Assess(ctx, score.Change{ItemID: second.ID, ServiceID: serviceID, AreaID: areaID})
	if err != nil {
		t.Fatalf("Assess again: %v", err)
	}
	after := levelOf(t, narrowed, "authorship.prior")
	if after >= before {
		t.Errorf("the prior reads %v after a window closed without harm, and %v before it", after, before)
	}

	// A window that condemned a release widens it again, and the reading says
	// which of the outcomes it counted.
	third, _ := cutItem(t, ctx, pool, "item/three")
	condemned := mint(t, ctx, pool, serviceID, third.ID, 2)
	openWindow(t, ctx, pool, serviceID, condemned.ID, window.ExitHarm, false)
	fourth, _ := cutItem(t, ctx, pool, "item/four")
	widened, err := s.Assess(ctx, score.Change{ItemID: fourth.ID, ServiceID: serviceID, AreaID: areaID})
	if err != nil {
		t.Fatalf("Assess a fourth time: %v", err)
	}
	if levelOf(t, widened, "authorship.prior") <= after {
		t.Error("a window that condemned a release did not widen the prior")
	}
	for _, f := range widened.Vector {
		if f.Name == "authorship.prior" && f.Reading == "" {
			t.Error("the prior's reading says nothing about what it counted")
		}
	}
}

// The records these tests write around the writers that own them. Nothing here is
// a shortcut around a writer: each is the writer's own call, with the ids of the
// records the score never follows made up.

// closeWindows opens and closes n windows of the test's service, each over a
// release of its own, at the cap.
func closeWindows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, svcID string, n int) {
	t.Helper()
	existing, err := window.All(ctx, pool, svcID)
	if err != nil {
		t.Fatalf("reading the windows: %v", err)
	}
	for i := range n {
		number := int64(len(existing) + i + 1)
		it, _ := cutItem(t, ctx, pool, "item/window")
		rel := mint(t, ctx, pool, svcID, it.ID, number)
		openWindow(t, ctx, pool, svcID, rel.ID, window.ExitCap, false)
	}
}

// mint is one release of the test's service, minted by the writer that owns the
// number.
func mint(t *testing.T, ctx context.Context, pool *pgxpool.Pool, svcID, itemID string, number int64) release.Release {
	t.Helper()
	rel, err := release.NewWriter(pool).Mint(ctx, mergeActor, svcID,
		"bl_0000000000000000000000000000000a", itemID)
	if err != nil {
		t.Fatalf("minting a release: %v", err)
	}
	if rel.Number != number {
		t.Fatalf("the release is number %d, want %d", rel.Number, number)
	}
	return rel
}

// openWindow opens a window over a deploy of one release and closes it at one
// exit, which is the outcome every rule here is folded over.
func openWindow(t *testing.T, ctx context.Context, pool *pgxpool.Pool, svcID, releaseID string, exit window.Exit, heldOut bool) window.Window {
	t.Helper()
	writer := window.NewWriter(pool)
	opened, err := writer.Open(ctx, record.Actor{Kind: record.KindComponent, Name: "comparison"}, window.Opening{
		DeployID:       record.NewID("dep"),
		ReleaseID:      releaseID,
		ServiceID:      svcID,
		CleanAvailable: !heldOut,
		HeldOut:        heldOut,
		Size:           0.02,
		Confidence:     0.95,
		CapSeconds:     60,
		Formula:        boundary.Formula,
		PolicyVersion:  "pv_00000000000000000000000000000001",
		ScoreVersion:   "scv_0000000000000000000000000000001",
	})
	if err != nil {
		t.Fatalf("opening a window: %v", err)
	}
	closed, err := writer.Close(ctx, opened.ID, exit, closedOn())
	if err != nil {
		t.Fatalf("closing a window at %s: %v", exit, err)
	}
	return closed
}

// rollBack is the rollback record that condemns one release, written by the
// writer that owns a deploy.
func rollBack(t *testing.T, ctx context.Context, pool *pgxpool.Pool, condemned string) {
	t.Helper()
	w := deploy.NewWriter(pool)
	dep, err := w.StartUndoing(ctx, record.Actor{Kind: record.KindComponent, Name: "agent.deployer"},
		serviceID, environmentID, deploy.OfRelease(condemned, "bl_0000000000000000000000000000000a"),
		deploy.Undoing{
			CondemnedReleaseID: condemned,
			Source:             deploy.SourceComparisonAtHarm,
			RevertIntentID:     "in_0000000000000000000000000000000b",
		})
	if err != nil {
		t.Fatalf("starting the rollback: %v", err)
	}
	if err := w.Complete(ctx, dep.ID); err != nil {
		t.Fatalf("completing the rollback: %v", err)
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
