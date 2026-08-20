package release

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The two counts the score reads. Both take an item to leave out, because both
// factors they feed are about what surrounds a change and not about the change:
// area churn is what else has been shipping in the area, and what a release can
// be rolled back to is a release other than its own. At the production deploy
// row the item's release already exists, so a count that included it would have
// every change reading as its own churn and as its own rollback target.

// CountForItemsSince is how many releases were minted for any of these items at
// or after since, leaving out the releases of exceptItemID. The timestamp is
// compared as text, which is an ordering because record.TimeLayout is fixed
// width and always UTC.
//
// No items is none and no error: an area with no items has had nothing shipped
// in it, which is a reading and not a missing one.
func CountForItemsSince(ctx context.Context, pool *pgxpool.Pool, itemIDs []string, exceptItemID, since string) (int, error) {
	if len(itemIDs) == 0 {
		return 0, nil
	}
	var count int
	err := pool.QueryRow(ctx, `select count(*) from `+Table+`
		where item_id = any($1) and item_id <> $2 and at >= $3`,
		itemIDs, exceptItemID, since).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("release: counting the releases of %d items since %s: %w", len(itemIDs), since, err)
	}
	return count, nil
}

// Highest is the service's highest-numbered release, and false where it has
// none. It is master's head: the commit of that release, reached through the
// build it names. Numbers are never reused and a rolled-back release keeps its
// own, so the maximum is always right, and a counter beside it would be the same
// fact written twice at one event.
//
// It takes the pool and not a [Writer], because reading what master is at is not
// a reason to be handed the thing that mints.
func Highest(ctx context.Context, pool *pgxpool.Pool, serviceID string) (Release, bool, error) {
	r, err := scan(pool.QueryRow(ctx, selectRelease+` where service_id = $1
		order by number desc limit 1`, serviceID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Release{}, false, nil
	} else if err != nil {
		return Release{}, false, fmt.Errorf("release: reading the highest number of %s: %w", serviceID, err)
	}
	return r, true, nil
}

// ForItem is the release minted for one item, and false where none was. One item
// is one release, so there is at most one.
//
// It is what says a fast-forward already happened. The fast-forward, the mint, and
// the item's advance to merged are three writes across a repository and two
// transactions, and a failure after the mint leaves an item that has a release and
// does not say it merged — so the caller asks this before minting rather than
// minting a second number for one merge.
func ForItem(ctx context.Context, pool *pgxpool.Pool, itemID string) (Release, bool, error) {
	if itemID == "" {
		return Release{}, false, nil
	}
	r, err := scan(pool.QueryRow(ctx, selectRelease+` where item_id = $1`, itemID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Release{}, false, nil
	} else if err != nil {
		return Release{}, false, fmt.Errorf("release: reading the release of %s: %w", itemID, err)
	}
	return r, true, nil
}

// Above is every release of the service numbered above number, lowest first. It
// is what a rollback sweeps: master is linear, so the release above a bad one
// includes it, and returning to a target below the bad one undoes every release
// above that target.
//
// It answers what is above and not how far above. How many a rollback may sweep is
// bounded by K, which is how many watch windows a service may hold open at once and
// is a parameter of an owner's rather than anything this package reads.
func Above(ctx context.Context, pool *pgxpool.Pool, serviceID string, number int64) ([]Release, error) {
	rows, err := pool.Query(ctx, selectRelease+`
		where service_id = $1 and number > $2 order by number`, serviceID, number)
	if err != nil {
		return nil, fmt.Errorf("release: reading the releases of %s above %d: %w", serviceID, number, err)
	}
	defer rows.Close()

	var read []Release
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("release: reading a release of %s above %d: %w", serviceID, number, err)
		}
		read = append(read, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("release: reading the releases of %s above %d: %w", serviceID, number, err)
	}
	return read, nil
}

// Below is whether the service has any release numbered below number. It is what
// says whether the clean exit is available to a window at all: a release with
// nothing below it has no baseline to be compared against, so nothing about it is
// ruled out by watching, and the design gives the choice of a control from the
// second release onward.
func Below(ctx context.Context, pool *pgxpool.Pool, serviceID string, number int64) (bool, error) {
	var count int
	err := pool.QueryRow(ctx, `select count(*) from `+Table+`
		where service_id = $1 and number < $2`, serviceID, number).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("release: counting the releases of %s below %d: %w", serviceID, number, err)
	}
	return count > 0, nil
}

// CountForService is how many releases the service has, leaving out the
// releases of exceptItemID.
func CountForService(ctx context.Context, pool *pgxpool.Pool, serviceID, exceptItemID string) (int, error) {
	var count int
	err := pool.QueryRow(ctx, `select count(*) from `+Table+`
		where service_id = $1 and item_id <> $2`, serviceID, exceptItemID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("release: counting the releases of %s: %w", serviceID, err)
	}
	return count, nil
}

// Between is every release of the service numbered from lowest to highest
// inclusive, lowest first. It is what the range of declarations in force is
// resolved through: the range is the service's restore floor up to its newest
// release, and the items of those releases are the ones whose declarations bind.
//
// A lowest above the highest is no releases and no error, which is the answer for
// a range that has nothing in it.
func Between(ctx context.Context, pool *pgxpool.Pool, serviceID string, lowest, highest int64) ([]Release, error) {
	rows, err := pool.Query(ctx, selectRelease+`
		where service_id = $1 and number >= $2 and number <= $3 order by number`,
		serviceID, lowest, highest)
	if err != nil {
		return nil, fmt.Errorf("release: reading the releases of %s from %d to %d: %w",
			serviceID, lowest, highest, err)
	}
	defer rows.Close()

	var read []Release
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("release: reading a release of %s from %d to %d: %w",
				serviceID, lowest, highest, err)
		}
		read = append(read, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("release: reading the releases of %s from %d to %d: %w",
			serviceID, lowest, highest, err)
	}
	return read, nil
}

// ItemsWithRelease is which of these items have a release, in the order given. It
// is what filters a declaration derived for a candidate that never merged: a
// declaration is written at the implementation stage, before any release exists, so
// what says one is a release's is the release naming the same item.
//
// No items is none and no error.
func ItemsWithRelease(ctx context.Context, pool *pgxpool.Pool, itemIDs []string) ([]string, error) {
	if len(itemIDs) == 0 {
		return nil, nil
	}
	rows, err := pool.Query(ctx, `select item_id from `+Table+` where item_id = any($1)`, itemIDs)
	if err != nil {
		return nil, fmt.Errorf("release: reading which of %d items have a release: %w", len(itemIDs), err)
	}
	defer rows.Close()

	have := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("release: reading an item that has a release: %w", err)
		}
		have[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("release: reading which of %d items have a release: %w", len(itemIDs), err)
	}
	var released []string
	for _, id := range itemIDs {
		if have[id] {
			released = append(released, id)
		}
	}
	return released, nil
}
