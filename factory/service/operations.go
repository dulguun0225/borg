package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/gatepolicy"
)

// Objective is the service level objective an owner authored: the proportion of
// a quantity that must be good, and the period it is read over. The two are
// authored together and are absent together — where an owner authors none there
// is no error budget and nothing is held.
type Objective struct {
	Target        gatepolicy.Authored
	PeriodSeconds gatepolicy.Authored
}

// Authored reports whether an owner authored one.
func (o Objective) Authored() bool { return o.Target.Present }

// PagingHours is the hours within which this service pages, and the time zone
// they were written in, which every authored calendar value carries. Where an
// owner authors none the service pages at any hour.
//
// The two hours are stored as they were written, as HH:MM, and the zone as the
// name the owner gave. Nothing here resolves the zone to an offset: what reads
// the hours is the notifier, which is where a wait of the second kind is held to
// the next hour the service allows.
type PagingHours struct {
	Start string
	End   string
	Zone  string
}

// Authored reports whether an owner authored them.
func (p PagingHours) Authored() bool { return p.Zone != "" }

var (
	// ErrHourFormat is returned by [SetPagingHours] for an hour that is not
	// HH:MM.
	ErrHourFormat = errors.New("service: an hour is HH:MM")
	// ErrZoneEmpty is returned by [SetPagingHours] where no zone is given. The
	// hours carry the zone they were read in, so hours without one are hours a
	// holder in another zone cannot render.
	ErrZoneEmpty = errors.New("service: the paging hours name no time zone")
	// ErrLicenceEmpty is returned by [SetProductLicence] for an empty licence.
	ErrLicenceEmpty = errors.New("service: the product licence is empty")
)

// SetObjective writes the proportion of a quantity that must be good over a
// stated period, and the period with it. The two are one write because an
// objective without its period states nothing.
func SetObjective(ctx context.Context, tx pgx.Tx, serviceID string, target, periodSeconds float64) error {
	if target <= 0 || target > 1 {
		return fmt.Errorf("%w: the objective %v is between 0 and 1", ErrShareOutOfRange, target)
	}
	if periodSeconds <= 0 {
		return fmt.Errorf("%w: the objective's period %v", ErrNotPositive, periodSeconds)
	}
	tag, err := tx.Exec(ctx, `update `+Table+`
		set objective = $1, objective_period_seconds = $2 where id = $3`,
		target, periodSeconds, serviceID)
	if err != nil {
		return fmt.Errorf("service: authoring the objective on %s: %w", serviceID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, serviceID)
	}
	return nil
}

// SetPagingHours writes the hours within which this service pages, with the zone
// they were written in. The three are one write for the reason the objective and
// its period are.
func SetPagingHours(ctx context.Context, tx pgx.Tx, serviceID string, hours PagingHours) error {
	for _, hour := range []string{hours.Start, hours.End} {
		if err := checkHour(hour); err != nil {
			return err
		}
	}
	if hours.Zone == "" {
		return ErrZoneEmpty
	}
	tag, err := tx.Exec(ctx, `update `+Table+`
		set paging_hours_start = $1, paging_hours_end = $2, paging_hours_zone = $3 where id = $4`,
		hours.Start, hours.End, hours.Zone, serviceID)
	if err != nil {
		return fmt.Errorf("service: authoring the paging hours on %s: %w", serviceID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, serviceID)
	}
	return nil
}

// checkHour is the shape an hour is stored in, checked here rather than left to
// the notifier: a value nothing can parse is a page that never fires.
func checkHour(hour string) error {
	h, m, ok := strings.Cut(hour, ":")
	if !ok || len(h) != 2 || len(m) != 2 {
		return fmt.Errorf("%w: %q", ErrHourFormat, hour)
	}
	hours, err := strconv.Atoi(h)
	if err != nil || hours < 0 || hours > 23 {
		return fmt.Errorf("%w: %q", ErrHourFormat, hour)
	}
	minutes, err := strconv.Atoi(m)
	if err != nil || minutes < 0 || minutes > 59 {
		return fmt.Errorf("%w: %q", ErrHourFormat, hour)
	}
	return nil
}

// SetProductLicence writes the licence the service's software ships under, so
// that a licence policy constraint has it to author against. Decomposition never
// writes it.
func SetProductLicence(ctx context.Context, tx pgx.Tx, serviceID, licence string) error {
	if licence == "" {
		return ErrLicenceEmpty
	}
	tag, err := tx.Exec(ctx, `update `+Table+` set product_licence = $1 where id = $2`, licence, serviceID)
	if err != nil {
		return fmt.Errorf("service: authoring the product licence on %s: %w", serviceID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, serviceID)
	}
	return nil
}

// SetSnapshotRetention writes how long a schema-change snapshot is kept, in
// seconds. Where an owner authors none, a snapshot stands until an owner deletes
// it, so an absent value is not a retention of nothing.
func SetSnapshotRetention(ctx context.Context, tx pgx.Tx, serviceID string, seconds float64) error {
	if seconds <= 0 {
		return fmt.Errorf("%w: the snapshot retention %v", ErrNotPositive, seconds)
	}
	return set(ctx, tx, serviceID, `snapshot_retention_seconds`, seconds)
}
