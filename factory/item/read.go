package item

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

// Get is one item by id. It takes the pool and not a writer, because reading
// an item is not a reason to be handed either of the things that write one.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (Item, error) {
	var it Item
	var kind, stage string
	err := pool.QueryRow(ctx, `select id, actor_kind, actor_name, at, intent_id, service_id, branch, stage
		from `+Table+` where id = $1`, id).
		Scan(&it.ID, &kind, &it.Actor.Name, &it.At, &it.IntentID, &it.ServiceID, &it.Branch, &stage)
	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	} else if err != nil {
		return Item{}, fmt.Errorf("item: reading %s: %w", id, err)
	}
	it.Actor.Kind = record.Kind(kind)
	it.Stage = Stage(stage)
	return it, nil
}

// Stages is every per-stage row of one item, in the order the stages first
// reported — the timestamp of the first report, with the stage name breaking
// a tie. The id column orders nothing, being random bytes.
func Stages(ctx context.Context, pool *pgxpool.Pool, itemID string) ([]StageTotals, error) {
	rows, err := pool.Query(ctx, `select id, actor_kind, actor_name, at, item_id, stage, attempts, spend_tokens
		from `+StageTable+` where item_id = $1 order by at, stage`, itemID)
	if err != nil {
		return nil, fmt.Errorf("item: reading the stages of %s: %w", itemID, err)
	}
	defer rows.Close()

	var read []StageTotals
	for rows.Next() {
		var s StageTotals
		var kind, stage string
		if err := rows.Scan(&s.ID, &kind, &s.Actor.Name, &s.At, &s.ItemID,
			&stage, &s.Attempts, &s.SpendTokens); err != nil {
			return nil, fmt.Errorf("item: reading a stage of %s: %w", itemID, err)
		}
		s.Actor.Kind = record.Kind(kind)
		s.Stage = Stage(stage)
		read = append(read, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("item: reading the stages of %s: %w", itemID, err)
	}
	return read, nil
}
