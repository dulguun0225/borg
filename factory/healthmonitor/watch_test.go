// watch_test.go is the pass itself: what one open window's reading does at each
// of the exits this package decides, and what the open writes onto the record.
// db_test.go holds the fixtures — the schema, the graph, and shipOne — and is
// where the two queries over the graph are; the two files are one external test
// package split by subject.
//
// These tests do not skip when the database is unreachable, for the reason
// db_test.go gives.
package healthmonitor_test

import (
	"context"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/boundary"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/incident"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/window"
)

// crossingEmission returns one operation's intervals on the error rate, at a
// rate the caller names against a baseline it names. It is the whole of what a
// reading needs: the arithmetic is package boundary's and is tested there.
type crossingEmission struct {
	rate         float64
	baselineRate float64
	intervals    int
	// history is what the reading against the service's own recent history sees.
	// It is flat here, so what fails a release in these tests is the comparison.
	history []boundary.Counts
}

func (e crossingEmission) series() healthmonitor.Series {
	var observed boundary.Observed
	for i := 0; i < e.intervals; i++ {
		observed.Intervals = append(observed.Intervals, boundary.Counts{
			Units: 1000, Count: int64(e.rate * 1000),
			BaselineUnits: 1000, BaselineCount: int64(e.baselineRate * 1000),
		})
	}
	return healthmonitor.Series{
		EmissionVersionRelease: "emission/1", Newest: "2026-01-01T00:00:00.000000000Z",
		Operations: []healthmonitor.OperationSeries{{
			Operation:  healthmonitor.PooledOperation,
			Quantities: map[gatepolicy.Quantity]boundary.Observed{gatepolicy.QuantityErrorRate: observed},
		}},
	}
}

func (e crossingEmission) Read(context.Context, healthmonitor.Reading) (healthmonitor.Series, error) {
	return e.series(), nil
}

func (e crossingEmission) History(context.Context, healthmonitor.History) (healthmonitor.Series, error) {
	var observed boundary.Observed
	observed.Intervals = e.history
	return healthmonitor.Series{
		EmissionVersionRelease: "emission/1",
		Operations: []healthmonitor.OperationSeries{{
			Operation:  healthmonitor.PooledOperation,
			Quantities: map[gatepolicy.Quantity]boundary.Observed{gatepolicy.QuantityErrorRate: observed},
		}},
	}, nil
}

func (crossingEmission) FailureRecords(context.Context, healthmonitor.Reading) ([]healthmonitor.FailureRecord, error) {
	return []healthmonitor.FailureRecord{{
		FailureClass: "timeout", CodeLocation: "checkout.go:41", Target: theTarget, Count: 12,
	}}, nil
}

func (crossingEmission) Spent(context.Context, string, time.Duration) (healthmonitor.Spend, error) {
	return healthmonitor.Spend{}, nil
}

func (crossingEmission) Shape(context.Context, healthmonitor.Arm) (string, error) {
	return "emission/1", nil
}

// fakeDeployer records what the health monitor asked the deployer to do, in the
// order it asked, which is what the failed exit's own ordering is read from.
type fakeDeployer struct {
	calls     []string
	rolledTo  string
	controls  []healthmonitor.Control
	tornDown  []healthmonitor.Control
	rollbacks []healthmonitor.Rollback
	kept      []healthmonitor.Kept
}

func (d *fakeDeployer) StartControl(_ context.Context, c healthmonitor.Control) error {
	d.calls = append(d.calls, "start control on "+c.Target)
	d.controls = append(d.controls, c)
	return nil
}

func (d *fakeDeployer) TearDownControl(_ context.Context, c healthmonitor.Control) error {
	d.calls = append(d.calls, "tear down control on "+c.Target)
	d.tornDown = append(d.tornDown, c)
	return nil
}

func (d *fakeDeployer) TearDownKept(_ context.Context, k healthmonitor.Kept) error {
	d.calls = append(d.calls, "tear down the fleet kept for release "+k.OfReleaseID+" on "+k.Target)
	d.kept = append(d.kept, k)
	return nil
}

func (d *fakeDeployer) RollBack(_ context.Context, r healthmonitor.Rollback) error {
	d.calls = append(d.calls, "roll back to "+r.ToReleaseID)
	d.rolledTo = r.ToReleaseID
	d.rollbacks = append(d.rollbacks, r)
	return nil
}

func (d *fakeDeployer) DeploySearch(_ context.Context, s healthmonitor.SearchDeploy) (string, error) {
	d.calls = append(d.calls, "deploy the search's build "+s.BuildID)
	return "dep_search", nil
}

// fakePager keeps the waits it was handed and the page events they left, so a
// pass that reads a standing condition sees the sequence its own earlier call
// wrote. Nothing is delivered: what this package decides is which wait fires,
// what it carries, and whether another event is owed on that row.
type fakePager struct {
	waits  []notifier.Wait
	events map[string][]notifier.Payload
}

func (p *fakePager) Notify(_ context.Context, w notifier.Wait) ([]decisionlog.Row, error) {
	p.waits = append(p.waits, w)
	p.append(w.Row, notifier.EventReached)
	return nil, nil
}

func (p *fakePager) Widen(_ context.Context, w notifier.Wait) (decisionlog.Row, error) {
	p.waits = append(p.waits, w)
	p.append(w.Row, notifier.EventWidened)
	return decisionlog.Row{}, nil
}

func (p *fakePager) EventsFor(_ context.Context, row string) ([]notifier.Payload, error) {
	return p.events[row], nil
}

func (p *fakePager) append(row string, event notifier.Event) {
	if p.events == nil {
		p.events = map[string][]notifier.Payload{}
	}
	p.events[row] = append(p.events[row], notifier.Payload{Row: row, Event: string(event)})
}

// TestTheFailedExitRollsBackRaisesTheIncidentAndClosesLast is the order the
// design states for the longest exit: the rollback's own deploy record and the
// releases it undid, the incident and the intent it raises, a page where nothing
// was rolled back, and this window closed failed last — never first, which would
// leave a release the factory had failed serving production with no rollback
// record and nothing that would ever retry.
func TestTheFailedExitRollsBackRaisesTheIncidentAndClosesLast(t *testing.T) {
	ctx, g := newGraph(t)
	below := shipOne(t, ctx, g, "in_below", window.ExitTimedOut)
	under := shipOne(t, ctx, g, "in_under", "")

	deployer := &fakeDeployer{}
	pager := &fakePager{}
	monitor := g.monitorWith(t, crossingEmission{rate: 0.5, baselineRate: 0.01, intervals: 8}, deployer, pager)

	watched, err := monitor.Watch(ctx, g.watching())
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if len(watched) != 1 {
		t.Fatalf("Watch read %d window(s), want the one open over the release under watch", len(watched))
	}
	one := watched[0]
	if one.Exit != window.ExitFailed {
		t.Fatalf("the window closed %q, want failed: the reading crossed against the release", one.Exit)
	}
	if one.WhyNoRollback != "" {
		t.Fatalf("nothing was rolled back: %s", one.WhyNoRollback)
	}
	if deployer.rolledTo != below.ID {
		t.Errorf("the rollback returned to %q, want the release below, %q", deployer.rolledTo, below.ID)
	}
	if one.IncidentID == "" {
		t.Error("the crossing raised no incident")
	}
	raised, found, err := incident.Open(ctx, g.pool, g.serviceID, under.ID)
	if err != nil || !found {
		t.Fatalf("reading the incident: found %t, %v", found, err)
	}
	if raised.FailureRecords == "" {
		t.Error("the incident carries no copy of the failure records at the crossing")
	}

	// The window's close is the last durable step: it is closed by the time
	// Watch returns, and the deployer was asked to roll back before it was.
	closed, found, err := window.ForRelease(ctx, g.pool, under.ID)
	if err != nil || !found {
		t.Fatalf("reading the window: found %t, %v", found, err)
	}
	if closed.Exit != window.ExitFailed || closed.ClosedAt == "" {
		t.Errorf("the stored window is %+v, want a failed close", closed)
	}
	if len(deployer.calls) == 0 || deployer.calls[0] != "roll back to "+below.ID {
		t.Errorf("the deployer was asked for %v, want the rollback first", deployer.calls)
	}
	if len(pager.waits) != 0 {
		t.Errorf("a page fired for a rollback that ran: %+v", pager.waits)
	}
}

// TestAFailedExitWithNothingToReturnToPagesAtAnyHour is the page condition
// Rollback states: production runs a release the factory has just failed, no
// mechanism it has will improve it, and the wait is of the first kind, so it
// fires whatever hour it arose in.
func TestAFailedExitWithNothingToReturnToPagesAtAnyHour(t *testing.T) {
	ctx, g := newGraph(t)
	shipOne(t, ctx, g, "in_first", "")

	deployer := &fakeDeployer{}
	pager := &fakePager{}
	monitor := g.monitorWith(t, crossingEmission{rate: 0.5, baselineRate: 0.01, intervals: 8}, deployer, pager)

	watched, err := monitor.Watch(ctx, g.watching())
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if len(watched) != 1 || watched[0].Exit != window.ExitFailed {
		t.Fatalf("Watch = %+v, want one failed exit", watched)
	}
	if watched[0].WhyNoRollback == "" {
		t.Fatal("a first release was rolled back, and there is nothing below it to return to")
	}
	if deployer.rolledTo != "" {
		t.Errorf("the deployer was asked to roll back to %q", deployer.rolledTo)
	}
	if len(pager.waits) != 1 {
		t.Fatalf("the failed exit with no rollback fired %d page(s), want one", len(pager.waits))
	}
	fired := pager.waits[0]
	if fired.Kind != notifier.KindFailedWithNoRollback || !fired.Worse {
		t.Errorf("the wait is %+v, want a failed-with-no-rollback that leaves production worse", fired)
	}
	if !fired.RollbackOutstanding || fired.ServiceID != g.serviceID {
		t.Errorf("the wait is %+v, want one of the first kind naming its service", fired)
	}
}

// TestAWindowThatRulesTheRegressionOutClosesPassedWithTheControlTornDownFirst is
// the shortest exit and its ordering: teardown is what a rollback needs and not
// what the exit alone decides, so the control ends before the window does.
func TestAWindowThatRulesTheRegressionOutClosesPassedWithTheControlTornDownFirst(t *testing.T) {
	ctx, g := newGraph(t)
	shipOne(t, ctx, g, "in_below", window.ExitTimedOut)
	under := shipOne(t, ctx, g, "in_under", "")

	deployer := &fakeDeployer{}
	// Both arms behaving identically over enough intervals rules the size out.
	monitor := g.monitorWith(t, crossingEmission{rate: 0.01, baselineRate: 0.01, intervals: 400}, deployer, &fakePager{})

	watched, err := monitor.Watch(ctx, g.watching())
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if len(watched) != 1 || watched[0].Exit != window.ExitPassed {
		t.Fatalf("Watch = exit %q, want passed: the comparison ruled the size out", watched[0].Exit)
	}
	// The control ends before the window closes and the fleet kept for a
	// rollback ends after it, that fleet being torn down when the last window
	// that could return to it closes and never at an exit of its own.
	if len(deployer.calls) != 2 || deployer.calls[0] != "tear down control on "+theTarget {
		t.Fatalf("the deployer was asked for %v, want the control torn down and then the kept fleet", deployer.calls)
	}
	if len(deployer.kept) != 1 || deployer.kept[0].Target != theTarget {
		t.Errorf("the kept fleets torn down are %+v, want the one on %s", deployer.kept, theTarget)
	}
	closed, found, err := window.ForRelease(ctx, g.pool, under.ID)
	if err != nil || !found {
		t.Fatalf("reading the window: found %t, %v", found, err)
	}
	if len(closed.ClosedOn.Quantities) == 0 || closed.ClosedOn.Quantities[gatepolicy.QuantityErrorRate].Units == 0 {
		t.Errorf("the window closed on %+v, want the counts the exit was decided on", closed.ClosedOn)
	}
	if len(closed.FinestSizeReached) == 0 {
		t.Error("the window records no finest size reached, which is what the score reads")
	}
}

// TestAMismatchStandingStopsTheRollbackAndTheWindowStillClosesFailed is what
// Drift detection states instead of a rollback: no deploy begins, the failed
// release keeps serving, and the health monitor raises the revert intent itself
// and pages.
func TestAMismatchStandingStopsTheRollbackAndTheWindowStillClosesFailed(t *testing.T) {
	ctx, g := newGraph(t)
	shipOne(t, ctx, g, "in_below", window.ExitTimedOut)
	shipOne(t, ctx, g, "in_under", "")

	deployer := &fakeDeployer{}
	pager := &fakePager{}
	monitor := g.monitorWithMismatch(t, crossingEmission{rate: 0.5, baselineRate: 0.01, intervals: 8},
		deployer, pager, standingMismatch{})

	watched, err := monitor.Watch(ctx, g.watching())
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if len(watched) != 1 || watched[0].Exit != window.ExitFailed {
		t.Fatalf("Watch = %+v, want one failed exit", watched)
	}
	if deployer.rolledTo != "" {
		t.Errorf("a rollback was performed while a mismatch stands: %v", deployer.calls)
	}
	if len(pager.waits) != 1 {
		t.Errorf("the failed exit fired %d page(s) with no rollback performed, want one", len(pager.waits))
	}
}

// standingMismatch is the drift detector's store answering that one stands.
type standingMismatch struct{}

func (standingMismatch) Mismatch(context.Context, string) (bool, string, error) {
	return true, "the target runs a build the factory did not record", nil
}

// TestOpenIsOnePerProductionDeployOfAReleaseNotWatchedBefore is the open's own
// rule, read off the record: a second call over a release already watched
// returns the window that exists and opens none.
func TestOpenIsOnePerProductionDeployOfAReleaseNotWatchedBefore(t *testing.T) {
	ctx, g := newGraph(t)
	under := shipOne(t, ctx, g, "in_under", "")

	monitor := g.monitorWith(t, crossingEmission{}, &fakeDeployer{}, &fakePager{})
	existing, opened, err := monitor.Open(ctx, g.watching(), "dep_second_attempt", under.ID, "scv_test", false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if opened {
		t.Error("a second window opened over a release the service has watched before")
	}
	if existing.ReleaseID != under.ID {
		t.Errorf("Open returned %+v, want the window already open over that release", existing)
	}
	if _, _, err := monitor.Open(ctx, g.watching(), "dep_x", under.ID, "", false); err == nil {
		t.Error("a window was opened naming no score version")
	}
}

// TestAnOpenIncidentWithNoOpenWindowPages is the sixth page condition: the
// crossing has not stopped, no window is open, and the deployed software is
// worse until a human ends it — whatever the factory has since raised from it.
func TestAnOpenIncidentWithNoOpenWindowPages(t *testing.T) {
	ctx, g := newGraph(t)
	g.authorTargets(t, ctx)
	shipOne(t, ctx, g, "in_below", window.ExitTimedOut)
	shipOne(t, ctx, g, "in_under", "")

	// The comparison crosses, the window closes failed, and the reading against
	// the service's own recent history goes on crossing afterwards.
	stillCrossing := crossingEmission{
		rate: 0.5, baselineRate: 0.01, intervals: 8,
		history: historyCrossing(),
	}
	deployer := &fakeDeployer{}
	pager := &fakePager{}
	monitor := g.monitorWith(t, stillCrossing, deployer, pager)
	if _, err := monitor.Watch(ctx, g.watching()); err != nil {
		t.Fatalf("Watch: %v", err)
	}
	pager.waits = nil

	paged, err := monitor.PageOpenIncidents(ctx, g.watching())
	if err != nil {
		t.Fatalf("PageOpenIncidents: %v", err)
	}
	if len(paged) != 1 || len(pager.waits) != 1 {
		t.Fatalf("PageOpenIncidents paged %v through %d wait(s), want the one open incident", paged, len(pager.waits))
	}
	fired := pager.waits[0]
	if fired.Kind != notifier.KindIncidentNoOpenWindow || !fired.Worse {
		t.Errorf("the wait is %+v, want an open incident with no open window", fired)
	}
	if fired.Holding.Duty != 2 {
		t.Errorf("the wait routes to %+v, want whoever holds (2)", fired.Holding)
	}
	if fired.ServiceID != g.serviceID {
		t.Errorf("the wait names service %q, want %q", fired.ServiceID, g.serviceID)
	}
}

// historyCrossing is a series against a service's own recent history that
// crosses: the release failing far more of its work than the history it is read
// against, over enough intervals for the spread to be read.
func historyCrossing() []boundary.Counts {
	var intervals []boundary.Counts
	for i := 0; i < 12; i++ {
		intervals = append(intervals, boundary.Counts{
			Units: 1000, Count: 400, BaselineUnits: 1000, BaselineCount: 10,
		})
	}
	return intervals
}

// TestTheErrorBudgetHoldsAndRaisesOnTheObjectivesOwnEvidence is what an
// objective does: while the budget is exhausted the service's production
// deploys are held, and the objective raises an intent keyed on the service and
// the period, so one stands per period.
func TestTheErrorBudgetHoldsAndRaisesOnTheObjectivesOwnEvidence(t *testing.T) {
	ctx, g := newGraph(t)
	g.authorObjective(t, ctx, 0.999, 30*24*60*60)

	spent := &spendingEmission{spend: healthmonitor.Spend{Units: 100_000, Good: 99_800, Covered: true}}
	monitor := g.monitorWith(t, spent, &fakeDeployer{}, &fakePager{})

	budget, err := monitor.ErrorBudget(ctx, g.watching())
	if err != nil {
		t.Fatalf("ErrorBudget: %v", err)
	}
	if !budget.Authored || !budget.Covered {
		t.Fatalf("the budget is %+v, want one authored and computed", budget)
	}
	if !budget.Exhausted || !budget.Holds() {
		t.Errorf("the budget is %+v, want it exhausted and holding: twice the objective's allowance is spent", budget)
	}

	// A period the store does not cover leaves the budget uncomputed, and an
	// uncomputed budget holds the way an exhausted one does.
	uncovered := &spendingEmission{spend: healthmonitor.Spend{Units: 10, Good: 10}}
	uncomputed, err := g.monitorWith(t, uncovered, &fakeDeployer{}, &fakePager{}).ErrorBudget(ctx, g.watching())
	if err != nil {
		t.Fatalf("ErrorBudget over a period the store does not cover: %v", err)
	}
	if uncomputed.Covered || !uncomputed.Holds() || uncomputed.Raises() {
		t.Errorf("an uncomputed budget is %+v, want one that holds and raises nothing", uncomputed)
	}

	// A service well inside its objective holds nothing.
	inside := &spendingEmission{spend: healthmonitor.Spend{Units: 100_000, Good: 100_000, Covered: true}}
	clear, err := g.monitorWith(t, inside, &fakeDeployer{}, &fakePager{}).ErrorBudget(ctx, g.watching())
	if err != nil {
		t.Fatalf("ErrorBudget: %v", err)
	}
	if clear.Exhausted || clear.Holds() {
		t.Errorf("a service that failed nothing is %+v, want a budget that holds nothing", clear)
	}
}

// spendingEmission answers the objective's read and nothing else.
type spendingEmission struct {
	crossingEmission
	spend healthmonitor.Spend
}

func (e *spendingEmission) Spent(_ context.Context, _ string, period time.Duration) (healthmonitor.Spend, error) {
	if period == time.Hour {
		return healthmonitor.Spend{}, nil
	}
	return e.spend, nil
}
