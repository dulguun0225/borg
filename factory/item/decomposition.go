package item

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
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
	// ErrAreaOutsideServiceProject is returned by [Decomposition.Create] where
	// the area's project is not the project of the item's service. An item's
	// area and its service agree by construction, and this is where the
	// construction is enforced.
	ErrAreaOutsideServiceProject = errors.New("item: the area is not inside the project of the item's service")
)

// Decomposition is the writer of the item's three writes: creating one,
// pointing a superseded one at what replaced it, and repointing what a
// standing item waits on. Every other write is [Dispatch]'s.
type Decomposition struct {
	pool  *pgxpool.Pool
	token lease.Token
}

// NewDecomposition returns the writer over pool, fencing every write with token.
func NewDecomposition(pool *pgxpool.Pool, token lease.Token) *Decomposition {
	return &Decomposition{pool: pool, token: token}
}

// New is what decomposition knows about an item when it creates one. It is a struct
// and not six arguments because most of them are strings and three of them are
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
	// RequirementsAnswered is the ids of the intent's requirements this item
	// answers. It is empty on an item whose intent has no requirement record
	// yet — package intent does not write one, which this package's doc.go
	// names as the caller that is not built.
	RequirementsAnswered []string
}

// Create writes an item at stage spec, where every item starts, with the
// priority at nothing — an owner reordering a queue is [Dispatch.SetPriority]
// and never decomposition — and counts the item's first attempt at spec, spec
// being entered to author the moment the item exists.
//
// areaProjectID and serviceProjectID are the project the item's area lies in
// and the project the item's service is in, read by the caller: an area chain
// is package area's to walk and a service's project is package service's
// field, and this package imports neither. They are compared rather than
// stored, the project being no field of the item. Where the item names no
// area there is nothing to compare and neither is read.
//
// holdEdges is what a rollback hold imposes: while one stands on a service,
// every unmerged item of that service other than the revert waits on the
// revert item, and no record holds those edges. The caller reads them off what
// the production deploy gate reads the hold from and passes them here, so the
// acyclic check is over the union of the declared edges and those.
func (c *Decomposition) Create(ctx context.Context, actor record.Actor, n New,
	areaProjectID, serviceProjectID string, holdEdges []Edge) (Item, error) {
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
	if n.AreaID != "" && areaProjectID != serviceProjectID {
		return Item{}, fmt.Errorf("%w: area %s is in %q and service %s is in %q",
			ErrAreaOutsideServiceProject, n.AreaID, areaProjectID, n.ServiceID, serviceProjectID)
	}
	for _, on := range n.WaitsOn {
		if on == "" {
			return Item{}, fmt.Errorf("%w: one of the items it waits on", ErrItemIDEmpty)
		}
	}
	for _, id := range n.RequirementsAnswered {
		if id == "" {
			return Item{}, fmt.Errorf("%w: one of the requirements it answers", ErrRequirementIDEmpty)
		}
	}

	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return Item{}, fmt.Errorf("item: beginning a decomposition's write: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	it, err := c.CreateTx(ctx, tx, actor, n, areaProjectID, serviceProjectID, holdEdges)
	if err != nil {
		return Item{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Item{}, fmt.Errorf("item: committing the decomposition of %s: %w", it.ID, err)
	}
	return it, nil
}

// CreateTx is [Decomposition.Create] on a transaction the caller began, so a
// re-decomposition writes its replacements and repoints what stands on them in
// one transaction. It fences with this writer's token, tx being begun
// elsewhere, the way every write of this module fences inside its own
// transaction.
func (c *Decomposition) CreateTx(ctx context.Context, tx pgx.Tx, actor record.Actor, n New,
	areaProjectID, serviceProjectID string, holdEdges []Edge) (Item, error) {
	if err := lease.Fence(ctx, tx, c.token); err != nil {
		return Item{}, err
	}

	it := Item{
		ID:                   record.NewID(IDPrefix),
		Actor:                actor,
		At:                   record.Now(),
		IntentID:             n.IntentID,
		ServiceID:            n.ServiceID,
		AreaID:               n.AreaID,
		Branch:               n.Branch,
		Stage:                StageSpec,
		WaitsOn:              n.WaitsOn,
		RequirementsAnswered: n.RequirementsAnswered,
	}

	standing, err := standingEdges(ctx, tx, it.ID)
	if err != nil {
		return Item{}, err
	}
	proposed := make([]Edge, 0, len(it.WaitsOn))
	for _, on := range it.WaitsOn {
		proposed = append(proposed, Edge{From: it.ID, To: on})
	}
	if err := checkAcyclic(standing, holdEdges, proposed); err != nil {
		return Item{}, err
	}

	_, err = tx.Exec(ctx, `insert into `+Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, intent_id, service_id, area_id, branch, stage,
		waits_on, requirements_answered, superseded_by, priority)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, '', 0)`,
		it.ID, FormatVersion, string(it.Actor.Kind), it.Actor.Key, string(it.Actor.Basis), it.At,
		it.IntentID, it.ServiceID, it.AreaID, it.Branch, string(it.Stage),
		joinIDs(it.WaitsOn), joinIDs(it.RequirementsAnswered),
	)
	if err != nil {
		return Item{}, fmt.Errorf("item: decomposing %s: %w", it.ID, err)
	}
	if err := countEntry(ctx, tx, actor, it.ID, StageSpec); err != nil {
		return Item{}, err
	}
	return it, nil
}

// ErrAlreadySuperseded is returned by [Decomposition.Supersede] for an item that is
// superseded already. Superseding does not run twice, and nothing puts an item
// back: what replaced it is what carries the work on.
var ErrAlreadySuperseded = errors.New("item: the item is superseded already")

// ErrMerged is returned by [Decomposition.Supersede] for a merged item. A merged item is
// out of reach of a re-decomposition — a rework request may be raised no later than the merge to
// master — so a re-decomposition leaves shipped work alone and declares the new set's order
// against it.
var ErrMerged = errors.New("item: a merged item is out of a re-decomposition's reach")

// Supersede ends one item because a decomposition replaced it: the stage becomes
// superseded and the item points at whatever replaced it, which is empty where a
// re-decomposition replaced it with nothing.
//
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
	if err := lease.Fence(ctx, tx, c.token); err != nil {
		return Item{}, err
	}

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

// Repoint rewrites what a standing item waits on. A re-decomposition that
// superseded a dependency points what waited on it at the replacements, which
// is the inverse of the pointer a superseded item carries: without it an item
// waits on something that will never be current and every instrument reads the
// wait as one about to lift itself.
//
// It is decomposition's write and never dispatch's, and it is refused on an
// item that has ended — merged, dropped, or superseded — there being nothing
// left to wait for.
func (c *Decomposition) Repoint(ctx context.Context, actor record.Actor, itemID string,
	waitsOn []string, holdEdges []Edge) (Item, error) {
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return Item{}, fmt.Errorf("item: beginning the repoint of %s: %w", itemID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	it, err := c.RepointTx(ctx, tx, actor, itemID, waitsOn, holdEdges)
	if err != nil {
		return Item{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Item{}, fmt.Errorf("item: committing the repoint of %s: %w", itemID, err)
	}
	return it, nil
}

// ErrEnded is returned by [Decomposition.Repoint] for an item that has ended.
var ErrEnded = errors.New("item: the item has ended and waits on nothing")

// RepointTx is [Decomposition.Repoint] on a transaction the caller began, so
// the replacements and the repointing of what waited on the item they replaced
// are one write.
func (c *Decomposition) RepointTx(ctx context.Context, tx pgx.Tx, actor record.Actor, itemID string,
	waitsOn []string, holdEdges []Edge) (Item, error) {
	if err := actor.Validate(); err != nil {
		return Item{}, err
	}
	if itemID == "" {
		return Item{}, ErrItemIDEmpty
	}
	for _, on := range waitsOn {
		if on == "" {
			return Item{}, fmt.Errorf("%w: one of the items it waits on", ErrItemIDEmpty)
		}
	}
	if err := lease.Fence(ctx, tx, c.token); err != nil {
		return Item{}, err
	}

	it, err := scanItem(tx.QueryRow(ctx, `select `+columns+` from `+Table+` where id = $1 for update`, itemID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, fmt.Errorf("%w: %s", ErrNotFound, itemID)
	} else if err != nil {
		return Item{}, fmt.Errorf("item: reading %s: %w", itemID, err)
	}
	switch it.Stage {
	case StageMerged, StageDropped, StageSuperseded:
		return Item{}, fmt.Errorf("%w: %s is %s", ErrEnded, itemID, it.Stage)
	}

	standing, err := standingEdges(ctx, tx, itemID)
	if err != nil {
		return Item{}, err
	}
	proposed := make([]Edge, 0, len(waitsOn))
	for _, on := range waitsOn {
		proposed = append(proposed, Edge{From: itemID, To: on})
	}
	if err := checkAcyclic(standing, holdEdges, proposed); err != nil {
		return Item{}, err
	}

	if _, err := tx.Exec(ctx, `update `+Table+` set waits_on = $1 where id = $2`,
		joinIDs(waitsOn), itemID); err != nil {
		return Item{}, fmt.Errorf("item: repointing %s: %w", itemID, err)
	}
	it.WaitsOn = waitsOn
	return it, nil
}
