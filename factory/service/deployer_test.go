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
	found := service.Reachability{TargetReached: true, InstancesReplaceable: true, RollbackPathPresent: false, EmissionReadable: true}
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
