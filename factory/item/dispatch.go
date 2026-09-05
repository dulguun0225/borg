package item

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrNotFound is returned where no item has the id.
	ErrNotFound = errors.New("item: no item has that id")
	// ErrStageUnknown is returned for a stage outside [StageOrder].
	ErrStageUnknown = errors.New("item: the stage is none of spec, implementation, queued, merged")
	// ErrNotNextStage is returned by [Dispatch.Advance] for a transition that
	// is not one stage forward in [StageOrder] — a skip, a backwards move,
	// staying put, and anything past merged are all this error.
	ErrNotNextStage = errors.New("item: an item advances spec to implementation to queued to merged, one stage forward at a time")
	// ErrNotBackUp is returned by [Dispatch.ReworkRequest] for a target that is not
	// at or above the stage the item is at. Going back up is the one way back,
	// and going forward is [Dispatch.Advance]'s.
	ErrNotBackUp = errors.New("item: an item is sent back to the stage it is at or to one above it")
	// ErrSpendNegative is returned by [Dispatch.ReportAttempt] for a negative
	// spend. The totals only go up; taking spend back is not a report.
	ErrSpendNegative = errors.New("item: a report's spend is not negative")
	// ErrItemIDEmpty is returned by [Dispatch.ReportAttempt] for a per-stage
	// row naming no item. record's doc.go states what a link is checked for.
	ErrItemIDEmpty = errors.New("item: the item id is empty")
)

// Dispatch is the item's one writer after decomposition. Every stage reports its
// transition and its spend here rather than writing the item itself.
type Dispatch struct {
	pool  *pgxpool.Pool
	token lease.Token
}

// NewDispatch returns the writer over pool, fencing every write with token.
func NewDispatch(pool *pgxpool.Pool, token lease.Token) *Dispatch { return &Dispatch{pool: pool, token: token} }

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
	if err := lease.Fence(ctx, tx, d.token); err != nil {
		return Item{}, err
	}

	it, err := scanItem(tx.QueryRow(ctx, `select `+columns+` from `+Table+` where id = $1 for update`, itemID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, fmt.Errorf("%w: %s", ErrNotFound, itemID)
	} else if err != nil {
		return Item{}, fmt.Errorf("item: reading %s: %w", itemID, err)
	}

	next, ok := nextStage(it.Stage)
	if !ok || next != stage {
		return Item{}, fmt.Errorf("%w: %s is at %s, not advancing to %s", ErrNotNextStage, itemID, it.Stage, stage)
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

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("item: beginning the attempt report of %s: %w", itemID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, d.token); err != nil {
		return err
	}
	if err := reportAttempt(ctx, tx, actor, itemID, stage, spendTokens); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("item: committing the attempt report of %s: %w", itemID, err)
	}
	return nil
}

// executor is the two things an attempt report is written through: the pool for
// [Dispatch.ReportAttempt], and a transaction for [Dispatch.ReworkRequest], which
// counts its attempt in the same transaction as the move.
type executor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// reportAttempt is the upsert both callers make. The freshly minted id and
// timestamp are discarded on a conflict; the row keeps the first report's. An id
// is never reused either way — record.NewID reads random bytes and holds no
// counter.
func reportAttempt(ctx context.Context, q executor, actor record.Actor, itemID string, stage Stage, spendTokens int64) error {
	_, err := q.Exec(ctx, `insert into `+StageTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, item_id, stage, attempts, spend_tokens)
		values ($1, $2, $3, $4, $5, $6, $7, $8, 1, $9)
		on conflict (item_id, stage) do update set
			attempts = `+StageTable+`.attempts + 1,
			spend_tokens = `+StageTable+`.spend_tokens + excluded.spend_tokens`,
		record.NewID(StageIDPrefix), FormatVersionStage, string(actor.Kind), actor.Key, string(actor.Basis), record.Now(),
		itemID, string(stage), spendTokens,
	)
	if err != nil {
		return fmt.Errorf("item: reporting an attempt at %s of %s: %w", stage, itemID, err)
	}
	return nil
}

// ReworkRequest moves the item back up the pipeline and counts one attempt at the
// stage it is sent to, in one transaction: the rework is booked against the thing
// that was wrong, and a move that counted nothing would leave the attempt limit
// comparing against a number the item never spent.
//
// The three callers are the two gate rows that reject and the merge queue's
// rejection of a candidate that failed its own re-verification. Each of them
// names the stage, and the default where a verdict names none is Implementation
// — there being no stage of their own and none between.
//
// The target may be the stage the item is already at, which is what a reject at
// the Implementation gate is: another attempt at the same artifact. It may not be
// below it; that is [ErrNotBackUp], and going forward is [Dispatch.Advance]'s.
// The spend is nothing, because a rework request spends no tokens: what the attempt
// after it spends is reported by that attempt.
func (d *Dispatch) ReworkRequest(ctx context.Context, actor record.Actor, itemID string, stage Stage) (Item, error) {
	if err := actor.Validate(); err != nil {
		return Item{}, err
	}
	if !slices.Contains(StageOrder, stage) {
		return Item{}, fmt.Errorf("%w: %q", ErrStageUnknown, stage)
	}

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return Item{}, fmt.Errorf("item: beginning the rework request of %s: %w", itemID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, d.token); err != nil {
		return Item{}, err
	}

	it, err := scanItem(tx.QueryRow(ctx, `select `+columns+` from `+Table+` where id = $1 for update`, itemID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, fmt.Errorf("%w: %s", ErrNotFound, itemID)
	} else if err != nil {
		return Item{}, fmt.Errorf("item: reading %s: %w", itemID, err)
	}
	if slices.Index(StageOrder, stage) > slices.Index(StageOrder, it.Stage) {
		return Item{}, fmt.Errorf("%w: %s is at %s, not sent back to %s", ErrNotBackUp, itemID, it.Stage, stage)
	}

	if _, err := tx.Exec(ctx, `update `+Table+` set stage = $1 where id = $2`, string(stage), itemID); err != nil {
		return Item{}, fmt.Errorf("item: sending %s back to %s: %w", itemID, stage, err)
	}
	if err := reportAttempt(ctx, tx, actor, itemID, stage, 0); err != nil {
		return Item{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Item{}, fmt.Errorf("item: committing the rework request of %s: %w", itemID, err)
	}
	it.Stage = stage
	return it, nil
}

// SetPriority writes the priority an owner reorders a queue with. It goes
// through dispatch rather than beside it, the way Work calls intake to answer a
// question: the item has one writer after decomposition, and reordering is a write to
// the item.
//
// Reordering changes when a candidate is re-verified and never what it has to
// pass, and it orders every queue the item waits in as an item — so an owner who
// rushes an item to the front here has rushed it at every gate it has left, and
// has no way at all to reorder a deploy.
func (d *Dispatch) SetPriority(ctx context.Context, actor record.Actor, itemID string, priority int) (Item, error) {
	if err := actor.Validate(); err != nil {
		return Item{}, err
	}
	if itemID == "" {
		return Item{}, ErrItemIDEmpty
	}

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return Item{}, fmt.Errorf("item: beginning the priority of %s: %w", itemID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, d.token); err != nil {
		return Item{}, err
	}
	if _, err := tx.Exec(ctx, `update `+Table+` set priority = $1 where id = $2`, priority, itemID); err != nil {
		return Item{}, fmt.Errorf("item: setting the priority of %s: %w", itemID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Item{}, fmt.Errorf("item: committing the priority of %s: %w", itemID, err)
	}
	it, err := Get(ctx, d.pool, itemID)
	if err != nil {
		return Item{}, err
	}
	return it, nil
}
