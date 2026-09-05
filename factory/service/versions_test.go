package service_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/service"
)

// TestAuthorSeedAndValueSetAreVersionedSeparately: each authoring is a
// version beside the earlier ones, never editing one, so the version a run
// was composed from stays readable.
func TestAuthorSeedAndValueSetAreVersionedSeparately(t *testing.T) {
	ctx, pool, w := newWriter(t)

	created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	token := acquire(ctx, t, pool)

	if _, err := service.SeedInForce(ctx, pool, created.ID); !errors.Is(err, service.ErrVersionNotFound) {
		t.Errorf("SeedInForce before any authoring = %v, want ErrVersionNotFound", err)
	}
	if _, err := service.ValueSetInForce(ctx, pool, created.ID); !errors.Is(err, service.ErrVersionNotFound) {
		t.Errorf("ValueSetInForce before any authoring = %v, want ErrVersionNotFound", err)
	}

	tx := begin(ctx, t, pool)
	first, err := service.AuthorSeed(ctx, tx, token, owner, created.ID, "insert into users values (1)")
	if err != nil {
		t.Fatalf("AuthorSeed: %v", err)
	}
	commit(ctx, t, tx)

	sum := sha256.Sum256([]byte("insert into users values (1)"))
	if first.Digest != hex.EncodeToString(sum[:]) {
		t.Errorf("the seed's digest is %q, want the sha256 of its content", first.Digest)
	}

	tx = begin(ctx, t, pool)
	second, err := service.AuthorSeed(ctx, tx, token, owner, created.ID, "insert into users values (1), (2)")
	if err != nil {
		t.Fatalf("AuthorSeed again: %v", err)
	}
	commit(ctx, t, tx)

	inForce, err := service.SeedInForce(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("SeedInForce: %v", err)
	}
	if inForce.ID != second.ID {
		t.Errorf("SeedInForce = %+v, want the newer authoring %+v", inForce, second)
	}

	versions, err := service.SeedVersions(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("SeedVersions: %v", err)
	}
	if len(versions) != 2 || versions[0].ID != second.ID || versions[1].ID != first.ID {
		t.Errorf("SeedVersions = %+v, want the newer then the older", versions)
	}

	// The value set is a version list of its own, untouched by the seed's
	// authorings.
	tx = begin(ctx, t, pool)
	if _, err := service.AuthorValueSet(ctx, tx, token, owner, created.ID, `{"feature_flag": true}`); err != nil {
		t.Fatalf("AuthorValueSet: %v", err)
	}
	commit(ctx, t, tx)

	valueSets, err := service.ValueSetVersions(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("ValueSetVersions: %v", err)
	}
	if len(valueSets) != 1 {
		t.Errorf("ValueSetVersions = %+v, want one authoring, untouched by the seed's two", valueSets)
	}
	seedVersionsAfter, err := service.SeedVersions(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("SeedVersions: %v", err)
	}
	if len(seedVersionsAfter) != 2 {
		t.Errorf("SeedVersions after authoring a value set = %+v, want still two", seedVersionsAfter)
	}
}

// TestAuthorVersionRefusals: a version cannot be authored on a service that
// does not exist, and the write is fenced.
func TestAuthorVersionRefusals(t *testing.T) {
	ctx, pool, w := newWriter(t)

	created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	token := acquire(ctx, t, pool)

	tx := begin(ctx, t, pool)
	if _, err := service.AuthorSeed(ctx, tx, token, owner, "svc_missing", "content"); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("AuthorSeed on a missing service = %v, want ErrNotFound", err)
	}
	_ = tx.Rollback(ctx)

	tx = begin(ctx, t, pool)
	if _, err := service.AuthorSeed(ctx, tx, lease.Token(0), owner, created.ID, "content"); !errors.Is(err, lease.ErrFenced) {
		t.Errorf("AuthorSeed with a stale token = %v, want lease.ErrFenced", err)
	}
	_ = tx.Rollback(ctx)
}
