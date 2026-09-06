package service_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/service"
)

// TestAChangeFreezeIsPeriodsAndAReadOfOne: a freeze is authored ahead of what it
// is for, one period at a time, and what reads it asks whether the service is
// frozen at a moment. Nothing is decided by the read — the hold it feeds lifts
// itself when the period passes.
func TestAChangeFreezeIsPeriodsAndAReadOfOne(t *testing.T) {
	ctx, pool, w := newWriter(t)

	created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	token := acquire(ctx, t, pool)

	// A service with nothing authored is frozen at no moment at all.
	frozen, _, err := service.Frozen(ctx, pool, created.ID, "2026-09-06T12:00:00.000000000Z")
	if err != nil {
		t.Fatalf("Frozen: %v", err)
	}
	if frozen {
		t.Errorf("a service with no period authored reads as frozen")
	}

	tx := begin(ctx, t, pool)
	err = service.AddFreezePeriod(ctx, tx, token, owner, created.ID,
		"2026-12-24T00:00:00.000000000Z", "2026-12-27T00:00:00.000000000Z")
	if err != nil {
		t.Fatalf("AddFreezePeriod: %v", err)
	}
	// A second period beside the first: the field names periods, plural, and one
	// authored is never edited into another.
	err = service.AddFreezePeriod(ctx, tx, token, owner, created.ID,
		"2027-01-01T00:00:00.000000000Z", "2027-01-02T00:00:00.000000000Z")
	if err != nil {
		t.Fatalf("AddFreezePeriod, the second: %v", err)
	}
	commit(ctx, t, tx)

	periods, err := service.FreezePeriods(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("FreezePeriods: %v", err)
	}
	if len(periods) != 2 {
		t.Fatalf("the service holds %d periods, want the two authored: %+v", len(periods), periods)
	}

	for _, at := range []struct {
		moment string
		want   bool
	}{
		{"2026-12-23T23:59:59.000000000Z", false},
		{"2026-12-24T00:00:00.000000000Z", true},
		{"2026-12-26T09:00:00.000000000Z", true},
		{"2026-12-27T00:00:00.000000000Z", true},
		{"2026-12-27T00:00:01.000000000Z", false},
		{"2027-01-01T12:00:00.000000000Z", true},
	} {
		frozen, period, err := service.Frozen(ctx, pool, created.ID, at.moment)
		if err != nil {
			t.Fatalf("Frozen at %s: %v", at.moment, err)
		}
		if frozen != at.want {
			t.Errorf("frozen at %s = %v (%+v), want %v", at.moment, frozen, period, at.want)
		}
	}

	// A period is two moments in the layout every record's timestamp takes, and
	// it ends after it starts.
	tx = begin(ctx, t, pool)
	defer func() { _ = tx.Rollback(ctx) }()
	err = service.AddFreezePeriod(ctx, tx, token, owner, created.ID, "next tuesday", "2027-01-02T00:00:00.000000000Z")
	if !errors.Is(err, service.ErrPeriodNotATime) {
		t.Errorf("AddFreezePeriod with a moment nothing can read = %v, want ErrPeriodNotATime", err)
	}
	err = service.AddFreezePeriod(ctx, tx, token, owner, created.ID,
		"2027-01-02T00:00:00.000000000Z", "2027-01-01T00:00:00.000000000Z")
	if !errors.Is(err, service.ErrPeriodEndsBeforeItStarts) {
		t.Errorf("AddFreezePeriod ending before it starts = %v, want ErrPeriodEndsBeforeItStarts", err)
	}
}
