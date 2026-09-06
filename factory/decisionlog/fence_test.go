package decisionlog_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/lease"
)

// TestAFenceWithAStaleTokenRefusesTheAppend is [lease.Fence] reached from
// inside this package's own append: a writer whose token is no longer the
// lease's current number is refused before it inserts anything.
func TestAFenceWithAStaleTokenRefusesTheAppend(t *testing.T) {
	ctx, pool, _, staleToken := newLog(t)

	// The holder releases the lease and another instance takes it, which is
	// what a start after a clean stop is: the number rises, and the token
	// newLog's writer holds is no longer current.
	if err := lease.Release(ctx, pool, staleToken); err != nil {
		t.Fatalf("releasing the lease: %v", err)
	}
	if _, err := lease.Acquire(ctx, pool, "another", time.Minute); err != nil {
		t.Fatalf("acquiring the lease: %v", err)
	}

	stale := decisionlog.NewWriter(pool, staleToken)
	if _, err := stale.AppendPageEvent(ctx, decisionlog.Entry{
		Actor: gate, Payload: "x", FormatVersion: "page_event/1",
	}); !errors.Is(err, lease.ErrFenced) {
		t.Errorf("appending with a stale token: %v, want %v", err, lease.ErrFenced)
	}

	rows, err := decisionlog.NewReader(pool, currentLeaseNumber(t, ctx, pool)).Read(ctx, ownerReading)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	for _, row := range rows {
		if row.Payload == "x" {
			t.Fatalf("the fenced append reached the store: %+v", row)
		}
	}
}

func currentLeaseNumber(t *testing.T, ctx context.Context, pool *pgxpool.Pool) lease.Token {
	t.Helper()
	var number int64
	if err := pool.QueryRow(ctx, `select number from `+lease.Table+` where id = 1`).Scan(&number); err != nil {
		t.Fatalf("reading the lease's current number: %v", err)
	}
	return lease.Token(number)
}
