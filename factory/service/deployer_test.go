package service_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/service"
)

// TestAdoptWritesTheDeployersFour: all four are false and At is empty until
// the deployer writes them, which tells a service nothing has adopted yet
// from one the deployer found wanting.
func TestAdoptWritesTheDeployersFour(t *testing.T) {
	ctx, pool, w := newWriter(t)

	created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Reachability.Written() {
		t.Errorf("a freshly created service carries reachability: %+v", created.Reachability)
	}

	token := acquire(ctx, t, pool)
	tx := begin(ctx, t, pool)
	found := service.Reachability{TargetReached: true, InstancesReplaceable: true, RollbackPathPresent: true, EmissionReadable: true}
	if err := service.Adopt(ctx, tx, token, owner, created.ID, found); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	commit(ctx, t, tx)

	read, err := service.Get(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !read.Reachability.Written() || read.Reachability.TargetReached != found.TargetReached ||
		read.Reachability.InstancesReplaceable != found.InstancesReplaceable ||
		read.Reachability.RollbackPathPresent != found.RollbackPathPresent ||
		read.Reachability.EmissionReadable != found.EmissionReadable {
		t.Errorf("Reachability = %+v, want %+v written", read.Reachability, found)
	}

	staleTx := begin(ctx, t, pool)
	if err := service.Adopt(ctx, staleTx, lease.Token(0), owner, created.ID, found); !errors.Is(err, lease.ErrFenced) {
		t.Errorf("Adopt with a stale token = %v, want lease.ErrFenced", err)
	}
	_ = staleTx.Rollback(ctx)

	noActorTx := begin(ctx, t, pool)
	if err := service.Adopt(ctx, noActorTx, token, record.Actor{}, created.ID, found); !errors.Is(err, record.ErrKindUnknown) {
		t.Errorf("Adopt with no actor = %v, want record.ErrKindUnknown", err)
	}
	_ = noActorTx.Rollback(ctx)
}

// TestAdoptAdmitsOnlyOneShape: adoption admits one shape — instances on a
// deploy target, already taking organic traffic, replaceable one at a time
// and returnable by shifting traffic — and refuses the write, leaving nothing
// written, unless all four hold.
func TestAdoptAdmitsOnlyOneShape(t *testing.T) {
	ctx, pool, w := newWriter(t)

	created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	token := acquire(ctx, t, pool)

	whole := service.Reachability{TargetReached: true, InstancesReplaceable: true, RollbackPathPresent: true, EmissionReadable: true}
	for _, missing := range []struct {
		name  string
		found service.Reachability
	}{
		{"no target reached", service.Reachability{InstancesReplaceable: true, RollbackPathPresent: true, EmissionReadable: true}},
		{"instances not replaceable", service.Reachability{TargetReached: true, RollbackPathPresent: true, EmissionReadable: true}},
		{"no rollback path", service.Reachability{TargetReached: true, InstancesReplaceable: true, EmissionReadable: true}},
		{"emission not readable", service.Reachability{TargetReached: true, InstancesReplaceable: true, RollbackPathPresent: true}},
	} {
		tx := begin(ctx, t, pool)
		if err := service.Adopt(ctx, tx, token, owner, created.ID, missing.found); !errors.Is(err, service.ErrShapeNotAdmitted) {
			t.Errorf("Adopt with %s = %v, want ErrShapeNotAdmitted", missing.name, err)
		}
		commit(ctx, t, tx)

		read, err := service.Get(ctx, pool, created.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if read.Reachability.Written() {
			t.Errorf("a refused adoption with %s wrote the four: %+v", missing.name, read.Reachability)
		}
	}

	tx := begin(ctx, t, pool)
	if err := service.Adopt(ctx, tx, token, owner, created.ID, whole); err != nil {
		t.Fatalf("Adopt with all four true: %v", err)
	}
	commit(ctx, t, tx)

	read, err := service.Get(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !read.Reachability.Written() {
		t.Error("Adopt with all four true wrote nothing")
	}
}
