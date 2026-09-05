package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrShareOutOfRange is returned for a window size, a confidence, a power,
	// an objective, or an unreliable bound outside the range its own setter
	// states. All of them are shares.
	ErrShareOutOfRange = errors.New("service: this value is a share")
	// ErrNotPositive is returned for a cap, a window limit, a bound, or a
	// retention that is not above zero. A cap of nothing would end every window
	// at the moment it opened, and a window limit of nothing would let a service
	// hold no window and so ship nothing.
	ErrNotPositive = errors.New("service: this value is above zero")
	// ErrQuantityEmpty is returned by [SetWindowSize] and [SetWindowPower] for a
	// value authored against no quantity. The size and the power are one value
	// per quantity, so an authoring that names none names nothing.
	ErrQuantityEmpty = errors.New("service: the quantity is empty")
)

// Parameters is the gate-policy values an owner authors on a service, each
// present only where they authored one. The size and the power are per quantity;
// the other four are one value for the service.
//
// What is not here is every authored field of this record that is not one of
// gate policy's eleven rows — the mutant cap, the two other bounds, the
// retention, the objective, the paging hours, and the licence — which are fields
// of [Service] itself. doc.go says where each is defined.
type Parameters struct {
	WindowSize       map[gatepolicy.Quantity]gatepolicy.Authored
	WindowPower      map[gatepolicy.Quantity]gatepolicy.Authored
	WindowConfidence gatepolicy.Authored
	WindowCapSeconds gatepolicy.Authored
	WindowLimit      gatepolicy.Authored
	ExposureBound    gatepolicy.Authored
}

// WindowSizeFor is the size an owner authored for one quantity, and absent where
// they authored none — where the value in force is what the score supplies.
func (p Parameters) WindowSizeFor(q gatepolicy.Quantity) gatepolicy.Authored { return p.WindowSize[q] }

// WindowPowerFor is the power an owner authored for one quantity, absent the
// same way.
func (p Parameters) WindowPowerFor(q gatepolicy.Quantity) gatepolicy.Authored {
	return p.WindowPower[q]
}

// SetWindowSize writes the smallest regression the comparison must rule out to
// close a window passed on one quantity, as a share. It is a row of
// [WindowSizeTable] rather than a column, one per service and quantity, and
// re-authoring one is that row updated rather than a second row.
//
// It takes the transaction package policy appends the policy version in, so the
// value and the version commit together or not at all, and fences that
// transaction with token before it writes — the arrangement package
// environment's threshold write already has, and for the same reason: this
// inserts a record row of its own rather than updating a column of one.
func SetWindowSize(ctx context.Context, tx pgx.Tx, token lease.Token, actor record.Actor,
	serviceID string, quantity gatepolicy.Quantity, size float64) error {
	if size <= 0 || size > 1 {
		return fmt.Errorf("%w: size %v is between 0 and 1", ErrShareOutOfRange, size)
	}
	return setPerQuantity(ctx, tx, token, actor, WindowSizeTable, WindowSizeIDPrefix,
		FormatVersionWindowSize, "size", serviceID, quantity, size)
}

// SetWindowPower writes how reliably a regression of the size in force is caught
// rather than reaching passed, as a share, per quantity. It is one under one: a
// power of one is what no finite volume reaches.
func SetWindowPower(ctx context.Context, tx pgx.Tx, token lease.Token, actor record.Actor,
	serviceID string, quantity gatepolicy.Quantity, power float64) error {
	if power <= 0 || power >= 1 {
		return fmt.Errorf("%w: power %v is above 0 and under 1", ErrShareOutOfRange, power)
	}
	return setPerQuantity(ctx, tx, token, actor, WindowPowerTable, WindowPowerIDPrefix,
		FormatVersionWindowPower, "power", serviceID, quantity, power)
}

// setPerQuantity writes one row of one per-quantity table. The table, the column
// and the prefixes are constants of this package and never input, so writing
// them into the statement is not a place anything can be injected.
func setPerQuantity(ctx context.Context, tx pgx.Tx, token lease.Token, actor record.Actor,
	table, idPrefix, formatVersion, column, serviceID string, quantity gatepolicy.Quantity, value float64) error {
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
	_, err := tx.Exec(ctx, `insert into `+table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, service_id, quantity, `+column+`)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		on conflict (service_id, quantity) do update set `+column+` = excluded.`+column,
		record.NewID(idPrefix), formatVersion, string(actor.Kind), actor.Key, string(actor.Basis), record.Now(),
		serviceID, string(quantity), value,
	)
	if err != nil {
		return fmt.Errorf("service: authoring the %s for %s on %s: %w", column, quantity, serviceID, err)
	}
	return nil
}

// SetWindowConfidence writes how sure the comparison must be, as a share.
func SetWindowConfidence(ctx context.Context, tx pgx.Tx, serviceID string, confidence float64) error {
	if confidence <= 0 || confidence > 1 {
		return fmt.Errorf("%w: confidence %v is between 0 and 1", ErrShareOutOfRange, confidence)
	}
	return set(ctx, tx, serviceID, `window_confidence`, confidence)
}

// SetWindowCap writes the elapsed time in seconds that ends a window which will
// never reach its volume.
func SetWindowCap(ctx context.Context, tx pgx.Tx, serviceID string, seconds float64) error {
	if seconds <= 0 {
		return fmt.Errorf("%w: the cap %v", ErrNotPositive, seconds)
	}
	return set(ctx, tx, serviceID, `window_cap_seconds`, seconds)
}

// SetWindowLimit writes how many analysis windows this service may hold open at
// once.
func SetWindowLimit(ctx context.Context, tx pgx.Tx, serviceID string, limit float64) error {
	if limit <= 0 {
		return fmt.Errorf("%w: the window limit %v", ErrNotPositive, limit)
	}
	return set(ctx, tx, serviceID, `window_limit`, limit)
}

// SetExposureBound writes where the exposure factor stops being weighed and puts
// a human at Implementation instead.
func SetExposureBound(ctx context.Context, tx pgx.Tx, serviceID string, bound float64) error {
	if bound <= 0 {
		return fmt.Errorf("%w: the exposure bound %v", ErrNotPositive, bound)
	}
	return set(ctx, tx, serviceID, `exposure_bound`, bound)
}

// SetMutantCap writes how many mutants the mutation score may spend per item.
func SetMutantCap(ctx context.Context, tx pgx.Tx, serviceID string, cap float64) error {
	if cap <= 0 {
		return fmt.Errorf("%w: the mutant cap %v", ErrNotPositive, cap)
	}
	return set(ctx, tx, serviceID, `mutant_cap`, cap)
}

// SetFailureRecordKeyCap writes how many distinct keys a release may hold open
// per interval for its failure records.
func SetFailureRecordKeyCap(ctx context.Context, tx pgx.Tx, serviceID string, cap float64) error {
	if cap <= 0 {
		return fmt.Errorf("%w: the failure-record key cap %v", ErrNotPositive, cap)
	}
	return set(ctx, tx, serviceID, `failure_record_key_cap`, cap)
}

// SetUnreliableBound writes the rate of disagreement above which a criterion of
// this service is unreliable. It is a rate, so it is between nothing and one,
// and nothing is a real value: every disagreement takes a criterion out of the
// gate.
func SetUnreliableBound(ctx context.Context, tx pgx.Tx, serviceID string, bound float64) error {
	if bound < 0 || bound > 1 {
		return fmt.Errorf("%w: the unreliable bound %v is between 0 and 1", ErrShareOutOfRange, bound)
	}
	return set(ctx, tx, serviceID, `unreliable_bound`, bound)
}

// SetIncidentItemBound writes how long an incident-raised item may be worked
// before a human is reached, in seconds.
func SetIncidentItemBound(ctx context.Context, tx pgx.Tx, serviceID string, seconds float64) error {
	if seconds <= 0 {
		return fmt.Errorf("%w: the incident-raised item bound %v", ErrNotPositive, seconds)
	}
	return set(ctx, tx, serviceID, `incident_item_bound_seconds`, seconds)
}

// set writes one authored column of [Table]. The column name is a constant of
// this package and never input, so writing it into the statement is not a place
// anything can be injected. It takes the caller's transaction and no token: the
// caller is package policy, which fences the transaction it appends the policy
// version in before anything writes inside it.
func set(ctx context.Context, tx pgx.Tx, serviceID, column string, value float64) error {
	tag, err := tx.Exec(ctx, `update `+Table+` set `+column+` = $1 where id = $2`, value, serviceID)
	if err != nil {
		return fmt.Errorf("service: authoring %s on %s: %w", column, serviceID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, serviceID)
	}
	return nil
}

// mustExist is the check a write into one of the tables beside [Table] makes for
// itself. Those tables name a service and there are no foreign keys between
// record tables, so without it an authoring against a service nobody wrote would
// be a stored row naming nothing.
func mustExist(ctx context.Context, tx pgx.Tx, serviceID string) error {
	var one int
	err := tx.QueryRow(ctx, `select 1 from `+Table+` where id = $1`, serviceID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrNotFound, serviceID)
	} else if err != nil {
		return fmt.Errorf("service: reading %s: %w", serviceID, err)
	}
	return nil
}

// withQuantities fills a service's per-quantity sizes and powers, which are rows
// of two tables rather than columns of the service's own.
func withQuantities(ctx context.Context, pool *pgxpool.Pool, s Service) (Service, error) {
	var err error
	if s.Parameters.WindowSize, err = perQuantity(ctx, pool, WindowSizeTable, "size", s.ID); err != nil {
		return Service{}, err
	}
	if s.Parameters.WindowPower, err = perQuantity(ctx, pool, WindowPowerTable, "power", s.ID); err != nil {
		return Service{}, err
	}
	return s, nil
}

func perQuantity(ctx context.Context, pool *pgxpool.Pool, table, column, serviceID string) (map[gatepolicy.Quantity]gatepolicy.Authored, error) {
	rows, err := pool.Query(ctx, `select quantity, `+column+` from `+table+` where service_id = $1`, serviceID)
	if err != nil {
		return nil, fmt.Errorf("service: reading the %s of %s: %w", column, serviceID, err)
	}
	defer rows.Close()

	authoredPer := map[gatepolicy.Quantity]gatepolicy.Authored{}
	for rows.Next() {
		var quantity string
		var value float64
		if err := rows.Scan(&quantity, &value); err != nil {
			return nil, fmt.Errorf("service: reading the %s of %s: %w", column, serviceID, err)
		}
		authoredPer[gatepolicy.Quantity(quantity)] = gatepolicy.Authored{Number: value, Present: true}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("service: reading the %s of %s: %w", column, serviceID, err)
	}
	return authoredPer, nil
}
