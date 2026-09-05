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
	// ErrSeverityNegative is returned for a severity below nothing. What the
	// scale is above that is the advisory feed's and not the factory's, so
	// nothing here bounds it from above.
	ErrSeverityNegative = errors.New("factorysettings: a severity is not below zero")
	// ErrPeriodNotPositive is returned by [SetRemediationPeriod] for a period
	// that is not above zero. A period of nothing would page at the raise, which
	// is the page the design refuses there.
	ErrPeriodNotPositive = errors.New("factorysettings: a remediation period is above zero")
)

// SetAdvisorySeverity writes the bound at or above which a matching advisory
// rejects at Implementation and holds at Deploy to production, inside tx. It is a
// field of this record because one pass over one feed reads it and it reaches
// every project at once.
func SetAdvisorySeverity(ctx context.Context, tx pgx.Tx, settingsID string, severity float64) error {
	if severity < 0 {
		return fmt.Errorf("%w: %v", ErrSeverityNegative, severity)
	}
	return update(ctx, tx, settingsID, `advisory_severity = $1`, severity)
}

// SetRemediationPeriod writes how long a matching advisory of one severity may
// stand before the intent it raised pages, inside tx. It is authored outright
// with nothing supplied: no outcome teaches how long a fix should take.
func SetRemediationPeriod(ctx context.Context, tx pgx.Tx, actor record.Actor, settingsID string,
	severity float64, seconds int64) error {
	if severity < 0 {
		return fmt.Errorf("%w: %v", ErrSeverityNegative, severity)
	}
	if seconds <= 0 {
		return fmt.Errorf("%w: %d", ErrPeriodNotPositive, seconds)
	}
	return insertKeyed(ctx, tx, actor, RemediationPeriodTable, RemediationPeriodIDPrefix,
		FormatVersionRemediationPeriod,
		`factory_settings_id, severity, period_seconds`, `$7, $8, $9`,
		`factory_settings_id, severity`, `period_seconds = excluded.period_seconds`,
		settingsID, severity, seconds)
}

// RemediationPeriod is the period an owner authored for one severity, and absent
// where they authored none.
func RemediationPeriod(ctx context.Context, pool *pgxpool.Pool, settingsID string,
	severity float64) (gatepolicy.Authored, error) {
	return keyedValue(ctx, pool, RemediationPeriodTable, "period_seconds", "severity", settingsID, severity)
}
