// The database tests of this package are in factorypolicy_test rather than in
// factorypolicy, because they open the pool through package postgres, which
// imports this one to apply its DDL. deps.txt records the edge as
// "test factorypolicy -> postgres".
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package factorypolicy_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/factorypolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

var owner = record.Actor{Kind: record.KindHuman, Name: "owner"}

func newTable(t *testing.T) (context.Context, *pgxpool.Pool, *factorypolicy.Writer) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m2_fp_" + hex.EncodeToString(suffix[:])

	pool, err := postgres.Open(ctx, inSchema(t, postgres.URL(), schema))
	if err != nil {
		t.Fatalf("the database at %s is not reachable, and these tests do not skip: %v", postgres.URL(), err)
	}
	t.Cleanup(func() {
		drop, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := pool.Exec(drop, `drop schema if exists `+pgx.Identifier{schema}.Sanitize()+` cascade`); err != nil {
			t.Errorf("dropping schema %s: %v", schema, err)
		}
		pool.Close()
	})
	if _, err := pool.Exec(ctx, `create schema `+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("creating schema %s: %v", schema, err)
	}
	if err := postgres.Apply(ctx, pool); err != nil {
		t.Fatalf("applying the schema: %v", err)
	}
	return ctx, pool, factorypolicy.NewWriter(pool)
}

func inSchema(t *testing.T, base, schema string) string {
	t.Helper()
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parsing %s: %v", base, err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// TestThereIsOneRecordAndEnsureIsIdempotent: the record exists before any
// project does, so whatever reaches it first creates it — and the store, not the
// caller that looked first, is what keeps there being one.
func TestThereIsOneRecordAndEnsureIsIdempotent(t *testing.T) {
	ctx, pool, w := newTable(t)

	if _, err := factorypolicy.Get(ctx, pool); !errors.Is(err, factorypolicy.ErrNotFound) {
		t.Fatalf("Get before the record exists = %v, want ErrNotFound", err)
	}

	first, err := w.Ensure(ctx, owner)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	second, err := w.Ensure(ctx, owner)
	if err != nil {
		t.Fatalf("Ensure again: %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("two ensures left two records: %s and %s", first.ID, second.ID)
	}
	if first.PredicateCatalog != nil || first.BriefOrSkillThreshold.Present {
		t.Errorf("a freshly created record carries something authored: %+v", first)
	}

	if _, err := pool.Exec(ctx, `insert into `+factorypolicy.Table+`
		(id, actor_kind, actor_name, at, only_row, predicate_catalog, brief_or_skill_threshold)
		values ('fp_second', 'human', 'owner', $1, true, '', null)`, record.Now()); err == nil {
		t.Error("the store accepted a second factory policy record")
	}
}

// TestTheAttemptBoundIsPerStage: the bound is per stage and the stages are the
// factory's own, so a bound on a stage an item cannot be at is refused — the
// store has no foreign key to refuse it, and a value nothing will ever read is
// worse than an error.
func TestTheAttemptBoundIsPerStage(t *testing.T) {
	ctx, pool, w := newTable(t)

	policy, err := w.Ensure(ctx, owner)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	authored, err := factorypolicy.AttemptBound(ctx, pool, policy.ID, item.StageImplementation)
	if err != nil {
		t.Fatalf("AttemptBound: %v", err)
	}
	if authored.Present {
		t.Errorf("an unauthored bound reads back as %+v", authored)
	}

	inTx(t, ctx, pool, func(tx pgx.Tx) error {
		return factorypolicy.SetAttemptBound(ctx, tx, owner, policy.ID, item.StageImplementation, 5)
	})
	inTx(t, ctx, pool, func(tx pgx.Tx) error {
		return factorypolicy.SetAttemptBound(ctx, tx, owner, policy.ID, item.StageSpec, 2)
	})

	implementation, err := factorypolicy.AttemptBound(ctx, pool, policy.ID, item.StageImplementation)
	if err != nil {
		t.Fatalf("AttemptBound: %v", err)
	}
	spec, err := factorypolicy.AttemptBound(ctx, pool, policy.ID, item.StageSpec)
	if err != nil {
		t.Fatalf("AttemptBound: %v", err)
	}
	if implementation.Number != 5 || spec.Number != 2 {
		t.Errorf("the bounds read back as %v at implementation and %v at spec, want 5 and 2",
			implementation.Number, spec.Number)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := factorypolicy.SetAttemptBound(ctx, tx, owner, policy.ID, item.Stage("review"), 3); !errors.Is(err, factorypolicy.ErrStageUnknown) {
		t.Errorf("a bound on a stage that does not exist = %v, want ErrStageUnknown", err)
	}
	if err := factorypolicy.SetAttemptBound(ctx, tx, owner, policy.ID, item.StageSpec, 0); !errors.Is(err, factorypolicy.ErrBoundNotPositive) {
		t.Errorf("a bound of zero = %v, want ErrBoundNotPositive", err)
	}
}

// TestReAuthoringABoundIsOneRow: the unique constraint on the record and the
// stage is what an authoring write conflicts on.
func TestReAuthoringABoundIsOneRow(t *testing.T) {
	ctx, pool, w := newTable(t)

	policy, err := w.Ensure(ctx, owner)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	for _, bound := range []int{5, 3} {
		inTx(t, ctx, pool, func(tx pgx.Tx) error {
			return factorypolicy.SetAttemptBound(ctx, tx, owner, policy.ID, item.StageImplementation, bound)
		})
	}

	var rows int
	if err := pool.QueryRow(ctx, `select count(*) from `+factorypolicy.BoundTable).Scan(&rows); err != nil {
		t.Fatalf("counting the bound rows: %v", err)
	}
	if rows != 1 {
		t.Errorf("two authorings of one stage left %d rows, want 1", rows)
	}
	authored, err := factorypolicy.AttemptBound(ctx, pool, policy.ID, item.StageImplementation)
	if err != nil {
		t.Fatalf("AttemptBound: %v", err)
	}
	if authored.Number != 3 {
		t.Errorf("the bound reads back as %v, want the second authoring's 3", authored.Number)
	}
}

// TestTheCatalogAndTheBriefThreshold: both are fields of this record and neither
// is read by anything at this milestone, which is what makes storing them the
// whole of what can be demonstrated about them.
func TestTheCatalogAndTheBriefThreshold(t *testing.T) {
	ctx, pool, w := newTable(t)

	policy, err := w.Ensure(ctx, owner)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	catalog := []string{"status", "field-present", "schema"}
	inTx(t, ctx, pool, func(tx pgx.Tx) error {
		return factorypolicy.SetPredicateCatalog(ctx, tx, policy.ID, catalog)
	})
	inTx(t, ctx, pool, func(tx pgx.Tx) error {
		return factorypolicy.SetBriefOrSkillThreshold(ctx, tx, policy.ID, 0.15)
	})

	read, err := factorypolicy.Get(ctx, pool)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !slices.Equal(read.PredicateCatalog, catalog) {
		t.Errorf("the catalog reads back as %v, want %v", read.PredicateCatalog, catalog)
	}
	if !read.BriefOrSkillThreshold.Present || read.BriefOrSkillThreshold.Number != 0.15 {
		t.Errorf("the brief-or-skill threshold reads back as %+v, want 0.15 present", read.BriefOrSkillThreshold)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := factorypolicy.SetPredicateCatalog(ctx, tx, policy.ID, nil); !errors.Is(err, factorypolicy.ErrCatalogEmpty) {
		t.Errorf("an empty authored catalog = %v, want ErrCatalogEmpty", err)
	}
	if err := factorypolicy.SetBriefOrSkillThreshold(ctx, tx, policy.ID, 2); !errors.Is(err, factorypolicy.ErrThresholdOutOfRange) {
		t.Errorf("a threshold of 2 = %v, want ErrThresholdOutOfRange", err)
	}
	if err := factorypolicy.SetBriefOrSkillThreshold(ctx, tx, "fp_nothing", 0.2); !errors.Is(err, factorypolicy.ErrNotFound) {
		t.Errorf("authoring on a record that does not exist = %v, want ErrNotFound", err)
	}
}

// inTx runs one authoring write in its own transaction, which is how its one
// real caller runs it — inside the transaction that appends the policy version.
func inTx(t *testing.T, ctx context.Context, pool *pgxpool.Pool, write func(pgx.Tx) error) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := write(tx); err != nil {
		t.Fatalf("the authoring write: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}
