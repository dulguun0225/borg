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

	"github.com/dulguun0225/borg/factory/lease"
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
	for n, statement := range lease.DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("applying lease statement %d: %v", n+1, err)
		}
	}
	for n, statement := range release.DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("applying statement %d: %v", n+1, err)
		}
	}
	token, err := lease.Acquire(ctx, pool, "test", time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	return ctx, pool, release.NewWriter(pool, token)
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

var merge = record.Actor{Kind: record.KindComponent, Key: "gate.merge_to_master"}

// minting is a release of one item, with a commit of its own. Every mint here
// names a distinct commit unless the test is about the commit.
func minting(serviceID string) release.Minting {
	return release.Minting{
		ServiceID: serviceID,
		BuildID:   record.NewID("bl"),
		Commit:    record.NewID("cm"),
		ItemID:    record.NewID("it"),
	}
}

func TestTheNumberIsAnOrdinalPerService(t *testing.T) {
	ctx, pool, w := newTable(t)
	one := record.NewID("svc")
	two := record.NewID("svc")

	first, err := w.Mint(ctx, merge, minting(one))
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	second, err := w.Mint(ctx, merge, minting(one))
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	other, err := w.Mint(ctx, merge, minting(two))
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
			r, err := w.Mint(ctx, merge, minting(serviceID))
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

	minted, err := w.Mint(ctx, merge, minting(serviceID))
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	_, err = pool.Exec(ctx, insertRelease,
		record.NewID(release.IDPrefix), release.FormatVersion, record.Now(), serviceID, minted.Number,
		record.NewID("bl"), record.NewID("cm"), record.NewID("it"))
	if err == nil {
		t.Error("the store seated a second release at a taken number")
	}

	_, err = pool.Exec(ctx, insertRelease,
		record.NewID(release.IDPrefix), release.FormatVersion, record.Now(), serviceID, int64(0),
		record.NewID("bl"), record.NewID("cm"), record.NewID("it"))
	if err == nil {
		t.Error("the store accepted number 0, and the ordinal starts at 1")
	}
}

// insertRelease is an insert around the writer, which is how the store's own
// refusals are tested: the writer's checks are not what these assert.
const insertRelease = `insert into release
	(id, format_version, actor_kind, actor_key, actor_key_basis, at, service_id, number, build_id, commit_id, item_id)
	values ($1, $2, 'component', 'gate.merge_to_master', '', $3, $4, $5, $6, $7, $8)`

// TestAnEmptyLinkIsRefusedTwice covers this package's link columns at the two
// that every release names. An empty link names nothing, so it is refused by the
// writer and by the store, the way every other required field is; record's
// doc.go states what a link is checked for. The item is not among them: a
// release over an accepted commit names none.
func TestAnEmptyLinkIsRefusedTwice(t *testing.T) {
	ctx, pool, w := newTable(t)

	empty := minting("")
	if _, err := w.Mint(ctx, merge, empty); !errors.Is(err, release.ErrServiceIDEmpty) {
		t.Errorf("Mint naming no service = %v, want %v", err, release.ErrServiceIDEmpty)
	}
	noBuild := minting(record.NewID("svc"))
	noBuild.BuildID = ""
	if _, err := w.Mint(ctx, merge, noBuild); !errors.Is(err, release.ErrBuildIDEmpty) {
		t.Errorf("Mint naming no build = %v, want %v", err, release.ErrBuildIDEmpty)
	}
	noCommit := minting(record.NewID("svc"))
	noCommit.Commit = ""
	if _, err := w.Mint(ctx, merge, noCommit); !errors.Is(err, release.ErrCommitEmpty) {
		t.Errorf("Mint naming no commit = %v, want %v", err, release.ErrCommitEmpty)
	}

	_, err := pool.Exec(ctx, insertRelease,
		record.NewID(release.IDPrefix), release.FormatVersion, record.Now(), "", int64(1),
		record.NewID("bl"), record.NewID("cm"), record.NewID("it"))
	if err == nil || !strings.Contains(err.Error(), "service_id_present") {
		t.Errorf("inserting a release naming no service = %v, want a violation of service_id_present", err)
	}
	_, err = pool.Exec(ctx, insertRelease,
		record.NewID(release.IDPrefix), release.FormatVersion, record.Now(), record.NewID("svc"), int64(1),
		record.NewID("bl"), "", record.NewID("it"))
	if err == nil || !strings.Contains(err.Error(), "commit_id_present") {
		t.Errorf("inserting a release naming no commit = %v, want a violation of commit_id_present", err)
	}
}

// TestAReleaseOverAnAcceptedCommitNamesNoItem: a commit that reached master by
// another path gets a release from the same writer once a human accepts it, and
// that record names a build and no item — which is what makes a release no gate
// decided readable as such. Several such releases exist per service, so the one
// item per release rule cannot reach them.
func TestAReleaseOverAnAcceptedCommitNamesNoItem(t *testing.T) {
	ctx, pool, w := newTable(t)
	serviceID := record.NewID("svc")

	accepted := minting(serviceID)
	accepted.ItemID = ""
	first, err := w.Mint(ctx, merge, accepted)
	if err != nil {
		t.Fatalf("Mint naming no item: %v", err)
	}
	if first.NamesAnItem() {
		t.Errorf("the release names item %q, want none", first.ItemID)
	}

	second := minting(serviceID)
	second.ItemID = ""
	if _, err := w.Mint(ctx, merge, second); err != nil {
		t.Fatalf("a second release naming no item: %v", err)
	}

	read, err := release.Get(ctx, pool, first.ID)
	if err != nil || read != first {
		t.Fatalf("Get = %+v, %v, want the record Mint returned", read, err)
	}
	if _, found, err := release.ForItem(ctx, pool, ""); err != nil || found {
		t.Errorf("ForItem(\"\") = found %v, %v, want no release and no error", found, err)
	}

	// Such a release is a release of the service like any other, so it is
	// counted, and its authorship rollup names nothing the factory wrote.
	count, err := release.CountForService(ctx, pool, serviceID, record.NewID("it"))
	if err != nil || count != 2 {
		t.Errorf("CountForService = %d, %v, want both releases counted", count, err)
	}
	rollup, err := release.AuthorshipRollup(ctx, pool, first.ID)
	if err != nil || rollup != nil {
		t.Errorf("AuthorshipRollup = %+v, %v, want nothing rolled up", rollup, err)
	}
}

// TestOneItemPerReleaseIsARuleOfTheStore: one item per release, always, at
// every stage, permanently — so a second release naming an item that already
// has one is a refused write and not two releases answering for one item.
func TestOneItemPerReleaseIsARuleOfTheStore(t *testing.T) {
	ctx, pool, w := newTable(t)
	serviceID := record.NewID("svc")

	first, err := w.Mint(ctx, merge, minting(serviceID))
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	again := minting(serviceID)
	again.ItemID = first.ItemID
	if _, err := w.Mint(ctx, merge, again); err == nil {
		t.Error("a second release was minted for one item")
	}

	_, err = pool.Exec(ctx, insertRelease,
		record.NewID(release.IDPrefix), release.FormatVersion, record.Now(), record.NewID("svc"), int64(1),
		record.NewID("bl"), record.NewID("cm"), first.ItemID)
	if err == nil {
		t.Error("the store seated a second release naming one item, on another service")
	}
}

// TestMintingOneCommitAgainWritesNothing: the record is keyed on the commit, so
// the fast-forward and this write are one operation restartable from either
// side — a queue that stopped after the fast-forward mints again and gets the
// release it already wrote, at the number it already took.
func TestMintingOneCommitAgainWritesNothing(t *testing.T) {
	ctx, pool, w := newTable(t)
	serviceID := record.NewID("svc")

	m := minting(serviceID)
	first, err := w.Mint(ctx, merge, m)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	inside := 0
	again, err := w.MintWith(ctx, merge, m, func(context.Context, pgx.Tx, release.Release) error {
		inside++
		return nil
	})
	if err != nil {
		t.Fatalf("minting the same commit again: %v", err)
	}
	if again != first {
		t.Errorf("the second mint returned %+v, want the release already written, %+v", again, first)
	}
	if inside != 0 {
		t.Errorf("what the caller writes beside the release ran %d times, want none on a mint that wrote nothing", inside)
	}

	all, err := release.All(ctx, pool)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("the store holds %d releases, want the one", len(all))
	}
}

// TestHighestIsTheServicesHighestNumber: the store answers which release holds
// the highest number and is empty until the first one. Numbers are never reused,
// so the maximum is always right. It is not master's head: what master holds is
// read from master.
func TestHighestIsTheServicesHighestNumber(t *testing.T) {
	ctx, pool, w := newTable(t)
	const serviceID, other = "svc_a", "svc_b"

	if _, found, err := release.Highest(ctx, pool, serviceID); err != nil || found {
		t.Fatalf("Highest before the first release = found %v, %v", found, err)
	}

	var last release.Release
	for n := 1; n <= 3; n++ {
		minted, err := w.Mint(ctx, merge, release.Minting{
			ServiceID: serviceID,
			BuildID:   fmt.Sprintf("bl_%d", n),
			Commit:    fmt.Sprintf("cm_%d", n),
			ItemID:    fmt.Sprintf("it_%d", n),
		})
		if err != nil {
			t.Fatalf("Mint %d: %v", n, err)
		}
		last = minted
	}
	if _, err := w.Mint(ctx, merge, release.Minting{
		ServiceID: other, BuildID: "bl_other", Commit: "cm_other", ItemID: "it_other",
	}); err != nil {
		t.Fatalf("Mint on another service: %v", err)
	}

	highest, found, err := release.Highest(ctx, pool, serviceID)
	if err != nil || !found {
		t.Fatalf("Highest = found %v, %v", found, err)
	}
	if highest.ID != last.ID || highest.Number != 3 {
		t.Errorf("Highest is %s number %d, want %s number 3", highest.ID, highest.Number, last.ID)
	}
	if highest.BuildID != last.BuildID || highest.Commit != last.Commit {
		t.Errorf("Highest names build %s and commit %s, want %s and %s",
			highest.BuildID, highest.Commit, last.BuildID, last.Commit)
	}
}
