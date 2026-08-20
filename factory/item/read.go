package item

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

// columns is every column of the item table, in the order [scanItem] reads
// them. It is written once because four callers read an item — two here, the
// advance, and the send back — and a fifth column added to one of five select
// lists is a bug the compiler cannot see.
const columns = `id, actor_kind, actor_name, at, intent_id, service_id, area_id, branch, stage,
	waits_on, superseded_by, priority`

// Two columns hold one id per line: the items this one waits on, and the items
// that replaced it. An id is [record.NewID]'s alphabet, which holds no line
// ending, so the separator needs no escaping — the arrangement package
// environment's targets column already has.

func joinIDs(ids []string) string { return strings.Join(ids, "\n") }

func splitIDs(stored string) []string {
	if stored == "" {
		return nil
	}
	return strings.Split(stored, "\n")
}

// scanItem reads one item row in [columns] order.
func scanItem(row pgx.Row) (Item, error) {
	var it Item
	var kind, stage, waitsOn, supersededBy string
	err := row.Scan(&it.ID, &kind, &it.Actor.Name, &it.At, &it.IntentID, &it.ServiceID,
		&it.AreaID, &it.Branch, &stage, &waitsOn, &supersededBy, &it.Priority)
	if err != nil {
		return Item{}, err
	}
	it.Actor.Kind = record.Kind(kind)
	it.Stage = Stage(stage)
	it.WaitsOn = splitIDs(waitsOn)
	it.SupersededBy = splitIDs(supersededBy)
	return it, nil
}

// Get is one item by id. It takes the pool and not a writer, because reading
// an item is not a reason to be handed either of the things that write one.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (Item, error) {
	it, err := scanItem(pool.QueryRow(ctx, `select `+columns+` from `+Table+` where id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	} else if err != nil {
		return Item{}, fmt.Errorf("item: reading %s: %w", id, err)
	}
	return it, nil
}

// IDsInArea is every item whose area is the one named, in the order they were
// cut. It answers one area and not a chain: a caller wanting every item a pin
// on an area reaches walks the chain itself and asks for each, the chain being
// package area's to read.
//
// An empty area is no items and no error. An item may name no area, and an
// empty id would otherwise match every one of them.
func IDsInArea(ctx context.Context, pool *pgxpool.Pool, areaID string) ([]string, error) {
	if areaID == "" {
		return nil, nil
	}
	rows, err := pool.Query(ctx, `select id from `+Table+` where area_id = $1 order by at, id`, areaID)
	if err != nil {
		return nil, fmt.Errorf("item: reading the items in %s: %w", areaID, err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("item: reading an item in %s: %w", areaID, err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("item: reading the items in %s: %w", areaID, err)
	}
	return ids, nil
}

// ForIntent is every item cut from one intent, in the order they were cut. One
// intent yields one item on the crude path and several where the cut divides the
// work, and both readers of this want all of them: what a rollback's revert intent
// became, and what an incident's intent became.
//
// An empty intent is no items and no error, for the reason [IDsInArea] gives about
// an empty area.
func ForIntent(ctx context.Context, pool *pgxpool.Pool, intentID string) ([]Item, error) {
	if intentID == "" {
		return nil, nil
	}
	rows, err := pool.Query(ctx, `select `+columns+` from `+Table+`
		where intent_id = $1 order by at, id`, intentID)
	if err != nil {
		return nil, fmt.Errorf("item: reading the items of %s: %w", intentID, err)
	}
	defer rows.Close()

	var read []Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, fmt.Errorf("item: reading an item of %s: %w", intentID, err)
		}
		read = append(read, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("item: reading the items of %s: %w", intentID, err)
	}
	return read, nil
}

// AtStage is every item of one service at one stage, ordered by the priority an
// owner set — greater first — and then by the time the item was cut.
//
// It is what the merge queue's membership is read with, and the order it returns
// is not the queue's own: the queue orders by that priority and then by the time
// of the merge approval in the log, which is a fact this package does not hold.
// The tie-break here is the cut's time, so a caller that reads no log still gets
// a stable order.
func AtStage(ctx context.Context, pool *pgxpool.Pool, serviceID string, stage Stage) ([]Item, error) {
	rows, err := pool.Query(ctx, `select `+columns+` from `+Table+`
		where service_id = $1 and stage = $2 order by priority desc, at, id`, serviceID, string(stage))
	if err != nil {
		return nil, fmt.Errorf("item: reading the items of %s at %s: %w", serviceID, stage, err)
	}
	defer rows.Close()

	var read []Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, fmt.Errorf("item: reading an item of %s at %s: %w", serviceID, stage, err)
		}
		read = append(read, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("item: reading the items of %s at %s: %w", serviceID, stage, err)
	}
	return read, nil
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
