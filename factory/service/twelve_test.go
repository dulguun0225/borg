package service_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/service"
)

// TestTheFiveRemainingOfTheTwelve: the design names twelve fields on this record
// beside the window limit and the analysis window's parameters. The bake volume,
// the backlog cap, the search budget, the objective, the paging hours, the
// explicit threshold and the change freeze have tests of their own; these are
// the five left — the mutation floor, the fraction of its instances a release
// keeps, the maximum concurrent kept fleets, the average run length of the
// reading against the service's own recent history, and the proof test rate —
// each absent until an owner authors one.
func TestTheFiveRemainingOfTheTwelve(t *testing.T) {
	ctx, pool, w := newWriter(t)

	created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	read, err := service.Get(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	for _, absent := range []struct {
		what  string
		value gatepolicy.Authored
	}{
		{"the mutation floor", read.MutationFloor},
		{"the kept fraction", read.KeptFraction},
		{"the maximum concurrent kept fleets", read.MaxConcurrentKeptFleets},
		{"the average run length", read.RecentHistoryRunLength},
		{"the proof test rate", read.ProofTestRate},
	} {
		if absent.value.Present {
			t.Errorf("%s of a service nobody authored one on = %+v, want absent", absent.what, absent.value)
		}
	}

	tx := begin(ctx, t, pool)
	if err := service.SetMutationFloor(ctx, tx, created.ID, 0.8); err != nil {
		t.Fatalf("SetMutationFloor: %v", err)
	}
	if err := service.SetKeptFraction(ctx, tx, created.ID, 0.5); err != nil {
		t.Fatalf("SetKeptFraction: %v", err)
	}
	if err := service.SetMaxConcurrentKeptFleets(ctx, tx, created.ID, 3); err != nil {
		t.Fatalf("SetMaxConcurrentKeptFleets: %v", err)
	}
	if err := service.SetRecentHistoryRunLength(ctx, tx, created.ID, 20000); err != nil {
		t.Fatalf("SetRecentHistoryRunLength: %v", err)
	}
	if err := service.SetProofTestRate(ctx, tx, created.ID, 0.25); err != nil {
		t.Fatalf("SetProofTestRate: %v", err)
	}
	commit(ctx, t, tx)

	read, err = service.Get(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.MutationFloor.Number != 0.8 || read.KeptFraction.Number != 0.5 ||
		read.MaxConcurrentKeptFleets.Number != 3 || read.RecentHistoryRunLength.Number != 20000 ||
		read.ProofTestRate.Number != 0.25 {
		t.Errorf("the five read back as %+v %+v %+v %+v %+v",
			read.MutationFloor, read.KeptFraction, read.MaxConcurrentKeptFleets,
			read.RecentHistoryRunLength, read.ProofTestRate)
	}

	tx = begin(ctx, t, pool)
	defer func() { _ = tx.Rollback(ctx) }()
	if err := service.SetMutationFloor(ctx, tx, created.ID, 1.5); !errors.Is(err, service.ErrShareOutOfRange) {
		t.Errorf("SetMutationFloor(1.5) = %v, want ErrShareOutOfRange", err)
	}
	// The fraction is a share of a release's own instances: nothing kept is not a
	// fraction of them, it is the strategy without a control.
	if err := service.SetKeptFraction(ctx, tx, created.ID, 0); !errors.Is(err, service.ErrShareOutOfRange) {
		t.Errorf("SetKeptFraction(0) = %v, want ErrShareOutOfRange", err)
	}
	if err := service.SetMaxConcurrentKeptFleets(ctx, tx, created.ID, 0); !errors.Is(err, service.ErrNotPositive) {
		t.Errorf("SetMaxConcurrentKeptFleets(0) = %v, want ErrNotPositive", err)
	}
	// A proof test rate of nothing is a real value: it is a test that never runs,
	// which is what an owner who authors none already gets.
	if err := service.SetProofTestRate(ctx, tx, created.ID, 0); err != nil {
		t.Errorf("SetProofTestRate(0) = %v, want a rate of nothing admitted", err)
	}
	if err := service.SetProofTestRate(ctx, tx, created.ID, -1); !errors.Is(err, service.ErrRateNegative) {
		t.Errorf("SetProofTestRate(-1) = %v, want ErrRateNegative", err)
	}
}

// TestTheRecentHistorySizeIsPerQuantity: the size of the reading against a
// service's own recent history takes the division the window's own size takes,
// so a value authored for one quantity is no value for another.
func TestTheRecentHistorySizeIsPerQuantity(t *testing.T) {
	ctx, pool, w := newWriter(t)

	created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	token := acquire(ctx, t, pool)

	tx := begin(ctx, t, pool)
	err = service.SetRecentHistorySize(ctx, tx, token, owner, created.ID, gatepolicy.QuantityErrorRate, 0.02)
	if err != nil {
		t.Fatalf("SetRecentHistorySize: %v", err)
	}
	commit(ctx, t, tx)

	read, err := service.Get(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := read.RecentHistorySize[gatepolicy.QuantityErrorRate]; !got.Present || got.Number != 0.02 {
		t.Errorf("the error rate's size = %+v, want 0.02 present", got)
	}
	if got := read.RecentHistorySize[gatepolicy.QuantityLatency]; got.Present {
		t.Errorf("the latency's size = %+v, want absent: a size authored on one quantity is none on another", got)
	}

	tx = begin(ctx, t, pool)
	defer func() { _ = tx.Rollback(ctx) }()
	err = service.SetRecentHistorySize(ctx, tx, token, owner, created.ID, "", 0.02)
	if !errors.Is(err, service.ErrQuantityEmpty) {
		t.Errorf("SetRecentHistorySize naming no quantity = %v, want ErrQuantityEmpty", err)
	}
	err = service.SetRecentHistorySize(ctx, tx, token, owner, created.ID, gatepolicy.QuantityErrorRate, 0)
	if !errors.Is(err, service.ErrShareOutOfRange) {
		t.Errorf("SetRecentHistorySize(0) = %v, want ErrShareOutOfRange", err)
	}
}
