package service_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/service"
)

// TestObjectiveAndItsPeriodAreOneWrite: an objective without its period
// states nothing, so the two are authored and refused together.
func TestObjectiveAndItsPeriodAreOneWrite(t *testing.T) {
	ctx, pool, w := newWriter(t)

	created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Objective.Authored() {
		t.Errorf("a freshly created service carries an objective: %+v", created.Objective)
	}

	tx := begin(ctx, t, pool)
	if err := service.SetObjective(ctx, tx, created.ID, 0.999, 604800); err != nil {
		t.Fatalf("SetObjective: %v", err)
	}
	commit(ctx, t, tx)

	read, err := service.Get(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !read.Objective.Authored() || read.Objective.Target.Number != 0.999 || read.Objective.PeriodSeconds.Number != 604800 {
		t.Errorf("Objective = %+v, want 0.999 over 604800 seconds", read.Objective)
	}

	tx = begin(ctx, t, pool)
	defer func() { _ = tx.Rollback(ctx) }()
	if err := service.SetObjective(ctx, tx, created.ID, 0, 604800); !errors.Is(err, service.ErrShareOutOfRange) {
		t.Errorf("SetObjective(0, ...) = %v, want ErrShareOutOfRange", err)
	}
	if err := service.SetObjective(ctx, tx, created.ID, 0.999, 0); !errors.Is(err, service.ErrNotPositive) {
		t.Errorf("SetObjective(..., 0) = %v, want ErrNotPositive", err)
	}
}

// TestPagingHoursCarryTheZoneTheyWereWrittenIn: the notifier reads them
// against the zone recorded with them.
func TestPagingHoursCarryTheZoneTheyWereWrittenIn(t *testing.T) {
	ctx, pool, w := newWriter(t)

	created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.PagingHours.Authored() {
		t.Errorf("a freshly created service carries paging hours: %+v", created.PagingHours)
	}

	hours := service.PagingHours{Start: "09:00", End: "17:00", Zone: "America/New_York"}
	tx := begin(ctx, t, pool)
	if err := service.SetPagingHours(ctx, tx, created.ID, hours); err != nil {
		t.Fatalf("SetPagingHours: %v", err)
	}
	commit(ctx, t, tx)

	read, err := service.Get(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !read.PagingHours.Authored() || read.PagingHours != hours {
		t.Errorf("PagingHours = %+v, want %+v", read.PagingHours, hours)
	}

	tx = begin(ctx, t, pool)
	defer func() { _ = tx.Rollback(ctx) }()
	for _, bad := range []service.PagingHours{
		{Start: "9:00", End: "17:00", Zone: "UTC"},
		{Start: "09:00", End: "25:00", Zone: "UTC"},
		{Start: "09:00", End: "17:00", Zone: ""},
	} {
		err := service.SetPagingHours(ctx, tx, created.ID, bad)
		wantHourErr := bad.Zone != ""
		if wantHourErr && !errors.Is(err, service.ErrHourFormat) {
			t.Errorf("SetPagingHours(%+v) = %v, want ErrHourFormat", bad, err)
		}
		if !wantHourErr && !errors.Is(err, service.ErrZoneEmpty) {
			t.Errorf("SetPagingHours(%+v) = %v, want ErrZoneEmpty", bad, err)
		}
	}
}

// TestProductLicenceAndSnapshotRetention: two owner-authored fields with no
// gate-policy row of their own.
func TestProductLicenceAndSnapshotRetention(t *testing.T) {
	ctx, pool, w := newWriter(t)

	created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	tx := begin(ctx, t, pool)
	if err := service.SetProductLicence(ctx, tx, created.ID, "Apache-2.0"); err != nil {
		t.Fatalf("SetProductLicence: %v", err)
	}
	if err := service.SetSnapshotRetention(ctx, tx, created.ID, 2_592_000); err != nil {
		t.Fatalf("SetSnapshotRetention: %v", err)
	}
	commit(ctx, t, tx)

	read, err := service.Get(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.ProductLicence != "Apache-2.0" || read.SnapshotRetentionSeconds.Number != 2_592_000 {
		t.Errorf("ProductLicence=%q SnapshotRetentionSeconds=%+v, want Apache-2.0 and 2592000",
			read.ProductLicence, read.SnapshotRetentionSeconds)
	}

	tx = begin(ctx, t, pool)
	defer func() { _ = tx.Rollback(ctx) }()
	if err := service.SetProductLicence(ctx, tx, created.ID, ""); !errors.Is(err, service.ErrLicenceEmpty) {
		t.Errorf("SetProductLicence(\"\") = %v, want ErrLicenceEmpty", err)
	}
	if err := service.SetSnapshotRetention(ctx, tx, created.ID, 0); !errors.Is(err, service.ErrNotPositive) {
		t.Errorf("SetSnapshotRetention(0) = %v, want ErrNotPositive", err)
	}
}
