package item

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/release"
)

// ShippedSiblings returns the live items of the original intent named by the
// release evidence, in the original decomposition order. It is the item
// reading that selects what a revert may create; the caller supplies the
// production environment and each service's production targets.
func ShippedSiblings(ctx context.Context, pool *pgxpool.Pool, releaseID, production string,
	addresses map[string][]string) ([]Item, error) {
	failed, err := release.Get(ctx, pool, releaseID)
	if err != nil {
		return nil, err
	}
	if failed.ItemID == "" {
		return nil, fmt.Errorf("item: release %s names no item", releaseID)
	}
	original, err := Get(ctx, pool, failed.ItemID)
	if err != nil {
		return nil, err
	}
	siblings, err := ForIntent(ctx, pool, original.IntentID)
	if err != nil {
		return nil, err
	}
	live, err := Live(ctx, pool, siblings, production, addresses)
	if err != nil {
		return nil, err
	}
	liveByID := make(map[string]bool, len(live))
	for _, id := range live {
		liveByID[id] = true
	}
	// The evidence release may have been rolled back, so its item is no longer
	// live even though it is the failed release that requires one revert item.
	liveByID[failed.ItemID] = true
	shipped := make([]Item, 0, len(live))
	for _, sibling := range siblings {
		if liveByID[sibling.ID] {
			shipped = append(shipped, sibling)
		}
	}
	return shipped, nil
}
