package healthmonitor

import (
	"context"
	"fmt"
	"time"

	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/service"
)

// Budget is what is left of a service's objective over its period, and what
// follows from it. The objective is the proportion of a quantity that must be
// good over a stated period; the error budget is the remainder, and the burn
// rate is the share of that budget spent per hour.
//
// Two readings, because they answer different questions: the long one sees a
// service failing the same fraction every day, and the short one sees a fault an
// hour old.
type Budget struct {
	// Authored is whether an owner authored an objective at all. Where they
	// authored none there is no budget and nothing is held.
	Authored bool
	// Covered is whether the store covers the whole period. A period it does not
	// cover leaves the budget uncomputed, and an uncomputed budget holds the way
	// an exhausted one does: a budget taken as intact over records that are not
	// there is an absent input read as evidence.
	Covered bool
	// PeriodSeconds is the period the objective is read over, and Objective is
	// the proportion that must be good.
	PeriodSeconds float64
	Objective     float64
	// Operation is the series the budget below was read on: the operation
	// nearest exhaustion of the one objective authored for the service. The
	// objective stays authored per service and is read per operation, one value
	// against each series, so what holds the service is whichever series has
	// least left.
	Operation string
	// Remaining is the share of the budget left, from one at nothing spent down
	// through zero and below where more has been spent than the objective allows.
	Remaining float64
	// BurnRatePeriod and BurnRateLastHour are the share of the budget spent per
	// hour, over the period so far and over the last hour.
	BurnRatePeriod   float64
	BurnRateLastHour float64
	// Exhausted is the budget being spent. ExhaustsBeforeThePeriodEnds is either
	// burn rate spending what is left before the period rolls forward far enough
	// to restore it.
	Exhausted                   bool
	ExhaustsBeforeThePeriodEnds bool
}

// Holds is whether this service's production deploys are held on the objective.
// An exhausted budget holds and so does an uncomputed one, and the hold lifts
// itself when the period rolls forward far enough to restore the budget or the
// store covers the period again. Nothing is decided and no page fires, which is
// the shape the hold a declared dependency that is not current sets already has.
func (b Budget) Holds() bool { return b.Authored && (!b.Covered || b.Exhausted) }

// Admits is whether one item passes that hold. Two items do: a revert, which
// passes the hold a rollback leaves for the same reason, and an item whose
// intent a detector raised on that service — the health monitor's at a crossing
// or the objective's own. The second is what makes the hold passable on a
// service that crossed nothing: the health monitor raises nothing there, and
// without the objective's raise the hold would stand with no item able to lift
// it. A request an owner raises on that service does not pass; the route is the
// objective's intent, which exists whenever the budget is exhausted.
//
// It takes the item's fields as plain values rather than the item, so the rule
// the objective sets lives with the objective while the records stay with the
// caller that reads them: source is what the intent the item was decomposed
// from was raised by, raisedOnThisService whether that intent's evidence names
// the service being deployed, and revert whether the item is the revert of the
// rollback outstanding on it.
//
// A budget that holds nothing admits everything, so a caller may ask this
// without asking [Budget.Holds] first.
func (b Budget) Admits(source intent.Source, raisedOnThisService, revert bool) bool {
	if !b.Holds() || revert {
		return true
	}
	return source == intent.SourceDetector && raisedOnThisService
}

// Raises is whether the objective raises an intent: the budget exhausted, or
// either burn rate exhausting it before the period ends. An uncomputed budget
// raises nothing — there is nothing to work on a service the store holds no
// records for — where it still holds.
func (b Budget) Raises() bool {
	return b.Authored && b.Covered && (b.Exhausted || b.ExhaustsBeforeThePeriodEnds)
}

// ErrorBudget is what is left of one service's objective, read from the same
// emission the comparison reads. A service whose owner authored no objective
// returns an unauthored budget: nothing is computed and nothing is held, that
// reading and the window being the whole of what protects it.
func (h *HealthMonitor) ErrorBudget(ctx context.Context, w Watching) (Budget, error) {
	if err := w.validate(); err != nil {
		return Budget{}, err
	}
	svc, err := service.Get(ctx, h.pool, w.ID)
	if err != nil {
		return Budget{}, err
	}
	if !svc.Objective.Authored() {
		return Budget{}, nil
	}

	period := time.Duration(svc.Objective.PeriodSeconds.Number) * time.Second
	b := Budget{
		Authored: true, PeriodSeconds: svc.Objective.PeriodSeconds.Number,
		Objective: svc.Objective.Target.Number,
	}
	overThePeriod, err := h.emission.Spent(ctx, w.Name, period)
	if err != nil {
		return Budget{}, fmt.Errorf("healthmonitor: reading what %s spent of its objective: %w", w.Name, err)
	}
	worst, covered := leastRemaining(overThePeriod, b.Objective)
	if !covered {
		return b, nil
	}
	b.Covered = true
	b.Operation, b.Remaining = worst.operation, worst.remaining
	b.Exhausted = b.Remaining <= 0

	// The burn rate is the share of the whole budget spent per hour. The
	// period's own reading is what it spent over the period divided by the
	// hours elapsed in it — the whole period's hours, since the store covers it
	// entirely or the budget is uncomputed above. The last hour's is the share
	// of that same whole-period budget spent in one hour — never the share of
	// an hour's own tiny allowance, which would read a bad hour on a service
	// serving little the same as a bad hour on a busy one.
	hours := period.Hours()
	if hours > 0 {
		b.BurnRatePeriod = (1 - b.Remaining) / hours
	}
	lastHour, err := h.emission.Spent(ctx, w.Name, time.Hour)
	if err != nil {
		return Budget{}, fmt.Errorf("healthmonitor: reading what %s spent in the last hour: %w", w.Name, err)
	}
	if periodSpend, found := spendFor(overThePeriod, b.Operation); found {
		if hourSpend, found := spendFor(lastHour, b.Operation); found && hourSpend.Covered {
			b.BurnRateLastHour = burnRateAgainstPeriod(hourSpend, periodSpend, b.Objective)
		}
	}
	// Either reading exhausts what is left before the period ends where the rate
	// it is burning at spends the remainder inside the hours the period has to
	// run. The period so far is what the long reading sees; the last hour is the
	// fault an hour old.
	for _, rate := range []float64{b.BurnRatePeriod, b.BurnRateLastHour} {
		if rate > 0 && b.Remaining > 0 && rate*hours > b.Remaining {
			b.ExhaustsBeforeThePeriodEnds = true
		}
	}
	return b, nil
}

// onOneSeries is one operation's share of the budget left, which is what the
// objective is read as against each series.
type onOneSeries struct {
	operation string
	remaining float64
}

// leastRemaining is the operation with least of the budget left and whether the
// store covered the period on every series read. The one authored objective is
// held against each series, so the service is as exhausted as its worst
// operation: read over the operations as one, an operation failing its
// objective is diluted by every operation that is not.
//
// A period the store does not cover on any one series leaves the whole budget
// uncomputed, and so does a store that returned no series at all — both are an
// absent input, and an absent input is never evidence that the budget is
// intact.
func leastRemaining(spends []Spend, objective float64) (onOneSeries, bool) {
	if len(spends) == 0 {
		return onOneSeries{}, false
	}
	worst := onOneSeries{remaining: 1}
	for i, spend := range spends {
		if !spend.Covered {
			return onOneSeries{}, false
		}
		left := remaining(spend, objective)
		if i == 0 || left < worst.remaining {
			worst = onOneSeries{operation: spend.Operation, remaining: left}
		}
	}
	return worst, true
}

// remaining is the share of the budget left: what the objective allows to be bad
// against what actually was. A period with no work counted spends nothing.
func remaining(spend Spend, objective float64) float64 {
	if spend.Units <= 0 {
		return 1
	}
	allowed := (1 - objective) * float64(spend.Units)
	if allowed <= 0 {
		// An objective of everything being good allows nothing, so one bad unit
		// spends the whole budget and no unit spends none of it.
		if spend.Units-spend.Good > 0 {
			return 0
		}
		return 1
	}
	return 1 - float64(spend.Units-spend.Good)/allowed
}

// spendFor is the one spend record over the operation named, so the last
// hour's reading and the period's own are read against the same series: the
// one the objective is held on, which [leastRemaining] found has least of the
// budget left over the period.
func spendFor(spends []Spend, operation string) (Spend, bool) {
	for _, s := range spends {
		if s.Operation == operation {
			return s, true
		}
	}
	return Spend{}, false
}

// burnRateAgainstPeriod is the share of the whole period's budget spent in
// hourSpend, which periodSpend's own units size that budget by — never the
// share of hourSpend's own allowance, which would scale a bad hour on a
// service serving little the same as a bad hour on a busy one.
func burnRateAgainstPeriod(hourSpend, periodSpend Spend, objective float64) float64 {
	allowed := (1 - objective) * float64(periodSpend.Units)
	bad := float64(hourSpend.Units - hourSpend.Good)
	if allowed <= 0 {
		// An objective of everything being good allows nothing, so one bad unit in
		// the hour spends the whole period's budget and no unit spends none of it.
		if bad > 0 {
			return 1
		}
		return 0
	}
	return bad / allowed
}

// RaiseObjectiveIntent is the objective's own raise: it writes an unrefined
// intent on the service where the budget is exhausted, or where either burn rate
// exhausts it before the period ends. It returns the intent's id, empty where
// nothing was raised.
//
// The evidence that keys it is the service and the objective's period, so one
// intent stands per period: a second pass inside the same period finds the one
// already open and raises nothing. The raise reaches Work as an unrefined intent
// and never as a page — an escalation on it pages, the way an escalation on an
// item raised from an incident does.
func (h *HealthMonitor) RaiseObjectiveIntent(ctx context.Context, w Watching, b Budget) (string, error) {
	if !b.Raises() || h.intake == nil {
		return "", nil
	}
	evidence := intent.Evidence{
		ServiceID:       w.ID,
		ObjectivePeriod: fmt.Sprintf("%.0fs", b.PeriodSeconds),
	}
	waiting, found, err := intent.OnEvidence(ctx, h.pool, evidence)
	if err != nil {
		return "", err
	}
	if found {
		return waiting.ID, nil
	}
	statement := fmt.Sprintf("%s has spent its error budget on %s: %.0f%% of the objective's allowance is left over a period of %.0f seconds",
		w.Name, b.Operation, b.Remaining*100, b.PeriodSeconds)
	if !b.Exhausted {
		statement = fmt.Sprintf("%s is spending its error budget on %s faster than the period restores it: %.0f%% left, burning %.3f of it an hour",
			w.Name, b.Operation, b.Remaining*100, max(b.BurnRatePeriod, b.BurnRateLastHour))
	}
	taken, err := h.intake.TakeIn(ctx, Actor, intent.Arrival{
		Source: intent.SourceDetector, Statement: statement, Evidence: evidence,
	})
	if err != nil {
		return "", err
	}
	return taken.ID, nil
}
