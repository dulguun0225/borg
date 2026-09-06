// Helpers shared by the authoring subcommand tests: a schema of its own per
// test, installing the factory and decomposing a service on it, and the reads
// of what a stage or an intent spent, which are over the agent run records the
// component that dispatched the role wrote.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/agentrun"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/project"
	"github.com/dulguun0225/borg/factory/score"
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
// testToken is the lease this test holds, acquired once and answered again on
// every call. Acquiring twice takes the lease a second time and fences the first
// token out, so a test that took one per write would refuse its own second
// write — which is what the one-process rule does to two processes and not what
// any of these tests is about.
func testToken(t *testing.T, ctx context.Context, pool *pgxpool.Pool) lease.Token {
	t.Helper()
	if held, taken := tokens[pool]; taken {
		return held
	}
	token, err := lease.Acquire(ctx, pool, defaultInstance(), time.Minute)
	if err != nil {
		t.Fatalf("acquiring the lease: %v", err)
	}
	if tokens == nil {
		tokens = map[*pgxpool.Pool]lease.Token{}
	}
	tokens[pool] = token
	t.Cleanup(func() { delete(tokens, pool) })
	return token
}

// tokens is the lease each test's pool holds, so [testToken] answers the same
// one twice. It is keyed by pool because each test opens one of its own, and the
// entry is dropped when that test ends.
var tokens map[*pgxpool.Pool]lease.Token

// install is what the run's first take does, which everything an owner authors on
// depends on.
func install(t *testing.T, ctx context.Context, pool *pgxpool.Pool) environment.Environment {
	t.Helper()
	installed, err := policy.NewFactory(pool, testToken(t, ctx, pool)).Install(ctx,
		owner(t, ctx, pool, testToken(t, ctx, pool), "owner"), defaultProjectName,
		[]string{t.TempDir()}, secretref.MustNew("deploy.local"), theCeiling)
	if err != nil {
		t.Fatalf("installing: %v", err)
	}
	return installed.Production
}

// policyVersions is every policy version, read the way a reader of the log
// reads one: through [policy.Reader], which carries the fencing token and
// appends a read event naming the human it is read as. The score version it is
// composed with is the zero value — a read of the versions resolves no
// parameter, so the version the reader holds decides nothing here.
func policyVersions(t *testing.T, ctx context.Context, pool *pgxpool.Pool) ([]policy.Version, error) {
	t.Helper()
	// The lease is taken again rather than reused: each subcommand takes one of
	// its own for the life of the command, so the token this test held before
	// they ran is fenced out by the last of them.
	token, err := lease.Acquire(ctx, pool, defaultInstance(), time.Minute)
	if err != nil {
		t.Fatalf("acquiring the lease: %v", err)
	}
	return policy.NewReader(pool, token, score.Version{}).Versions(ctx, owner(t, ctx, pool, token, "owner"))
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

// spendOn is what the agentrun records say one item's stage cost, summed over
// the units recorded under kind "total" — package agentrun's own read, spend
// no longer being a field of the item.
func spendOn(t *testing.T, ctx context.Context, d deps, itemID string, stage item.Stage) int64 {
	t.Helper()
	runs, err := agentrun.ForItem(ctx, d.pool, itemID)
	if err != nil {
		t.Fatalf("reading the agent runs of %s: %v", itemID, err)
	}
	var total int64
	for _, r := range runs {
		if r.Stage != string(stage) {
			continue
		}
		for _, units := range r.UnitsByKind {
			total += units
		}
	}
	return total
}

// spendCallsOn is how many agentrun records name one item's stage — the number
// of model calls made there, refused ones included, which is what a retry's
// own count is now read from rather than from the item's per-stage attempts.
func spendCallsOn(t *testing.T, ctx context.Context, d deps, itemID string, stage item.Stage) int {
	t.Helper()
	runs, err := agentrun.ForItem(ctx, d.pool, itemID)
	if err != nil {
		t.Fatalf("reading the agent runs of %s: %v", itemID, err)
	}
	var calls int
	for _, r := range runs {
		if r.Stage == string(stage) {
			calls++
		}
	}
	return calls
}

// spendOnIntent is [spendOn] scoped to the intent rather than to an item: the
// interview's own model calls are recorded there, since they happen before
// the first item exists.
func spendOnIntent(t *testing.T, ctx context.Context, d deps, intentID string) int64 {
	t.Helper()
	runs, err := agentrun.ForIntent(ctx, d.pool, intentID)
	if err != nil {
		t.Fatalf("reading the agent runs of %s: %v", intentID, err)
	}
	var total int64
	for _, r := range runs {
		for _, units := range r.UnitsByKind {
			total += units
		}
	}
	return total
}
