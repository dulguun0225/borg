package window

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

// Writer is the one writer of watch windows: the comparison. It opens a window
// when a production deploy record is written and closes it once.
type Writer struct {
	pool *pgxpool.Pool
}

// NewWriter returns the writer over pool.
func NewWriter(pool *pgxpool.Pool) *Writer { return &Writer{pool: pool} }

// Open writes the window. A second window over one deploy, or a second over one
// release, is refused by the unique constraints rather than by this method: the
// rule is "one per production deploy of a release its service has not watched
// before", and a caller that asked twice gets the store's answer.
func (w *Writer) Open(ctx context.Context, actor record.Actor, o Opening) (Window, error) {
	if err := actor.Validate(); err != nil {
		return Window{}, err
	}
	if err := o.validate(); err != nil {
		return Window{}, err
	}

	win := Window{
		ID:             record.NewID(IDPrefix),
		Actor:          actor,
		At:             record.Now(),
		DeployID:       o.DeployID,
		ReleaseID:      o.ReleaseID,
		ServiceID:      o.ServiceID,
		CleanAvailable: o.CleanAvailable,
		HeldOut:        o.HeldOut,
		Size:           o.Size,
		Confidence:     o.Confidence,
		CapSeconds:     o.CapSeconds,
		Formula:        o.Formula,
		PolicyVersion:  o.PolicyVersion,
		ScoreVersion:   o.ScoreVersion,
	}
	_, err := w.pool.Exec(ctx, `insert into `+Table+`
		(id, actor_kind, actor_name, at, deploy_id, release_id, service_id, clean_available, held_out,
		 size, confidence, cap_seconds, formula, policy_version, score_version, exit, closed_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, '', '')`,
		win.ID, string(win.Actor.Kind), win.Actor.Name, win.At,
		win.DeployID, win.ReleaseID, win.ServiceID, win.CleanAvailable, win.HeldOut,
		win.Size, win.Confidence, win.CapSeconds, win.Formula, win.PolicyVersion, win.ScoreVersion,
	)
	if err != nil {
		return Window{}, fmt.Errorf("window: opening %s over deploy %s: %w", win.ID, o.DeployID, err)
	}
	return win, nil
}

// Close writes the exit and the time together, once. A window that already has
// an exit is [ErrAlreadyClosed]: the comparison evaluates every exit, so a second
// close would be two answers to a question that has one.
//
// The row is locked while its exit is read, so two closes racing are one close
// and one error rather than one exit overwriting another.
func (w *Writer) Close(ctx context.Context, id string, exit Exit) (Window, error) {
	if !slices.Contains(Exits, exit) {
		return Window{}, fmt.Errorf("%w: %q", ErrExitUnknown, exit)
	}

	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return Window{}, fmt.Errorf("window: beginning the close of %s: %w", id, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	win, err := scan(tx.QueryRow(ctx, selectWindow+` where id = $1 for update`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Window{}, fmt.Errorf("%w: id %s", ErrNotFound, id)
	} else if err != nil {
		return Window{}, fmt.Errorf("window: reading %s: %w", id, err)
	}
	if !win.Open() {
		return Window{}, fmt.Errorf("%w: %s closed %s at %s", ErrAlreadyClosed, id, win.Exit, win.ClosedAt)
	}

	win.Exit = exit
	win.ClosedAt = record.Now()
	if _, err := tx.Exec(ctx, `update `+Table+` set exit = $1, closed_at = $2 where id = $3`,
		string(win.Exit), win.ClosedAt, id); err != nil {
		return Window{}, fmt.Errorf("window: closing %s at %s: %w", id, exit, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Window{}, fmt.Errorf("window: committing the close of %s: %w", id, err)
	}
	return win, nil
}
