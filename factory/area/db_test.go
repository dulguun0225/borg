// The database tests of this package are in area_test rather than in area,
// because they open the pool through package postgres, which imports this one to
// apply its DDL. deps.txt records the edge as "test area -> postgres".
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package area_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

var owner = record.Actor{Kind: record.KindHuman, Name: "owner"}

func newTable(t *testing.T) (context.Context, *pgxpool.Pool, *area.Writer) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m2_area_" + hex.EncodeToString(suffix[:])

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
	return ctx, pool, area.NewWriter(pool)
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

// TestAnAreaIsDeclaredWithNothingAuthored: an owner declares a grouping and the
// item-size target on it is absent, which is not a target of zero — the value in
// force is what the score supplies.
func TestAnAreaIsDeclaredWithNothingAuthored(t *testing.T) {
	ctx, pool, w := newTable(t)

	declared, err := w.Declare(ctx, owner, "payments", "")
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}
	if declared.Actor != owner {
		t.Errorf("the area's actor is %+v, want the owner", declared.Actor)
	}
	if declared.ItemSizeTarget.Present {
		t.Errorf("a freshly declared area carries an authored target: %+v", declared.ItemSizeTarget)
	}

	read, err := area.Get(ctx, pool, declared.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read != declared {
		t.Errorf("read back %+v, want %+v", read, declared)
	}

	byName, found, err := area.ByName(ctx, pool, "payments")
	if err != nil || !found {
		t.Fatalf("ByName = %+v, %v, %v", byName, found, err)
	}
	if byName.ID != declared.ID {
		t.Errorf("ByName found %s, want %s", byName.ID, declared.ID)
	}
	if _, found, err := area.ByName(ctx, pool, "marketing"); err != nil || found {
		t.Errorf("ByName of an area nobody declared = %v, %v", found, err)
	}
}

// TestOneNamePerArea: the store refuses the second declaration of one name, so
// two owners declaring at once leave one area rather than two.
func TestOneNamePerArea(t *testing.T) {
	ctx, _, w := newTable(t)

	if _, err := w.Declare(ctx, owner, "payments", ""); err != nil {
		t.Fatalf("Declare: %v", err)
	}
	if _, err := w.Declare(ctx, owner, "payments", ""); err == nil {
		t.Fatal("the second declaration of one name was accepted")
	}
	if _, err := w.Declare(ctx, owner, "", ""); !errors.Is(err, area.ErrNameEmpty) {
		t.Errorf("Declare with no name = %v, want ErrNameEmpty", err)
	}
	if _, err := w.Declare(ctx, record.Actor{}, "billing", ""); !errors.Is(err, record.ErrKindUnknown) {
		t.Errorf("Declare with no actor = %v, want ErrKindUnknown", err)
	}
}

// TestTheChainIsWalkedNarrowestFirst: a safeguard drawn on any area in the
// chain reaches an item in the narrowest, so the walk is what a mechanism
// reads.
func TestTheChainIsWalkedNarrowestFirst(t *testing.T) {
	ctx, pool, w := newTable(t)

	outer, err := w.Declare(ctx, owner, "payments", "")
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}
	inner, err := w.Declare(ctx, owner, "payments/refunds", outer.ID)
	if err != nil {
		t.Fatalf("Declare inside: %v", err)
	}

	chain, err := area.Chain(ctx, pool, inner.ID)
	if err != nil {
		t.Fatalf("Chain: %v", err)
	}
	if len(chain) != 2 || chain[0].ID != inner.ID || chain[1].ID != outer.ID {
		t.Fatalf("the chain is %v, want the narrowest then the one it lies inside", names(chain))
	}

	// An item may name no area, and the answer for one is no areas rather than
	// an error.
	empty, err := area.Chain(ctx, pool, "")
	if err != nil || len(empty) != 0 {
		t.Errorf("Chain(\"\") = %v, %v, want no areas and no error", names(empty), err)
	}
}

// TestAChainThatCyclesIsFound: nothing in the store refuses one, there being no
// foreign keys between records, so the walk is where it is found rather than
// where it loops.
func TestAChainThatCyclesIsFound(t *testing.T) {
	ctx, pool, w := newTable(t)

	first, err := w.Declare(ctx, owner, "one", "")
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}
	second, err := w.Declare(ctx, owner, "two", first.ID)
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}
	if _, err := pool.Exec(ctx, `update `+area.Table+` set inside = $1 where id = $2`, second.ID, first.ID); err != nil {
		t.Fatalf("closing the loop by raw SQL: %v", err)
	}

	if _, err := area.Chain(ctx, pool, first.ID); !errors.Is(err, area.ErrChainCycles) {
		t.Fatalf("Chain over a cycle = %v, want ErrChainCycles", err)
	}
}

// TestTheTargetIsAuthoredInsideATransaction: the write takes a transaction
// because its one caller appends the policy version in the same one, so the field
// and the version commit together or not at all.
func TestTheTargetIsAuthoredInsideATransaction(t *testing.T) {
	ctx, pool, w := newTable(t)

	declared, err := w.Declare(ctx, owner, "payments", "")
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}

	// A rolled-back transaction leaves the field as it was, which is what makes
	// the pair atomic for the caller that appends a version beside it.
	rolled, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := area.SetItemSizeTarget(ctx, rolled, declared.ID, 250); err != nil {
		t.Fatalf("SetItemSizeTarget: %v", err)
	}
	if err := rolled.Rollback(ctx); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	read, err := area.Get(ctx, pool, declared.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.ItemSizeTarget.Present {
		t.Errorf("a rolled-back authoring left the target at %+v", read.ItemSizeTarget)
	}

	committed, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := area.SetItemSizeTarget(ctx, committed, declared.ID, 250); err != nil {
		t.Fatalf("SetItemSizeTarget: %v", err)
	}
	if err := committed.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	read, err = area.Get(ctx, pool, declared.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !read.ItemSizeTarget.Present || read.ItemSizeTarget.Number != 250 {
		t.Errorf("the authored target reads back as %+v, want 250 present", read.ItemSizeTarget)
	}
}

// TestATargetThatNoItemCouldMeetIsRefusedTwice: by the writer, and by the store
// around it.
func TestATargetThatNoItemCouldMeetIsRefusedTwice(t *testing.T) {
	ctx, pool, w := newTable(t)

	declared, err := w.Declare(ctx, owner, "payments", "")
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := area.SetItemSizeTarget(ctx, tx, declared.ID, 0); !errors.Is(err, area.ErrTargetNotPositive) {
		t.Errorf("SetItemSizeTarget(0) = %v, want ErrTargetNotPositive", err)
	}
	if err := area.SetItemSizeTarget(ctx, tx, "ar_nothing", 250); !errors.Is(err, area.ErrNotFound) {
		t.Errorf("SetItemSizeTarget on an area that does not exist = %v, want ErrNotFound", err)
	}

	if _, err := pool.Exec(ctx, `update `+area.Table+` set item_size_target = -1 where id = $1`, declared.ID); err == nil {
		t.Error("the store accepted a negative item-size target written around the writer")
	}
}

func names(areas []area.Area) []string {
	read := make([]string, 0, len(areas))
	for _, a := range areas {
		read = append(read, a.Name)
	}
	return read
}
