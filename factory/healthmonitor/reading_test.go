// reading_test.go is what one read of the emission is: the set of operations
// the window hands the store, the age past which a read is no volume rather
// than a low one, the failure records an incident carries a copy of, and the
// one window that reads more than the producer's own numbers.
package healthmonitor_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/boundary"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/incident"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/window"
)

// recordingEmission is [crossingEmission] with the readings it was handed kept,
// which is how the set of operations the window carries is read back.
type recordingEmission struct {
	crossingEmission
	readings []healthmonitor.Reading
}

func (e *recordingEmission) Read(ctx context.Context, r healthmonitor.Reading) (healthmonitor.Series, error) {
	e.readings = append(e.readings, r)
	return e.crossingEmission.Read(ctx, r)
}

// TestTheOperationsReadAloneAreHandedToTheStore is what makes the pooling
// happen at all: the set is decided at the open and named on the window, and
// the store is told it, so every operation outside it is pooled into one series
// per quantity per target. Handed no set, the store would answer per operation
// and the boundary would be read over series the window was not allocated over.
func TestTheOperationsReadAloneAreHandedToTheStore(t *testing.T) {
	ctx, g := newGraph(t)
	shipOne(t, ctx, g, "in_below", window.ExitTimedOut)
	shipOneWith(t, ctx, g, "in_under", "", func(o *window.OpenEvent) {
		o.OperationsReadAlone = []string{"checkout", "search"}
	})

	emission := &recordingEmission{crossingEmission: crossingEmission{rate: 0.01, baselineRate: 0.01, intervals: 4}}
	if _, err := g.monitorWith(t, emission, &fakeDeployer{}, &fakePager{}).Watch(ctx, g.watching()); err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if len(emission.readings) != 1 {
		t.Fatalf("the store was asked for %d reading(s), want the one target the window was allocated over", len(emission.readings))
	}
	alone := emission.readings[0].OperationsReadAlone
	if len(alone) != 2 || alone[0] != "checkout" || alone[1] != "search" {
		t.Errorf("the store was handed %v as the operations read alone, want the two the window names", alone)
	}
}

// TestAReadOlderThanTheIntervalIsNoVolumeAndNeverALowOne is the rule the health
// monitor reads the store by. A store that stopped keeping records reads as
// every service gone quiet at once, and a window over no volume cannot pass —
// where a low volume read off stale records would close one on evidence nothing
// produced.
func TestAReadOlderThanTheIntervalIsNoVolumeAndNeverALowOne(t *testing.T) {
	ctx, g := newGraph(t)
	shipOne(t, ctx, g, "in_below", window.ExitTimedOut)
	under := shipOne(t, ctx, g, "in_under", "")

	// Both arms behaving identically over enough intervals rules the size out,
	// so what decides the exit here is the age of the newest record alone.
	ruledOut := crossingEmission{rate: 0.01, baselineRate: 0.01, intervals: 400}
	stale := ruledOut
	stale.newest = record.FormatTime(time.Now().Add(-time.Hour))
	readings := healthmonitor.Readings{PassInterval: time.Minute}

	monitor := g.monitorComposed(t, stale, &fakeDeployer{}, &fakePager{}, nil, nil, readings)
	watched, err := monitor.Watch(ctx, g.watching())
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if len(watched) != 1 {
		t.Fatalf("Watch read %d window(s), want the one open", len(watched))
	}
	if watched[0].Exit != "" {
		t.Fatalf("the window closed %q on records an hour older than the interval its last check carries, want it still open",
			watched[0].Exit)
	}
	if watched[0].Evaluated.Volume {
		t.Error("the reading was read as volume, and a read older than that interval is no volume")
	}

	// The same series with a newest record inside the interval closes passed:
	// what the rule turns on is the age and nothing else.
	fresh := ruledOut
	fresh.newest = record.FormatTime(time.Now())
	watched, err = g.monitorComposed(t, fresh, &fakeDeployer{}, &fakePager{}, nil, nil, readings).
		Watch(ctx, g.watching())
	if err != nil {
		t.Fatalf("Watch over a fresh read: %v", err)
	}
	if len(watched) != 1 || watched[0].Exit != window.ExitPassed {
		t.Fatalf("Watch over a fresh read = exit %q, want passed", watched[0].Exit)
	}
	closed, found, err := window.ForRelease(ctx, g.pool, under.ID)
	if err != nil || !found || closed.Exit != window.ExitPassed {
		t.Errorf("the stored window is %+v (found %t, %v), want a passed close", closed.Exit, found, err)
	}
}

// TestTheIncidentsFailureRecordsCarryTheWholeKey is what makes the copy on an
// incident readable: the store keeps a count per interval per service, build,
// deploy, target, failure class and code location, and a copy missing part of
// that key is a count nobody can place.
func TestTheIncidentsFailureRecordsCarryTheWholeKey(t *testing.T) {
	ctx, g := newGraph(t)
	shipOne(t, ctx, g, "in_below", window.ExitTimedOut)
	under := shipOne(t, ctx, g, "in_under", "")

	monitor := g.monitorWith(t, crossingEmission{rate: 0.5, baselineRate: 0.01, intervals: 8},
		&fakeDeployer{}, &fakePager{})
	if _, err := monitor.Watch(ctx, g.watching()); err != nil {
		t.Fatalf("Watch: %v", err)
	}
	raised, found, err := incident.Open(ctx, g.pool, g.serviceID, under.ID)
	if err != nil || !found {
		t.Fatalf("reading the incident: found %t, %v", found, err)
	}

	var copied []healthmonitor.FailureRecord
	if err := json.Unmarshal([]byte(raised.FailureRecords), &copied); err != nil {
		t.Fatalf("reading the failure records the incident carries: %v", err)
	}
	if len(copied) != 1 {
		t.Fatalf("the incident carries %d failure record(s), want the one the store keeps", len(copied))
	}
	one := copied[0]
	if one.Interval == "" || one.ServiceName != theServiceName {
		t.Errorf("the copied record is %+v, want the interval and the service the store keys it by", one)
	}
	if one.FailureClass == "" || one.CodeLocation == "" || one.Target == "" || one.Count == 0 {
		t.Errorf("the copied record is %+v, want the rest of the key and the count", one)
	}
}

// brownoutEmission crosses the reading against a service's own recent history
// and nothing else: the comparison is flat, so what can fail the window under
// test is the reading a brownout's window takes beside the producer's numbers.
type brownoutEmission struct{ crossingEmission }

func (e brownoutEmission) History(context.Context, healthmonitor.History) (healthmonitor.Series, error) {
	return healthmonitor.Series{
		EmissionVersionRelease: "emission/1",
		Operations: []healthmonitor.OperationSeries{{
			Operation: healthmonitor.PooledOperation,
			Quantities: map[gatepolicy.Quantity]boundary.Observed{
				gatepolicy.QuantityErrorRate: {Intervals: historyCrossing()},
			},
		}},
	}, nil
}

// aBrownout answers whether the release under watch is a brownout of a marked
// element, which is package contractcheck's walk in the composition.
type aBrownout bool

func (b aBrownout) IsBrownout(context.Context, string) (bool, error) { return bool(b), nil }

// TestABrownoutsWindowFailsOnAnyServiceCrossingItsOwnHistory is the one window
// that reads more than the producer's own numbers. A brownout's effect lands
// wherever the hidden read is: a field a consumer parses and now fails on errs
// in that consumer's numbers alone. So any service crossing the reading against
// its own recent history while the window is open fails it, and the same
// reading over a release that is no brownout fails nothing.
func TestABrownoutsWindowFailsOnAnyServiceCrossingItsOwnHistory(t *testing.T) {
	ordinary, ordinaryDeployer := watchOneWindow(t, aBrownout(false))
	if ordinary.Exit == window.ExitFailed || len(ordinaryDeployer.rollbacks) != 0 {
		t.Fatalf("an ordinary window closed %q with %d rollback(s) on a crossing it does not read: it reads this service's numbers alone",
			ordinary.Exit, len(ordinaryDeployer.rollbacks))
	}

	failed, deployer := watchOneWindow(t, aBrownout(true))
	if failed.Exit != window.ExitFailed {
		t.Fatalf("the brownout's window closed %q, want failed: a service crossed its own recent history while it was open",
			failed.Exit)
	}
	if failed.Evaluated.Crossed == nil || failed.Evaluated.Crossed.Kind != healthmonitor.KindOwnHistory {
		t.Errorf("the crossing is %+v, want the reading against a service's own recent history", failed.Evaluated.Crossed)
	}
	if len(deployer.rollbacks) != 1 {
		t.Errorf("the brownout's window failed and %d rollback(s) followed, want the element restored", len(deployer.rollbacks))
	}
}

// watchOneWindow ships a release whose window carries no size for the reading
// against the service's own recent history — so the reading beside the
// comparison does not run and what can fail the window is the brownout's
// cross-service reading alone — and evaluates it once, told whether the release
// is a brownout. Each case takes a graph of its own, an exit reached in one
// leaving no open window for the next.
func watchOneWindow(t *testing.T, brownouts healthmonitor.Brownouts) (healthmonitor.Watched, *fakeDeployer) {
	t.Helper()
	ctx, g := newGraph(t)
	g.authorTargets(t, ctx)
	noOwnHistory := func(o *window.OpenEvent) { o.OwnHistorySize = nil }
	shipOneWith(t, ctx, g, "in_below", window.ExitTimedOut, noOwnHistory)
	shipOneWith(t, ctx, g, "in_under", "", noOwnHistory)

	emission := brownoutEmission{crossingEmission{rate: 0.01, baselineRate: 0.01, intervals: 4}}
	deployer := &fakeDeployer{}
	monitor := g.monitorComposed(t, emission, deployer, &fakePager{}, nil, brownouts, healthmonitor.Readings{
		OwnHistorySize:      map[gatepolicy.Quantity]float64{errorRate: 0.1},
		OwnHistoryRunLength: 500,
	})
	watched, err := monitor.Watch(ctx, g.watching())
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if len(watched) != 1 {
		t.Fatalf("Watch read %d window(s), want the one open over the release under watch", len(watched))
	}
	return watched[0], deployer
}

// TestAnInterruptedExitIsFinishedAtTheExitThatBegan is the close being the
// exit's last step: a stop between the first record the failed exit writes and
// that close leaves the window carrying the exit that began, and the next
// evaluation finishes it — where reading the release again would find nothing
// crossing, the rollback the interrupted attempt performed having removed it,
// and would close the window timed out.
func TestAnInterruptedExitIsFinishedAtTheExitThatBegan(t *testing.T) {
	ctx, g := newGraph(t)
	below := shipOne(t, ctx, g, "in_below", window.ExitTimedOut)
	under := shipOne(t, ctx, g, "in_under", "")

	open, found, err := window.ForRelease(ctx, g.pool, under.ID)
	if err != nil || !found {
		t.Fatalf("reading the window: found %t, %v", found, err)
	}
	if _, err := g.windows.Begin(ctx, open.ID, window.ExitFailed); err != nil {
		t.Fatalf("recording that the failed exit began: %v", err)
	}

	// Nothing crosses now: the release the exit failed is no longer serving what
	// crossed, which is what the rollback the interrupted attempt performed did.
	deployer := &fakeDeployer{}
	monitor := g.monitorWith(t, crossingEmission{rate: 0.01, baselineRate: 0.01, intervals: 4}, deployer, &fakePager{})
	watched, err := monitor.Watch(ctx, g.watching())
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if len(watched) != 1 || watched[0].Exit != window.ExitFailed {
		t.Fatalf("the window closed %q, want the failed exit that began on it finished", watched[0].Exit)
	}
	if len(deployer.rollbacks) != 1 || deployer.rolledTo != below.ID {
		t.Errorf("the deployer was asked for %+v, want the rollback the interrupted exit had not reached", deployer.rollbacks)
	}

	closed, found, err := window.ForRelease(ctx, g.pool, under.ID)
	if err != nil || !found {
		t.Fatalf("reading the closed window: found %t, %v", found, err)
	}
	if closed.Exit != window.ExitFailed || closed.ExitBegun != window.ExitFailed {
		t.Errorf("the stored window is exit %q begun %q, want both failed", closed.Exit, closed.ExitBegun)
	}
}

// TestASecondEvaluationDoesNotRollBackTwice is the other half of finishing an
// interrupted exit: the rollback is the one step that is not its own answer
// twice, so a record of it already performed is what stops the second
// evaluation taking production back past a release the first call removed.
func TestASecondEvaluationDoesNotRollBackTwice(t *testing.T) {
	ctx, g := newGraph(t)
	below := shipOne(t, ctx, g, "in_below", window.ExitTimedOut)
	under := shipOne(t, ctx, g, "in_under", "")

	deployer := &fakeDeployer{}
	monitor := g.monitorWith(t, crossingEmission{rate: 0.5, baselineRate: 0.01, intervals: 8}, deployer, &fakePager{})
	if _, err := monitor.Watch(ctx, g.watching()); err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if len(deployer.rollbacks) != 1 || deployer.rolledTo != below.ID {
		t.Fatalf("the first pass asked for %+v, want the one rollback", deployer.rollbacks)
	}

	// The window closed, so the rollback is not asked for again by any later
	// pass; what the guard answers is the exit that begins again on a window the
	// stop left open, which the deploy record already says was performed.
	closed, found, err := window.ForRelease(ctx, g.pool, under.ID)
	if err != nil || !found || closed.Exit != window.ExitFailed {
		t.Fatalf("the window is %+v (found %t, %v), want it closed failed", closed.Exit, found, err)
	}
	if strings.Count(strings.Join(deployer.calls, "\n"), "roll back to") != 1 {
		t.Errorf("the deployer was asked for %v, want one rollback", deployer.calls)
	}
}

// TestAnInterruptedPassedExitIsFinishedAtTheExitThatBegan is the same
// guarantee for an exit that writes more than one record without a rollback:
// the control's teardown before the close, and the search-deploy ending
// before it. A stop after the first of those and before the close leaves the
// window's own record naming passed as the exit under way, and the next
// evaluation finishes that one rather than deciding again from a reading a
// step already taken — here the control's own teardown — has since changed:
// what would read as a crossing on a second look is not read as one.
func TestAnInterruptedPassedExitIsFinishedAtTheExitThatBegan(t *testing.T) {
	ctx, g := newGraph(t)
	shipOne(t, ctx, g, "in_below", window.ExitTimedOut)
	under := shipOne(t, ctx, g, "in_under", "")

	open, found, err := window.ForRelease(ctx, g.pool, under.ID)
	if err != nil || !found {
		t.Fatalf("reading the window: found %t, %v", found, err)
	}
	if _, err := g.windows.Begin(ctx, open.ID, window.ExitPassed); err != nil {
		t.Fatalf("recording that the passed exit began: %v", err)
	}

	// A comparison that would fail this window on a fresh reading: the exit
	// already under way is what the second evaluation finishes regardless.
	deployer := &fakeDeployer{}
	monitor := g.monitorWith(t, crossingEmission{rate: 0.5, baselineRate: 0.01, intervals: 8}, deployer, &fakePager{})
	watched, err := monitor.Watch(ctx, g.watching())
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if len(watched) != 1 || watched[0].Exit != window.ExitPassed {
		t.Fatalf("the window closed %q, want the passed exit that began on it finished", watched[0].Exit)
	}
	if len(deployer.rollbacks) != 0 {
		t.Errorf("the deployer was asked for %+v, want no rollback: the exit that began was passed", deployer.rollbacks)
	}

	closed, found, err := window.ForRelease(ctx, g.pool, under.ID)
	if err != nil || !found {
		t.Fatalf("reading the closed window: found %t, %v", found, err)
	}
	if closed.Exit != window.ExitPassed || closed.ExitBegun != window.ExitPassed {
		t.Errorf("the stored window is exit %q begun %q, want both passed", closed.Exit, closed.ExitBegun)
	}
}

// TestAnInterruptedTimedOutExitIsFinishedAtTheExitThatBegan is the same for
// the cap: a stop after the control's teardown and before the close leaves
// the record naming timed out as the exit under way, finished the same way
// regardless of what a fresh reading now shows.
func TestAnInterruptedTimedOutExitIsFinishedAtTheExitThatBegan(t *testing.T) {
	ctx, g := newGraph(t)
	shipOne(t, ctx, g, "in_below", window.ExitTimedOut)
	under := shipOne(t, ctx, g, "in_under", "")

	open, found, err := window.ForRelease(ctx, g.pool, under.ID)
	if err != nil || !found {
		t.Fatalf("reading the window: found %t, %v", found, err)
	}
	if _, err := g.windows.Begin(ctx, open.ID, window.ExitTimedOut); err != nil {
		t.Fatalf("recording that the timed out exit began: %v", err)
	}

	deployer := &fakeDeployer{}
	monitor := g.monitorWith(t, crossingEmission{rate: 0.5, baselineRate: 0.01, intervals: 8}, deployer, &fakePager{})
	watched, err := monitor.Watch(ctx, g.watching())
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if len(watched) != 1 || watched[0].Exit != window.ExitTimedOut {
		t.Fatalf("the window closed %q, want the timed out exit that began on it finished", watched[0].Exit)
	}
	if len(deployer.rollbacks) != 0 {
		t.Errorf("the deployer was asked for %+v, want no rollback: the exit that began was timed out", deployer.rollbacks)
	}

	closed, found, err := window.ForRelease(ctx, g.pool, under.ID)
	if err != nil || !found {
		t.Fatalf("reading the closed window: found %t, %v", found, err)
	}
	if closed.Exit != window.ExitTimedOut || closed.ExitBegun != window.ExitTimedOut {
		t.Errorf("the stored window is exit %q begun %q, want both timed out", closed.Exit, closed.ExitBegun)
	}
}
