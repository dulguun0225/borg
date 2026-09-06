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
//
// The two items that pass the hold are the caller's to admit: a revert, and an
// item whose intent a detector raised on that service. doc.go names the caller.
func (b Budget) Holds() bool { return b.Authored && (!b.Covered || b.Exhausted) }

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
	if !overThePeriod.Covered {
		return b, nil
	}
	b.Covered = true
	b.Remaining = remaining(overThePeriod, b.Objective)
	b.Exhausted = b.Remaining <= 0

	// The burn rate is the share of the whole budget spent per hour, so the
	// period's own reading is what it spent over how long it has run, and the
	// last hour's is what it spent in one.
	hours := period.Hours()
	if hours > 0 {
		b.BurnRatePeriod = (1 - b.Remaining) / hours
	}
	lastHour, err := h.emission.Spent(ctx, w.Name, time.Hour)
	if err != nil {
		return Budget{}, fmt.Errorf("healthmonitor: reading what %s spent in the last hour: %w", w.Name, err)
	}
	if lastHour.Covered {
		b.BurnRateLastHour = 1 - remaining(lastHour, b.Objective)
	}
	// Either reading exhausts what is left before the period ends where the rate
	// it is burning at spends the remainder inside the hours the period has to
	// run. The period so far is what the long reading sees; the last hour is the
	// fault an hour old.
	for _, rate := range []float64{b.BurnRatePeriod, b.BurnRateLastHour} {
		if rate > 0 && b.Remaining > 0 && b.Remaining/rate < hours {
			b.ExhaustsBeforeThePeriodEnds = true
		}
	}
	return b, nil
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
	statement := fmt.Sprintf("%s has spent its error budget: %.0f%% of the objective's allowance is left over a period of %.0f seconds",
		w.Name, b.Remaining*100, b.PeriodSeconds)
	if !b.Exhausted {
		statement = fmt.Sprintf("%s is spending its error budget faster than the period restores it: %.0f%% left, burning %.3f of it an hour",
			w.Name, b.Remaining*100, max(b.BurnRatePeriod, b.BurnRateLastHour))
	}
	taken, err := h.intake.TakeIn(ctx, Actor, intent.Arrival{
		Source: intent.SourceDetector, Statement: statement, Evidence: evidence,
	})
	if err != nil {
		return "", err
	}
	return taken.ID, nil
}
