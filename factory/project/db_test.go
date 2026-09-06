// The database tests of this package are in project_test rather than in
// project, because they open the pool through package postgres. An external
// test package is a separate package to the compiler, so the edge is a test
// edge and not a cycle. deps.txt records it as "test project -> postgres".
//
// The package's own DDL is applied statement by statement rather than through
// postgres.Apply, so these tests depend on this package's schema alone.
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package project_test

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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/project"
	"github.com/dulguun0225/borg/factory/record"
)

var owner = record.Actor{Kind: record.KindHuman, Key: "owner", Basis: record.BasisClaimed}

// newWriter gives a test a schema of its own, this package's DDL applied inside
// it, and a writer over it. The schema is dropped when the test ends, so a
// rerun on a database a previous run left dirty starts clean.
func newWriter(t *testing.T) (context.Context, *pgxpool.Pool, *project.Writer, lease.Token) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "prj_" + hex.EncodeToString(suffix[:])

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
	for n, statement := range lease.DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("applying lease statement %d: %v", n+1, err)
		}
	}
	for n, statement := range project.DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("applying project statement %d: %v", n+1, err)
		}
	}
	token, err := lease.Acquire(ctx, pool, "test", time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	t.Cleanup(func() {
		if err := lease.Release(context.Background(), pool, token); err != nil {
			t.Errorf("Release: %v", err)
		}
	})
	return ctx, pool, project.NewWriter(pool, token), token
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

func TestCreateGetAndByName(t *testing.T) {
	ctx, pool, w, _ := newWriter(t)

	created, err := w.Create(ctx, owner, "payments")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Name != "payments" {
		t.Errorf("Create = %+v, want the name as given", created)
	}
	if _, err := time.Parse(record.TimeLayout, created.At); err != nil {
		t.Errorf("the project's timestamp %q: %v", created.At, err)
	}

	read, err := project.Get(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read != created {
		t.Errorf("Get = %+v, want the project as created, %+v", read, created)
	}

	byName, found, err := project.ByName(ctx, pool, "payments")
	if err != nil || !found || byName != created {
		t.Errorf("ByName = %+v found=%t, %v, want the project as created", byName, found, err)
	}
	if _, found, err := project.ByName(ctx, pool, "marketing"); err != nil || found {
		t.Errorf("ByName on a name nobody wrote = found %t, %v, want false and no error", found, err)
	}
	if _, err := project.Get(ctx, pool, "prj_missing"); !errors.Is(err, project.ErrNotFound) {
		t.Errorf("Get on a missing id = %v, want ErrNotFound", err)
	}
}

func TestAllIsEveryProjectInOrder(t *testing.T) {
	ctx, pool, w, _ := newWriter(t)

	for _, name := range []string{"payments", "marketing"} {
		if _, err := w.Create(ctx, owner, name); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}
	all, err := project.All(ctx, pool)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 2 || all[0].Name != "payments" || all[1].Name != "marketing" {
		t.Errorf("All = %+v, want payments then marketing", all)
	}
}

func TestCreateRefusals(t *testing.T) {
	ctx, _, w, _ := newWriter(t)

	if _, err := w.Create(ctx, owner, ""); !errors.Is(err, project.ErrNameEmpty) {
		t.Errorf("Create with no name = %v, want ErrNameEmpty", err)
	}
	if _, err := w.Create(ctx, record.Actor{}, "payments"); !errors.Is(err, record.ErrKindUnknown) {
		t.Errorf("Create with no actor = %v, want record.ErrKindUnknown", err)
	}
}

// TestDuplicateNameRefusedByTheStore writes one name twice. The second write is
// refused by the unique constraint and not by a pre-check the writer makes, so
// the refusal holds against two owners writing at once.
func TestDuplicateNameRefusedByTheStore(t *testing.T) {
	ctx, _, w, _ := newWriter(t)

	if _, err := w.Create(ctx, owner, "payments"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err := w.Create(ctx, owner, "payments")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Errorf("Create with a taken name = %v, want a unique violation (23505)", err)
	}
}

// TestTheStoreRefusesAroundTheWriter inserts by raw SQL, so what it exercises is
// the CHECK constraint and not the writer's own refusal.
func TestTheStoreRefusesAroundTheWriter(t *testing.T) {
	ctx, pool, _, _ := newWriter(t)

	_, err := pool.Exec(ctx, `insert into project
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, name)
		values ($1, '`+project.FormatVersion+`', 'human', 'owner', 'claimed', $2, '')`,
		record.NewID(project.IDPrefix), record.Now())
	if err == nil || !strings.Contains(err.Error(), "name_present") {
		t.Errorf("inserting an empty name = %v, want a violation of name_present", err)
	}
}

// TestInsertIsFenced is the write package policy makes inside its own
// transaction: a stale token is refused there as it is in every other write.
func TestInsertIsFenced(t *testing.T) {
	ctx, pool, _, _ := newWriter(t)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := project.Insert(ctx, tx, lease.Token(0), owner, "payments"); !errors.Is(err, lease.ErrFenced) {
		t.Errorf("Insert with a stale token = %v, want lease.ErrFenced", err)
	}
}
