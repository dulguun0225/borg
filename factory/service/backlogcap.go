package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/gatepolicy"
)

// BacklogCapInForce is the backlog cap in force: the value an owner
// authored, or the window limit in force where they authored none — the
// backlog cap is the window limit until an owner separates the two.
func (s Service) BacklogCapInForce() float64 {
	return s.BacklogCap.Or(WindowLimitInForce(s.Parameters.WindowLimit))
}

// backlogCapFallback is the value a component actor's write of the backlog
// cap is enforced against where nothing is authored on it yet: the window
// limit in force, read fresh off the column rather than off a [Service]
// already in hand, because [enforceDirection] runs inside the same
// transaction the write commits in.
func backlogCapFallback(ctx context.Context, tx pgx.Tx, serviceID string) (float64, error) {
	var windowLimit *float64
	err := tx.QueryRow(ctx, `select window_limit from `+Table+` where id = $1`, serviceID).Scan(&windowLimit)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("%w: %s", ErrNotFound, serviceID)
	} else if err != nil {
		return 0, fmt.Errorf("service: reading the window limit in force on %s: %w", serviceID, err)
	}
	authored := gatepolicy.Authored{}
	if windowLimit != nil {
		authored = gatepolicy.Authored{Number: *windowLimit, Present: true}
	}
	return WindowLimitInForce(authored), nil
}
