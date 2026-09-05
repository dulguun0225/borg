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

// TearDownKept closes the kept fleet's span on one target: when it was torn
// down, the hours those instances ran, and what that converted to at the rate in
// force at this write. The amount is fixed here and never repriced by a rate
// corrected later; where no rate was in force, priced.InForce is false and the
// columns stay null, which is not an amount of zero.
func (w *Writer) TearDownKept(ctx context.Context, id, address string, hours float64, priced Priced) error {
	var amount, rate any
	if priced.InForce {
		amount, rate = priced.Amount, priced.Rate
	}
	return w.updateTarget(ctx, id, address, "tearing down the kept instances of", `update `+TargetTable+`
		set replaced_at = $1, instance_hours = $2, amount = $3, rate = $4
		where deploy_id = $5 and address = $6`, record.Now(), hours, amount, rate)
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

// Undo marks every target of a deploy rolled back, a target the release never
// reached included, which is what the record of the rollback that undid it
// advances as it completes on each. It is what happens to the deploy of the
// failed release and to the deploy of every release the same rollback skipped.
//
// It takes no source, where the rollback's own record does. The source is a fact
// of the rollback and is written once, on the record of the rollback that named
// it — so a reader asking why a deploy was undone follows the rollback rather
// than finding the reason copied onto every deploy the same event touched.
func (w *Writer) Undo(ctx context.Context, id string) error {
	return w.inTransaction(ctx, "rolling back "+id, func(tx pgx.Tx) error {
		if _, err := lockStatus(ctx, tx, id); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `update `+TargetTable+` set completion = $1 where deploy_id = $2`,
			string(CompletionRolledBack), id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("%w: %s has no target", ErrTargetNotFound, id)
		}
		return nil
	})
}

// UndoTarget marks one target of a deploy rolled back, which is how a rollback
// advances the deploys it undoes as it completes on each target: a rollback that
// stopped undoes nothing on the record beyond the targets it reached.
func (w *Writer) UndoTarget(ctx context.Context, id, address string) error {
	return w.updateTarget(ctx, id, address, "rolling back", `update `+TargetTable+`
		set completion = $1 where deploy_id = $2 and address = $3`, string(CompletionRolledBack))
}
