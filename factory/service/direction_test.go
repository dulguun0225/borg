// direction_test.go is the safeguard direction stated per field on the
// parameter setters: a component actor may move the value only the stated
// way against the one in force, and a human actor may set it either way. It
// shares db_test.go's newWriter and helpers_test.go's acquire, begin and
// commit.
package service_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/service"
)

var componentActor = record.Actor{Kind: record.KindComponent, Key: "safeguard.9", Basis: record.BasisClaimed}

// TestSafeguardDirectionPerField is a table test over every setter the design
// states a direction for: a component actor may move the value the stated
// way, is refused the other way, and a human actor may move it either way.
func TestSafeguardDirectionPerField(t *testing.T) {
	t.Run("failure-record key cap, lower only", func(t *testing.T) {
		ctx, pool, w := newWriter(t)
		created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}

		tx := begin(ctx, t, pool)
		if err := service.SetFailureRecordKeyCap(ctx, tx, owner, created.ID, 100); err != nil {
			t.Fatalf("SetFailureRecordKeyCap (owner, first): %v", err)
		}
		commit(ctx, t, tx)

		tx = begin(ctx, t, pool)
		if err := service.SetFailureRecordKeyCap(ctx, tx, componentActor, created.ID, 200); !errors.Is(err, service.ErrSafeguardDirection) {
			t.Errorf("a safeguard raising the failure-record key cap = %v, want ErrSafeguardDirection", err)
		}
		if err := service.SetFailureRecordKeyCap(ctx, tx, componentActor, created.ID, 50); err != nil {
			t.Errorf("a safeguard lowering the failure-record key cap = %v, want no error", err)
		}
		commit(ctx, t, tx)

		tx = begin(ctx, t, pool)
		if err := service.SetFailureRecordKeyCap(ctx, tx, owner, created.ID, 500); err != nil {
			t.Errorf("an owner raising the failure-record key cap = %v, want no error", err)
		}
		commit(ctx, t, tx)
	})

	t.Run("unreliable bound, raise only", func(t *testing.T) {
		ctx, pool, w := newWriter(t)
		created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}

		tx := begin(ctx, t, pool)
		if err := service.SetUnreliableBound(ctx, tx, owner, created.ID, 0.2); err != nil {
			t.Fatalf("SetUnreliableBound (owner, first): %v", err)
		}
		commit(ctx, t, tx)

		tx = begin(ctx, t, pool)
		if err := service.SetUnreliableBound(ctx, tx, componentActor, created.ID, 0.1); !errors.Is(err, service.ErrSafeguardDirection) {
			t.Errorf("a safeguard lowering the unreliable bound = %v, want ErrSafeguardDirection", err)
		}
		if err := service.SetUnreliableBound(ctx, tx, componentActor, created.ID, 0.3); err != nil {
			t.Errorf("a safeguard raising the unreliable bound = %v, want no error", err)
		}
		commit(ctx, t, tx)

		tx = begin(ctx, t, pool)
		if err := service.SetUnreliableBound(ctx, tx, owner, created.ID, 0.05); err != nil {
			t.Errorf("an owner lowering the unreliable bound = %v, want no error", err)
		}
		commit(ctx, t, tx)
	})

	t.Run("recent-history run length, shorten only", func(t *testing.T) {
		ctx, pool, w := newWriter(t)
		created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}

		tx := begin(ctx, t, pool)
		if err := service.SetRecentHistoryRunLength(ctx, tx, owner, created.ID, 20000); err != nil {
			t.Fatalf("SetRecentHistoryRunLength (owner, first): %v", err)
		}
		commit(ctx, t, tx)

		tx = begin(ctx, t, pool)
		if err := service.SetRecentHistoryRunLength(ctx, tx, componentActor, created.ID, 30000); !errors.Is(err, service.ErrSafeguardDirection) {
			t.Errorf("a safeguard lengthening the run length = %v, want ErrSafeguardDirection", err)
		}
		if err := service.SetRecentHistoryRunLength(ctx, tx, componentActor, created.ID, 10000); err != nil {
			t.Errorf("a safeguard shortening the run length = %v, want no error", err)
		}
		commit(ctx, t, tx)

		tx = begin(ctx, t, pool)
		if err := service.SetRecentHistoryRunLength(ctx, tx, owner, created.ID, 50000); err != nil {
			t.Errorf("an owner lengthening the run length = %v, want no error", err)
		}
		commit(ctx, t, tx)
	})

	t.Run("backlog cap, lower only", func(t *testing.T) {
		ctx, pool, w := newWriter(t)
		created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}

		tx := begin(ctx, t, pool)
		if err := service.SetBacklogCap(ctx, tx, owner, created.ID, 4); err != nil {
			t.Fatalf("SetBacklogCap (owner, first): %v", err)
		}
		commit(ctx, t, tx)

		tx = begin(ctx, t, pool)
		if err := service.SetBacklogCap(ctx, tx, componentActor, created.ID, 8); !errors.Is(err, service.ErrSafeguardDirection) {
			t.Errorf("a safeguard raising the backlog cap = %v, want ErrSafeguardDirection", err)
		}
		if err := service.SetBacklogCap(ctx, tx, componentActor, created.ID, 2); err != nil {
			t.Errorf("a safeguard lowering the backlog cap = %v, want no error", err)
		}
		commit(ctx, t, tx)

		tx = begin(ctx, t, pool)
		if err := service.SetBacklogCap(ctx, tx, owner, created.ID, 10); err != nil {
			t.Errorf("an owner raising the backlog cap = %v, want no error", err)
		}
		commit(ctx, t, tx)
	})

	t.Run("search budget, lower only, both fields", func(t *testing.T) {
		ctx, pool, w := newWriter(t)
		created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}

		tx := begin(ctx, t, pool)
		if err := service.SetSearchBudget(ctx, tx, owner, created.ID, 10, 3600); err != nil {
			t.Fatalf("SetSearchBudget (owner, first): %v", err)
		}
		commit(ctx, t, tx)

		tx = begin(ctx, t, pool)
		if err := service.SetSearchBudget(ctx, tx, componentActor, created.ID, 20, 1800); !errors.Is(err, service.ErrSafeguardDirection) {
			t.Errorf("a safeguard raising the search budget's builds = %v, want ErrSafeguardDirection", err)
		}
		if err := service.SetSearchBudget(ctx, tx, componentActor, created.ID, 5, 7200); !errors.Is(err, service.ErrSafeguardDirection) {
			t.Errorf("a safeguard raising the search budget's seconds = %v, want ErrSafeguardDirection", err)
		}
		if err := service.SetSearchBudget(ctx, tx, componentActor, created.ID, 5, 1800); err != nil {
			t.Errorf("a safeguard lowering both fields of the search budget = %v, want no error", err)
		}
		commit(ctx, t, tx)

		tx = begin(ctx, t, pool)
		if err := service.SetSearchBudget(ctx, tx, owner, created.ID, 50, 9000); err != nil {
			t.Errorf("an owner raising the search budget = %v, want no error", err)
		}
		commit(ctx, t, tx)
	})

	t.Run("bake volume, raise only", func(t *testing.T) {
		ctx, pool, w := newWriter(t)
		created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}

		tx := begin(ctx, t, pool)
		if err := service.SetBakeVolume(ctx, tx, owner, created.ID, 5000); err != nil {
			t.Fatalf("SetBakeVolume (owner, first): %v", err)
		}
		commit(ctx, t, tx)

		tx = begin(ctx, t, pool)
		if err := service.SetBakeVolume(ctx, tx, componentActor, created.ID, 1000); !errors.Is(err, service.ErrSafeguardDirection) {
			t.Errorf("a safeguard lowering the bake volume = %v, want ErrSafeguardDirection", err)
		}
		if err := service.SetBakeVolume(ctx, tx, componentActor, created.ID, 9000); err != nil {
			t.Errorf("a safeguard raising the bake volume = %v, want no error", err)
		}
		commit(ctx, t, tx)

		tx = begin(ctx, t, pool)
		if err := service.SetBakeVolume(ctx, tx, owner, created.ID, 100); err != nil {
			t.Errorf("an owner lowering the bake volume = %v, want no error", err)
		}
		commit(ctx, t, tx)
	})

	t.Run("mutation floor, raise only", func(t *testing.T) {
		ctx, pool, w := newWriter(t)
		created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}

		tx := begin(ctx, t, pool)
		if err := service.SetMutationFloor(ctx, tx, owner, created.ID, 0.6); err != nil {
			t.Fatalf("SetMutationFloor (owner, first): %v", err)
		}
		commit(ctx, t, tx)

		tx = begin(ctx, t, pool)
		if err := service.SetMutationFloor(ctx, tx, componentActor, created.ID, 0.4); !errors.Is(err, service.ErrSafeguardDirection) {
			t.Errorf("a safeguard lowering the mutation floor = %v, want ErrSafeguardDirection", err)
		}
		if err := service.SetMutationFloor(ctx, tx, componentActor, created.ID, 0.8); err != nil {
			t.Errorf("a safeguard raising the mutation floor = %v, want no error", err)
		}
		commit(ctx, t, tx)

		tx = begin(ctx, t, pool)
		if err := service.SetMutationFloor(ctx, tx, owner, created.ID, 0.1); err != nil {
			t.Errorf("an owner lowering the mutation floor = %v, want no error", err)
		}
		commit(ctx, t, tx)
	})

	// A field with a shipped default is enforced against that default on a
	// component actor's first write, there being a value in force even though
	// nothing is authored yet: the window limit for the backlog cap
	// ([ShippedWindowLimit] where nothing is authored either), and
	// [ShippedFailureRecordKeyCap] and [ShippedUnreliableBound] for their
	// fields. A human actor's first write is unrestricted regardless, the
	// direction never being checked for a human.
	t.Run("first component write against the shipped value in force", func(t *testing.T) {
		ctx, pool, w := newWriter(t)
		created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if got := created.BacklogCapInForce(); got != service.ShippedWindowLimit {
			t.Fatalf("BacklogCapInForce with nothing authored = %v, want %v", got, service.ShippedWindowLimit)
		}

		tx := begin(ctx, t, pool)
		if err := service.SetBacklogCap(ctx, tx, componentActor, created.ID, service.ShippedWindowLimit+1); !errors.Is(err, service.ErrSafeguardDirection) {
			t.Errorf("a safeguard's first write raising the backlog cap above the window limit in force = %v, want ErrSafeguardDirection", err)
		}
		if err := service.SetBacklogCap(ctx, tx, componentActor, created.ID, service.ShippedWindowLimit); err != nil {
			t.Errorf("a safeguard's first write at the window limit in force = %v, want no error", err)
		}
		commit(ctx, t, tx)

		tx = begin(ctx, t, pool)
		if err := service.SetBacklogCap(ctx, tx, owner, created.ID, 100); err != nil {
			t.Errorf("an owner's first write on the backlog cap = %v, want no error", err)
		}
		commit(ctx, t, tx)
	})

	t.Run("first component write on the failure-record key cap against the shipped default", func(t *testing.T) {
		ctx, pool, w := newWriter(t)
		created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}

		tx := begin(ctx, t, pool)
		if err := service.SetFailureRecordKeyCap(ctx, tx, componentActor, created.ID, service.ShippedFailureRecordKeyCap+10); !errors.Is(err, service.ErrSafeguardDirection) {
			t.Errorf("a safeguard's first write raising the key cap above the shipped default = %v, want ErrSafeguardDirection", err)
		}
		commit(ctx, t, tx)

		tx = begin(ctx, t, pool)
		if err := service.SetFailureRecordKeyCap(ctx, tx, componentActor, created.ID, service.ShippedFailureRecordKeyCap-10); err != nil {
			t.Errorf("a safeguard's first write lowering the key cap below the shipped default = %v, want no error", err)
		}
		commit(ctx, t, tx)

		created2, err := w.Create(ctx, decomposition, "cart", "/srv/repos/cart", aProject)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		tx = begin(ctx, t, pool)
		if err := service.SetFailureRecordKeyCap(ctx, tx, owner, created2.ID, service.ShippedFailureRecordKeyCap+10); err != nil {
			t.Errorf("an owner's first write on the key cap = %v, want no error", err)
		}
		commit(ctx, t, tx)
	})

	t.Run("first component write on the unreliable bound against the shipped default", func(t *testing.T) {
		ctx, pool, w := newWriter(t)
		created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}

		tx := begin(ctx, t, pool)
		if err := service.SetUnreliableBound(ctx, tx, componentActor, created.ID, service.ShippedUnreliableBound-0.1); !errors.Is(err, service.ErrSafeguardDirection) {
			t.Errorf("a safeguard's first write lowering the unreliable bound below the shipped default = %v, want ErrSafeguardDirection", err)
		}
		commit(ctx, t, tx)

		tx = begin(ctx, t, pool)
		if err := service.SetUnreliableBound(ctx, tx, componentActor, created.ID, service.ShippedUnreliableBound+0.1); err != nil {
			t.Errorf("a safeguard's first write raising the unreliable bound above the shipped default = %v, want no error", err)
		}
		commit(ctx, t, tx)

		created2, err := w.Create(ctx, decomposition, "cart", "/srv/repos/cart", aProject)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		tx = begin(ctx, t, pool)
		if err := service.SetUnreliableBound(ctx, tx, owner, created2.ID, service.ShippedUnreliableBound-0.1); err != nil {
			t.Errorf("an owner's first write on the unreliable bound = %v, want no error", err)
		}
		commit(ctx, t, tx)
	})
}
