package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

// The change freeze: periods on one service within which its production deploys
// are held. It is periods and not one period, so each is a row of
// [ChangeFreezeTable] rather than a pair of columns, and a period authored is
// never edited into another — an owner adds one, and a safeguard may add one or
// lengthen one and may never shorten one.

var (
	// ErrPeriodNotATime is returned by [AddFreezePeriod] for a moment that is not
	// in the layout every stored timestamp takes. Two stored timestamps compare as
	// text because the layout is fixed width and always UTC, and a moment outside
	// it would compare against the periods wrongly rather than not at all.
	ErrPeriodNotATime = errors.New("service: a period's moments are written the way every record's timestamp is")
	// ErrPeriodEndsBeforeItStarts is returned by [AddFreezePeriod] for a period
	// whose last moment is before its first. It names no time at all, so no read
	// of it could hold a deploy.
	ErrPeriodEndsBeforeItStarts = errors.New("service: a period ends after it starts")
)

// storedTime is [record.TimePattern] as this package checks it, before the store
// does. The check is here as well so that the error a caller gets names the
// value rather than a constraint.
var storedTime = regexp.MustCompile(record.TimePattern)

// Period is one period of a change freeze: the first moment the freeze holds and
// the last, both in the layout every stored timestamp takes.
type Period struct {
	StartsAt string
	EndsAt   string
}

// AddFreezePeriod writes one period of this service's change freeze. It is
// authored outright with nothing supplied, ahead of what it is for: nothing the
// factory observes says when a customer's peak trading period or notified
// maintenance window falls.
//
// It takes the transaction package policy appends the policy version in and
// fences it with token first, the arrangement [SetWindowSize] already has: this
// inserts a record row of its own rather than updating a column of one. Adding
// the same period twice is the row already there and not a second row.
func AddFreezePeriod(ctx context.Context, tx pgx.Tx, token lease.Token, actor record.Actor,
	serviceID, startsAt, endsAt string) error {
	if err := lease.Fence(ctx, tx, token); err != nil {
		return err
	}
	if err := actor.Validate(); err != nil {
		return err
	}
	for _, moment := range []string{startsAt, endsAt} {
		if !storedTime.MatchString(moment) {
			return fmt.Errorf("%w: %q", ErrPeriodNotATime, moment)
		}
	}
	if endsAt < startsAt {
		return fmt.Errorf("%w: %s to %s", ErrPeriodEndsBeforeItStarts, startsAt, endsAt)
	}
	if err := mustExist(ctx, tx, serviceID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `insert into `+ChangeFreezeTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, service_id, starts_at, ends_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		on conflict (service_id, starts_at, ends_at) do nothing`,
		record.NewID(ChangeFreezeIDPrefix), FormatVersionChangeFreeze,
		string(actor.Kind), actor.Key, string(actor.Basis), record.Now(),
		serviceID, startsAt, endsAt,
	)
	if err != nil {
		return fmt.Errorf("service: authoring a change freeze period on %s: %w", serviceID, err)
	}
	return nil
}

// FreezePeriods is every period authored on one service, earliest first.
func FreezePeriods(ctx context.Context, pool *pgxpool.Pool, serviceID string) ([]Period, error) {
	rows, err := pool.Query(ctx, `select starts_at, ends_at from `+ChangeFreezeTable+`
		where service_id = $1 order by starts_at, ends_at`, serviceID)
	if err != nil {
		return nil, fmt.Errorf("service: reading the change freeze of %s: %w", serviceID, err)
	}
	defer rows.Close()

	var read []Period
	for rows.Next() {
		var p Period
		if err := rows.Scan(&p.StartsAt, &p.EndsAt); err != nil {
			return nil, fmt.Errorf("service: reading a change freeze period of %s: %w", serviceID, err)
		}
		read = append(read, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("service: reading the change freeze of %s: %w", serviceID, err)
	}
	return read, nil
}

// Frozen is whether one service's production deploys are held at that moment,
// and the period holding them where one is. A period holds from its first moment
// to its last, both included.
//
// It is a read and decides nothing: what holds a deploy is the gate row, which
// asks this at every firing, and the hold lifts itself when the period passes
// because the next firing reads a moment outside every period.
func Frozen(ctx context.Context, pool *pgxpool.Pool, serviceID, at string) (bool, Period, error) {
	var p Period
	err := pool.QueryRow(ctx, `select starts_at, ends_at from `+ChangeFreezeTable+`
		where service_id = $1 and starts_at <= $2 and ends_at >= $2
		order by starts_at limit 1`, serviceID, at).Scan(&p.StartsAt, &p.EndsAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, Period{}, nil
	} else if err != nil {
		return false, Period{}, fmt.Errorf("service: reading whether %s is frozen at %s: %w", serviceID, at, err)
	}
	return true, p, nil
}
