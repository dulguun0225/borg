package deploy

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
)

var (
	// ErrEnvironmentEmpty is returned by [Writer.Start] for a deploy naming
	// no environment.
	ErrEnvironmentEmpty = errors.New("deploy: the environment is empty")
	// ErrServiceIDEmpty is returned by [Writer.Start] for a deploy naming no
	// service. record's doc.go states what a link is checked for.
	ErrServiceIDEmpty = errors.New("deploy: the service id is empty")
	// ErrBuildIDEmpty is returned by [Writer.Start] for a deploy naming a
	// release and no build. A removal names neither and is admitted; every other
	// deploy names the build it put there.
	ErrBuildIDEmpty = errors.New("deploy: the deploy names a release and no build")
	// ErrNoTargets is returned by [Writer.Start] for a deploy naming no target.
	// Completion is per target, so a deploy with none could never complete and
	// no reader could ever read it as running anywhere.
	ErrNoTargets = errors.New("deploy: the deploy names no target")
	// ErrStrategyNotProduction is returned for a strategy on a deploy that is
	// not into production, and for a production deploy naming none. A strategy
	// attaches to a production deploy and to no other.
	ErrStrategyNotProduction = errors.New("deploy: a strategy attaches to a production deploy and to no other")
	// ErrNotTheDeployer is returned by [Writer.Start] for a record whose actor
	// is not the deployer. No agent performs a deploy — deploying is not a stage
	// an agent is dispatched to — and a human who called for one, the human at
	// Ops whose rollback it is, is the record's source and never its actor: the
	// actor stays the deployer that performed it, a program the factory runs and
	// can check.
	ErrNotTheDeployer = errors.New("deploy: the deploy record's actor is the deployer that performed it")
	// ErrNotFound is returned where the named deploy does not exist.
	ErrNotFound = errors.New("deploy: no deploy has that id")
	// ErrTargetNotFound is returned where the deploy has no row for that
	// address.
	ErrTargetNotFound = errors.New("deploy: the deploy names no such target")
	// ErrNotStarted is returned by the writes that advance a started deploy.
	// Neither completion nor failure runs twice, and nothing here un-completes.
	ErrNotStarted = errors.New("deploy: only a started deploy is advanced")
	// ErrTargetsIncomplete is returned by [Writer.Complete] for a deploy some
	// target of which is not complete. The record as a whole is complete when
	// every target is.
	ErrTargetsIncomplete = errors.New("deploy: a target of the deploy is not complete")
	// ErrATargetCompleted is returned by [Writer.MarkFailed] for a deploy a
	// target of which is already complete. Failed is where the deployer stopped
	// before any target was complete; a deploy that reached one and stopped is a
	// recorded partial deploy and stays started.
	ErrATargetCompleted = errors.New("deploy: a deploy with a target complete is not marked failed")
	// ErrUndoingIncomplete is returned by [Writer.StartUndoing] for a rollback
	// missing something every rollback names, or naming one release as both
	// failed and skipped.
	ErrUndoingIncomplete = errors.New("deploy: the rollback is missing something every rollback names")
	// ErrNoSnapshot is returned by [DeleteSnapshot] and
	// [Writer.MarkSnapshotDeleted] for a record naming no copy, or one whose
	// deletion is already written. There is nothing for a deletion to stand
	// beside, and nothing to reach on the target.
	ErrNoSnapshot = errors.New("deploy: the deploy names no snapshot")
	// ErrControlIncomplete is returned by [Writer.ControlStarted] for a control
	// naming no release or no build. A control is defined by which release it
	// runs, and the record names the build that release is beside it.
	ErrControlIncomplete = errors.New("deploy: a control names the release it runs and the build that release is")
	// ErrBackfillNotCopied is returned by [Writer.Complete] for a backfill's
	// record whose copy has not finished. The deployer marks a backfill's deploy
	// record complete only once every row the old form holds is present in the
	// new, and that fact is what enforcement reads before it admits the item
	// that moves reads to that element and the drop after it.
	ErrBackfillNotCopied = errors.New("deploy: a backfill's record completes once every row the old form holds is present in the new")
	// ErrBackfillIncomplete is returned by [Writer.Start] for a backfill naming
	// some of the three. What a backfill declares is the element it fills and
	// the element it fills from, on one store contract, and a pair missing a
	// side is a mark enforcement cannot read.
	ErrBackfillIncomplete = errors.New("deploy: a backfill names a store contract, the element it fills, and the element it fills from")
)

// Writer is the one writer of the deploy record, its completion per target, and
// the mitigation. All three are the deployer's.
type Writer struct {
	pool  *pgxpool.Pool
	token lease.Token
}

// NewWriter returns the writer over pool, fencing every write with token.
func NewWriter(pool *pgxpool.Pool, token lease.Token) *Writer {
	return &Writer{pool: pool, token: token}
}

// Pool is the pool this writer holds, for the reads a deploy makes of its own
// records while it runs. Reading a deploy needs no writer; performing one needs
// both.
func (w *Writer) Pool() *pgxpool.Pool { return w.pool }

// Complete advances the deploy from started to complete, and refuses a deploy
// any target the service runs on is not complete on: the record as a whole is
// complete when every one of those targets is, which is also when the release it
// names becomes current. The rows for the environment's other targets are not
// read here — the service runs on none of them, so nothing was ever going to
// reach them.
//
// A backfill's record is refused until its copy has finished: the deployer marks
// one complete only once every row the old form holds is present in the new, and
// what says so is [Writer.MarkBackfillCopied]. A copy that runs for hours leaves
// the deploy standing incomplete on Ops for as long as it runs.
func (w *Writer) Complete(ctx context.Context, id string) error {
	return w.inTransaction(ctx, "completing "+id, func(tx pgx.Tx) error {
		status, err := lockStatus(ctx, tx, id)
		if err != nil {
			return err
		}
		if status != StatusStarted {
			return fmt.Errorf("%w: %s is %s", ErrNotStarted, id, status)
		}
		var element string
		var copied bool
		err = tx.QueryRow(ctx, `select backfill_element, backfill_copied from `+Table+`
			where id = $1`, id).Scan(&element, &copied)
		if err != nil {
			return err
		}
		if element != "" && !copied {
			return fmt.Errorf("%w: %s of %s", ErrBackfillNotCopied, element, id)
		}
		var unfinished int
		err = tx.QueryRow(ctx, `select count(*) from `+TargetTable+`
			where deploy_id = $1 and runs_here and completion <> $2`,
			id, string(CompletionComplete)).Scan(&unfinished)
		if err != nil {
			return err
		}
		if unfinished != 0 {
			return fmt.Errorf("%w: %d of %s", ErrTargetsIncomplete, unfinished, id)
		}
		_, err = tx.Exec(ctx, `update `+Table+` set status = $1 where id = $2`, string(StatusComplete), id)
		return err
	})
}

// MarkBackfillCopied records that every row the old form holds is present in
// the new, which is what [Writer.Complete] requires of a backfill's record
// before it completes it. The copy is the release's own change, rerun from
// where it stopped, so what marks it is a read of the store and never this
// package: the caller that can see both forms writes it.
func (w *Writer) MarkBackfillCopied(ctx context.Context, id string) error {
	return w.inTransaction(ctx, "marking the backfill of "+id+" copied", func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `update `+Table+` set backfill_copied = true
			where id = $1 and backfill_element <> ''`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("%w: %s carries no backfill", ErrNotFound, id)
		}
		return nil
	})
}

// MarkFailed marks the record failed at the step that stopped it, and refuses
// where a target is already complete. A failure stands for Ops here, the
// restart leaves it alone, and both queries over overlapping windows descend
// past it — all three read a record no target completed.
func (w *Writer) MarkFailed(ctx context.Context, id, step string) error {
	if step == "" {
		return fmt.Errorf("%w: %s names no step", ErrNotStarted, id)
	}
	return w.inTransaction(ctx, "failing "+id, func(tx pgx.Tx) error {
		status, err := lockStatus(ctx, tx, id)
		if err != nil {
			return err
		}
		if status != StatusStarted {
			return fmt.Errorf("%w: %s is %s", ErrNotStarted, id, status)
		}
		var complete int
		err = tx.QueryRow(ctx, `select count(*) from `+TargetTable+`
			where deploy_id = $1 and completion = $2`, id, string(CompletionComplete)).Scan(&complete)
		if err != nil {
			return err
		}
		if complete != 0 {
			return fmt.Errorf("%w: %d of %s", ErrATargetCompleted, complete, id)
		}
		_, err = tx.Exec(ctx, `update `+Table+` set status = $1, failed_step = $2 where id = $3`,
			string(StatusFailed), step, id)
		return err
	})
}

// PerformedWithControl records that the deployer performed the row with a
// control: the shift the control's schedule asked for returned. It writes only
// where nothing is recorded as performed yet, so a target that refused the shift
// earlier in the walk is not overwritten by a later one that took it — a rollout
// one target of which ran no comparison is a rollout without a control on the
// record.
func (w *Writer) PerformedWithControl(ctx context.Context, id string) error {
	return w.performed(ctx, id, StrategyWithControl, ` and strategy_performed = ''`)
}

// PerformedWithoutControl records that the deployer performed the row without a
// control: either the row the score picked, once the instances of a target have
// been replaced with none of the build they replace left running, or the row a
// target declared as serving a share leaves by refusing the shift. The picked
// field is left as it was, so an owner reading a rollout that ran no comparison
// reads on one record whether the platform was the reason.
//
// Nothing is written at the start of a deploy: a deployer that stopped between
// the record's write and the shift would otherwise leave a record naming a
// control that never ran, which is the reading the performed field exists to
// prevent.
func (w *Writer) PerformedWithoutControl(ctx context.Context, id string) error {
	return w.performed(ctx, id, StrategyWithoutControl, ``)
}

func (w *Writer) performed(ctx context.Context, id string, strategy Strategy, onlyWhere string) error {
	return w.inTransaction(ctx, "recording the strategy performed on "+id, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `update `+Table+` set strategy_performed = $1
			where id = $2 and strategy_picked <> ''`+onlyWhere, string(strategy), id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			var picked string
			err := tx.QueryRow(ctx, `select strategy_picked from `+Table+` where id = $1`, id).Scan(&picked)
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: %s", ErrNotFound, id)
			} else if err != nil {
				return err
			}
			if picked == "" {
				return fmt.Errorf("%w: %s is not a production deploy", ErrStrategyNotProduction, id)
			}
		}
		return nil
	})
}

// inTransaction runs one write, fenced with this writer's token, so that the
// refusal and the write commit together or neither does.
func (w *Writer) inTransaction(ctx context.Context, doing string, write func(pgx.Tx) error) error {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("deploy: beginning %s: %w", doing, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, w.token); err != nil {
		return err
	}
	if err := write(tx); err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrNotStarted) || errors.Is(err, ErrTargetNotFound) ||
			errors.Is(err, ErrTargetsIncomplete) || errors.Is(err, ErrATargetCompleted) ||
			errors.Is(err, ErrStrategyNotProduction) || errors.Is(err, ErrNoSnapshot) ||
			errors.Is(err, ErrBackfillNotCopied) {
			return err
		}
		return fmt.Errorf("deploy: %s: %w", doing, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("deploy: committing %s: %w", doing, err)
	}
	return nil
}

// lockStatus reads and locks the deploy's status for the rest of the
// transaction, which is what keeps two advances of one record from interleaving.
func lockStatus(ctx context.Context, tx pgx.Tx, id string) (Status, error) {
	var status string
	err := tx.QueryRow(ctx, `select status from `+Table+` where id = $1 for update`, id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("%w: %s", ErrNotFound, id)
	} else if err != nil {
		return "", err
	}
	return Status(status), nil
}

// The releases a rollback skipped, the ones a revert's deploy delivers, and the
// schema changes the build carries are each stored as one column holding one
// value per line, the arrangement item's waits_on and environment's targets
// already have: an id is record.NewID's alphabet and a change's identity is a
// path element, neither of which holds a line ending, so the separator needs no
// escaping. It is a column rather than a table because what reads it reads all
// of one deploy's at once, and a table would be a row per edge for a list
// bounded by the window limit and the backlog cap.

func joinLines(values []string) string { return strings.Join(values, "\n") }

func splitLines(stored string) []string {
	if stored == "" {
		return nil
	}
	return strings.Split(stored, "\n")
}
