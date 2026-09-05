package item

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

// columns is every column of the item table, in the order [scanItem] reads
// them. It is written once because every read of an item goes through it — the
// four here, and the row lock each write takes — and a column added to one of
// several select lists is a bug the compiler cannot see.
const columns = `id, actor_kind, actor_key, actor_key_basis, at, intent_id, service_id, area_id, branch, stage,
	waits_on, requirements_answered, superseded_by, priority`

// Three columns hold one id per line: the items this one waits on, the
// requirements it answers, and the items that replaced it. An id is
// [record.NewID]'s alphabet, which holds no line ending, so the separator needs
// no escaping — the arrangement package environment's targets column already
// has.

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
	var kind, basis, stage, waitsOn, requirementsAnswered, supersededBy string
	err := row.Scan(&it.ID, &kind, &it.Actor.Key, &basis, &it.At, &it.IntentID, &it.ServiceID,
		&it.AreaID, &it.Branch, &stage, &waitsOn, &requirementsAnswered, &supersededBy, &it.Priority)
	if err != nil {
		return Item{}, err
	}
	it.Actor.Kind = record.Kind(kind)
	it.Actor.Basis = record.Basis(basis)
	it.Stage = Stage(stage)
	it.WaitsOn = splitIDs(waitsOn)
	it.RequirementsAnswered = splitIDs(requirementsAnswered)
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
// decomposition. It answers one area and not a chain: a caller wanting every item a
// safeguard on an area reaches walks the chain itself and asks for each, the chain
// being package area's to read.
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

// ForIntent is every item decomposed from one intent, in the order they were decomposed. One
// intent yields one item through the crude interface and several where decomposition
// divides the work, and both readers of this want all of them: what a rollback's
// revert intent became, and what an incident's intent became.
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
// owner set — greater first — and then by the time the item was decomposed.
//
// It is what the merge queue's membership is read with, and the order it returns
// is not the queue's own: the queue orders by that priority and then by the time
// of the merge approval in the log, which is a fact this package does not hold.
// The tie-break here is decomposition's time, so a caller that reads no log still gets
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

// All is every item in the store, oldest decomposed first. It is what the score learns
// from: the subjects it supplies a value for are the areas the items name and the
// stages they reported at, so a reader asking per area or per service would first
// have to be told which to ask about.
//
// The whole table at once is what learning over every outcome costs while the
// store is small, and it is the cost the decision log's own whole-log read
// already carries.
func All(ctx context.Context, pool *pgxpool.Pool) ([]Item, error) {
	rows, err := pool.Query(ctx, `select `+columns+` from `+Table+` order by at, id`)
	if err != nil {
		return nil, fmt.Errorf("item: reading every item: %w", err)
	}
	defer rows.Close()

	var read []Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, fmt.Errorf("item: reading an item: %w", err)
		}
		read = append(read, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("item: reading every item: %w", err)
	}
	return read, nil
}

// AllStages is every per-stage row of every item, in the order the stages first
// reported. It is [Stages] over the whole table, and it is a read of its own
// rather than a loop over [All] calling [Stages] for each, because what reads it
// reads every attempt at one stage across every item — the attempt limit being
// per stage and not per item.
func AllStages(ctx context.Context, pool *pgxpool.Pool) ([]StageTotals, error) {
	rows, err := pool.Query(ctx, `select id, actor_kind, actor_key, actor_key_basis, at, item_id, stage, attempts, cleared_at_attempts
		from `+StageTable+` order by at, stage`)
	if err != nil {
		return nil, fmt.Errorf("item: reading every stage: %w", err)
	}
	defer rows.Close()

	var read []StageTotals
	for rows.Next() {
		s, err := scanStage(rows)
		if err != nil {
			return nil, fmt.Errorf("item: reading a stage: %w", err)
		}
		read = append(read, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("item: reading every stage: %w", err)
	}
	return read, nil
}

// Stages is every per-stage row of one item, in the order the stages first
// reported — the timestamp of the first report, with the stage name breaking
// a tie. The id column orders nothing, being random bytes.
func Stages(ctx context.Context, pool *pgxpool.Pool, itemID string) ([]StageTotals, error) {
	rows, err := pool.Query(ctx, `select id, actor_kind, actor_key, actor_key_basis, at, item_id, stage, attempts, cleared_at_attempts
		from `+StageTable+` where item_id = $1 order by at, stage`, itemID)
	if err != nil {
		return nil, fmt.Errorf("item: reading the stages of %s: %w", itemID, err)
	}
	defer rows.Close()

	var read []StageTotals
	for rows.Next() {
		s, err := scanStage(rows)
		if err != nil {
			return nil, fmt.Errorf("item: reading a stage of %s: %w", itemID, err)
		}
		read = append(read, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("item: reading the stages of %s: %w", itemID, err)
	}
	return read, nil
}

// scanStage reads one per-stage row. Both reads of that table use it, so the
// column order is written once.
func scanStage(row pgx.Row) (StageTotals, error) {
	var s StageTotals
	var kind, basis, stage string
	if err := row.Scan(&s.ID, &kind, &s.Actor.Key, &basis, &s.At, &s.ItemID,
		&stage, &s.Attempts, &s.ClearedAtAttempts); err != nil {
		return StageTotals{}, err
	}
	s.Actor.Kind = record.Kind(kind)
	s.Actor.Basis = record.Basis(basis)
	s.Stage = Stage(stage)
	return s, nil
}

// PartlyDelivered reports whether an intent's items did not all ship: at least
// one of them stopped without reaching production, and at least one sibling is
// live. Nothing writes it down, so it is a reading and not a field — a human
// taking over an escalated item can finish it, and the intent stops being
// partly delivered with no event anywhere.
//
// An item stopped when it is dropped or escalated. A superseded item is not
// stopped: an intent whose superseded items were replaced by a
// re-decomposition is judged on the replacements. An item still moving is not
// stopped either, which is why an intent whose items are all still moving is in
// progress rather than partly delivered.
//
// Whether an item is live is not a fact this package holds — a production
// deploy record naming the item's release, complete on every production target,
// is — so live is the ids the caller read as live. Handing this an empty list
// is an intent none of whose items shipped, which is stopped rather than partly
// delivered, and the answer is false.
func PartlyDelivered(ctx context.Context, pool *pgxpool.Pool, intentID string, live []string) (bool, error) {
	items, err := ForIntent(ctx, pool, intentID)
	if err != nil {
		return false, err
	}
	stopped, shipped := false, false
	for _, it := range items {
		if slices.Contains(live, it.ID) {
			shipped = true
			continue
		}
		if it.Stage == StageDropped || it.Stage == StageEscalated {
			stopped = true
		}
	}
	return stopped && shipped, nil
}
