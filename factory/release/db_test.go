// The database tests of this package are in release_test and open the pool
// through package postgres, the way decisionlog's do; deps.txt records the
// test edge. They apply this package's DDL themselves rather than calling
// postgres.Apply, which does not know this package until integration wires it
// in.
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package release_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
)

// newTable gives a test a schema of its own, this package's DDL applied
// inside it, and a writer over it. The schema is dropped when the test ends,
// so a rerun on a database a previous run left dirty starts clean.
func newTable(t *testing.T) (context.Context, *pgxpool.Pool, *release.Writer) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m1_" + hex.EncodeToString(suffix[:])

	pool, err := postgres.Open(ctx, inSchema(t, postgres.URL(), schema))
	if err != nil {
		t.Fatalf("the database at %s is not reachable, and these tests do not skip: %v", postgres.URL(), err)
	}
	t.Cleanup(func() {
		// t.Context is already cancelled by the time cleanup runs.
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
	for n, statement := range release.DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("applying statement %d: %v", n+1, err)
		}
	}
	return ctx, pool, release.NewWriter(pool)
}

// inSchema points a connection URL at one schema and nothing else, so every
// unqualified name in the DDL and in the writer's statements resolves there.
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

var merge = record.Actor{Kind: record.KindComponent, Name: "gate.merge_to_master"}

func TestTheNumberIsAnOrdinalPerService(t *testing.T) {
	ctx, pool, w := newTable(t)
	one := record.NewID("svc")
	two := record.NewID("svc")

	first, err := w.Mint(ctx, merge, one, record.NewID("bl"), record.NewID("it"))
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	second, err := w.Mint(ctx, merge, one, record.NewID("bl"), record.NewID("it"))
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	other, err := w.Mint(ctx, merge, two, record.NewID("bl"), record.NewID("it"))
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	if first.Number != 1 || second.Number != 2 {
		t.Errorf("one service minted numbers %d and %d, want 1 and 2", first.Number, second.Number)
	}
	if other.Number != 1 {
		t.Errorf("another service's first number is %d, want 1 — the ordinal is per service", other.Number)
	}

	read, err := release.Get(ctx, pool, first.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read != first {
		t.Errorf("Get = %+v, want the record Mint returned, %+v", read, first)
	}
	if _, err := release.Get(ctx, pool, "rel_00000000000000000000000000000000"); !errors.Is(err, release.ErrNotFound) {
		t.Errorf("Get of nothing = %v, want %v", err, release.ErrNotFound)
	}
}

// TestConcurrentMintsTakeDistinctNumbers is the lock doing its work: every
// goroutine mints against one service at once, and the numbers read back
// consecutive from 1 — no gap, no duplicate.
func TestConcurrentMintsTakeDistinctNumbers(t *testing.T) {
	ctx, _, w := newTable(t)
	serviceID := record.NewID("svc")

	const mints = 8
	numbers := make([]int64, mints)
	errs := make([]error, mints)
	var wg sync.WaitGroup
	for n := range mints {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := w.Mint(ctx, merge, serviceID, record.NewID("bl"), record.NewID("it"))
			numbers[n], errs[n] = r.Number, err
		}()
	}
	wg.Wait()

	for n, err := range errs {
		if err != nil {
			t.Fatalf("mint %d: %v", n+1, err)
		}
	}
	sort.Slice(numbers, func(i, j int) bool { return numbers[i] < numbers[j] })
	for n, number := range numbers {
		if number != int64(n)+1 {
			t.Fatalf("the numbers minted are %v, want 1 through %d with no gap and no duplicate", numbers, mints)
		}
	}
}

// TestTheStoreRefusesWhatASkippedLockWouldProduce inserts around Mint at a
// number the service already has, and the unique constraint refuses it.
func TestTheStoreRefusesWhatASkippedLockWouldProduce(t *testing.T) {
	ctx, pool, w := newTable(t)
	serviceID := record.NewID("svc")

	minted, err := w.Mint(ctx, merge, serviceID, record.NewID("bl"), record.NewID("it"))
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	_, err = pool.Exec(ctx, `insert into release (id, actor_kind, actor_name, at, service_id, number, build_id, item_id)
		values ($1, 'component', 'gate.merge_to_master', $2, $3, $4, $5, $6)`,
		record.NewID(release.IDPrefix), record.Now(), serviceID, minted.Number, record.NewID("bl"), record.NewID("it"))
	if err == nil {
		t.Error("the store seated a second release at a taken number")
	}

	_, err = pool.Exec(ctx, `insert into release (id, actor_kind, actor_name, at, service_id, number, build_id, item_id)
		values ($1, 'component', 'gate.merge_to_master', $2, $3, 0, $4, $5)`,
		record.NewID(release.IDPrefix), record.Now(), serviceID, record.NewID("bl"), record.NewID("it"))
	if err == nil {
		t.Error("the store accepted number 0, and the ordinal starts at 1")
	}
}

// TestAnEmptyLinkIsRefusedTwice covers this package's three link columns at
// one of them. An empty link names nothing, so it is refused by the writer and
// by the store, the way every other required field is; record's doc.go states
// what a link is checked for.
func TestAnEmptyLinkIsRefusedTwice(t *testing.T) {
	ctx, pool, w := newTable(t)

	if _, err := w.Mint(ctx, merge, "", record.NewID("bl"), record.NewID("it")); !errors.Is(err, release.ErrServiceIDEmpty) {
		t.Errorf("Mint naming no service = %v, want %v", err, release.ErrServiceIDEmpty)
	}
	if _, err := w.Mint(ctx, merge, record.NewID("svc"), "", record.NewID("it")); !errors.Is(err, release.ErrBuildIDEmpty) {
		t.Errorf("Mint naming no build = %v, want %v", err, release.ErrBuildIDEmpty)
	}
	if _, err := w.Mint(ctx, merge, record.NewID("svc"), record.NewID("bl"), ""); !errors.Is(err, release.ErrItemIDEmpty) {
		t.Errorf("Mint naming no item = %v, want %v", err, release.ErrItemIDEmpty)
	}

	_, err := pool.Exec(ctx, `insert into release (id, actor_kind, actor_name, at, service_id, number, build_id, item_id)
		values ($1, 'component', 'gate.merge_to_master', $2, '', 1, $3, $4)`,
		record.NewID(release.IDPrefix), record.Now(), record.NewID("bl"), record.NewID("it"))
	if err == nil || !strings.Contains(err.Error(), "service_id_present") {
		t.Errorf("inserting a release naming no service = %v, want a violation of service_id_present", err)
	}
}

// TestHighestIsMastersHead: master's head is the commit of the service's
// highest-numbered release, so the store answers which release that is and is
// empty until the first one. Numbers are never reused, so the maximum is always
// right.
func TestHighestIsMastersHead(t *testing.T) {
	ctx, pool, w := newTable(t)
	const serviceID, other = "svc_a", "svc_b"

	if _, found, err := release.Highest(ctx, pool, serviceID); err != nil || found {
		t.Fatalf("Highest before the first release = found %v, %v", found, err)
	}

	var last release.Release
	for n := 1; n <= 3; n++ {
		minted, err := w.Mint(ctx, merge, serviceID, fmt.Sprintf("bl_%d", n), fmt.Sprintf("it_%d", n))
		if err != nil {
			t.Fatalf("Mint %d: %v", n, err)
		}
		last = minted
	}
	if _, err := w.Mint(ctx, merge, other, "bl_other", "it_other"); err != nil {
		t.Fatalf("Mint on another service: %v", err)
	}

	highest, found, err := release.Highest(ctx, pool, serviceID)
	if err != nil || !found {
		t.Fatalf("Highest = found %v, %v", found, err)
	}
	if highest.ID != last.ID || highest.Number != 3 {
		t.Errorf("Highest is %s number %d, want %s number 3", highest.ID, highest.Number, last.ID)
	}
	if highest.BuildID != last.BuildID {
		t.Errorf("Highest names build %s, want %s — which is the commit master is at", highest.BuildID, last.BuildID)
	}
}
