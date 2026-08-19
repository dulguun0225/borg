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
