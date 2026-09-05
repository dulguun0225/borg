package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
)

// acquire is a second lease token over the same pool, for a test that writes
// an owner-authored field directly rather than through [service.Writer].
func acquire(ctx context.Context, t *testing.T, pool *pgxpool.Pool) lease.Token {
	t.Helper()
	token, err := lease.Acquire(ctx, pool, "test", time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	return token
}

// begin is the transaction package policy would append a version write to:
// every owner-authored field on this record takes the caller's transaction
// rather than opening its own.
func begin(ctx context.Context, t *testing.T, pool *pgxpool.Pool) pgx.Tx {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	return tx
}

func commit(ctx context.Context, t *testing.T, tx pgx.Tx) {
	t.Helper()
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}
