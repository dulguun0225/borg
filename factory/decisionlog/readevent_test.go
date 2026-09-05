package decisionlog_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/record"
)

// TestEveryReadAppendsAReadEventNamingThePrincipal checks Read, Verify,
// ClosedDecisions, and Pending each append exactly one read event, naming
// the principal passed to them.
func TestEveryReadAppendsAReadEventNamingThePrincipal(t *testing.T) {
	ctx, pool, log, token := newLog(t)
	reader := decisionlog.NewReader(pool, token)

	opening, err := log.AppendDecisionOpen(ctx, decisionlog.Entry{
		Actor: gate, Payload: "x", FormatVersion: "decision/1", PolicyVersion: "policy-1", ScoreVersion: "score-1",
	})
	if err != nil {
		t.Fatalf("AppendDecisionOpen: %v", err)
	}

	calls := []struct {
		name      string
		principal record.Actor
		call      func() error
	}{
		{"Read", owner, func() error { _, err := reader.Read(ctx, owner); return err }},
		{"Verify", owner, func() error { return reader.Verify(ctx, owner) }},
		{"ClosedDecisions", otherHuman, func() error { _, err := reader.ClosedDecisions(ctx, otherHuman); return err }},
		{"Pending", otherHuman, func() error { _, err := reader.Pending(ctx, otherHuman); return err }},
	}
	for _, c := range calls {
		before := readEventCount(t, ctx, pool)
		if err := c.call(); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		after := readEventCount(t, ctx, pool)
		if after != before+1 {
			t.Errorf("%s appended %d read events, want 1", c.name, after-before)
		}
		last := lastReadEvent(t, ctx, pool)
		if last.Actor.Key != c.principal.Key {
			t.Errorf("%s's read event names actor %q, want the principal %q", c.name, last.Actor.Key, c.principal.Key)
		}
	}

	pending, err := reader.Pending(ctx, owner)
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	found := false
	for _, row := range pending {
		if row.ID == opening.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("Pending did not return the still-open decision %s", opening.ID)
	}
}

func readEventCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `select count(*) from decision_log where shape = $1`,
		string(decisionlog.ShapeReadEvent)).Scan(&count); err != nil {
		t.Fatalf("counting read events: %v", err)
	}
	return count
}

func lastReadEvent(t *testing.T, ctx context.Context, pool *pgxpool.Pool) decisionlog.Row {
	t.Helper()
	var key string
	if err := pool.QueryRow(ctx,
		`select actor_key from decision_log where shape = $1 order by seq desc limit 1`, string(decisionlog.ShapeReadEvent),
	).Scan(&key); err != nil {
		t.Fatalf("reading the newest read event: %v", err)
	}
	return decisionlog.Row{Actor: record.Actor{Key: key}}
}
