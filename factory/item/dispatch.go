package item

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrNotFound is returned where no item has the id.
	ErrNotFound = errors.New("item: no item has that id")
	// ErrStageUnknown is returned for a stage outside [StageOrder].
	ErrStageUnknown = errors.New("item: the stage is none of spec, implementation, merged")
	// ErrNotNextStage is returned by [Dispatch.Advance] for a transition that
	// is not one stage forward in [StageOrder] — a skip, a backwards move,
	// staying put, and anything past merged are all this error.
	ErrNotNextStage = errors.New("item: an item advances spec to implementation to merged, one stage forward at a time")
	// ErrSpendNegative is returned by [Dispatch.ReportAttempt] for a negative
	// spend. The totals only go up; taking spend back is not a report.
	ErrSpendNegative = errors.New("item: a report's spend is not negative")
	// ErrItemIDEmpty is returned by [Dispatch.ReportAttempt] for a per-stage
	// row naming no item. record's doc.go states what a link is checked for.
	ErrItemIDEmpty = errors.New("item: the item id is empty")
)

// Dispatch is the item's one writer after the cut. Every stage reports its
// transition and its spend here rather than writing the item itself.
type Dispatch struct {
	pool *pgxpool.Pool
}

// NewDispatch returns the writer over pool.
func NewDispatch(pool *pgxpool.Pool) *Dispatch { return &Dispatch{pool: pool} }

// Advance moves the item to stage, which must be the next stage in
// [StageOrder]. The item row is locked while its current stage is read, so
// two concurrent advances are one advance and one [ErrNotNextStage].
func (d *Dispatch) Advance(ctx context.Context, actor record.Actor, itemID string, stage Stage) (Item, error) {
	if err := actor.Validate(); err != nil {
		return Item{}, err
	}
	if !slices.Contains(StageOrder, stage) {
		return Item{}, fmt.Errorf("%w: %q", ErrStageUnknown, stage)
	}

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return Item{}, fmt.Errorf("item: beginning the advance of %s: %w", itemID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var it Item
	var kind, current string
	err = tx.QueryRow(ctx, `select id, actor_kind, actor_name, at, intent_id, service_id, branch, stage
		from `+Table+` where id = $1 for update`, itemID).
		Scan(&it.ID, &kind, &it.Actor.Name, &it.At, &it.IntentID, &it.ServiceID, &it.Branch, &current)
	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, fmt.Errorf("%w: %s", ErrNotFound, itemID)
	} else if err != nil {
		return Item{}, fmt.Errorf("item: reading %s: %w", itemID, err)
	}
	it.Actor.Kind = record.Kind(kind)

	next, ok := nextStage(Stage(current))
	if !ok || next != stage {
		return Item{}, fmt.Errorf("%w: %s is at %s, not advancing to %s", ErrNotNextStage, itemID, current, stage)
	}

	if _, err := tx.Exec(ctx, `update `+Table+` set stage = $1 where id = $2`, string(stage), itemID); err != nil {
		return Item{}, fmt.Errorf("item: advancing %s to %s: %w", itemID, stage, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Item{}, fmt.Errorf("item: committing the advance of %s: %w", itemID, err)
	}
	it.Stage = stage
	return it, nil
}

// nextStage is the stage after s in [StageOrder], and false where s is the
// last or is not a stage.
func nextStage(s Stage) (Stage, bool) {
	for n, stage := range StageOrder[:len(StageOrder)-1] {
		if stage == s {
			return StageOrder[n+1], true
		}
	}
	return "", false
}

// ReportAttempt adds one attempt and its spend to the item's totals for
// stage, writing the row on the stage's first report and adding to it after —
// one statement, so two concurrent reports both land. The row's actor and
// time stay the first report's; a later report validates its actor and stores
// it nowhere.
func (d *Dispatch) ReportAttempt(ctx context.Context, actor record.Actor, itemID string, stage Stage, spendTokens int64) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	if itemID == "" {
		return ErrItemIDEmpty
	}
	if !slices.Contains(StageOrder, stage) {
		return fmt.Errorf("%w: %q", ErrStageUnknown, stage)
	}
	if spendTokens < 0 {
		return fmt.Errorf("%w: %d", ErrSpendNegative, spendTokens)
	}

	// The freshly minted id and timestamp are discarded on a conflict; the
	// row keeps the first report's. An id is never reused either way —
	// record.NewID reads random bytes and holds no counter.
	_, err := d.pool.Exec(ctx, `insert into `+StageTable+`
		(id, actor_kind, actor_name, at, item_id, stage, attempts, spend_tokens)
		values ($1, $2, $3, $4, $5, $6, 1, $7)
		on conflict (item_id, stage) do update set
			attempts = `+StageTable+`.attempts + 1,
			spend_tokens = `+StageTable+`.spend_tokens + excluded.spend_tokens`,
		record.NewID(StageIDPrefix), string(actor.Kind), actor.Name, record.Now(),
		itemID, string(stage), spendTokens,
	)
	if err != nil {
		return fmt.Errorf("item: reporting an attempt at %s of %s: %w", stage, itemID, err)
	}
	return nil
}
