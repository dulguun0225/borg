package deploy

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// ReachTarget writes the target's row before the deployer calls that target,
// carrying the fencing token. A stalled deployer's claim is refused here, so it
// makes no call; one that lapsed mid-call completes nothing, because
// [Writer.CompleteTarget] carries the token too. That order is the whole of what
// bounds the far side of seam 4, which checks no token of its own.
//
// A repeat writes nothing. The step the restart must not repeat is keyed on the
// deploy record and the target, so a row already reached is left with the date
// it was first reached and no target is deployed to twice.
func (w *Writer) ReachTarget(ctx context.Context, id, address string) error {
	return w.updateTarget(ctx, id, address, "reaching", `update `+TargetTable+`
		set reached_at = $1 where deploy_id = $2 and address = $3 and reached_at = ''`, record.Now())
}

// CompleteTarget marks the target complete after the call to it returned,
// naming what the seam reported, which is the drain: neither rollout row drops a
// request.
//
// A repeat writes nothing, for the reason [Writer.ReachTarget]'s does, and a row
// a rollback has advanced to rolled back is not completed by a repeat of the
// deploy that put the build there: the write is guarded on the row being not
// reached.
func (w *Writer) CompleteTarget(ctx context.Context, id, address string, replacement targetseam.Replacement) error {
	if replacement == "" {
		return fmt.Errorf("%w: target %s of %s reports no replacement", ErrTargetNotFound, address, id)
	}
	return w.updateTarget(ctx, id, address, "completing", `update `+TargetTable+`
		set completion = $1, replacement = $2, complete_at = $3
		where deploy_id = $4 and address = $5 and completion = '`+string(CompletionNotReached)+`'`,
		string(CompletionComplete), string(replacement), record.Now())
}

// Control is what a control on one target runs: the release, which is what
// defines a control, the build that release is, and how many instances of it
// run there.
type Control struct {
	ReleaseID string
	BuildID   string
	Instances int
}

// ControlStarted names the control on one target of the deploy: there is one
// control per production target the release has reached, started on that target
// when the rollout reaches it, so this is written then and not at the start —
// a record naming a control on a target the rollout never reached would say a
// comparison ran where none did.
//
// A repeat writes nothing: the row keeps the control it was first given.
func (w *Writer) ControlStarted(ctx context.Context, id, address string, c Control) error {
	if c.ReleaseID == "" || c.BuildID == "" {
		return fmt.Errorf("%w: the control on %s of %s names release %q and build %q",
			ErrControlIncomplete, address, id, c.ReleaseID, c.BuildID)
	}
	return w.updateTarget(ctx, id, address, "naming the control on", `update `+TargetTable+`
		set control_release_id = $1, control_build_id = $2, control_instances = $3
		where deploy_id = $4 and address = $5 and control_release_id = ''`,
		c.ReleaseID, c.BuildID, c.Instances)
}

// updateTarget runs one write against one target's row, fenced, and refuses
// where the deploy has no row for that address.
//
// A statement that wrote nothing is one of two things, and the row is what tells
// them apart: no such row, which is [ErrTargetNotFound], or a row the write is
// guarded against — already reached, already complete, already naming its
// control — which is the repeat that writes nothing and is not an error. The
// restart is what makes that distinction worth drawing: a repeat of a reach or a
// completion is what a deployer that stopped mid-walk leaves for the next one.
func (w *Writer) updateTarget(ctx context.Context, id, address, doing, statement string, args ...any) error {
	if id == "" || address == "" {
		return fmt.Errorf("%w: %q of %q", ErrTargetNotFound, address, id)
	}
	return w.inTransaction(ctx, doing+" target "+address+" of "+id, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, statement, append(args, id, address)...)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 0 {
			return nil
		}
		var rows int
		err = tx.QueryRow(ctx, `select count(*) from `+TargetTable+`
			where deploy_id = $1 and address = $2`, id, address).Scan(&rows)
		if err != nil {
			return err
		}
		if rows == 0 {
			return fmt.Errorf("%w: %s of %s", ErrTargetNotFound, address, id)
		}
		return nil
	})
}

// UndoTarget marks one target of a deploy rolled back, which is how a rollback
// advances the deploys it undoes as it completes on each target: a rollback that
// stopped undoes nothing on the record beyond the targets it reached. It is what
// happens to the deploy of the failed release and to the deploy of every release
// the same rollback skipped, one target at a time.
//
// It takes no source, where the rollback's own record does. The source is a fact
// of the rollback and is written once, on the record of the rollback that named
// it — so a reader asking why a deploy was undone follows the rollback rather
// than finding the reason copied onto every deploy the same event touched.
//
// A deploy with no row for that address is [ErrTargetNotFound], which the
// rollback reads as a deploy that never reached the target it has just finished
// with: there is nothing there to undo.
func (w *Writer) UndoTarget(ctx context.Context, id, address string) error {
	return w.updateTarget(ctx, id, address, "rolling back", `update `+TargetTable+`
		set completion = $1 where deploy_id = $2 and address = $3`, string(CompletionRolledBack))
}
