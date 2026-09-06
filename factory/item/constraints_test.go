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
	ctx, pool, decomposition, _ := newWriters(t)
	it := oneItem(ctx, t, decomposition)

	insertItem := `insert into item (id, format_version, actor_kind, actor_key, actor_key_basis, at, intent_id, service_id, area_id,
		branch, stage, waits_on, requirements_answered, superseded_by, priority)
		values ($1, '` + item.FormatVersion + `', 'component', 'decomposition', 'claimed', $2, 'in_x', 'svc_x', 'ar_x', $3, $4, '', '', '', 0)`
	for _, refused := range []struct {
		name       string
		branch     string
		stage      string
		constraint string
	}{
		{"an empty branch", "", "spec", "branch_present"},
		{"a stage outside the nine", "item/x", "shipped", "stage_known"},
	} {
		_, err := pool.Exec(ctx, insertItem, record.NewID(item.IDPrefix), record.Now(), refused.branch, refused.stage)
		if err == nil || !strings.Contains(err.Error(), refused.constraint) {
			t.Errorf("inserting %s = %v, want a violation of %s", refused.name, err, refused.constraint)
		}
	}

	// An empty link, at the column representing this package's three: the
	// store refuses it around the writer too.
	if _, err := pool.Exec(ctx, `insert into item (id, format_version, actor_kind, actor_key, actor_key_basis, at, intent_id, service_id, area_id,
		branch, stage, waits_on, requirements_answered, superseded_by, priority)
		values ($1, '`+item.FormatVersion+`', 'component', 'decomposition', 'claimed', $2, '', 'svc_x', 'ar_x', 'item/x', 'spec', '', '', '', 0)`,
		record.NewID(item.IDPrefix), record.Now(),
	); err == nil || !strings.Contains(err.Error(), "intent_id_present") {
		t.Errorf("inserting an item naming no intent = %v, want a violation of intent_id_present", err)
	}

	insertStage := `insert into item_stage (id, format_version, actor_kind, actor_key, actor_key_basis, at, item_id, stage, attempts, cleared_at_attempts)
		values ($1, '` + item.FormatVersionStage + `', 'component', 'dispatch', 'claimed', $2, $3, $4, $5, $6)`
	for _, refused := range []struct {
		name       string
		stage      string
		attempts   int
		cleared    int
		constraint string
	}{
		{"a stage outside the nine", "shipped", 1, 0, "stage_known"},
		{"negative attempts", "implementation", -1, 0, "attempts_not_negative"},
		{"a negative cleared-at mark", "implementation", 1, -1, "cleared_at_attempts_within_attempts"},
		{"a cleared-at mark above the attempts", "implementation", 1, 2, "cleared_at_attempts_within_attempts"},
	} {
		_, err := pool.Exec(ctx, insertStage, record.NewID(item.StageIDPrefix), record.Now(),
			it.ID, refused.stage, refused.attempts, refused.cleared)
		if err == nil || !strings.Contains(err.Error(), refused.constraint) {
			t.Errorf("inserting %s = %v, want a violation of %s", refused.name, err, refused.constraint)
		}
	}

	// A second row for one item and stage is refused by the unique
	// constraint, which is what the entry count's upsert conflicts on. The
	// row for spec is already there: decomposition counts the item's first
	// entry to author when it creates it.
	_, err := pool.Exec(ctx, insertStage, record.NewID(item.StageIDPrefix), record.Now(),
		it.ID, string(item.StageSpec), 1, 0)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Errorf("a second row for one item and stage = %v, want a unique violation (23505)", err)
	}
}
