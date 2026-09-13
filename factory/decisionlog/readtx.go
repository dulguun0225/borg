package decisionlog

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/principal"
)

// WithByShape commits the read audit before opening a read-committed
// transaction for a read and the changes that depend on it. The audit remains
// even when a callback fails or the transaction rolls back. It needs at most
// one pool connection at a time.
//
// beforeRead takes the caller's lock before the query; use receives the
// resulting rows and makes the dependent writes in the same transaction.
// Either callback may be nil. Callbacks must not commit or roll back tx.
func (r *Reader) WithByShape(ctx context.Context, p principal.Principal, shape Shape,
	beforeRead func(context.Context, pgx.Tx) error,
	use func(context.Context, pgx.Tx, []Row) error,
) error {
	if err := r.appendReadEvent(ctx, p, string(shape)+" rows"); err != nil {
		return err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("decisionlog: beginning the read: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := lease.Fence(ctx, tx, r.token); err != nil {
		return err
	}
	if beforeRead != nil {
		if err := beforeRead(ctx, tx); err != nil {
			return err
		}
	}
	rows, err := tx.Query(ctx, `select `+selectColumns+` from `+Table+` where shape = $1 order by seq`, string(shape))
	if err != nil {
		return fmt.Errorf("decisionlog: reading %s rows: %w", shape, err)
	}
	var of []Row
	for rows.Next() {
		row, err := scan(rows)
		if err != nil {
			rows.Close()
			return err
		}
		of = append(of, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("decisionlog: reading %s rows: %w", shape, err)
	}
	if use != nil {
		if err := use(ctx, tx, of); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("decisionlog: committing the read: %w", err)
	}
	return nil
}
