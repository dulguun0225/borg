// The database tests of this package are in item_test rather than in item,
// because they open the pool through package postgres. An external test
// package is a separate package to the compiler, so the edge is a test edge
// and not a cycle. deps.txt records it as "test item -> postgres".
//
// The package's own DDL is applied statement by statement rather than through
// postgres.Apply, so these tests depend on this package's schema alone.
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
//
// This file holds what every other test file in the package shares: the
// schema-per-test setup, the two actors, and the one-item fixture.
package item_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

// newWriters gives a test a schema of its own, this package's DDL applied
// inside it, and both writers over it. The schema is dropped when the test
// ends, so a rerun on a database a previous run left dirty starts clean.
func newWriters(t *testing.T) (context.Context, *pgxpool.Pool, *item.Decomposition, *item.Dispatch) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m1it_" + hex.EncodeToString(suffix[:])

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
	for n, statement := range item.DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("applying item statement %d: %v", n+1, err)
		}
	}
	token, err := lease.Acquire(ctx, pool, "test", time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	return ctx, pool, item.NewDecomposition(pool, token), item.NewDispatch(pool, token)
}

// inSchema points a connection URL at one schema and nothing else, so every
// unqualified name in the DDL and in the writers' statements resolves there.
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

var decompositionActor = record.Actor{Kind: record.KindComponent, Key: "decomposition", Basis: record.BasisClaimed}

// oneProject is the project both the area and the service of a fixture item are
// in. Create compares the two rather than storing either, so a test that gives
// one value twice is an item whose area lies inside its service's project.
const oneProject = "pr_00000000000000000000000000000000"

var dispatchActor = record.Actor{Kind: record.KindComponent, Key: "dispatch", Basis: record.BasisClaimed}

// oneItem is an item freshly decomposed, for the tests that need one to advance or
// report against.
func oneItem(ctx context.Context, t *testing.T, decomposition *item.Decomposition) item.Item {
	t.Helper()
	it, err := decomposition.Create(ctx, decompositionActor, item.New{
		IntentID:  "in_" + strings.Repeat("0", 32),
		ServiceID: "svc_" + strings.Repeat("0", 32),
		AreaID:    "ar_" + strings.Repeat("0", 32),
		Branch:    "item/checkout-retry",
	}, oneProject, oneProject, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return it
}
