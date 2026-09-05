package item

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrNotFound is returned where no item has the id.
	ErrNotFound = errors.New("item: no item has that id")
	// ErrStageUnknown is returned for a stage outside [StageOrder].
	ErrStageUnknown = errors.New("item: the stage is none of the six an item moves through")
	// ErrNotAuthoringStage is returned for a stage outside [AuthoringStages]
	// where the write is only meaningful at one an artifact is authored at.
	ErrNotAuthoringStage = errors.New("item: the stage is none of the four an item is authored at")
	// ErrNotNextStage is returned by [Dispatch.Advance] for a transition that
	// is not one stage forward in [StageOrder] — a skip, a backwards move,
	// staying put, and anything at or past merged are all this error. Merged is
	// [Dispatch.End]'s and never an advance.
	ErrNotNextStage = errors.New("item: an item advances one stage forward at a time, spec through queued")
	// ErrNotBackUp is returned by [Dispatch.ReturnTo] for a target that is not
	// at or above the stage the item is at. Going back up is the one way back,
	// and going forward is [Dispatch.Advance]'s.
	ErrNotBackUp = errors.New("item: an item is sent back to the stage it is at or to one above it")
	// ErrNotQueued is returned by [Dispatch.End] for an item that is not
	// queued. Merged follows queued and nothing else.
	ErrNotQueued = errors.New("item: only a queued item is merged")
	// ErrNotEscalated is returned by [Dispatch.ClearEscalation] for an item
	// that is not escalated. There is nothing to clear on one that is not.
	ErrNotEscalated = errors.New("item: the item is not escalated")
	// ErrItemIDEmpty is returned for a write naming no item. record's doc.go
	// states what a link is checked for.
	ErrItemIDEmpty = errors.New("item: the item id is empty")
	// ErrRequirementIDEmpty is returned by [Decomposition.Create] for an empty
	// id among the requirements an item answers.
	ErrRequirementIDEmpty = errors.New("item: the requirement id is empty")
)

// Dispatch is the item's one writer after decomposition. Every stage reports
// its transition here rather than writing the item itself.
type Dispatch struct {
	pool  *pgxpool.Pool
	token lease.Token
}

// NewDispatch returns the writer over pool, fencing every write with token.
func NewDispatch(pool *pgxpool.Pool, token lease.Token) *Dispatch {
	return &Dispatch{pool: pool, token: token}
}

// Advance moves the item to stage, which must be the next stage in
// [StageOrder], and counts an attempt where that stage is one an artifact is
// authored at: an attempt is counted when a stage is entered to author, so a
// stage reached once and passed stands at one. The item row is locked while
// its current stage is read, so two concurrent advances are one advance and
// one [ErrNotNextStage].
//
// Merged is not an advance: [Dispatch.End] is the one write of it, so the
// value the merge queue's fast-forward writes has one writer and not two.
func (d *Dispatch) Advance(ctx context.Context, actor record.Actor, itemID string, stage Stage) (Item, error) {
	if err := actor.Validate(); err != nil {
		return Item{}, err
	}
	if !slices.Contains(StageOrder, stage) {
		return Item{}, fmt.Errorf("%w: %q", ErrStageUnknown, stage)
	}

	return d.move(ctx, itemID, "the advance", func(tx pgx.Tx, it Item) (Stage, error) {
		next, ok := nextStage(it.Stage)
		if !ok || next != stage {
			return "", fmt.Errorf("%w: %s is at %s, not advancing to %s", ErrNotNextStage, itemID, it.Stage, stage)
		}
		if !slices.Contains(AuthoringStages, stage) {
			return stage, nil
		}
		return stage, countEntry(ctx, tx, actor, itemID, stage)
	})
}

// nextStage is the stage after s in [StageOrder], and false where s is queued,
// is the last, or is not a stage an item moves through. Queued's successor is
// merged, which [Dispatch.End] writes.
func nextStage(s Stage) (Stage, bool) {
	at := slices.Index(StageOrder, s)
	if at < 0 || at+1 >= len(StageOrder) {
		return "", false
	}
	next := StageOrder[at+1]
	if next == StageMerged {
		return "", false
	}
	return next, true
}

// ReturnTo sends the item back up the pipeline: a gate's reject, an author's
// rework request, or the merge queue's rejection of a candidate that failed its
// own re-verification, each naming the stage the item returns to.
//
// It counts nothing. An attempt is counted when a stage is entered to author,
// and what these events do is send the item back to be entered again rather
// than increment anything themselves. The rework request itself is a row of the
// decision log, written by the log with whoever was authoring as the actor;
// this moves the item and nothing more.
//
// The target may be the stage the item is already at, which is what a reject at
// the stage that fired is: another attempt at the same artifact. It may not be
// below it; that is [ErrNotBackUp], and going forward is [Dispatch.Advance]'s.
func (d *Dispatch) ReturnTo(ctx context.Context, actor record.Actor, itemID string, stage Stage) (Item, error) {
	if err := actor.Validate(); err != nil {
		return Item{}, err
	}
	if !slices.Contains(AuthoringStages, stage) {
		return Item{}, fmt.Errorf("%w: %q", ErrNotAuthoringStage, stage)
	}

	return d.move(ctx, itemID, "the return", func(tx pgx.Tx, it Item) (Stage, error) {
		at := slices.Index(StageOrder, it.Stage)
		if at < 0 || slices.Index(StageOrder, stage) > at {
			return "", fmt.Errorf("%w: %s is at %s, not sent back to %s", ErrNotBackUp, itemID, it.Stage, stage)
		}
		return stage, nil
	})
}

// End writes merged, the value the merge queue's fast-forward reports. Only a
// queued item is merged: queued is the queue's membership, so an item that is
// not in the queue was never approved to leave it.
func (d *Dispatch) End(ctx context.Context, actor record.Actor, itemID string) (Item, error) {
	if err := actor.Validate(); err != nil {
		return Item{}, err
	}
	return d.move(ctx, itemID, "the merge", func(tx pgx.Tx, it Item) (Stage, error) {
		if it.Stage != StageQueued {
			return "", fmt.Errorf("%w: %s is at %s", ErrNotQueued, itemID, it.Stage)
		}
		return StageMerged, nil
	})
}

// Escalate writes escalated: the factory saying it cannot do this one. The
// caller compares the stage's [StageTotals.AttemptsSinceCleared] against the
// attempt limit in force and calls this where the count is over it — the limit
// is an authored value package policy reads, and an item that read it here
// would be the record reading its own gate policy.
//
// Only an item at a stage it is authored at escalates, the limit being per
// stage and only an authoring stage counting an attempt.
func (d *Dispatch) Escalate(ctx context.Context, actor record.Actor, itemID string) (Item, error) {
	if err := actor.Validate(); err != nil {
		return Item{}, err
	}
	return d.move(ctx, itemID, "the escalation", func(tx pgx.Tx, it Item) (Stage, error) {
		if !slices.Contains(AuthoringStages, it.Stage) {
			return "", fmt.Errorf("%w: %s is at %s", ErrNotAuthoringStage, itemID, it.Stage)
		}
		return StageEscalated, nil
	})
}

// ClearEscalation is a human taking an escalated item over (12): the item
// returns to the stage they are taking over, and the count that stage stands at
// is written as the mark the limit counts from. The attempts already taken stay
// on the row, so the clearing is a mark and not a reset, and what the limit is
// compared against afterwards is [StageTotals.AttemptsSinceCleared].
//
// The stage is the caller's because the escalated value replaced the stage the
// item was at: a human at Work chooses which stage they are authoring.
func (d *Dispatch) ClearEscalation(ctx context.Context, actor record.Actor, itemID string, stage Stage) (Item, error) {
	if err := actor.Validate(); err != nil {
		return Item{}, err
	}
	if !slices.Contains(AuthoringStages, stage) {
		return Item{}, fmt.Errorf("%w: %q", ErrNotAuthoringStage, stage)
	}

	return d.move(ctx, itemID, "the clearing", func(tx pgx.Tx, it Item) (Stage, error) {
		if it.Stage != StageEscalated {
			return "", fmt.Errorf("%w: %s is at %s", ErrNotEscalated, itemID, it.Stage)
		}
		_, err := tx.Exec(ctx, `insert into `+StageTable+`
			(id, format_version, actor_kind, actor_key, actor_key_basis, at, item_id, stage, attempts, cleared_at_attempts)
			values ($1, $2, $3, $4, $5, $6, $7, $8, 0, 0)
			on conflict (item_id, stage) do update set
				cleared_at_attempts = `+StageTable+`.attempts`,
			record.NewID(StageIDPrefix), FormatVersionStage, string(actor.Kind), actor.Key, string(actor.Basis),
			record.Now(), itemID, string(stage),
		)
		if err != nil {
			return "", fmt.Errorf("item: clearing the escalation of %s at %s: %w", itemID, stage, err)
		}
		return stage, nil
	})
}

// Drop writes dropped: the item stops for good without reaching a release.
// Work is the caller where a human ends one that escalated and nobody took
// over, or ends the intent above it; Ops is the caller where a mark ends a
// revert item before it ships. An item that merged or that a re-decomposition
// superseded is already ended, and dropping one is [ErrEnded].
func (d *Dispatch) Drop(ctx context.Context, actor record.Actor, itemID string) (Item, error) {
	if err := actor.Validate(); err != nil {
		return Item{}, err
	}
	return d.move(ctx, itemID, "the drop", func(tx pgx.Tx, it Item) (Stage, error) {
		switch it.Stage {
		case StageMerged, StageDropped, StageSuperseded:
			return "", fmt.Errorf("%w: %s is %s", ErrEnded, itemID, it.Stage)
		}
		return StageDropped, nil
	})
}

// move is the transaction every stage write of this package shares: the fence,
// the item read under a row lock, the caller's decision about what the stage
// becomes and what else the write does, and the update. decide returns the
// stage to write, and an error refuses the whole transaction.
func (d *Dispatch) move(ctx context.Context, itemID, what string,
	decide func(tx pgx.Tx, it Item) (Stage, error)) (Item, error) {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return Item{}, fmt.Errorf("item: beginning %s of %s: %w", what, itemID, err)
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

	stage, err := decide(tx, it)
	if err != nil {
		return Item{}, err
	}
	if _, err := tx.Exec(ctx, `update `+Table+` set stage = $1 where id = $2`, string(stage), itemID); err != nil {
		return Item{}, fmt.Errorf("item: writing %s of %s: %w", what, itemID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Item{}, fmt.Errorf("item: committing %s of %s: %w", what, itemID, err)
	}
	it.Stage = stage
	return it, nil
}

// countEntry adds one to the item's attempts at stage, writing the row on the
// stage's first entry and adding to it after — one statement, so two entries
// recorded at once both land. The row's actor and time stay the first entry's;
// a later entry stores neither, so which attempt was entered when is not in the
// record. The freshly minted id is discarded on a conflict and never reused
// either way — record.NewID reads random bytes and holds no counter.
func countEntry(ctx context.Context, tx pgx.Tx, actor record.Actor, itemID string, stage Stage) error {
	_, err := tx.Exec(ctx, `insert into `+StageTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, item_id, stage, attempts, cleared_at_attempts)
		values ($1, $2, $3, $4, $5, $6, $7, $8, 1, 0)
		on conflict (item_id, stage) do update set
			attempts = `+StageTable+`.attempts + 1`,
		record.NewID(StageIDPrefix), FormatVersionStage, string(actor.Kind), actor.Key, string(actor.Basis),
		record.Now(), itemID, string(stage),
	)
	if err != nil {
		return fmt.Errorf("item: counting an entry to %s of %s: %w", stage, itemID, err)
	}
	return nil
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
