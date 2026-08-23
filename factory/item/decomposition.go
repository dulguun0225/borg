package item

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrBranchEmpty is returned by [Decomposition.Create] for an item with no branch.
	ErrBranchEmpty = errors.New("item: the branch is empty")
	// ErrIntentIDEmpty is returned by [Decomposition.Create] for an item naming no
	// intent. record's doc.go states what a link is checked for.
	ErrIntentIDEmpty = errors.New("item: the intent id is empty")
	// ErrServiceIDEmpty is returned by [Decomposition.Create] for an item naming no
	// service.
	ErrServiceIDEmpty = errors.New("item: the service id is empty")
)

// Decomposition is the writer of the item's one creating write. It has no update
// method, because decomposition writes an item once and never again — every later
// write is [Dispatch]'s.
type Decomposition struct {
	pool *pgxpool.Pool
}

// NewDecomposition returns the writer over pool.
func NewDecomposition(pool *pgxpool.Pool) *Decomposition { return &Decomposition{pool: pool} }

// New is what decomposition knows about an item when it creates one. It is a struct
// and not four arguments because all four are strings and three of them are
// ids: a caller that swapped two would compile.
type New struct {
	IntentID  string
	ServiceID string
	// AreaID is the narrowest area whose declaration covers the work, and is empty
	// where no area covers it. Empty is stored: an item with no area is one a
	// safeguard drawn on an area does not reach and one the score cannot read a
	// context factor for, which puts a human at its gates rather than being
	// refused here.
	AreaID string
	Branch string
	// WaitsOn is the items this one cannot be verified until they have shipped.
	// Decomposition records the order, so this is where a dependency is declared and
	// not something discovered at deploy time.
	WaitsOn []string
}

// Create writes an item at stage spec, where every item starts, with the
// priority at nothing — an owner reordering a queue is [Dispatch.SetPriority]
// and never decomposition.
func (c *Decomposition) Create(ctx context.Context, actor record.Actor, n New) (Item, error) {
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
		(id, actor_kind, actor_name, at, intent_id, service_id, area_id, branch, stage,
		waits_on, superseded_by, priority)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, '', 0)`,
		it.ID, string(it.Actor.Kind), it.Actor.Name, it.At,
		it.IntentID, it.ServiceID, it.AreaID, it.Branch, string(it.Stage), joinIDs(it.WaitsOn),
	)
	if err != nil {
		return Item{}, fmt.Errorf("item: decomposing %s: %w", it.ID, err)
	}
	return it, nil
}

// ErrAlreadySuperseded is returned by [Decomposition.Supersede] for an item that is
// superseded already. Superseding does not run twice, and nothing puts an item
// back: what replaced it is what carries the work on.
var ErrAlreadySuperseded = errors.New("item: the item is superseded already")

// ErrMerged is returned by [Decomposition.Supersede] for a merged item. A merged item is
// out of reach of a re-decomposition — a send-back may be raised no later than the merge to
// master — so a re-decomposition leaves shipped work alone and declares the new set's order
// against it.
var ErrMerged = errors.New("item: a merged item is out of a re-decomposition's reach")

// Supersede ends one item because a decomposition replaced it: the stage becomes
// superseded and the item points at whatever replaced it, which is empty where a
// re-decomposition replaced it with nothing.
//
// It is decomposition's second write and its only write to an existing item, which is
// the seam this package's doc.go states: decomposition writes an item when it creates
// one and writes one again only to point a superseded one at its replacements.
// Both fields go in one transaction because they are one event — an item at the
// superseded stage with no pointer and no replacement is a state no reader can
// tell from one a re-decomposition dropped.
func (c *Decomposition) Supersede(ctx context.Context, actor record.Actor, itemID string, replacedBy []string) (Item, error) {
	if err := actor.Validate(); err != nil {
		return Item{}, err
	}
	if itemID == "" {
		return Item{}, ErrItemIDEmpty
	}
	for _, by := range replacedBy {
		if by == "" {
			return Item{}, fmt.Errorf("%w: one of the items that replaced it", ErrItemIDEmpty)
		}
	}

	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return Item{}, fmt.Errorf("item: beginning the supersede of %s: %w", itemID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	it, err := scanItem(tx.QueryRow(ctx, `select `+columns+` from `+Table+` where id = $1 for update`, itemID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, fmt.Errorf("%w: %s", ErrNotFound, itemID)
	} else if err != nil {
		return Item{}, fmt.Errorf("item: reading %s: %w", itemID, err)
	}
	switch it.Stage {
	case StageSuperseded:
		return Item{}, fmt.Errorf("%w: %s", ErrAlreadySuperseded, itemID)
	case StageMerged:
		return Item{}, fmt.Errorf("%w: %s", ErrMerged, itemID)
	}

	if _, err := tx.Exec(ctx, `update `+Table+` set stage = $1, superseded_by = $2 where id = $3`,
		string(StageSuperseded), joinIDs(replacedBy), itemID); err != nil {
		return Item{}, fmt.Errorf("item: superseding %s: %w", itemID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Item{}, fmt.Errorf("item: committing the supersede of %s: %w", itemID, err)
	}
	it.Stage = StageSuperseded
	it.SupersededBy = replacedBy
	return it, nil
}
