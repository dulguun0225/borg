// The database tests of this package are in build_test and open the pool
// through package postgres, the way decisionlog's do; deps.txt records the
// test edge. They apply this package's DDL themselves rather than calling
// postgres.Apply, which does not know this package until integration wires it
// in.
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package build_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

// newTable gives a test a schema of its own, this package's DDL applied
// inside it, and a writer over it. The schema is dropped when the test ends,
// so a rerun on a database a previous run left dirty starts clean.
func newTable(t *testing.T) (context.Context, *pgxpool.Pool, *build.Writer) {
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
	for n, statement := range build.DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("applying statement %d: %v", n+1, err)
		}
	}
	token, err := lease.Acquire(ctx, pool, "test", time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	return ctx, pool, build.NewWriter(pool, token)
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

var dispatch = record.Actor{Kind: record.KindComponent, Key: "dispatch"}

func TestCreateWritesTheRecordOnce(t *testing.T) {
	ctx, pool, w := newTable(t)

	itemID := record.NewID("it")
	created, err := w.Create(ctx, dispatch, itemID, "0badc0de0badc0de0badc0de0badc0de0badc0de")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ItemID != itemID || created.CommitHash != "0badc0de0badc0de0badc0de0badc0de0badc0de" {
		t.Errorf("Create returned %+v, which does not name what it was given", created)
	}
	if _, err := time.Parse(record.TimeLayout, created.At); err != nil {
		t.Errorf("the record has timestamp %q: %v", created.At, err)
	}

	read, err := build.Get(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read != created {
		t.Errorf("Get = %+v, want the record Create returned, %+v", read, created)
	}
}

func TestASecondBuildOfOneCommitIsRefused(t *testing.T) {
	ctx, _, w := newTable(t)
	itemID := record.NewID("it")

	if _, err := w.Create(ctx, dispatch, itemID, "aaaa"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := w.Create(ctx, dispatch, itemID, "aaaa"); err == nil {
		t.Error("a second record of the same item and commit was accepted")
	}
	if _, err := w.Create(ctx, dispatch, itemID, "bbbb"); err != nil {
		t.Errorf("a second commit of the same item was refused: %v", err)
	}
	if _, err := w.Create(ctx, dispatch, record.NewID("it"), "aaaa"); err != nil {
		t.Errorf("the same commit for another item was refused: %v", err)
	}
}

func TestAnEmptyCommitHashIsRefusedTwice(t *testing.T) {
	ctx, pool, w := newTable(t)

	if _, err := w.Create(ctx, dispatch, record.NewID("it"), ""); !errors.Is(err, build.ErrCommitHashEmpty) {
		t.Errorf("Create = %v, want %v", err, build.ErrCommitHashEmpty)
	}

	// Around the writer, the CHECK constraint is what refuses it.
	_, err := pool.Exec(ctx, `insert into build (id, format_version, actor_kind, actor_key, actor_key_basis, at, item_id, commit_hash)
		values ($1, $2, 'component', 'dispatch', '', $3, $4, '')`,
		record.NewID(build.IDPrefix), build.FormatVersion, record.Now(), record.NewID("it"))
	if err == nil {
		t.Error("the store accepted a build with no commit hash")
	}
}

// TestAnEmptyItemIDIsRefusedTwice is this package's link column. An empty link
// names nothing, so it is refused by the writer and by the store, the way
// every other required field is; record's doc.go states what a link is checked
// for.
func TestAnEmptyItemIDIsRefusedTwice(t *testing.T) {
	ctx, pool, w := newTable(t)

	if _, err := w.Create(ctx, dispatch, "", "aaaa"); !errors.Is(err, build.ErrItemIDEmpty) {
		t.Errorf("Create = %v, want %v", err, build.ErrItemIDEmpty)
	}

	_, err := pool.Exec(ctx, `insert into build (id, format_version, actor_kind, actor_key, actor_key_basis, at, item_id, commit_hash)
		values ($1, $2, 'component', 'dispatch', '', $3, '', 'aaaa')`,
		record.NewID(build.IDPrefix), build.FormatVersion, record.Now())
	if err == nil || !strings.Contains(err.Error(), "item_id_present") {
		t.Errorf("inserting a build naming no item = %v, want a violation of item_id_present", err)
	}
}

func TestABadActorIsRefused(t *testing.T) {
	ctx, _, w := newTable(t)
	if _, err := w.Create(ctx, record.Actor{}, record.NewID("it"), "aaaa"); !errors.Is(err, record.ErrKindUnknown) {
		t.Errorf("Create = %v, want %v", err, record.ErrKindUnknown)
	}
}

func TestGetOfNothingIsNotFound(t *testing.T) {
	ctx, pool, _ := newTable(t)
	if _, err := build.Get(ctx, pool, "bl_00000000000000000000000000000000"); !errors.Is(err, build.ErrNotFound) {
		t.Errorf("Get = %v, want %v", err, build.ErrNotFound)
	}
}

// TestForCommitAnswersWhichBuildIsAlreadyThere: a rebuild is a new build, so a
// re-verification that produced the commit already built produced no build. The
// caller asks before it writes one, rather than being refused by the unique
// constraint and left without the record that is there.
func TestForCommitAnswersWhichBuildIsAlreadyThere(t *testing.T) {
	ctx, pool, w := newTable(t)
	const itemID, commit = "it_a", "8bd35e6a5b0f1ee5f0f2f6f39c5d0f0f6a2b1c3d"

	if _, found, err := build.ForCommit(ctx, pool, itemID, commit); err != nil || found {
		t.Fatalf("ForCommit before anything was built = found %v, %v", found, err)
	}

	made, err := w.Create(ctx, dispatch, itemID, commit)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	found, ok, err := build.ForCommit(ctx, pool, itemID, commit)
	if err != nil || !ok {
		t.Fatalf("ForCommit = ok %v, %v", ok, err)
	}
	if found != made {
		t.Errorf("ForCommit = %+v, want the build that was made, %+v", found, made)
	}

	// Another item at the same commit is another build, the record being one per
	// commit built for an item.
	if _, ok, err := build.ForCommit(ctx, pool, "it_b", commit); err != nil || ok {
		t.Errorf("ForCommit for another item = ok %v, %v", ok, err)
	}
}
