// This file holds the test of the store's own CHECK and unique constraints,
// exercised by raw SQL rather than through the writers.
package item_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/record"
)

// TestTheStoreRefusesAroundTheWriters inserts by raw SQL, so what it
// exercises is the CHECK and unique constraints and not the writers' own
// refusals.
func TestTheStoreRefusesAroundTheWriters(t *testing.T) {
	ctx, pool, decomposition, dispatch := newWriters(t)
	it := oneItem(ctx, t, decomposition)
	if err := dispatch.ReportAttempt(ctx, dispatchActor, it.ID, item.StageSpec, 100); err != nil {
		t.Fatalf("ReportAttempt: %v", err)
	}

	insertItem := `insert into item (id, actor_kind, actor_name, at, intent_id, service_id, area_id,
		branch, stage, waits_on, superseded_by, priority)
		values ($1, 'component', 'decomposition', $2, 'in_x', 'svc_x', 'ar_x', $3, $4, '', '', 0)`
	for _, refused := range []struct {
		name       string
		branch     string
		stage      string
		constraint string
	}{
		{"an empty branch", "", "spec", "branch_present"},
		{"a stage outside the five", "item/x", "shipped", "stage_known"},
	} {
		_, err := pool.Exec(ctx, insertItem, record.NewID(item.IDPrefix), record.Now(), refused.branch, refused.stage)
		if err == nil || !strings.Contains(err.Error(), refused.constraint) {
			t.Errorf("inserting %s = %v, want a violation of %s", refused.name, err, refused.constraint)
		}
	}

	// An empty link, at the column representing this package's three: the
	// store refuses it around the writer too.
	if _, err := pool.Exec(ctx, `insert into item (id, actor_kind, actor_name, at, intent_id, service_id, area_id,
		branch, stage, waits_on, superseded_by, priority)
		values ($1, 'component', 'decomposition', $2, '', 'svc_x', 'ar_x', 'item/x', 'spec', '', '', 0)`,
		record.NewID(item.IDPrefix), record.Now(),
	); err == nil || !strings.Contains(err.Error(), "intent_id_present") {
		t.Errorf("inserting an item naming no intent = %v, want a violation of intent_id_present", err)
	}

	insertStage := `insert into item_stage (id, actor_kind, actor_name, at, item_id, stage, attempts, spend_tokens)
		values ($1, 'component', 'dispatch', $2, $3, $4, $5, $6)`
	for _, refused := range []struct {
		name       string
		stage      string
		attempts   int
		spend      int64
		constraint string
	}{
		{"a stage outside the three", "shipped", 1, 1, "stage_known"},
		{"negative attempts", "implementation", -1, 1, "attempts_not_negative"},
		{"negative spend", "implementation", 1, -1, "spend_not_negative"},
	} {
		_, err := pool.Exec(ctx, insertStage, record.NewID(item.StageIDPrefix), record.Now(),
			it.ID, refused.stage, refused.attempts, refused.spend)
		if err == nil || !strings.Contains(err.Error(), refused.constraint) {
			t.Errorf("inserting %s = %v, want a violation of %s", refused.name, err, refused.constraint)
		}
	}

	// A second row for one item and stage is refused by the unique
	// constraint, which is what ReportAttempt's upsert conflicts on.
	_, err := pool.Exec(ctx, insertStage, record.NewID(item.StageIDPrefix), record.Now(),
		it.ID, string(item.StageSpec), 1, 1)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Errorf("a second row for one item and stage = %v, want a unique violation (23505)", err)
	}
}
