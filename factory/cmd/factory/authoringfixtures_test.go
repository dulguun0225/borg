// Helpers shared by the authoring subcommand tests: a schema of its own per
// test, and installing the factory and decomposing a service on it.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/project"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/service"
)

// newOwner gives a test a schema of its own with the whole schema applied,
// DATABASE_URL pointed at it for the length of the test, an installed factory,
// and a service decomposition wrote. What it returns is the pool, for reading back what
// a subcommand wrote.
func newOwner(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m2_owner_" + hex.EncodeToString(suffix[:])
	url := inSchema(t, postgres.URL(), schema)

	// The subcommands read the environment, so this is how a test hands them a
	// schema of their own. Every pool they open inside this test opens there.
	t.Setenv(postgres.URLEnv, url)

	pool, err := postgres.Open(ctx, url)
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
	return ctx, pool
}

// testToken acquires the lease as the instance a subcommand called later in the
// same process acquires it as, so a fixture's own write and a subcommand's
// withPool re-acquiring afterward are the same instance reacquiring rather than
// two instances disagreeing over who holds it.
func testToken(t *testing.T, ctx context.Context, pool *pgxpool.Pool) lease.Token {
	t.Helper()
	token, err := lease.Acquire(ctx, pool, defaultInstance(), time.Minute)
	if err != nil {
		t.Fatalf("acquiring the lease: %v", err)
	}
	return token
}

// install is what the run's first take does, which everything an owner authors on
// depends on.
func install(t *testing.T, ctx context.Context, pool *pgxpool.Pool) environment.Environment {
	t.Helper()
	installed, err := policy.NewFactory(pool, testToken(t, ctx, pool)).Install(ctx,
		owner("owner"), defaultProjectName,
		[]string{t.TempDir()}, secretref.MustNew("deploy.local"), theCeiling)
	if err != nil {
		t.Fatalf("installing: %v", err)
	}
	return installed.Production
}

func decomposeService(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) service.Service {
	t.Helper()
	prj, found, err := project.ByName(ctx, pool, defaultProjectName)
	if err != nil {
		t.Fatalf("reading the project: %v", err)
	}
	if !found {
		t.Fatalf("no project is named %q; call install(t, ctx, pool) first", defaultProjectName)
	}
	svc, err := service.NewWriter(pool, testToken(t, ctx, pool)).Create(ctx,
		decompositionActor, name, "/repos/"+name, prj.ID)
	if err != nil {
		t.Fatalf("creating the service: %v", err)
	}
	return svc
}
