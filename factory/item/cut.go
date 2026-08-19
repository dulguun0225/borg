package item

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrBranchEmpty is returned by [Cut.Create] for an item with no branch.
	ErrBranchEmpty = errors.New("item: the branch is empty")
	// ErrIntentIDEmpty is returned by [Cut.Create] for an item naming no
	// intent. record's doc.go states what a link is checked for.
	ErrIntentIDEmpty = errors.New("item: the intent id is empty")
	// ErrServiceIDEmpty is returned by [Cut.Create] for an item naming no
	// service.
	ErrServiceIDEmpty = errors.New("item: the service id is empty")
)

// Cut is the writer of the item's one creating write. It has no update
// method, because the cut writes an item once and never again — every later
// write is [Dispatch]'s.
type Cut struct {
	pool *pgxpool.Pool
}

// NewCut returns the writer over pool.
func NewCut(pool *pgxpool.Pool) *Cut { return &Cut{pool: pool} }

// New is what the cut knows about an item when it creates one. It is a struct
// and not four arguments because all four are strings and three of them are
// ids: a caller that swapped two would compile.
type New struct {
	IntentID  string
	ServiceID string
	// AreaID is the narrowest area whose declaration covers the work, and is
	// empty where no area covers it. Empty is stored: an item with no area is
	// one a pin drawn on an area does not reach and one the score cannot read a
	// context factor for, which puts a human at its gates rather than being
	// refused here.
	AreaID string
	Branch string
	// WaitsOn is the items this one cannot be verified until they have shipped.
	// The cut records the order, so this is where a dependency is declared and
	// not something discovered at deploy time.
	WaitsOn []string
}

// Create writes an item at stage spec, where every item starts, with the
// priority at nothing — an owner reordering a queue is [Dispatch.SetPriority]
// and never the cut.
func (c *Cut) Create(ctx context.Context, actor record.Actor, n New) (Item, error) {
	if err := actor.Validate(); err != nil {
		return Item{}, err
	}
	if n.IntentID == "" {
		return Item{}, ErrIntentIDEmpty
	}
	if n.ServiceID == "" {
		return Item{}, ErrServiceIDEmpty
	}
	if n.Branch == "" {
		return Item{}, ErrBranchEmpty
	}

	for _, on := range n.WaitsOn {
		if on == "" {
			return Item{}, fmt.Errorf("%w: one of the items it waits on", ErrItemIDEmpty)
		}
	}

	it := Item{
		ID:        record.NewID(IDPrefix),
		Actor:     actor,
		At:        record.Now(),
		IntentID:  n.IntentID,
		ServiceID: n.ServiceID,
		AreaID:    n.AreaID,
		Branch:    n.Branch,
		Stage:     StageSpec,
		WaitsOn:   n.WaitsOn,
	}
	_, err := c.pool.Exec(ctx, `insert into `+Table+`
		(id, actor_kind, actor_name, at, intent_id, service_id, area_id, branch, stage, waits_on, priority)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 0)`,
		it.ID, string(it.Actor.Kind), it.Actor.Name, it.At,
		it.IntentID, it.ServiceID, it.AreaID, it.Branch, string(it.Stage), joinWaitsOn(it.WaitsOn),
	)
	if err != nil {
		return Item{}, fmt.Errorf("item: cutting %s: %w", it.ID, err)
	}
	return it, nil
}
