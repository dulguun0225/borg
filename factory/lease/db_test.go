// The database tests of this package are in lease_test, and they open their
// own pool rather than reaching through package postgres for it: postgres
// imports lease to apply this package's DDL in its own list, and reaching
// back here for nothing but the pool is the one edge deps.txt exists to keep
// out. defaultURL and databaseURLEnv duplicate postgres.DefaultURL and
// postgres.URLEnv for that reason.
//
// None of these tests skips when the database is unreachable. The milestone
// is demonstrated by them running, so an unreachable database fails the run.
package lease_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
)

const (
	defaultURL     = "postgres://factory:factory@localhost:5433/factory"
	databaseURLEnv = "DATABASE_URL"
)

func databaseURL() string {
	if u := os.Getenv(databaseURLEnv); u != "" {
		return u
	}
	return defaultURL
}

// newLease gives a test a schema of its own, this package's DDL applied
// inside it, and a pool over it. The schema is dropped when the test ends,
// so a rerun on a database a previous run left dirty starts clean.
func newLease(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "lease_" + hex.EncodeToString(suffix[:])

	pool, err := pgxpool.New(ctx, inSchema(t, databaseURL(), schema))
	if err != nil {
		t.Fatalf("opening the pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("the database at %s is not reachable, and these tests do not skip: %v", databaseURL(), err)
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
			t.Fatalf("applying statement %d: %v", n+1, err)
		}
	}
	return ctx, pool
}

// inSchema points a connection URL at one schema and nothing else, so every
// unqualified name in the DDL and in the package's statements resolves there.
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

func TestAcquireThenARefusalWhileHeld(t *testing.T) {
	ctx, pool := newLease(t)

	token, err := lease.Acquire(ctx, pool, "instance-a", time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if token != 1 {
		t.Fatalf("Acquire = %d, want 1", token)
	}

	if _, err := lease.Acquire(ctx, pool, "instance-b", time.Minute); !errors.Is(err, lease.ErrHeld) {
		t.Errorf("a second instance's Acquire = %v, want %v", err, lease.ErrHeld)
	}
}

// TestAnUnexpiredLeaseRefusesTheNameHoldingIt: the two conditions an
// acquisition takes are unheld and expired, and the holder's own name is
// neither. Two processes on one host under one name would otherwise both
// start, which is the case the lease exists to refuse.
func TestAnUnexpiredLeaseRefusesTheNameHoldingIt(t *testing.T) {
	ctx, pool := newLease(t)

	token, err := lease.Acquire(ctx, pool, "instance-a", time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	if _, err := lease.Acquire(ctx, pool, "instance-a", time.Minute); !errors.Is(err, lease.ErrHeld) {
		t.Fatalf("Acquire under the holder's own name = %v, want %v", err, lease.ErrHeld)
	}

	// The refusal moved no number, so the holder's token still fences.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer tx.Rollback(ctx)
	if err := lease.Fence(ctx, tx, token); err != nil {
		t.Errorf("Fence(token) = %v, want nil", err)
	}
}

// TestAnExpiredLeaseIsAcquiredByTheSameInstance: the holder that let its own
// lease lapse acquires it again the way any other starting process does, and
// takes the next number.
func TestAnExpiredLeaseIsAcquiredByTheSameInstance(t *testing.T) {
	ctx, pool := newLease(t)

	first, err := lease.Acquire(ctx, pool, "instance-a", -time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	second, err := lease.Acquire(ctx, pool, "instance-a", time.Minute)
	if err != nil {
		t.Fatalf("Acquire after its own lease lapsed: %v", err)
	}
	if second != first+1 {
		t.Fatalf("Acquire = %d, want %d", second, first+1)
	}
}

func TestAnExpiredLeaseIsAcquiredByAnotherInstance(t *testing.T) {
	ctx, pool := newLease(t)

	first, err := lease.Acquire(ctx, pool, "instance-a", -time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if first != 1 {
		t.Fatalf("Acquire = %d, want 1", first)
	}

	second, err := lease.Acquire(ctx, pool, "instance-b", time.Minute)
	if err != nil {
		t.Fatalf("Acquire by the second instance: %v", err)
	}
	if second != 2 {
		t.Fatalf("Acquire = %d, want 2", second)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer tx.Rollback(ctx)
	if err := lease.Fence(ctx, tx, first); !errors.Is(err, lease.ErrFenced) {
		t.Errorf("Fence(first) = %v, want %v", err, lease.ErrFenced)
	}
	if err := lease.Fence(ctx, tx, second); err != nil {
		t.Errorf("Fence(second) = %v, want nil", err)
	}
}

func TestRenewWithAStaleTokenFails(t *testing.T) {
	ctx, pool := newLease(t)

	first, err := lease.Acquire(ctx, pool, "instance-a", -time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if _, err := lease.Acquire(ctx, pool, "instance-b", time.Minute); err != nil {
		t.Fatalf("Acquire by the second instance: %v", err)
	}

	if err := lease.Renew(ctx, pool, first, time.Minute); !errors.Is(err, lease.ErrFenced) {
		t.Errorf("Renew(stale) = %v, want %v", err, lease.ErrFenced)
	}
}

func TestRenewExtendsTheLease(t *testing.T) {
	ctx, pool := newLease(t)

	token, err := lease.Acquire(ctx, pool, "instance-a", time.Second)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := lease.Renew(ctx, pool, token, time.Minute); err != nil {
		t.Fatalf("Renew: %v", err)
	}

	// Renewed past what the original ttl would have covered: a second
	// instance is still refused, which is what shows the renewal took.
	if _, err := lease.Acquire(ctx, pool, "instance-b", time.Minute); !errors.Is(err, lease.ErrHeld) {
		t.Errorf("Acquire by another instance after Renew = %v, want %v", err, lease.ErrHeld)
	}
}

// TestReleaseLeavesTheLeaseUnheld: a process that stops cleanly leaves the
// lease for the next one rather than making it wait out the interval, and a
// token that is no longer the lease's number releases nothing.
func TestReleaseLeavesTheLeaseUnheld(t *testing.T) {
	ctx, pool := newLease(t)

	first, err := lease.Acquire(ctx, pool, "instance-a", time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := lease.Release(ctx, pool, first); err != nil {
		t.Fatalf("Release: %v", err)
	}
	second, err := lease.Acquire(ctx, pool, "instance-b", time.Minute)
	if err != nil {
		t.Fatalf("Acquire after Release: %v", err)
	}

	// A stale token releases nothing: the holder that took the lease keeps it.
	if err := lease.Release(ctx, pool, first); err != nil {
		t.Fatalf("Release with a stale token: %v", err)
	}
	if _, err := lease.Acquire(ctx, pool, "instance-c", time.Minute); !errors.Is(err, lease.ErrHeld) {
		t.Errorf("Acquire after a stale Release = %v, want %v", err, lease.ErrHeld)
	}

	// The released token moved no number, so it is still fenced out, and the
	// token the second instance took is the one that passes.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer tx.Rollback(ctx)
	if err := lease.Fence(ctx, tx, first); !errors.Is(err, lease.ErrFenced) {
		t.Errorf("Fence(released) = %v, want %v", err, lease.ErrFenced)
	}
	if err := lease.Fence(ctx, tx, second); err != nil {
		t.Errorf("Fence(second) = %v, want nil", err)
	}
}

func TestFenceWithNoLeaseRowFails(t *testing.T) {
	ctx, pool := newLease(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer tx.Rollback(ctx)
	if err := lease.Fence(ctx, tx, lease.Token(1)); !errors.Is(err, lease.ErrNoLease) {
		t.Errorf("Fence with no row = %v, want %v", err, lease.ErrNoLease)
	}
}
