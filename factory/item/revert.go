package item

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/record"
)

// RevertItem is one item the revert creates for one shipped sibling. The
// project ids are the caller's package-owned readings used by decomposition's
// area check.
type RevertItem struct {
	New              New
	AreaProjectID    string
	ServiceProjectID string
}

// CreateReverts creates every revert item in one transaction. Each input is one
// shipped sibling, and every item must name the same revert intent; a single
// undo item is not a shape this writer accepts.
func (c *Decomposition) CreateReverts(ctx context.Context, actor record.Actor, items []RevertItem) ([]Item, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("item: a revert names no shipped sibling")
	}
	intentID := items[0].New.IntentID
	if intentID == "" {
		return nil, ErrIntentIDEmpty
	}
	seen := make(map[string]bool, len(items))
	for _, one := range items {
		if one.New.IntentID != intentID {
			return nil, fmt.Errorf("item: revert items name intents %q and %q", intentID, one.New.IntentID)
		}
		if one.New.ID != "" && seen[one.New.ID] {
			return nil, fmt.Errorf("item: revert names item %s more than once", one.New.ID)
		}
		seen[one.New.ID] = true
	}

	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("item: beginning the revert of %s: %w", intentID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	created := make([]Item, 0, len(items))
	for _, one := range items {
		createdOne, err := c.CreateTx(ctx, tx, actor, one.New, one.AreaProjectID, one.ServiceProjectID)
		if err != nil {
			return nil, err
		}
		created = append(created, createdOne)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("item: committing the revert of %s: %w", intentID, err)
	}
	return created, nil
}
