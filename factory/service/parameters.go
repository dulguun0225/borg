package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/gatepolicy"
)

// The service record's second writer is an owner at Factory, putting the window limit and the
// watch window's parameters on it, and the seam between the two writers is the
// field: decomposition writes the service's identity and never a parameter, an owner
// writes parameters and never creates a service. That is why these are functions
// taking a transaction rather than methods on [Writer], which is decomposition's.
//
// Each takes the transaction package policy appends the policy version in, so
// the field and the version commit together or not at all. Nothing reads any of
// the four yet: the window limit and the window's parameters are read by the
// watch window and
// the overlapping-windows limit, which are a later milestone, and authoring one
// here changes nothing until then. They are authorable now because the
// parameter is a field of this record, and a field of a record that has to exist
// before its mechanism does.

var (
	// ErrShareOutOfRange is returned for a window size or confidence outside
	// nothing to one. Both are shares.
	ErrShareOutOfRange = errors.New("service: a window size and a confidence are between 0 and 1")
	// ErrNotPositive is returned for a cap or a window limit that is not above
	// zero. A cap of nothing would end every window at the moment it opened, and
	// a window limit of nothing would let a service hold no window and so ship
	// nothing.
	ErrNotPositive = errors.New("service: a window cap and a window limit are above zero")
)

// SetWindowSize writes the smallest regression the comparison must rule out to
// close a window clean, as a share.
func SetWindowSize(ctx context.Context, tx pgx.Tx, serviceID string, size float64) error {
	if size <= 0 || size > 1 {
		return fmt.Errorf("%w: size %v", ErrShareOutOfRange, size)
	}
	return set(ctx, tx, serviceID, `window_size`, size)
}

// SetWindowConfidence writes how sure that comparison must be, as a share.
func SetWindowConfidence(ctx context.Context, tx pgx.Tx, serviceID string, confidence float64) error {
	if confidence <= 0 || confidence > 1 {
		return fmt.Errorf("%w: confidence %v", ErrShareOutOfRange, confidence)
	}
	return set(ctx, tx, serviceID, `window_confidence`, confidence)
}

// SetWindowCap writes the elapsed time in seconds that ends a window which will
// never reach its volume.
func SetWindowCap(ctx context.Context, tx pgx.Tx, serviceID string, seconds float64) error {
	if seconds <= 0 {
		return fmt.Errorf("%w: cap %v", ErrNotPositive, seconds)
	}
	return set(ctx, tx, serviceID, `window_cap_seconds`, seconds)
}

// SetWindowLimit writes how many watch windows this service may hold open at once.
func SetWindowLimit(ctx context.Context, tx pgx.Tx, serviceID string, limit float64) error {
	if limit <= 0 {
		return fmt.Errorf("%w: the window limit %v", ErrNotPositive, limit)
	}
	return set(ctx, tx, serviceID, `window_limit`, limit)
}

// set writes one authored column. The column name is a constant of this package
// and never input, so writing it into the statement is not a place anything can
// be injected.
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

// Parameters is the four an owner authors on a service, each present only where
// they authored one.
type Parameters struct {
	WindowSize       gatepolicy.Authored
	WindowConfidence gatepolicy.Authored
	WindowCapSeconds gatepolicy.Authored
	WindowLimit      gatepolicy.Authored
}
