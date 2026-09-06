package dispatch

import (
	"context"
	"fmt"
	"sort"

	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
)

// Admit is the order items are admitted in where more is ready than the
// infrastructure admits: the tier of the intent each item was decomposed from
// first, greater first, and the item's own priority breaking a tie within one
// tier. It returns a list of its own and leaves the caller's alone.
//
// What a tier orders is admission and never a bound already reached. A spend
// ceiling, the platform's own room for candidate environments, and the backlog
// cap each stay a stop no tier lifts, the same as a hold: this says which item
// goes first among those that can go at all.
//
// An item whose intent cannot be read stops the ordering rather than being
// ordered as though it had no tier: the tier is what the order begins with, and
// an order made without it is not this order. An item naming no intent reads as
// tier nothing, which is where an item nobody proposed a tier for already sits.
//
// The sort is stable and the two comparisons are its whole vocabulary, so items
// equal on both keep the order the caller gave — which is the order the store
// answered, the way the merge queue's own ordering leaves a tie.
func (d *Dispatch) Admit(ctx context.Context, items []item.Item) ([]item.Item, error) {
	tiers := make(map[string]int, len(items))
	for _, it := range items {
		if it.IntentID == "" {
			continue
		}
		if _, read := tiers[it.IntentID]; read {
			continue
		}
		in, err := intent.Get(ctx, d.c.Pool, it.IntentID)
		if err != nil {
			return nil, fmt.Errorf("dispatch: reading the tier of the intent %s was decomposed from: %w", it.ID, err)
		}
		tiers[it.IntentID] = in.Tier.Value
	}

	ordered := make([]item.Item, len(items))
	copy(ordered, items)
	sort.SliceStable(ordered, func(a, b int) bool {
		first, second := ordered[a], ordered[b]
		if firstTier, secondTier := tiers[first.IntentID], tiers[second.IntentID]; firstTier != secondTier {
			return firstTier > secondTier
		}
		return first.Priority > second.Priority
	})
	return ordered, nil
}
