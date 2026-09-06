package service

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

// Threshold is an explicit threshold on one quantity: the absolute number the
// quantity is read against, and the smallest change from it worth catching. The
// two are authored together — the owner set the size when they set the number —
// and the run length it is read at is supplied where they authored none.
//
// Absolute is what the number is and not what the reading is: the quantity
// tested against it is counted from finite traffic, so it is read against a
// boundary of its own on the same terms as the reading against a service's own
// recent history.
type Threshold struct {
	Number float64
	Size   float64
}

// SetExplicitThreshold writes the absolute number one quantity of this service
// is read against, and the size beside it. It is what a safeguard sets: an
// explicit threshold applies in addition to the comparison rather than instead
// of it, so the release passes both or neither and a safeguard here can only add
// a check.
//
// It is a row of [ExplicitThresholdTable] rather than a column, one per service
// and quantity, and re-authoring one is that row updated rather than a second
// row. It takes the transaction package policy appends the policy version in and
// fences it with token first, the arrangement [SetWindowSize] already has.
func SetExplicitThreshold(ctx context.Context, tx pgx.Tx, token lease.Token, actor record.Actor,
	serviceID string, quantity gatepolicy.Quantity, threshold, size float64) error {
	if threshold < 0 || threshold > 1 {
		return fmt.Errorf("%w: the threshold %v is between 0 and 1", ErrShareOutOfRange, threshold)
	}
	if size <= 0 || size > 1 {
		return fmt.Errorf("%w: the threshold's size %v is above 0 and at most 1", ErrShareOutOfRange, size)
	}
	if err := lease.Fence(ctx, tx, token); err != nil {
		return err
	}
	if err := actor.Validate(); err != nil {
		return err
	}
	if quantity == "" {
		return ErrQuantityEmpty
	}
	if err := mustExist(ctx, tx, serviceID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `insert into `+ExplicitThresholdTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, service_id, quantity, threshold, size)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		on conflict (service_id, quantity) do update set
			threshold = excluded.threshold, size = excluded.size`,
		record.NewID(ExplicitThresholdIDPrefix), FormatVersionExplicitThreshold,
		string(actor.Kind), actor.Key, string(actor.Basis), record.Now(),
		serviceID, string(quantity), threshold, size,
	)
	if err != nil {
		return fmt.Errorf("service: authoring the explicit threshold for %s on %s: %w", quantity, serviceID, err)
	}
	return nil
}

// SetRecentHistorySize writes the smallest change in one quantity the reading
// against this service's own recent history has to detect, as a share. It is one
// value per quantity, as the window's own size is, and the average run length
// that reading is taken at is one number for the service.
//
// It is a row of [RecentHistorySizeTable] rather than a column, and it takes the
// transaction package policy appends the policy version in and fences it with
// token first, the arrangement [SetWindowSize] already has.
func SetRecentHistorySize(ctx context.Context, tx pgx.Tx, token lease.Token, actor record.Actor,
	serviceID string, quantity gatepolicy.Quantity, size float64) error {
	if size <= 0 || size > 1 {
		return fmt.Errorf("%w: size %v is between 0 and 1", ErrShareOutOfRange, size)
	}
	return setPerQuantity(ctx, tx, token, actor, RecentHistorySizeTable, RecentHistorySizeIDPrefix,
		FormatVersionRecentHistorySize, "size", serviceID, quantity, size)
}

// SetOperationCap writes how many operations one release may hold open per
// interval and the name the excess lands in. The two are one write because a cap
// with no overflow operation would truncate the count and leave nowhere for the
// rest to land, and a service that names an operation per identifier would grow
// the store without bound.
func SetOperationCap(ctx context.Context, tx pgx.Tx, serviceID string, operationCap float64, overflow string) error {
	if operationCap <= 0 {
		return fmt.Errorf("%w: the operation cap %v", ErrNotPositive, operationCap)
	}
	if overflow == "" {
		return ErrOverflowOperationEmpty
	}
	tag, err := tx.Exec(ctx, `update `+Table+`
		set operation_cap = $1, overflow_operation = $2 where id = $3`, operationCap, overflow, serviceID)
	if err != nil {
		return fmt.Errorf("service: authoring the operation cap on %s: %w", serviceID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, serviceID)
	}
	return nil
}

// SetEnvironmentHourRate writes what one environment-hour converts to, in the
// currency the owner's rates are authored in. It is the second of the two rates
// that price the hosting a candidate and a kept fleet consume outside the
// factory; a rate of nothing is a real value and is admitted, where an absent
// rate leaves those figures units only.
func SetEnvironmentHourRate(ctx context.Context, tx pgx.Tx, serviceID string, rate float64) error {
	if rate < 0 {
		return fmt.Errorf("%w: the environment-hour rate %v", ErrRateNegative, rate)
	}
	return set(ctx, tx, serviceID, `environment_hour_rate`, rate)
}

// SetSearchBudget writes what a search may spend before it stops: a maximum
// count of builds and a maximum total time production spends on them. Each build
// the search deploys puts a build that passed no gate in front of real traffic,
// which is what the budget bounds; a safeguard may lower it and never raise it.
func SetSearchBudget(ctx context.Context, tx pgx.Tx, serviceID string, builds, seconds float64) error {
	if builds <= 0 {
		return fmt.Errorf("%w: the search budget's build count %v", ErrNotPositive, builds)
	}
	if seconds <= 0 {
		return fmt.Errorf("%w: the search budget's time %v", ErrNotPositive, seconds)
	}
	tag, err := tx.Exec(ctx, `update `+Table+`
		set search_budget_builds = $1, search_budget_seconds = $2 where id = $3`, builds, seconds, serviceID)
	if err != nil {
		return fmt.Errorf("service: authoring the search budget on %s: %w", serviceID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, serviceID)
	}
	return nil
}

// explicitThresholds is every threshold a safeguard set on one service, keyed by
// quantity. It is read into the record beside the sizes and the powers, so a
// caller holding a [Service] holds every authored value on it.
func explicitThresholds(ctx context.Context, pool *pgxpool.Pool, serviceID string) (map[gatepolicy.Quantity]Threshold, error) {
	rows, err := pool.Query(ctx, `select quantity, threshold, size from `+ExplicitThresholdTable+`
		where service_id = $1`, serviceID)
	if err != nil {
		return nil, fmt.Errorf("service: reading the explicit thresholds of %s: %w", serviceID, err)
	}
	defer rows.Close()

	read := map[gatepolicy.Quantity]Threshold{}
	for rows.Next() {
		var quantity string
		var t Threshold
		if err := rows.Scan(&quantity, &t.Number, &t.Size); err != nil {
			return nil, fmt.Errorf("service: reading an explicit threshold of %s: %w", serviceID, err)
		}
		read[gatepolicy.Quantity(quantity)] = t
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("service: reading the explicit thresholds of %s: %w", serviceID, err)
	}
	return read, nil
}
