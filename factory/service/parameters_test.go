package service_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/service"
)

// TestWindowSizeAndPowerArePerQuantity: the size and the power are one value
// per quantity, so authoring one leaves the other quantities untouched, and
// re-authoring one updates the row rather than inserting a second.
func TestWindowSizeAndPowerArePerQuantity(t *testing.T) {
	ctx, pool, w := newWriter(t)

	created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	token := acquire(ctx, t, pool)

	tx := begin(ctx, t, pool)
	if err := service.SetWindowSize(ctx, tx, token, owner, created.ID, gatepolicy.QuantityErrorRate, 0.02); err != nil {
		t.Fatalf("SetWindowSize: %v", err)
	}
	if err := service.SetWindowPower(ctx, tx, token, owner, created.ID, gatepolicy.QuantityErrorRate, 0.8); err != nil {
		t.Fatalf("SetWindowPower: %v", err)
	}
	commit(ctx, t, tx)

	read, err := service.Get(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v := read.Parameters.WindowSizeFor(gatepolicy.QuantityErrorRate); !v.Present || v.Number != 0.02 {
		t.Errorf("WindowSizeFor(error_rate) = %+v, want 0.02 present", v)
	}
	if v := read.Parameters.WindowSizeFor(gatepolicy.QuantityLatency); v.Present {
		t.Errorf("WindowSizeFor(latency) = %+v, want absent", v)
	}
	if v := read.Parameters.WindowPowerFor(gatepolicy.QuantityErrorRate); !v.Present || v.Number != 0.8 {
		t.Errorf("WindowPowerFor(error_rate) = %+v, want 0.8 present", v)
	}

	// Re-authoring updates the row rather than adding a second.
	tx = begin(ctx, t, pool)
	if err := service.SetWindowSize(ctx, tx, token, owner, created.ID, gatepolicy.QuantityErrorRate, 0.05); err != nil {
		t.Fatalf("SetWindowSize again: %v", err)
	}
	commit(ctx, t, tx)
	read, err = service.Get(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v := read.Parameters.WindowSizeFor(gatepolicy.QuantityErrorRate); !v.Present || v.Number != 0.05 {
		t.Errorf("WindowSizeFor(error_rate) after re-authoring = %+v, want 0.05 present", v)
	}
}

// TestWindowSizeAndPowerRefusals: the shares out of range, the empty
// quantity, and the fence over a stale token.
func TestWindowSizeAndPowerRefusals(t *testing.T) {
	ctx, pool, w := newWriter(t)

	created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	token := acquire(ctx, t, pool)

	tx := begin(ctx, t, pool)
	defer func() { _ = tx.Rollback(ctx) }()
	if err := service.SetWindowSize(ctx, tx, token, owner, created.ID, gatepolicy.QuantityErrorRate, 0); !errors.Is(err, service.ErrShareOutOfRange) {
		t.Errorf("SetWindowSize(0) = %v, want ErrShareOutOfRange", err)
	}
	if err := service.SetWindowSize(ctx, tx, token, owner, created.ID, gatepolicy.QuantityErrorRate, 1.5); !errors.Is(err, service.ErrShareOutOfRange) {
		t.Errorf("SetWindowSize(1.5) = %v, want ErrShareOutOfRange", err)
	}
	if err := service.SetWindowSize(ctx, tx, token, owner, created.ID, "", 0.02); !errors.Is(err, service.ErrQuantityEmpty) {
		t.Errorf("SetWindowSize with no quantity = %v, want ErrQuantityEmpty", err)
	}
	if err := service.SetWindowPower(ctx, tx, token, owner, created.ID, gatepolicy.QuantityErrorRate, 0); !errors.Is(err, service.ErrShareOutOfRange) {
		t.Errorf("SetWindowPower(0) = %v, want ErrShareOutOfRange", err)
	}
	if err := service.SetWindowPower(ctx, tx, token, owner, created.ID, gatepolicy.QuantityErrorRate, 1); !errors.Is(err, service.ErrShareOutOfRange) {
		t.Errorf("SetWindowPower(1) = %v, want ErrShareOutOfRange (a power of one is what no finite volume reaches)", err)
	}
	if err := service.SetWindowSize(ctx, tx, token, owner, "svc_missing", gatepolicy.QuantityErrorRate, 0.02); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("SetWindowSize on a missing service = %v, want ErrNotFound", err)
	}

	if err := service.SetWindowSize(ctx, tx, lease.Token(0), owner, created.ID, gatepolicy.QuantityErrorRate, 0.02); !errors.Is(err, lease.ErrFenced) {
		t.Errorf("SetWindowSize with a stale token = %v, want lease.ErrFenced", err)
	}
}

// TestServiceWideGatePolicyFields: confidence, cap, limit and exposure bound
// are one column each, written by the same set-then-read pattern.
func TestServiceWideGatePolicyFields(t *testing.T) {
	ctx, pool, w := newWriter(t)

	created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	tx := begin(ctx, t, pool)
	if err := service.SetWindowConfidence(ctx, tx, created.ID, 0.95); err != nil {
		t.Fatalf("SetWindowConfidence: %v", err)
	}
	if err := service.SetWindowCap(ctx, tx, created.ID, 3600); err != nil {
		t.Fatalf("SetWindowCap: %v", err)
	}
	if err := service.SetWindowLimit(ctx, tx, created.ID, 5); err != nil {
		t.Fatalf("SetWindowLimit: %v", err)
	}
	if err := service.SetExposureBound(ctx, tx, created.ID, 0.3); err != nil {
		t.Fatalf("SetExposureBound: %v", err)
	}
	commit(ctx, t, tx)

	read, err := service.Get(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	want := service.Parameters{
		WindowSize:       map[gatepolicy.Quantity]gatepolicy.Authored{},
		WindowPower:      map[gatepolicy.Quantity]gatepolicy.Authored{},
		WindowConfidence: gatepolicy.Authored{Number: 0.95, Present: true},
		WindowCapSeconds: gatepolicy.Authored{Number: 3600, Present: true},
		WindowLimit:      gatepolicy.Authored{Number: 5, Present: true},
		ExposureBound:    gatepolicy.Authored{Number: 0.3, Present: true},
	}
	if got := read.Parameters; got.WindowConfidence != want.WindowConfidence || got.WindowCapSeconds != want.WindowCapSeconds ||
		got.WindowLimit != want.WindowLimit || got.ExposureBound != want.ExposureBound {
		t.Errorf("Parameters = %+v, want %+v", got, want)
	}

	tx = begin(ctx, t, pool)
	defer func() { _ = tx.Rollback(ctx) }()
	if err := service.SetWindowConfidence(ctx, tx, created.ID, 0); !errors.Is(err, service.ErrShareOutOfRange) {
		t.Errorf("SetWindowConfidence(0) = %v, want ErrShareOutOfRange", err)
	}
	if err := service.SetWindowCap(ctx, tx, created.ID, 0); !errors.Is(err, service.ErrNotPositive) {
		t.Errorf("SetWindowCap(0) = %v, want ErrNotPositive", err)
	}
	if err := service.SetWindowLimit(ctx, tx, created.ID, -1); !errors.Is(err, service.ErrNotPositive) {
		t.Errorf("SetWindowLimit(-1) = %v, want ErrNotPositive", err)
	}
	if err := service.SetExposureBound(ctx, tx, created.ID, 0); !errors.Is(err, service.ErrNotPositive) {
		t.Errorf("SetExposureBound(0) = %v, want ErrNotPositive", err)
	}
	if err := service.SetWindowConfidence(ctx, tx, "svc_missing", 0.95); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("SetWindowConfidence on a missing service = %v, want ErrNotFound", err)
	}
}

// TestFieldsAuthoredLikeGatePolicyButNotItsRows: the mutant cap, the
// failure-record key cap, the unreliable bound and the incident-raised item
// bound, none of which is one of gate policy's eleven.
func TestFieldsAuthoredLikeGatePolicyButNotItsRows(t *testing.T) {
	ctx, pool, w := newWriter(t)

	created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	tx := begin(ctx, t, pool)
	if err := service.SetMutantCap(ctx, tx, created.ID, 40); err != nil {
		t.Fatalf("SetMutantCap: %v", err)
	}
	if err := service.SetFailureRecordKeyCap(ctx, tx, created.ID, 100); err != nil {
		t.Fatalf("SetFailureRecordKeyCap: %v", err)
	}
	if err := service.SetUnreliableBound(ctx, tx, created.ID, 0.1); err != nil {
		t.Fatalf("SetUnreliableBound: %v", err)
	}
	if err := service.SetIncidentItemBound(ctx, tx, created.ID, 7200); err != nil {
		t.Fatalf("SetIncidentItemBound: %v", err)
	}
	commit(ctx, t, tx)

	read, err := service.Get(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.MutantCap.Number != 40 || read.FailureRecordKeyCap.Number != 100 ||
		read.UnreliableBound.Number != 0.1 || read.IncidentItemBoundSeconds.Number != 7200 {
		t.Errorf("the four fields read back as %+v %+v %+v %+v, want 40 100 0.1 7200",
			read.MutantCap, read.FailureRecordKeyCap, read.UnreliableBound, read.IncidentItemBoundSeconds)
	}

	tx = begin(ctx, t, pool)
	defer func() { _ = tx.Rollback(ctx) }()
	if err := service.SetMutantCap(ctx, tx, created.ID, 0); !errors.Is(err, service.ErrNotPositive) {
		t.Errorf("SetMutantCap(0) = %v, want ErrNotPositive", err)
	}
	if err := service.SetFailureRecordKeyCap(ctx, tx, created.ID, 0); !errors.Is(err, service.ErrNotPositive) {
		t.Errorf("SetFailureRecordKeyCap(0) = %v, want ErrNotPositive", err)
	}
	// The unreliable bound is a rate that may be zero: no disagreement takes a
	// criterion out of the gate.
	if err := service.SetUnreliableBound(ctx, tx, created.ID, 0); err != nil {
		t.Errorf("SetUnreliableBound(0) = %v, want no error", err)
	}
	if err := service.SetUnreliableBound(ctx, tx, created.ID, 1.5); !errors.Is(err, service.ErrShareOutOfRange) {
		t.Errorf("SetUnreliableBound(1.5) = %v, want ErrShareOutOfRange", err)
	}
	if err := service.SetIncidentItemBound(ctx, tx, created.ID, 0); !errors.Is(err, service.ErrNotPositive) {
		t.Errorf("SetIncidentItemBound(0) = %v, want ErrNotPositive", err)
	}
}
