package decisionlog_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/principal"
)

// TestEveryReadAppendsAReadEventNamingThePrincipal checks Read, Verify,
// ClosedDecisions, Pending and ByShape each append exactly one read event,
// naming the principal passed to them — its actor in the row's own columns, and
// an agent's dispatch and scope in the payload, which is the rest of what a
// principal carries and what no record has a column for.
func TestEveryReadAppendsAReadEventNamingThePrincipal(t *testing.T) {
	ctx, pool, log, token := newLog(t)
	reader := decisionlog.NewReader(pool, token)

	opening, err := log.AppendDecisionOpen(ctx, decisionlog.Entry{
		Actor: gate, Payload: "x", FormatVersion: "decision/1", PolicyVersion: "policy-1", ScoreVersion: "score-1",
	})
	if err != nil {
		t.Fatalf("AppendDecisionOpen: %v", err)
	}

	agent := principal.OfAgent("anthropic/claude-opus-4.8", "dsp_00112233445566778899aabbccddeeff", "the item's own repository")
	calls := []struct {
		name string
		p    principal.Principal
		call func() error
	}{
		{"Read", ownerReading, func() error { _, err := reader.Read(ctx, ownerReading); return err }},
		{"Verify", ownerReading, func() error { return reader.Verify(ctx, ownerReading) }},
		{"ClosedDecisions", otherHumanReading, func() error { _, err := reader.ClosedDecisions(ctx, otherHumanReading); return err }},
		{"Pending", otherHumanReading, func() error { _, err := reader.Pending(ctx, otherHumanReading); return err }},
		{"ByShape", componentReading, func() error {
			_, err := reader.ByShape(ctx, componentReading, decisionlog.ShapeDecision)
			return err
		}},
		{"Read as an agent", agent, func() error { _, err := reader.Read(ctx, agent); return err }},
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
		key, payload := lastReadEvent(t, ctx, pool)
		if key != c.p.Actor.Key {
			t.Errorf("%s's read event names actor %q, want the principal %q", c.name, key, c.p.Actor.Key)
		}
		var said struct {
			Dispatch string `json:"dispatch"`
			Scope    string `json:"scope"`
			Read     string `json:"read"`
		}
		if err := json.Unmarshal([]byte(payload), &said); err != nil {
			t.Fatalf("%s's read event payload does not parse: %v", c.name, err)
		}
		if said.Dispatch != c.p.DispatchID || said.Scope != c.p.Scope {
			t.Errorf("%s's read event names dispatch %q under scope %q, want %q and %q",
				c.name, said.Dispatch, said.Scope, c.p.DispatchID, c.p.Scope)
		}
		if said.Read == "" {
			t.Errorf("%s's read event says nothing about what was asked for", c.name)
		}
	}

	pending, err := reader.Pending(ctx, ownerReading)
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

// TestAReadEventIsRefusedForAnIncompletePrincipal: an agent's principal names
// the dispatch that put it on the item and the scope it was dispatched under, so
// one missing either is refused rather than written as a read nobody can attach
// to a dispatch.
func TestAReadEventIsRefusedForAnIncompletePrincipal(t *testing.T) {
	ctx, pool, _, token := newLog(t)
	reader := decisionlog.NewReader(pool, token)

	before := readEventCount(t, ctx, pool)
	if _, err := reader.Read(ctx, principal.OfAgent("anthropic/claude-opus-4.8", "", "")); err == nil {
		t.Error("a read as an agent naming no dispatch was accepted")
	}
	if _, err := reader.Read(ctx, principal.Principal{}); err == nil {
		t.Error("a read as nobody was accepted")
	}
	if after := readEventCount(t, ctx, pool); after != before {
		t.Errorf("%d read events were appended for a refused principal, want none", after-before)
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

// lastReadEvent is the newest read event's actor key and payload, which is
// where the principal it names is read from.
func lastReadEvent(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (string, string) {
	t.Helper()
	var key, payload string
	if err := pool.QueryRow(ctx,
		`select actor_key, payload from decision_log where shape = $1 order by seq desc limit 1`,
		string(decisionlog.ShapeReadEvent),
	).Scan(&key, &payload); err != nil {
		t.Fatalf("reading the newest read event: %v", err)
	}
	return key, payload
}
