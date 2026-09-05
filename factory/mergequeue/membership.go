package mergequeue

import (
	"context"
	"fmt"
	"sort"

	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
)

// Membership is what the queue read about what is in the queue for one service:
// the members in the queue's order, the ones the intent's state stops, and the
// intent each was decomposed from. The intents are read once and kept, because
// the tier the order begins with and the two exceptions the halt's stop takes are
// fields of the same record.
type Membership struct {
	Members []item.Item
	Stopped []item.Item
	Intents map[string]intent.Intent
}

// Members is the queue's membership for one service, in the queue's order: the
// items whose stage says Merge to master approved them, whose fast-forward has
// not happened, and whose intent's state permits it, ordered by the tier the
// requester proposed, then by the priority an owner set within it — greater
// first — and then by the time of that approval in the log.
//
// An item at that stage with no approval in the log is ordered last among its
// priority. It is not a state the path produces, the stage being written on the
// approval; ordering it rather than refusing it is what keeps a reader of the
// queue from being unable to see it at all.
func (q *Queue) Members(ctx context.Context, serviceID string) ([]item.Item, error) {
	m, err := q.membership(ctx, serviceID)
	return m.Members, err
}

// membership is [Queue.Members] and, beside it, the items the intent's state
// stops and the intents themselves.
func (q *Queue) membership(ctx context.Context, serviceID string) (Membership, error) {
	if serviceID == "" {
		return Membership{}, ErrServiceIDEmpty
	}
	m := Membership{Intents: map[string]intent.Intent{}}
	atStage, err := item.AtStage(ctx, q.pool, serviceID, item.StageQueued)
	if err != nil {
		return Membership{}, err
	}
	if len(atStage) == 0 {
		return m, nil
	}
	approved, err := gate.ApprovalTimes(ctx, q.pool, q.token, Actor, gate.MergeToMaster)
	if err != nil {
		return Membership{}, err
	}

	for _, it := range atStage {
		in, err := intent.Get(ctx, q.pool, it.IntentID)
		if err != nil {
			return Membership{}, fmt.Errorf("mergequeue: reading the intent of %s: %w", it.ID, err)
		}
		m.Intents[it.ID] = in
		if stops(in.State) {
			m.Stopped = append(m.Stopped, it)
			continue
		}
		m.Members = append(m.Members, it)
	}

	sort.SliceStable(m.Members, func(a, b int) bool {
		first, second := m.Members[a], m.Members[b]
		firstTier, secondTier := m.Intents[first.ID].Tier.Value, m.Intents[second.ID].Tier.Value
		if firstTier != secondTier {
			return firstTier > secondTier
		}
		if first.Priority != second.Priority {
			return first.Priority > second.Priority
		}
		firstAt, secondAt := approved[first.ID], approved[second.ID]
		if firstAt == secondAt {
			// The sort is stable and this is the last word, so a tie keeps the
			// order the membership query returned — which is the time the item
			// was decomposed. Ordering on the id instead would be an order
			// derived from random bytes, and it would throw away the one the
			// store already gave.
			return false
		}
		// An unapproved item's empty time would sort first, and it belongs last:
		// an empty string is less than every timestamp.
		if firstAt == "" || secondAt == "" {
			return secondAt == ""
		}
		return firstAt < secondAt
	})
	return m, nil
}

// stops reports whether the intent's state stops the queue from fast-forwarding
// this item's candidate. The four are the design's own list, and refined and
// delivered are the two that do not stop one; package intent names the merge
// queue as one of the three components that read it.
func stops(state intent.State) bool {
	switch state {
	case intent.StateUnrefined, intent.StateReDecomposing, intent.StateEscalated, intent.StateDropped:
		return true
	default:
		return false
	}
}
