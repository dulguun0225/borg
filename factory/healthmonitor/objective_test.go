// objective_test.go is the service level objective: which items pass the hold an
// exhausted budget sets, and the budget read per operation rather than over the
// service as one. The first needs no database — the rule is a method over a
// budget the caller has already read — and the second reads the service record
// the fixtures write.
package healthmonitor_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/intent"
)

// TestTwoItemsPassTheBudgetHoldAndAnOwnersRequestDoesNot is the exception the
// design states: a revert passes, and so does an item whose intent a detector
// raised on that service — the health monitor's at a crossing or the
// objective's own. A request an owner raises on that service does not; the
// route is the objective's intent, which exists whenever the budget is
// exhausted. Without the second the hold would stand hardest exactly where
// production is worst.
func TestTwoItemsPassTheBudgetHoldAndAnOwnersRequestDoesNot(t *testing.T) {
	exhausted := healthmonitor.Budget{Authored: true, Covered: true, Exhausted: true}
	if !exhausted.Holds() {
		t.Fatalf("the budget is %+v, want one that holds", exhausted)
	}

	for _, one := range []struct {
		what                string
		source              intent.Source
		raisedOnThisService bool
		revert              bool
		admitted            bool
	}{
		{"a revert of the rollback outstanding on the service", intent.SourceOwner, false, true, true},
		{"an item whose intent a detector raised on this service", intent.SourceDetector, true, false, true},
		{"a request an owner raised on this service", intent.SourceOwner, true, false, false},
		{"an intent grouped from end users' reports on this service", intent.SourceReports, true, false, false},
		{"an item whose intent a detector raised on another service", intent.SourceDetector, false, false, false},
		{"an item decomposed from no intent at all", "", false, false, false},
	} {
		admitted := exhausted.Admits(one.source, one.raisedOnThisService, one.revert)
		if admitted != one.admitted {
			t.Errorf("the exhausted budget admits %s: %t, want %t", one.what, admitted, one.admitted)
		}
	}

	// A budget that holds nothing admits everything, so the caller reads one
	// answer and not two.
	intact := healthmonitor.Budget{Authored: true, Covered: true}
	if !intact.Admits(intent.SourceOwner, true, false) {
		t.Error("a budget that holds nothing refused an owner's request on the service")
	}
	// An uncomputed budget holds the way an exhausted one does, and the same
	// two items pass it.
	uncomputed := healthmonitor.Budget{Authored: true}
	if uncomputed.Admits(intent.SourceOwner, true, false) {
		t.Error("an uncomputed budget admitted an owner's request, and it holds the way an exhausted one does")
	}
	if !uncomputed.Admits(intent.SourceDetector, true, false) {
		t.Error("an uncomputed budget refused an item a detector raised on the service")
	}
}

// TestEitherBurnRateExhaustsBeforeThePeriodEnds is the two readings the burn
// rate takes: the period so far, share spent divided by the hours elapsed in
// it, and the last hour, the share of the whole budget — never the share of
// that hour's own allowance — spent in it. Either exhausting the remainder
// inside the period's own hours raises the objective's intent, and elapsed
// time is what the table below varies: the same last hour's traffic reads
// differently against a long period and a short one.
func TestEitherBurnRateExhaustsBeforeThePeriodEnds(t *testing.T) {
	ctx, g := newGraph(t)
	const periodSeconds = 30 * 24 * 60 * 60 // 720 hours
	g.authorObjective(t, ctx, 0.999, periodSeconds)

	// Over the period: 1,000,000 units served, 1,000 allowed bad (the
	// objective's own allowance), 400 actually bad — 60% of the period's
	// budget left, over half — so the period-so-far rate alone does not
	// exhaust it, and what can is the last hour's own reading.
	overPeriod := []healthmonitor.Spend{
		{Operation: healthmonitor.PooledOperation, Units: 1_000_000, Good: 999_600, Covered: true},
	}

	for _, one := range []struct {
		what          string
		lastHourGood  int64
		lastHourUnits int64
		exhausts      bool
	}{
		{"a service well within its objective in the last hour, over a long period", 1_000, 1_000, false},
		{"a sudden spike in the last hour alone, over the same long period", 900, 1_000, true},
	} {
		spent := &spendingEmission{spend: overPeriod, spendLastHour: []healthmonitor.Spend{
			{Operation: healthmonitor.PooledOperation, Units: one.lastHourUnits, Good: one.lastHourGood, Covered: true},
		}}
		budget, err := g.monitorWith(t, spent, &fakeDeployer{}, &fakePager{}).ErrorBudget(ctx, g.watching())
		if err != nil {
			t.Fatalf("ErrorBudget for %s: %v", one.what, err)
		}
		if budget.Exhausted {
			t.Fatalf("%s: the budget is %+v, want it not yet exhausted — 40%% of the period's budget spent, 60%% left", one.what, budget)
		}
		if budget.ExhaustsBeforeThePeriodEnds != one.exhausts {
			t.Errorf("%s: ExhaustsBeforeThePeriodEnds = %t (burn rate period %v, last hour %v), want %t",
				one.what, budget.ExhaustsBeforeThePeriodEnds, budget.BurnRatePeriod, budget.BurnRateLastHour, one.exhausts)
		}
	}

	// The period reading alone: spent past half the period's budget (550 of the
	// 1,000 allowed) is what the period-so-far rate, projected over the
	// period's own hours, exhausts — with no last-hour reading covering it.
	overHalf := &spendingEmission{spend: []healthmonitor.Spend{
		{Operation: healthmonitor.PooledOperation, Units: 1_000_000, Good: 999_450, Covered: true},
	}}
	budget, err := g.monitorWith(t, overHalf, &fakeDeployer{}, &fakePager{}).ErrorBudget(ctx, g.watching())
	if err != nil {
		t.Fatalf("ErrorBudget over half the budget spent: %v", err)
	}
	if budget.Remaining >= 0.5 || budget.Exhausted {
		t.Fatalf("the budget is %+v, want more than half spent and not yet exhausted", budget)
	}
	if !budget.ExhaustsBeforeThePeriodEnds {
		t.Errorf("the budget is %+v, want the period-so-far rate to exhaust it before the period ends", budget)
	}
}

// TestTheObjectiveIsReadPerOperationAgainstEachSeries is the objective read the
// way the size and the explicit threshold are: one value authored per service,
// held against each series. An operation failing its objective exhausts the
// budget where the service read as one is well inside it, since the operations
// that are not failing are most of the traffic.
func TestTheObjectiveIsReadPerOperationAgainstEachSeries(t *testing.T) {
	ctx, g := newGraph(t)
	g.authorObjective(t, ctx, 0.999, 30*24*60*60)

	// Read over the service as one this is 200 bad units in 1,000,200, and an
	// objective of 99.9% allows a thousand: four fifths of the budget left.
	// Read per operation, checkout failed every unit it served against an
	// allowance of a fifth of one.
	perOperation := &spendingEmission{spend: []healthmonitor.Spend{
		{Operation: healthmonitor.PooledOperation, Units: 1_000_000, Good: 1_000_000, Covered: true},
		{Operation: "checkout", Units: 200, Good: 0, Covered: true},
	}}
	budget, err := g.monitorWith(t, perOperation, &fakeDeployer{}, &fakePager{}).ErrorBudget(ctx, g.watching())
	if err != nil {
		t.Fatalf("ErrorBudget: %v", err)
	}
	if !budget.Exhausted || !budget.Holds() {
		t.Errorf("the budget is %+v, want it exhausted: checkout failed every unit it served", budget)
	}
	if budget.Operation != "checkout" {
		t.Errorf("the budget was read on %q, want the operation with least of it left", budget.Operation)
	}

	// A period the store does not cover on one series leaves the whole budget
	// uncomputed: an absent input is never evidence that the budget is intact.
	partly := &spendingEmission{spend: []healthmonitor.Spend{
		{Operation: healthmonitor.PooledOperation, Units: 1_000_000, Good: 1_000_000, Covered: true},
		{Operation: "checkout", Units: 0, Good: 0},
	}}
	uncomputed, err := g.monitorWith(t, partly, &fakeDeployer{}, &fakePager{}).ErrorBudget(ctx, g.watching())
	if err != nil {
		t.Fatalf("ErrorBudget over a series the store does not cover: %v", err)
	}
	if uncomputed.Covered || !uncomputed.Holds() {
		t.Errorf("the budget is %+v, want it uncomputed and holding", uncomputed)
	}
}
