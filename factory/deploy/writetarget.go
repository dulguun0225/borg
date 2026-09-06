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
func (w *Writer) ReachTarget(ctx context.Context, id, address string) error {
	return w.updateTarget(ctx, id, address, "reaching", `update `+TargetTable+`
		set reached_at = $1 where deploy_id = $2 and address = $3`, record.Now())
}

// CompleteTarget marks the target complete after the call to it returned,
// naming what the seam reported: a drain, or a cut where the platform could not
// hold a request open across the replacement.
func (w *Writer) CompleteTarget(ctx context.Context, id, address string, replacement targetseam.Replacement) error {
	if replacement == "" {
		return fmt.Errorf("%w: target %s of %s reports no replacement", ErrTargetNotFound, address, id)
	}
	return w.updateTarget(ctx, id, address, "completing", `update `+TargetTable+`
		set completion = $1, replacement = $2, complete_at = $3
		where deploy_id = $4 and address = $5`,
		string(CompletionComplete), string(replacement), record.Now())
}

// updateTarget runs one write against one target's row, fenced, and refuses
// where the deploy has no row for that address.
func (w *Writer) updateTarget(ctx context.Context, id, address, doing, statement string, args ...any) error {
	if id == "" || address == "" {
		return fmt.Errorf("%w: %q of %q", ErrTargetNotFound, address, id)
	}
	return w.inTransaction(ctx, doing+" target "+address+" of "+id, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, statement, append(args, id, address)...)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
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
