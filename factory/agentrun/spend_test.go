// spend_test.go holds the database tests of the sum a spend ceiling compares —
// the period derived at the read, the one currency, and the runs that carry no
// amount. It is a second test file beside db_test.go, split from it by subject
// because the two together pass the 500-line bound; doc.go states the
// departure from the shape a record package takes.
package agentrun_test

import (
	"errors"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/agentrun"
	"github.com/dulguun0225/borg/factory/record"
)

// thePeriod is one credential's authored period: a length from the fifteenth of
// January, in the zone the owner authored it in, which is what the read derives
// every run's period from.
func thePeriod(length int, unit agentrun.PeriodUnit) agentrun.Period {
	return agentrun.Period{StartDate: "2026-01-15", StartZone: "Asia/Ulaanbaatar", Length: length, Unit: unit}
}

// spentAt is a run of the fixture's units and rates, on one credential, at the
// time the provider returned them. Every priced one converts to 16.
func spentAt(credential string, at time.Time, currency string) agentrun.New {
	n := runOnItem()
	n.CredentialName = credential
	n.UnitsAt = record.FormatTime(at)
	n.ConvertedAmount = 16.0
	n.Currency = currency
	return n
}

// TestTheSumIsOverThePeriodThatContainsTheInstantAsked is the period derived at
// the read: bounded at both ends, so a run in the next period is not summed
// into this one, and a period lengthened later re-buckets a run already
// written rather than stranding it.
func TestTheSumIsOverThePeriodThatContainsTheInstantAsked(t *testing.T) {
	ctx, pool, w := newTable(t)
	const credential = "anthropic.spend-test"
	january := time.Date(2026, 1, 20, 12, 0, 0, 0, time.UTC)
	february := time.Date(2026, 2, 20, 12, 0, 0, 0, time.UTC)

	for _, at := range []time.Time{january, february} {
		if _, err := w.Record(ctx, dispatcher, spentAt(credential, at, "USD")); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	spend, err := agentrun.SpendByCredentialIn(ctx, pool, credential, thePeriod(1, agentrun.PeriodMonth), january)
	if err != nil {
		t.Fatalf("SpendByCredentialIn: %v", err)
	}
	// The period starts at midnight in the zone it was authored in, which is
	// eight hours before UTC's.
	if spend.PeriodStart != "2026-01-14T16:00:00.000000000Z" || spend.PeriodEnd != "2026-02-14T16:00:00.000000000Z" {
		t.Errorf("the period is %s to %s, want the month from the fifteenth in the zone authored",
			spend.PeriodStart, spend.PeriodEnd)
	}
	if spend.Amount != 16.0 || spend.Currency != "USD" {
		t.Errorf("the sum over the period = %v %s, want the one run in it", spend.Amount, spend.Currency)
	}

	next, err := agentrun.SpendByCredentialIn(ctx, pool, credential, thePeriod(1, agentrun.PeriodMonth), february)
	if err != nil || next.Amount != 16.0 || next.PeriodStart != "2026-02-14T16:00:00.000000000Z" {
		t.Errorf("the next period = %+v, %v, want the February run alone", next, err)
	}

	lengthened, err := agentrun.SpendByCredentialIn(ctx, pool, credential, thePeriod(2, agentrun.PeriodMonth), january)
	if err != nil {
		t.Fatalf("SpendByCredentialIn over the lengthened period: %v", err)
	}
	if lengthened.Amount != 32.0 {
		t.Errorf("the lengthened period sums %v, want 32: a run already written re-buckets into it",
			lengthened.Amount)
	}
}

// TestTheSumIsOverOneCurrency is the other half of what the ceiling compares: a
// credential is one account at one provider and one invoice, so two currencies
// among the priced runs of one period is no total at all.
func TestTheSumIsOverOneCurrency(t *testing.T) {
	ctx, pool, w := newTable(t)
	const credential = "anthropic.two-currencies"
	at := time.Date(2026, 1, 20, 12, 0, 0, 0, time.UTC)

	for _, currency := range []string{"USD", "EUR"} {
		if _, err := w.Record(ctx, dispatcher, spentAt(credential, at, currency)); err != nil {
			t.Fatalf("Record in %s: %v", currency, err)
		}
	}

	_, err := agentrun.SpendByCredentialIn(ctx, pool, credential, thePeriod(1, agentrun.PeriodMonth), at)
	if !errors.Is(err, agentrun.ErrCurrenciesDiffer) {
		t.Errorf("SpendByCredentialIn over two currencies = %v, want ErrCurrenciesDiffer", err)
	}
}

// TestSpendKeepsTheUnpricedRunsApart is what the ceiling fails closed on: the
// runs in the period whose converted amount is absent are returned beside the
// sum rather than counted as nothing.
func TestSpendKeepsTheUnpricedRunsApart(t *testing.T) {
	ctx, pool, w := newTable(t)
	const credential = "anthropic.unpriced"
	at := time.Date(2026, 1, 20, 12, 0, 0, 0, time.UTC)

	if _, err := w.Record(ctx, dispatcher, spentAt(credential, at, "USD")); err != nil {
		t.Fatalf("Record: %v", err)
	}
	n := spentAt(credential, at, "USD")
	n.RatesByKind = map[string]float64{"input": 0.01} // output has no rate
	n.ConvertedAmount = 0
	unpriced, err := w.Record(ctx, dispatcher, n)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	spend, err := agentrun.SpendByCredentialIn(ctx, pool, credential, thePeriod(30, agentrun.PeriodDay), at)
	if err != nil {
		t.Fatalf("SpendByCredentialIn: %v", err)
	}
	if spend.Amount != 16.0 || len(spend.Unpriced) != 1 || spend.Unpriced[0].ID != unpriced.ID {
		t.Errorf("SpendByCredentialIn = %+v, want 16 summed and [%s] unpriced", spend, unpriced.ID)
	}
}

func TestSpendByCredentialInWithNoCredentialIsRefused(t *testing.T) {
	ctx, pool, _ := newTable(t)
	_, err := agentrun.SpendByCredentialIn(ctx, pool, "", thePeriod(1, agentrun.PeriodMonth), time.Now())
	if !errors.Is(err, agentrun.ErrCredentialNameEmpty) {
		t.Errorf("SpendByCredentialIn with no credential = %v, want ErrCredentialNameEmpty", err)
	}
}

func TestAPeriodTheOwnerNeverAuthoredIsRefused(t *testing.T) {
	ctx, pool, _ := newTable(t)

	for _, c := range []struct {
		what   string
		period agentrun.Period
		want   error
	}{
		{"no length", agentrun.Period{StartDate: "2026-01-15", StartZone: "UTC"}, agentrun.ErrPeriodUnknown},
		{"a unit finer than a day", agentrun.Period{StartDate: "2026-01-15", StartZone: "UTC",
			Length: 1, Unit: "hour"}, agentrun.ErrPeriodUnknown},
		{"a zone that is nowhere", agentrun.Period{StartDate: "2026-01-15", StartZone: "Nowhere/Nowhere",
			Length: 1, Unit: agentrun.PeriodMonth}, agentrun.ErrStartDateUnknown},
		{"no start date", agentrun.Period{StartZone: "UTC", Length: 1, Unit: agentrun.PeriodMonth},
			agentrun.ErrStartDateUnknown},
		{"no zone", agentrun.Period{StartDate: "2026-01-15", Length: 1, Unit: agentrun.PeriodMonth},
			agentrun.ErrStartDateUnknown},
	} {
		_, err := agentrun.SpendByCredentialIn(ctx, pool, "anthropic.owner", c.period, time.Now())
		if !errors.Is(err, c.want) {
			t.Errorf("SpendByCredentialIn over a period with %s = %v, want %v", c.what, err, c.want)
		}
	}
}
