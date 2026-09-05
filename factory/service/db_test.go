// The database tests of this package are in service_test rather than in
// service, because they open the pool through package postgres. An external
// test package is a separate package to the compiler, so the edge is a test
// edge and not a cycle. deps.txt records it as "test service -> postgres".
//
// The package's own DDL is applied statement by statement rather than through
// postgres.Apply, so these tests depend on this package's schema alone.
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package service_test

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
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/service"
)

// newWriter gives a test a schema of its own, this package's DDL applied
// inside it, and a writer over it. The schema is dropped when the test ends,
// so a rerun on a database a previous run left dirty starts clean.
func newWriter(t *testing.T) (context.Context, *pgxpool.Pool, *service.Writer) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m1svc_" + hex.EncodeToString(suffix[:])

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
	for n, statement := range service.DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("applying service statement %d: %v", n+1, err)
		}
	}
	token, err := lease.Acquire(ctx, pool, "test", time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	return ctx, pool, service.NewWriter(pool, token)
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

var decomposition = record.Actor{Kind: record.KindComponent, Key: "decomposition"}

func TestCreateAndGet(t *testing.T) {
	ctx, pool, w := newWriter(t)

	created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Name != "checkout" || created.Repository != "/srv/repos/checkout" {
		t.Errorf("Create = %+v, want the name and repository as given", created)
	}
	if _, err := time.Parse(record.TimeLayout, created.At); err != nil {
		t.Errorf("the service's timestamp %q: %v", created.At, err)
	}

	read, err := service.Get(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read != created {
		t.Errorf("Get = %+v, want the service as created, %+v", read, created)
	}

	if _, err := service.Get(ctx, pool, "svc_missing"); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("Get on a missing id = %v, want ErrNotFound", err)
	}
}

// TestByNameIsWhatDecompositionReads is the look-up the decomposition does before it creates:
// the name it is given either names a service already or names none.
func TestByNameIsWhatDecompositionReads(t *testing.T) {
	ctx, pool, w := newWriter(t)

	if _, found, err := service.ByName(ctx, pool, "checkout"); err != nil || found {
		t.Errorf("ByName before any create = found %t, %v, want false and no error", found, err)
	}

	created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	read, found, err := service.ByName(ctx, pool, "checkout")
	if err != nil {
		t.Fatalf("ByName: %v", err)
	}
	if !found || read != created {
		t.Errorf("ByName = %+v found=%t, want the service as created, %+v", read, found, created)
	}
}

// TestDuplicateNameRefusedByTheStore creates one name twice. The second
// create is refused by the unique constraint, not by a pre-check the writer
// makes, so the refusal holds against two concurrent decompositions too.
func TestDuplicateNameRefusedByTheStore(t *testing.T) {
	ctx, _, w := newWriter(t)

	if _, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout-again")
	if err == nil {
		t.Fatal("Create with a taken name returned nil, want the unique constraint's refusal")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Errorf("Create with a taken name = %v, want a unique violation (23505)", err)
	}
}

func TestCreateRefusals(t *testing.T) {
	ctx, _, w := newWriter(t)

	if _, err := w.Create(ctx, decomposition, "", "/srv/repos/checkout"); !errors.Is(err, service.ErrNameEmpty) {
		t.Errorf("Create with no name = %v, want ErrNameEmpty", err)
	}
	if _, err := w.Create(ctx, decomposition, "checkout", ""); !errors.Is(err, service.ErrRepositoryEmpty) {
		t.Errorf("Create with no repository = %v, want ErrRepositoryEmpty", err)
	}
	if _, err := w.Create(ctx, record.Actor{}, "checkout", "/srv/repos/checkout"); !errors.Is(err, record.ErrKindUnknown) {
		t.Errorf("Create with no actor = %v, want record.ErrKindUnknown", err)
	}
}

// TestTheStoreRefusesAroundTheWriter inserts by raw SQL, so what it exercises
// is the CHECK constraints and not the writer's own refusals.
func TestTheStoreRefusesAroundTheWriter(t *testing.T) {
	ctx, pool, _ := newWriter(t)

	insert := `insert into service (id, format_version, actor_kind, actor_key, actor_key_basis, at, name, repository)
		values ($1, '` + service.FormatVersion + `', 'component', 'decomposition', '', $2, $3, $4)`
	for _, refused := range []struct {
		name       string
		serviceN   string
		repository string
		constraint string
	}{
		{"an empty name", "", "/srv/repos/checkout", "name_present"},
		{"an empty repository", "checkout", "", "repository_present"},
	} {
		_, err := pool.Exec(ctx, insert,
			record.NewID(service.IDPrefix), record.Now(), refused.serviceN, refused.repository)
		if err == nil || !strings.Contains(err.Error(), refused.constraint) {
			t.Errorf("inserting %s = %v, want a violation of %s", refused.name, err, refused.constraint)
		}
	}
}
