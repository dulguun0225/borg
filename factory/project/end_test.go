package project_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/project"
)

// TestAProjectIsEndedOnceEveryServiceInItIsRetired: a project ends at Factory,
// and the count of services in it that are not retired is the caller's argument
// because this package may not read a service record. The row stays: an area, a
// constraint, a safeguard or a scope naming the project still points at it.
func TestAProjectIsEndedOnceEveryServiceInItIsRetired(t *testing.T) {
	ctx, pool, w, token := newWriter(t)

	created, err := w.Create(ctx, owner, "payments")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Ended() {
		t.Errorf("a project reads as ended at its creation: %+v", created)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := project.End(ctx, tx, token, owner, created.ID, 2); !errors.Is(err, project.ErrServicesStandInIt) {
		t.Errorf("End with two services not retired = %v, want ErrServicesStandInIt", err)
	}
	if err := project.End(ctx, tx, token, owner, created.ID, 0); err != nil {
		t.Fatalf("End: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	read, err := project.Get(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !read.Ended() {
		t.Fatalf("the project reads as standing after it was ended: %+v", read)
	}

	// Ending one already ended is refused: what ends a project is one event, and
	// the production environment that ends with it is withdrawn once.
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := project.End(ctx, tx, token, owner, created.ID, 0); !errors.Is(err, project.ErrAlreadyEnded) {
		t.Errorf("End of a project already ended = %v, want ErrAlreadyEnded", err)
	}
	if err := project.End(ctx, tx, token, owner, "prj_missing", 0); !errors.Is(err, project.ErrNotFound) {
		t.Errorf("End of a project nobody wrote = %v, want ErrNotFound", err)
	}
}
