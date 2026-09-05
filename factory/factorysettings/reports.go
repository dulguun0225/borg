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

// DefaultHarmMarkPageCap is how many intents one service's marked reports may
// page per [DefaultHarmMarkPageInterval] where an owner has authored no cap. It is
// shipped rather than supplied: no outcome teaches it, and the cap exists because
// the report channel's own rates are keyed per source, which is a session a
// stranger renews at no cost.
const (
	DefaultHarmMarkPageCap      = 3
	DefaultHarmMarkPageInterval = int64(24 * 60 * 60)
)

var (
	// ErrRateNegative is returned by the report channel's rate calls for a rate
	// below nothing. Zero is a rate an owner may mean: it closes one service's way
	// in, which the design names as the far end of a safeguard on the channel.
	ErrRateNegative = errors.New("factorysettings: a report channel rate is not below zero")
	// ErrPageCapNegative is returned by [SetHarmMarkPageCap] for a cap below
	// nothing. Zero is a cap an owner may mean: past it a marked intent waits at
	// Work and one page per interval goes out instead.
	ErrPageCapNegative = errors.New("factorysettings: a page cap is not below zero")
	// ErrIntervalNotPositive is returned by [SetHarmMarkPageCap] for an interval
	// that is not above zero, there being nothing to count a cap over.
	ErrIntervalNotPositive = errors.New("factorysettings: the interval a page cap is counted over is above zero")
	// ErrServiceEmpty is returned by the two per-service calls for a row naming
	// no service.
	ErrServiceEmpty = errors.New("factorysettings: a per-service row names the service it is for")
)

// SetReportChannelRate writes the factory-wide rate bounding arrival at the way
// in, inside tx. Where an owner authors none, arrival is unbounded: no outcome
// teaches a rate, so there is nothing for the score to supply.
func SetReportChannelRate(ctx context.Context, tx pgx.Tx, settingsID string, rate int64) error {
	if rate < 0 {
		return fmt.Errorf("%w: %d", ErrRateNegative, rate)
	}
	return update(ctx, tx, settingsID, `report_channel_rate = $1`, rate)
}

// SetServiceReportChannelRate writes the rate for one service, inside tx. It is
// the second of the report channel's two rates, and a field of this record rather
// than of the service's, both being authored here beside retention.
func SetServiceReportChannelRate(ctx context.Context, tx pgx.Tx, actor record.Actor, settingsID,
	serviceID string, rate int64) error {
	if serviceID == "" {
		return ErrServiceEmpty
	}
	if rate < 0 {
		return fmt.Errorf("%w: %d", ErrRateNegative, rate)
	}
	return insertKeyed(ctx, tx, actor, ReportChannelRateTable, ReportChannelRateIDPrefix,
		FormatVersionReportChannelRate,
		`factory_settings_id, service_id, rate`, `$7, $8, $9`,
		`factory_settings_id, service_id`, `rate = excluded.rate`,
		settingsID, serviceID, rate)
}

// ReportChannelRate is the rate an owner authored for one service, and absent
// where they authored none — where the factory-wide rate on the record is what
// bounds arrival.
func ReportChannelRate(ctx context.Context, pool *pgxpool.Pool, settingsID,
	serviceID string) (gatepolicy.Authored, error) {
	return keyedValue(ctx, pool, ReportChannelRateTable, "rate", "service_id", settingsID, serviceID)
}

// PageCap is the harm mark's cap for one service: how many intents that service's
// marked reports may page, and the interval it is counted over. Where an owner
// authored none, it is [DefaultHarmMarkPageCap] over
// [DefaultHarmMarkPageInterval], which the product ships.
type PageCap struct {
	Cap             int
	IntervalSeconds int64
	// Authored is whether an owner authored this cap. Absent is the shipped
	// default and not nothing, which is what makes a cap of zero readable as an
	// owner's decision.
	Authored bool
}

// SetHarmMarkPageCap writes one service's cap and the interval it is counted
// over, inside tx. An owner may lower it and a safeguard may lower it and never
// raise it, which is the direction package gatepolicy holds for the parameter.
func SetHarmMarkPageCap(ctx context.Context, tx pgx.Tx, actor record.Actor, settingsID,
	serviceID string, pageCap int, intervalSeconds int64) error {
	if serviceID == "" {
		return ErrServiceEmpty
	}
	if pageCap < 0 {
		return fmt.Errorf("%w: %d", ErrPageCapNegative, pageCap)
	}
	if intervalSeconds <= 0 {
		return fmt.Errorf("%w: %d", ErrIntervalNotPositive, intervalSeconds)
	}
	return insertKeyed(ctx, tx, actor, PageCapTable, PageCapIDPrefix, FormatVersionPageCap,
		`factory_settings_id, service_id, page_cap, interval_seconds`, `$7, $8, $9, $10`,
		`factory_settings_id, service_id`,
		`page_cap = excluded.page_cap, interval_seconds = excluded.interval_seconds`,
		settingsID, serviceID, pageCap, intervalSeconds)
}

// HarmMarkPageCap is the cap in force for one service: what an owner authored, or
// the shipped default where they authored none.
func HarmMarkPageCap(ctx context.Context, pool *pgxpool.Pool, settingsID, serviceID string) (PageCap, error) {
	shipped := PageCap{Cap: DefaultHarmMarkPageCap, IntervalSeconds: DefaultHarmMarkPageInterval}
	var read PageCap
	err := pool.QueryRow(ctx, `select page_cap, interval_seconds from `+PageCapTable+`
		where factory_settings_id = $1 and service_id = $2`, settingsID, serviceID).
		Scan(&read.Cap, &read.IntervalSeconds)
	if errors.Is(err, pgx.ErrNoRows) {
		return shipped, nil
	} else if err != nil {
		return PageCap{}, fmt.Errorf("factorysettings: reading the page cap of %s: %w", serviceID, err)
	}
	read.Authored = true
	return read, nil
}

// SetHarmMarkPages writes whether a report marked as describing harm to a person
// pages at all, inside tx. It ships on, so an owner who will not be woken by a
// stranger turns it off — which is a field and not a safeguard, a safeguard being
// what only adds protection.
func SetHarmMarkPages(ctx context.Context, tx pgx.Tx, settingsID string, pages bool) error {
	return update(ctx, tx, settingsID, `harm_mark_pages = $1`, pages)
}
