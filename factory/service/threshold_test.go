// threshold_test.go is the values the health monitor's readings are authored
// against on this record: the explicit threshold a safeguard sets per quantity,
// the operation cap and the overflow operation the excess lands in, the
// environment-hour rate, and the search budget. It shares db_test.go's
// newWriter and helpers_test.go's acquire, begin and commit.
package service_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/service"
)

// TestTheExplicitThresholdIsPerQuantityAndCarriesItsSize is the reading an
// owner's safeguard adds: an absolute number per quantity, with the size it is
// read at beside it, applying in addition to the comparison.
func TestTheExplicitThresholdIsPerQuantityAndCarriesItsSize(t *testing.T) {
	ctx, pool, w := newWriter(t)
	created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	token := acquire(ctx, t, pool)

	tx := begin(ctx, t, pool)
	if err := service.SetExplicitThreshold(ctx, tx, token, owner, created.ID,
		gatepolicy.QuantityErrorRate, 0.05, 0.01); err != nil {
		t.Fatalf("SetExplicitThreshold: %v", err)
	}
	commit(ctx, t, tx)

	read, err := service.Get(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	set := read.ExplicitThreshold[gatepolicy.QuantityErrorRate]
	if set.Number != 0.05 || set.Size != 0.01 {
		t.Errorf("the explicit threshold on the error rate = %+v, want 0.05 at a size of 0.01", set)
	}
	if _, on := read.ExplicitThreshold[gatepolicy.QuantityLatency]; on {
		t.Error("a threshold set on one quantity was read on another")
	}

	// Re-authoring one updates the row rather than inserting a second.
	tx = begin(ctx, t, pool)
	if err := service.SetExplicitThreshold(ctx, tx, token, owner, created.ID,
		gatepolicy.QuantityErrorRate, 0.02, 0.005); err != nil {
		t.Fatalf("SetExplicitThreshold again: %v", err)
	}
	commit(ctx, t, tx)
	if read, err = service.Get(ctx, pool, created.ID); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(read.ExplicitThreshold) != 1 || read.ExplicitThreshold[gatepolicy.QuantityErrorRate].Number != 0.02 {
		t.Errorf("after re-authoring, the thresholds = %+v, want the one row updated", read.ExplicitThreshold)
	}

	tx = begin(ctx, t, pool)
	if err := service.SetExplicitThreshold(ctx, tx, token, owner, created.ID,
		gatepolicy.QuantityErrorRate, 2, 0.01); !errors.Is(err, service.ErrShareOutOfRange) {
		t.Errorf("a threshold above one = %v, want ErrShareOutOfRange", err)
	}
	_ = tx.Rollback(ctx)
}

// TestTheOperationCapNamesItsOverflowOperation is the cap the store's own key
// set takes: a cap with nowhere for the excess to land would truncate the count
// and hide where it was truncated.
func TestTheOperationCapNamesItsOverflowOperation(t *testing.T) {
	ctx, pool, w := newWriter(t)
	created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	tx := begin(ctx, t, pool)
	if err := service.SetOperationCap(ctx, tx, created.ID, 50, ""); !errors.Is(err, service.ErrOverflowOperationEmpty) {
		t.Errorf("a cap naming no overflow operation = %v, want ErrOverflowOperationEmpty", err)
	}
	if err := service.SetOperationCap(ctx, tx, created.ID, 50, "other"); err != nil {
		t.Fatalf("SetOperationCap: %v", err)
	}
	commit(ctx, t, tx)

	read, err := service.Get(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !read.OperationCap.Present || read.OperationCap.Number != 50 || read.OverflowOperation != "other" {
		t.Errorf("the operation cap = %+v into %q, want 50 into other", read.OperationCap, read.OverflowOperation)
	}
}

// TestTheHostingRatesAndTheSearchBudget is the two rates that price hosting
// outside the factory and what a search may spend before it stops.
func TestTheHostingRatesAndTheSearchBudget(t *testing.T) {
	ctx, pool, w := newWriter(t)
	created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	tx := begin(ctx, t, pool)
	if err := service.SetEnvironmentHourRate(ctx, tx, created.ID, 0.4); err != nil {
		t.Fatalf("SetEnvironmentHourRate: %v", err)
	}
	if err := service.SetSearchBudget(ctx, tx, created.ID, 5, 3600); err != nil {
		t.Fatalf("SetSearchBudget: %v", err)
	}
	commit(ctx, t, tx)

	read, err := service.Get(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !read.EnvironmentHourRate.Present || read.EnvironmentHourRate.Number != 0.4 {
		t.Errorf("the environment-hour rate = %+v, want 0.4 present", read.EnvironmentHourRate)
	}
	if read.SearchBudgetBuilds.Number != 5 || read.SearchBudgetSeconds.Number != 3600 {
		t.Errorf("the search budget = %+v builds over %+v seconds, want 5 over 3600",
			read.SearchBudgetBuilds, read.SearchBudgetSeconds)
	}

	tx = begin(ctx, t, pool)
	if err := service.SetEnvironmentHourRate(ctx, tx, created.ID, -1); !errors.Is(err, service.ErrRateNegative) {
		t.Errorf("SetEnvironmentHourRate(-1) = %v, want ErrRateNegative", err)
	}
	if err := service.SetSearchBudget(ctx, tx, created.ID, 0, 3600); !errors.Is(err, service.ErrNotPositive) {
		t.Errorf("a search budget of no builds = %v, want ErrNotPositive", err)
	}
	_ = tx.Rollback(ctx)
}
