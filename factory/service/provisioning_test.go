package service_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/service"
)

func ref(t *testing.T, name string) secretref.Ref {
	t.Helper()
	r, err := secretref.New(name)
	if err != nil {
		t.Fatalf("secretref.New(%q): %v", name, err)
	}
	return r
}

// TestSetProvisionedBothShapes: shape one names a branch credential and no
// master, shape two names both.
func TestSetProvisionedBothShapes(t *testing.T) {
	ctx, pool, w := newWriter(t)

	created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	tx := begin(ctx, t, pool)
	if err := service.SetProvisioned(ctx, tx, created.ID, service.ShapeTwo,
		ref(t, "checkout-branch"), ref(t, "checkout-master")); err != nil {
		t.Fatalf("SetProvisioned shape two: %v", err)
	}
	commit(ctx, t, tx)

	read, err := service.Get(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !read.Provisioned.Written() || read.Provisioned.Shape != service.ShapeTwo ||
		read.Provisioned.BranchCredential.Name() != "checkout-branch" || read.Provisioned.MasterCredential.Name() != "checkout-master" {
		t.Errorf("Provisioned = %+v, want shape two with both credentials", read.Provisioned)
	}

	second, err := w.Create(ctx, decomposition, "billing", "/srv/repos/billing", aProject)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	tx = begin(ctx, t, pool)
	if err := service.SetProvisioned(ctx, tx, second.ID, service.ShapeOne, ref(t, "billing-branch"), secretref.Ref{}); err != nil {
		t.Fatalf("SetProvisioned shape one: %v", err)
	}
	commit(ctx, t, tx)

	read, err = service.Get(ctx, pool, second.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Provisioned.Shape != service.ShapeOne || read.Provisioned.MasterCredential.Name() != "" {
		t.Errorf("Provisioned = %+v, want shape one with no master credential", read.Provisioned)
	}
}

// TestSetProvisionedRefusals: an unknown shape, and credentials that do not
// match the shape given.
func TestSetProvisionedRefusals(t *testing.T) {
	ctx, pool, w := newWriter(t)

	created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	tx := begin(ctx, t, pool)
	defer func() { _ = tx.Rollback(ctx) }()
	if err := service.SetProvisioned(ctx, tx, created.ID, "three", ref(t, "b"), secretref.Ref{}); !errors.Is(err, service.ErrShapeUnknown) {
		t.Errorf("SetProvisioned with an unknown shape = %v, want ErrShapeUnknown", err)
	}
	if err := service.SetProvisioned(ctx, tx, created.ID, service.ShapeOne, secretref.Ref{}, secretref.Ref{}); !errors.Is(err, service.ErrCredentialsDoNotMatchShape) {
		t.Errorf("SetProvisioned shape one with no branch credential = %v, want ErrCredentialsDoNotMatchShape", err)
	}
	if err := service.SetProvisioned(ctx, tx, created.ID, service.ShapeOne, ref(t, "b"), ref(t, "m")); !errors.Is(err, service.ErrCredentialsDoNotMatchShape) {
		t.Errorf("SetProvisioned shape one with a master credential = %v, want ErrCredentialsDoNotMatchShape", err)
	}
	if err := service.SetProvisioned(ctx, tx, created.ID, service.ShapeTwo, ref(t, "b"), secretref.Ref{}); !errors.Is(err, service.ErrCredentialsDoNotMatchShape) {
		t.Errorf("SetProvisioned shape two with no master credential = %v, want ErrCredentialsDoNotMatchShape", err)
	}
	if err := service.SetProvisioned(ctx, tx, "svc_missing", service.ShapeOne, ref(t, "b"), secretref.Ref{}); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("SetProvisioned on a missing service = %v, want ErrNotFound", err)
	}
}

// TestSetTargetsRefusesATargetTheEnvironmentDoesNotHold: this package cannot
// read an environment record, so the check is made over the caller's own
// list.
func TestSetTargetsRefusesATargetTheEnvironmentDoesNotHold(t *testing.T) {
	ctx, pool, w := newWriter(t)

	created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	environmentTargets := []string{"10.0.0.1:8080", "10.0.0.2:8080"}

	tx := begin(ctx, t, pool)
	if err := service.SetTargets(ctx, tx, created.ID, []string{"10.0.0.1:8080"}, environmentTargets); err != nil {
		t.Fatalf("SetTargets: %v", err)
	}
	commit(ctx, t, tx)

	read, err := service.Get(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(read.Targets) != 1 || read.Targets[0] != "10.0.0.1:8080" {
		t.Errorf("Targets = %v, want [10.0.0.1:8080]", read.Targets)
	}

	tx = begin(ctx, t, pool)
	defer func() { _ = tx.Rollback(ctx) }()
	if err := service.SetTargets(ctx, tx, created.ID, []string{"10.0.0.9:8080"}, environmentTargets); !errors.Is(err, service.ErrTargetNotInEnvironment) {
		t.Errorf("SetTargets with a target the environment does not hold = %v, want ErrTargetNotInEnvironment", err)
	}
	if err := service.SetTargets(ctx, tx, created.ID, []string{"10.0.0.1:8080\nextra"}, environmentTargets); !errors.Is(err, service.ErrTargetNotInEnvironment) {
		t.Errorf("SetTargets with a line ending embedded = %v, want ErrTargetNotInEnvironment", err)
	}

	// An empty list is a real value: the service runs on every target of the
	// environment.
	tx2 := begin(ctx, t, pool)
	if err := service.SetTargets(ctx, tx2, created.ID, nil, environmentTargets); err != nil {
		t.Fatalf("SetTargets(nil): %v", err)
	}
	commit(ctx, t, tx2)
	read, err = service.Get(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(read.Targets) != 0 {
		t.Errorf("Targets after SetTargets(nil) = %v, want none", read.Targets)
	}
}

// TestRetireRefusesWhileSomethingStillNamesTheService: the three counts are
// read by the caller, one package this one may not import each, and passed
// in; Retire only refuses over the numbers.
func TestRetireRefusesWhileSomethingStillNamesTheService(t *testing.T) {
	ctx, pool, w := newWriter(t)

	created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	for _, still := range []struct {
		name                      string
		contracts, items, depends int
	}{
		{"a consumer contract in force", 1, 0, 0},
		{"an unmerged item", 0, 1, 0},
		{"an unmerged item's dependency", 0, 0, 1},
	} {
		tx := begin(ctx, t, pool)
		err := service.Retire(ctx, tx, created.ID, still.contracts, still.items, still.depends)
		_ = tx.Rollback(ctx)
		if !errors.Is(err, service.ErrRetiredNotEmpty) {
			t.Errorf("Retire while %s = %v, want ErrRetiredNotEmpty", still.name, err)
		}
	}

	tx := begin(ctx, t, pool)
	if err := service.Retire(ctx, tx, created.ID, -1, 0, 0); err == nil {
		t.Error("Retire with a negative count was accepted")
	}
	_ = tx.Rollback(ctx)

	tx = begin(ctx, t, pool)
	if err := service.Retire(ctx, tx, created.ID, 0, 0, 0); err != nil {
		t.Fatalf("Retire with nothing left naming it: %v", err)
	}
	commit(ctx, t, tx)

	read, err := service.Get(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !read.Retired() {
		t.Error("the service is not Retired after Retire committed")
	}
}
