package window

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

// Writer is the one writer of analysis windows: the health monitor. It opens a
// window when a production deploy record is written and closes it once.
type Writer struct {
	pool  *pgxpool.Pool
	token lease.Token
}

// NewWriter returns the writer over pool, fencing every write with token.
func NewWriter(pool *pgxpool.Pool, token lease.Token) *Writer {
	return &Writer{pool: pool, token: token}
}

// Open writes the window. A second window over one deploy, or a second over one
// release, is refused by the unique constraint and the partial unique index
// rather than by this method: the rule is "one per production deploy of a
// release its service has not watched before", and a caller that asked twice
// gets the store's answer.
//
// A window that measures nothing closes timed out in this same write: there is
// nothing for the cap to wait on, so it is written already closed rather than
// left open for a caller to close.
func (w *Writer) Open(ctx context.Context, actor record.Actor, o OpenEvent) (Window, error) {
	if err := actor.Validate(); err != nil {
		return Window{}, err
	}
	if err := o.validate(); err != nil {
		return Window{}, err
	}

	win := Window{
		ID:                     record.NewID(IDPrefix),
		Actor:                  actor,
		At:                     record.Now(),
		DeployID:               o.DeployID,
		ReleaseID:              o.ReleaseID,
		BuildID:                o.BuildID,
		ServiceID:              o.ServiceID,
		MeasuresNothing:        o.MeasuresNothing,
		PassedAvailable:        o.PassedAvailable,
		HeldOut:                o.HeldOut,
		Size:                   o.Size,
		Power:                  o.Power,
		Confidence:             o.Confidence,
		CapSeconds:             o.CapSeconds,
		BoundaryVersion:        o.BoundaryVersion,
		Targets:                o.Targets,
		OperationsReadAlone:    o.OperationsReadAlone,
		EmissionVersionRelease: o.EmissionVersionRelease,
		EmissionVersionControl: o.EmissionVersionControl,
		QuantitiesOutside:      o.QuantitiesOutside,
		OwnHistorySize:         o.OwnHistorySize,
		OwnHistoryRunLength:    o.OwnHistoryRunLength,
		ThresholdSize:          o.ThresholdSize,
		ThresholdRunLength:     o.ThresholdRunLength,
		PolicyVersion:          o.PolicyVersion,
		ScoreVersion:           o.ScoreVersion,
	}
	if o.MeasuresNothing {
		// There is nothing for the cap to wait on, so this is not a window the
		// health monitor goes on to decide: it closes timed out in the same write
		// that opens it, and stays what a window closed timed out stays below — a
		// rollback's target — without ever being read as open.
		win.Exit = ExitTimedOut
		win.ClosedAt = record.Now()
	}
	sizes, err := encodeShares(win.Size)
	if err != nil {
		return Window{}, err
	}
	powers, err := encodeShares(win.Power)
	if err != nil {
		return Window{}, err
	}
	ownHistorySizes, err := encodeShares(win.OwnHistorySize)
	if err != nil {
		return Window{}, err
	}
	thresholdSizes, err := encodeShares(win.ThresholdSize)
	if err != nil {
		return Window{}, err
	}

	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return Window{}, fmt.Errorf("window: beginning the open of %s: %w", win.ID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, w.token); err != nil {
		return Window{}, err
	}

	_, err = tx.Exec(ctx, `insert into `+Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at,
		 deploy_id, release_id, build_id, service_id, measures_nothing, passed_available, held_out,
		 sizes, powers, confidence, cap_seconds, boundary_version, targets, operations_read_alone,
		 emission_version_release, emission_version_control, quantities_outside,
		 own_history_sizes, own_history_run_length, threshold_sizes, threshold_run_length,
		 policy_version, score_version, exit, exit_begun, closed_at, closed_on, finest_size_reached)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20,
		 $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, '', $31, '', '')`,
		win.ID, FormatVersion, string(win.Actor.Kind), win.Actor.Key, string(win.Actor.Basis), win.At,
		win.DeployID, win.ReleaseID, win.BuildID, win.ServiceID,
		win.MeasuresNothing, win.PassedAvailable, win.HeldOut,
		sizes, powers, win.Confidence, win.CapSeconds, win.BoundaryVersion,
		encodeNames(win.Targets), encodeNames(win.OperationsReadAlone),
		win.EmissionVersionRelease, win.EmissionVersionControl, encodeQuantities(win.QuantitiesOutside),
		ownHistorySizes, win.OwnHistoryRunLength, thresholdSizes, win.ThresholdRunLength,
		win.PolicyVersion, win.ScoreVersion, string(win.Exit), win.ClosedAt,
	)
	if err != nil {
		return Window{}, fmt.Errorf("window: opening %s over deploy %s: %w", win.ID, o.DeployID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Window{}, fmt.Errorf("window: committing the open of %s: %w", win.ID, err)
	}
	return win, nil
}

// Closing is what [Writer.Close] is given beside the exit: the read the window
// closed on and the finest size its traffic reached per quantity. The skipped
// exit takes neither — a rollback aimed below the release ended that window, so
// a read there would be a reading nothing performed.
type Closing struct {
	On Read
	// FinestSizeReached is per quantity, and it is what the score reads: the size
	// in force is the coarser of what the evidence asks for and what the traffic
	// can rule anything out at.
	FinestSizeReached map[gatepolicy.Quantity]float64
}

// Begin records that an exit has started on the window, before the first record
// that exit writes. It is the health monitor's own note to its next pass: the
// close is the exit's last step, so an exit interrupted between the two is
// finished at the exit that began rather than decided again.
//
// A second begin is [ErrExitAlreadyBegun] and a begin on a closed window is
// [ErrAlreadyClosed]. The row is locked while it is read, so two passes racing
// are one begin and one error.
func (w *Writer) Begin(ctx context.Context, id string, exit Exit) (Window, error) {
	if !slices.Contains(Exits, exit) {
		return Window{}, fmt.Errorf("%w: %q", ErrExitUnknown, exit)
	}

	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return Window{}, fmt.Errorf("window: beginning the %s exit of %s: %w", exit, id, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, w.token); err != nil {
		return Window{}, err
	}

	win, err := scan(tx.QueryRow(ctx, selectWindow+` where id = $1 for update`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Window{}, fmt.Errorf("%w: id %s", ErrNotFound, id)
	} else if err != nil {
		return Window{}, fmt.Errorf("window: reading %s: %w", id, err)
	}
	if !win.Open() {
		return Window{}, fmt.Errorf("%w: %s closed %s at %s", ErrAlreadyClosed, id, win.Exit, win.ClosedAt)
	}
	if win.ExitBegun != "" {
		return Window{}, fmt.Errorf("%w: %s began %s", ErrExitAlreadyBegun, id, win.ExitBegun)
	}

	win.ExitBegun = exit
	if _, err := tx.Exec(ctx, `update `+Table+` set exit_begun = $1 where id = $2`,
		string(exit), id); err != nil {
		return Window{}, fmt.Errorf("window: recording that %s began on %s: %w", exit, id, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Window{}, fmt.Errorf("window: committing the begin of %s on %s: %w", exit, id, err)
	}
	return win, nil
}

// Close writes the exit and the time together, once. A window that already has
// an exit is [ErrAlreadyClosed]: the health monitor evaluates every exit, so a
// second close would be two answers to a question that has one.
//
// A passed close on a window that never had that exit available is
// [ErrPassedUnavailable]. The held-out sample runs to the cap rather than
// stopping where the boundary would allow, and so does a window with nothing to
// compare against or too little traffic for the power in force, so the refusal
// is here as well as in the caller.
//
// The row is locked while its exit is read, so two closes racing are one close
// and one error rather than one exit overwriting another.
func (w *Writer) Close(ctx context.Context, id string, exit Exit, closing Closing) (Window, error) {
	if !slices.Contains(Exits, exit) {
		return Window{}, fmt.Errorf("%w: %q", ErrExitUnknown, exit)
	}
	if exit == ExitSkipped && (!closing.On.Empty() || len(closing.FinestSizeReached) > 0) {
		return Window{}, fmt.Errorf("%w: %s carries a read, and that exit is a rollback aimed below the release rather than a reading",
			ErrReadRefused, exit)
	}
	on, err := encodeRead(closing.On)
	if err != nil {
		return Window{}, err
	}
	finest, err := encodeShares(closing.FinestSizeReached)
	if err != nil {
		return Window{}, err
	}
	if len(closing.FinestSizeReached) == 0 {
		finest = ""
	}

	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return Window{}, fmt.Errorf("window: beginning the close of %s: %w", id, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, w.token); err != nil {
		return Window{}, err
	}

	win, err := scan(tx.QueryRow(ctx, selectWindow+` where id = $1 for update`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Window{}, fmt.Errorf("%w: id %s", ErrNotFound, id)
	} else if err != nil {
		return Window{}, fmt.Errorf("window: reading %s: %w", id, err)
	}
	if !win.Open() {
		return Window{}, fmt.Errorf("%w: %s closed %s at %s", ErrAlreadyClosed, id, win.Exit, win.ClosedAt)
	}
	if exit == ExitPassed && !win.PassedAvailable {
		return Window{}, fmt.Errorf("%w: %s", ErrPassedUnavailable, id)
	}
	if win.ExitBegun != "" && win.ExitBegun != exit {
		return Window{}, fmt.Errorf("%w: %s began %s and is closing %s", ErrExitNotTheOneBegun, id, win.ExitBegun, exit)
	}

	win.Exit = exit
	win.ClosedAt = record.Now()
	win.ClosedOn = closing.On
	win.FinestSizeReached = closing.FinestSizeReached
	if _, err := tx.Exec(ctx, `update `+Table+` set exit = $1, closed_at = $2,
		closed_on = $4, finest_size_reached = $5 where id = $3`,
		string(win.Exit), win.ClosedAt, id, on, finest); err != nil {
		return Window{}, fmt.Errorf("window: closing %s at %s: %w", id, exit, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Window{}, fmt.Errorf("window: committing the close of %s: %w", id, err)
	}
	return win, nil
}
