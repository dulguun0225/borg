// The database tests of this package are in service_test rather than in
// service, because they open the pool through package postgres. An external
// test package is a separate package to the compiler, so the edge is a test
// edge and not a cycle. deps.txt records it as "test service -> postgres".
//
// The package's own DDL is applied statement by statement rather than through
// postgres.Apply, so these tests depend on this package's schema alone. This
// file, parameters_test.go, provisioning_test.go, operations_test.go,
// deployer_test.go and versions_test.go share the newWriter and inSchema
// helpers below.
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
	"reflect"
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
	// The ttl is already lapsed, so a test that needs a second token over the
	// same pool can take one: lease.Acquire takes a lease that is unheld or
	// expired and refuses every other, whichever name asks.
	token, err := lease.Acquire(ctx, pool, "test", -time.Second)
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

var decomposition = record.Actor{Kind: record.KindComponent, Key: "decomposition", Basis: record.BasisClaimed}
var owner = record.Actor{Kind: record.KindHuman, Key: "person:owner", Basis: record.BasisClaimed}

// aProject stands in for a project id: this package neither imports package
// project nor checks the id exists, there being no foreign keys between
// records.
const aProject = "prj_test"

func TestCreateAndGet(t *testing.T) {
	ctx, pool, w := newWriter(t)

	created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Name != "checkout" || created.Repository != "/srv/repos/checkout" || created.ProjectID != aProject {
		t.Errorf("Create = %+v, want the name, repository and project as given", created)
	}
	if _, err := time.Parse(record.TimeLayout, created.At); err != nil {
		t.Errorf("the service's timestamp %q: %v", created.At, err)
	}
	if created.Retired() {
		t.Error("a freshly created service reports Retired")
	}
	if created.Provisioned.Written() || created.Reachability.Written() {
		t.Errorf("a freshly created service carries provisioned=%+v reachability=%+v", created.Provisioned, created.Reachability)
	}

	read, err := service.Get(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !reflect.DeepEqual(read, created) {
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

	created, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	read, found, err := service.ByName(ctx, pool, "checkout")
	if err != nil {
		t.Fatalf("ByName: %v", err)
	}
	if !found || !reflect.DeepEqual(read, created) {
		t.Errorf("ByName = %+v found=%t, want the service as created, %+v", read, found, created)
	}
}

// TestAllIsEveryServiceInOrderRetiredIncluded: the drift detector and every
// other reader that walks every service must be able to tell a retired one
// from an install with fewer services, so All never shortens the list.
func TestAllIsEveryServiceInOrderRetiredIncluded(t *testing.T) {
	ctx, pool, w := newWriter(t)

	for _, name := range []string{"checkout", "billing"} {
		if _, err := w.Create(ctx, decomposition, name, "/srv/repos/"+name, aProject); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}
	all, err := service.All(ctx, pool)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 2 || all[0].Name != "checkout" || all[1].Name != "billing" {
		t.Errorf("All = %+v, want checkout then billing", all)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := service.Retire(ctx, tx, all[0].ID, 0, 0, 0); err != nil {
		t.Fatalf("Retire: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	all, err = service.All(ctx, pool)
	if err != nil {
		t.Fatalf("All after a retirement: %v", err)
	}
	if len(all) != 2 || !all[0].Retired() {
		t.Errorf("All after a retirement = %+v, want both rows, the first retired", all)
	}
}

// TestDuplicateNameRefusedByTheStore creates one name twice. The second
// create is refused by the unique constraint, not by a pre-check the writer
// makes, so the refusal holds against two concurrent decompositions too.
func TestDuplicateNameRefusedByTheStore(t *testing.T) {
	ctx, _, w := newWriter(t)

	if _, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", aProject); err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout-again", aProject)
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

	if _, err := w.Create(ctx, decomposition, "", "/srv/repos/checkout", aProject); !errors.Is(err, service.ErrNameEmpty) {
		t.Errorf("Create with no name = %v, want ErrNameEmpty", err)
	}
	if _, err := w.Create(ctx, decomposition, "checkout", "", aProject); !errors.Is(err, service.ErrRepositoryEmpty) {
		t.Errorf("Create with no repository = %v, want ErrRepositoryEmpty", err)
	}
	if _, err := w.Create(ctx, decomposition, "checkout", "/srv/repos/checkout", ""); !errors.Is(err, service.ErrProjectEmpty) {
		t.Errorf("Create with no project = %v, want ErrProjectEmpty", err)
	}
	if _, err := w.Create(ctx, record.Actor{}, "checkout", "/srv/repos/checkout", aProject); !errors.Is(err, record.ErrKindUnknown) {
		t.Errorf("Create with no actor = %v, want record.ErrKindUnknown", err)
	}
}

// TestTheStoreRefusesAroundTheWriter inserts by raw SQL, so what it exercises
// is the CHECK constraints and not the writer's own refusals.
func TestTheStoreRefusesAroundTheWriter(t *testing.T) {
	ctx, pool, _ := newWriter(t)

	insert := `insert into ` + service.Table + `
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, name, repository, project_id,
		provisioned_at, repository_credential_shape, repository_credential_branch, repository_credential_master,
		retired_at, targets,
		window_confidence, window_cap_seconds, window_limit, exposure_bound,
		mutant_cap, failure_record_key_cap, unreliable_bound, incident_item_bound_seconds,
		snapshot_retention_seconds, objective, objective_period_seconds,
		paging_hours_start, paging_hours_end, paging_hours_zone, product_licence,
		target_reached, instances_replaceable, rollback_path_present, emission_readable, deployer_wrote_at)
		values ($1, '` + service.FormatVersion + `', 'component', 'decomposition', 'claimed', $2, $3, $4, $5,
		'', '', '', '',
		'', '',
		null, null, null, null,
		null, null, null, null,
		null, null, null,
		'', '', '', '',
		false, false, false, false, '')`
	for _, refused := range []struct {
		name       string
		serviceN   string
		repository string
		project    string
		constraint string
	}{
		{"an empty name", "", "/srv/repos/checkout", aProject, "name_present"},
		{"an empty repository", "checkout", "", aProject, "repository_present"},
		{"an empty project", "checkout", "/srv/repos/checkout", "", "project_id_present"},
	} {
		_, err := pool.Exec(ctx, insert,
			record.NewID(service.IDPrefix), record.Now(), refused.serviceN, refused.repository, refused.project)
		if err == nil || !strings.Contains(err.Error(), refused.constraint) {
			t.Errorf("inserting %s = %v, want a violation of %s", refused.name, err, refused.constraint)
		}
	}
}
