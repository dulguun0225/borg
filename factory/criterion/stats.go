package criterion

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/record"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Withdrawal is one withdrawn criterion and the actor who wrote its
// withdrawal.
type Withdrawal struct {
	CriterionID string
	ServiceID   string
	Actor       record.Actor
}

// WithdrawalsForService reads withdrawals for a service's items, retaining
// the actor on the withdrawal rather than the actor that introduced the
// criterion.
func WithdrawalsForService(ctx context.Context, pool *pgxpool.Pool, serviceID string, itemIDs []string) ([]Withdrawal, error) {
	if serviceID == "" || len(itemIDs) == 0 {
		return nil, nil
	}
	rows, err := pool.Query(ctx, `select w.criterion_id, c.service_id, w.actor_kind, w.actor_key, w.actor_key_basis
		from `+WithdrawalTable+` w join `+Table+` c on c.id = w.criterion_id
		where c.service_id = $1 and w.item_id = any($2) order by w.at, w.criterion_id`, serviceID, itemIDs)
	if err != nil {
		return nil, fmt.Errorf("criterion: reading withdrawals of %s: %w", serviceID, err)
	}
	defer rows.Close()

	var read []Withdrawal
	for rows.Next() {
		var one Withdrawal
		var kind, basis string
		if err := rows.Scan(&one.CriterionID, &one.ServiceID, &kind, &one.Actor.Key, &basis); err != nil {
			return nil, fmt.Errorf("criterion: reading a withdrawal of %s: %w", serviceID, err)
		}
		one.Actor.Kind = record.Kind(kind)
		one.Actor.Basis = record.Basis(basis)
		read = append(read, one)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("criterion: reading withdrawals of %s: %w", serviceID, err)
	}
	return read, nil
}
