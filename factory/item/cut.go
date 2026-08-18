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

// Create writes an item at stage spec, where every item starts.
func (c *Cut) Create(ctx context.Context, actor record.Actor, intentID, serviceID, branch string) (Item, error) {
	if err := actor.Validate(); err != nil {
		return Item{}, err
	}
	if intentID == "" {
		return Item{}, ErrIntentIDEmpty
	}
	if serviceID == "" {
		return Item{}, ErrServiceIDEmpty
	}
	if branch == "" {
		return Item{}, ErrBranchEmpty
	}

	it := Item{
		ID:        record.NewID(IDPrefix),
		Actor:     actor,
		At:        record.Now(),
		IntentID:  intentID,
		ServiceID: serviceID,
		Branch:    branch,
		Stage:     StageSpec,
	}
	_, err := c.pool.Exec(ctx, `insert into `+Table+`
		(id, actor_kind, actor_name, at, intent_id, service_id, branch, stage)
		values ($1, $2, $3, $4, $5, $6, $7, $8)`,
		it.ID, string(it.Actor.Kind), it.Actor.Name, it.At,
		it.IntentID, it.ServiceID, it.Branch, string(it.Stage),
	)
	if err != nil {
		return Item{}, fmt.Errorf("item: cutting %s: %w", it.ID, err)
	}
	return it, nil
}
