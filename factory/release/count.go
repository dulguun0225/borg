package release

import (
	"context"
	"fmt"

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
