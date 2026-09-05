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

	// The same instance reacquires the lease, which lease.Acquire treats as
	// a fresh acquisition and not a renewal: the number rises, and the token
	// newLog's writer holds is no longer current.
	if _, err := lease.Acquire(ctx, pool, "test", time.Minute); err != nil {
		t.Fatalf("reacquiring the lease: %v", err)
	}

	stale := decisionlog.NewWriter(pool, staleToken)
	if _, err := stale.AppendPageEvent(ctx, decisionlog.Entry{
		Actor: gate, Payload: "x", FormatVersion: "page_event/1",
	}); !errors.Is(err, lease.ErrFenced) {
		t.Errorf("appending with a stale token: %v, want %v", err, lease.ErrFenced)
	}

	rows, err := decisionlog.NewReader(pool, currentLeaseNumber(t, ctx, pool)).Read(ctx, owner)
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
