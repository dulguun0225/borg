package factorysettings

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

var (
	// ErrRetentionNotPositive is returned for a retention that is not above zero.
	// Zero is not "keep nothing": where an owner authors none, the value in force
	// is the life of the install, and a retention of nothing would be a truncation
	// authored as a field.
	ErrRetentionNotPositive = errors.New("factorysettings: a retention is above zero")
	// ErrUnderTheRetentionFloor is returned by [SetDecisionLogRetention] for a
	// value under the retention floor, which neither an authored value nor a
	// safeguard may go under. The store refuses it as well.
	ErrUnderTheRetentionFloor = errors.New("factorysettings: decision-log retention does not go under the retention floor")
)

// SetDecisionLogRetention writes how long the decision log is kept, inside tx.
// Shortening it is decided rather than written: the gate row that decides a
// shortening approves it and this is what that row's approval calls, and
// lengthening is in force on write. The refusal here is the floor and not the
// gate, the gate being what the caller is.
func SetDecisionLogRetention(ctx context.Context, tx pgx.Tx, settingsID string, seconds int64) error {
	if seconds <= 0 {
		return fmt.Errorf("%w: %d", ErrRetentionNotPositive, seconds)
	}
	var floor *int64
	if err := tx.QueryRow(ctx, `select retention_floor_seconds from `+Table+` where id = $1`,
		settingsID).Scan(&floor); err != nil {
		return fmt.Errorf("%w: %s", ErrNotFound, settingsID)
	}
	if floor != nil && seconds < *floor {
		return fmt.Errorf("%w: %d under %d", ErrUnderTheRetentionFloor, seconds, *floor)
	}
	return update(ctx, tx, settingsID, `decision_log_retention_seconds = $1`, seconds)
}

// SetReportRetention writes how long the report store keeps a report, inside tx.
// Nothing reads it until the report store is built.
func SetReportRetention(ctx context.Context, tx pgx.Tx, settingsID string, seconds int64) error {
	if seconds <= 0 {
		return fmt.Errorf("%w: %d", ErrRetentionNotPositive, seconds)
	}
	return update(ctx, tx, settingsID, `report_retention_seconds = $1`, seconds)
}

// SetBackupRetention writes how far back a backup may reach, inside tx. It is
// authored outright with nothing supplied: no outcome teaches how far back a
// restore should reach. Its one reader is the erasure list's retirement, which is
// not built.
func SetBackupRetention(ctx context.Context, tx pgx.Tx, settingsID string, seconds int64) error {
	if seconds <= 0 {
		return fmt.Errorf("%w: %d", ErrRetentionNotPositive, seconds)
	}
	return update(ctx, tx, settingsID, `backup_retention_seconds = $1`, seconds)
}

// SetRetentionFloor writes how low an authored value or a safeguard may ever take
// decision-log retention, inside tx. It has two callers and no third: an owner
// at Factory, through package policy, and intake on the arrival of a
// records-retention constraint, which writes the floor at arrival instead of it
// being read at drafting. Intake's constraint kind is not built, so the second
// caller is absent and the first is what authors the floor the refusal above
// compares against.
func SetRetentionFloor(ctx context.Context, tx pgx.Tx, settingsID string, seconds int64) error {
	if seconds <= 0 {
		return fmt.Errorf("%w: %d", ErrRetentionNotPositive, seconds)
	}
	return update(ctx, tx, settingsID, `retention_floor_seconds = $1`, seconds)
}
