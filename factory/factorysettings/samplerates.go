package factorysettings

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrRateOutOfRange is returned by [SetHeldOutSampleRate] and
	// [SetReviewSampleRate] for a rate outside nothing to one. Both are shares of
	// the firings they select from.
	ErrRateOutOfRange = errors.New("factorysettings: a sample rate is between 0 and 1")
	// ErrDutyOutOfRange is returned by [SetReviewSampleRate] for a duty outside
	// the owner's twelve, which are the factory's own the way a stage is.
	ErrDutyOutOfRange = errors.New("factorysettings: a duty is one of the owner's twelve")
)

// SetHeldOutSampleRate writes how often the score auto-passes a change it would
// have gated, inside tx. It is a field of this record because the sample is one
// formula's and no service's: what narrows it on a service, a project or an area
// is a safeguard.
func SetHeldOutSampleRate(ctx context.Context, tx pgx.Tx, settingsID string, rate float64) error {
	if rate < 0 || rate > 1 {
		return fmt.Errorf("%w: %v", ErrRateOutOfRange, rate)
	}
	return update(ctx, tx, settingsID, `held_out_sample_rate = $1`, rate)
}

// SetReviewSampleRate writes how often a change the score would have auto-passed
// is put in front of one duty's human anyway, inside tx. It is per duty, and a
// duty is the factory's own the way a stage is.
func SetReviewSampleRate(ctx context.Context, tx pgx.Tx, actor record.Actor, settingsID string,
	duty int, rate float64) error {
	if duty < 1 || duty > 12 {
		return fmt.Errorf("%w: %d", ErrDutyOutOfRange, duty)
	}
	if rate < 0 || rate > 1 {
		return fmt.Errorf("%w: %v", ErrRateOutOfRange, rate)
	}
	return insertKeyed(ctx, tx, actor, ReviewSampleRateTable, ReviewSampleRateIDPrefix,
		FormatVersionReviewSampleRate,
		`factory_settings_id, duty, rate`, `$7, $8, $9`,
		`factory_settings_id, duty`, `rate = excluded.rate`,
		settingsID, duty, rate)
}

// ReviewSampleRate is the rate an owner authored for one duty, and absent where
// they authored none.
func ReviewSampleRate(ctx context.Context, pool *pgxpool.Pool, settingsID string, duty int) (gatepolicy.Authored, error) {
	return keyedValue(ctx, pool, ReviewSampleRateTable, "rate", "duty", settingsID, duty)
}
