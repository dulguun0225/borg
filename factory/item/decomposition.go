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
	// ErrAreaIDEmpty is returned by [Decomposition.Create] for an empty id in
	// the chain of areas covering the work. record's doc.go states what a link
	// is checked for.
	ErrAreaIDEmpty = errors.New("item: the area id is empty")
	// ErrAnswersNoRequirement is returned by [Decomposition.Create] for an item
	// answering no requirement, whole or derived. Work nobody asked for has
	// criteria at Spec that trace to nothing, so the item that creates a
	// service and each step of a migration carry a requirement like every
	// other item.
	ErrAnswersNoRequirement = errors.New("item: the item answers no requirement, whole or derived")
)

// Decomposition is the writer of the item's three writes: creating one,
// pointing a superseded one at what replaced it, and repointing what a
// standing item waits on. Every other write is [Dispatch]'s.
type Decomposition struct {
	pool  *pgxpool.Pool
	token lease.Token
	// dispatch is where the stage of a superseded item is written. The stage is
	// dispatch's field wherever it is written from, so decomposition reports
	// that transition here like every other rather than writing the column
	// itself.
	dispatch *Dispatch
	// Holds is where Create, CreateTx, Repoint, and RepointTx read every
	// rollback hold standing, at each write, since no record holds them. It is
	// nil until a caller wires it — cmd/factory's own composition does, over
	// path.rollbackHolds, the same reading the production deploy gate makes —
	// and nil is a decomposition checked against no hold standing, which is
	// every composition of this writer but cmd/factory's own.
	Holds RollbackHolds
}

// NewDecomposition returns the writer over pool, fencing every write with token.
// Holds is nil until the caller sets it.
func NewDecomposition(pool *pgxpool.Pool, token lease.Token) *Decomposition {
	return &Decomposition{pool: pool, token: token, dispatch: NewDispatch(pool, token)}
}

// standingHolds is every rollback hold standing, read through Holds where a
// caller has wired one and nothing where it is nil: a decomposition composed
// with no seam onto the gate's own reading is checked against no hold.
func (c *Decomposition) standingHolds(ctx context.Context) ([]Hold, error) {
	if c.Holds == nil {
		return nil, nil
	}
	return c.Holds.Standing(ctx)
}

// NewID mints an item id, which is what [New.ID] takes. It is exported so that
// a caller writing a record that names the item before the item exists mints
// the id under this package's own prefix rather than composing one of its own.
func NewID() string { return record.NewID(IDPrefix) }

// New is what decomposition knows about an item when it creates one. It is a struct
// and not six arguments because most of them are strings and three of them are
// ids: a caller that swapped two would compile.
type New struct {
	// ID is the id the item is written under, minted with [NewID] by a caller
	// that has to write a record naming the item before the item exists, and
	// minted here where it is empty. Decomposition mints one for the shares it
	// derives: a derived requirement names the item that answers it and the item
	// answers the share, so one of the two ids exists before either row does.
	ID        string
	IntentID  string
	ServiceID string
	// AreaChain is the areas whose declarations cover the work, narrowest
	// first: the chain package area walks from the area the work is in up to
	// the project, every area of which covers the work the narrowest one
	// covers. Decomposition writes the narrowest of them, which is the head,
	// and an empty chain is an item with no area — one a safeguard drawn on an
	// area does not reach and one the score cannot read a context factor for,
	// which puts a human at its gates rather than being refused here.
	//
	// What the head costs is that the chain arrives ordered: an area chain is
	// package area's to walk and this package imports neither it nor the
	// project the chain ends at, so the order is the caller's and the choice
	// among the covering areas is this package's.
	AreaChain []string
	Branch    string
	// WaitsOn is the items this one cannot be verified until they have shipped.
	// Decomposition records the order, so this is where a dependency is declared and
	// not something discovered at deploy time.
	WaitsOn []string
	// RequirementsAnswered is the ids of the intent's requirements this item
	// answers — rows of package intent's requirement table, written by intake
	// at the confirming round and at decomposition's own split. Every item
	// answers a requirement whole or carries a derived share of one, so an
	// empty list is [ErrAnswersNoRequirement]; the ids are checked for being
	// present and never for pointing at anything.
	RequirementsAnswered []string
}

// narrowestArea is the area an item names: the head of the chain whose
// declarations cover the work, the chain arriving narrowest first, and nothing
// where no declared area covers it.
func narrowestArea(chain []string) string {
	if len(chain) == 0 {
		return ""
	}
	return chain[0]
}

// Create writes an item at stage spec, where every item starts, with the
// priority at nothing — an owner reordering a queue is [Dispatch.SetPriority]
// and never decomposition — and counts the item's first attempt at spec, spec
// being entered to author the moment the item exists.
//
// The area written is the narrowest of [New.AreaChain], and only an area
// inside the project of the item's service: areaProjectID and serviceProjectID
// are the project the chain ends at and the project the item's service is in,
// read by the caller because an area chain is package area's to walk and a
// service's project is package service's field, and this package imports
// neither. They are compared rather than stored, the project being no field of
// the item. Where no declared area covers the work there is no chain, nothing
// to compare, and neither is read.
//
// The rollback holds standing are read through [Decomposition.Holds], which no
// record holds: while one stands on a service, every unmerged item of that
// service other than the revert waits on the revert item, and the edges it
// imposes are computed here at this write, so the acyclic check is over the
// union of the declared edges and those.
func (c *Decomposition) Create(ctx context.Context, actor record.Actor, n New,
	areaProjectID, serviceProjectID string) (Item, error) {
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return Item{}, fmt.Errorf("item: beginning a decomposition's write: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	it, err := c.CreateTx(ctx, tx, actor, n, areaProjectID, serviceProjectID)
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
	areaProjectID, serviceProjectID string) (Item, error) {
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
	for _, id := range n.AreaChain {
		if id == "" {
			return Item{}, fmt.Errorf("%w: one of the areas whose declaration covers the work", ErrAreaIDEmpty)
		}
	}
	if len(n.AreaChain) > 0 && areaProjectID != serviceProjectID {
		return Item{}, fmt.Errorf("%w: area %s is in %q and service %s is in %q",
			ErrAreaOutsideServiceProject, narrowestArea(n.AreaChain), areaProjectID, n.ServiceID, serviceProjectID)
	}
	for _, on := range n.WaitsOn {
		if on == "" {
			return Item{}, fmt.Errorf("%w: one of the items it waits on", ErrItemIDEmpty)
		}
	}
	if len(n.RequirementsAnswered) == 0 {
		return Item{}, fmt.Errorf("%w: %s on service %s", ErrAnswersNoRequirement, n.Branch, n.ServiceID)
	}
	for _, id := range n.RequirementsAnswered {
		if id == "" {
			return Item{}, fmt.Errorf("%w: one of the requirements it answers", ErrRequirementIDEmpty)
		}
	}
	if err := lease.Fence(ctx, tx, c.token); err != nil {
		return Item{}, err
	}

	id := n.ID
	if id == "" {
		id = NewID()
	}
	it := Item{
		ID:                   id,
		Actor:                actor,
		At:                   record.Now(),
		IntentID:             n.IntentID,
		ServiceID:            n.ServiceID,
		AreaID:               narrowestArea(n.AreaChain),
		Branch:               n.Branch,
		Stage:                StageSpec,
		WaitsOn:              n.WaitsOn,
		RequirementsAnswered: n.RequirementsAnswered,
	}

	standing, err := standingEdges(ctx, tx, it.ID)
	if err != nil {
		return Item{}, err
	}
	// The item being created is one of the held service's unmerged items the
	// moment this write lands, and a revert decomposed while its own hold
	// stands is what that hold's edges lead into, so the edges are computed
	// with it among them rather than against the rows already there.
	holds, err := c.standingHolds(ctx)
	if err != nil {
		return Item{}, err
	}
	held, err := heldEdges(ctx, tx, holds, it)
	if err != nil {
		return Item{}, err
	}
	proposed := make([]edge, 0, len(it.WaitsOn))
	for _, on := range it.WaitsOn {
		proposed = append(proposed, edge{From: it.ID, To: on})
	}
	if err := checkAcyclic(standing, held, proposed); err != nil {
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
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return Item{}, fmt.Errorf("item: beginning the supersede of %s: %w", itemID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	it, err := c.SupersedeTx(ctx, tx, actor, itemID, replacedBy)
	if err != nil {
		return Item{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Item{}, fmt.Errorf("item: committing the supersede of %s: %w", itemID, err)
	}
	return it, nil
}

// SupersedeTx is [Decomposition.Supersede] on a transaction the caller began,
// so a rejected item points at its replacements in the write that creates
// them: what was decomposed wrong is readable beside what replaced it from the
// first moment either row exists, and no reader sees a superseded item whose
// pointer names items that are not there.
//
// The stage is reported to [Dispatch] and the pointer is written here, which
// is the seam the record keeps everywhere: decomposition writes the item's own
// fields and dispatch writes the stage, in one transaction because superseding
// is one event.
func (c *Decomposition) SupersedeTx(ctx context.Context, tx pgx.Tx, actor record.Actor,
	itemID string, replacedBy []string) (Item, error) {
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
	if err := lease.Fence(ctx, tx, c.token); err != nil {
		return Item{}, err
	}

	it, err := c.dispatch.superseded(ctx, tx, itemID)
	if err != nil {
		return Item{}, err
	}
	if _, err := tx.Exec(ctx, `update `+Table+` set superseded_by = $1 where id = $2`,
		joinIDs(replacedBy), itemID); err != nil {
		return Item{}, fmt.Errorf("item: pointing %s at what replaced it: %w", itemID, err)
	}
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
	waitsOn []string) (Item, error) {
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return Item{}, fmt.Errorf("item: beginning the repoint of %s: %w", itemID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	it, err := c.RepointTx(ctx, tx, actor, itemID, waitsOn)
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
	waitsOn []string) (Item, error) {
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
	holds, err := c.standingHolds(ctx)
	if err != nil {
		return Item{}, err
	}
	held, err := heldEdges(ctx, tx, holds, it)
	if err != nil {
		return Item{}, err
	}
	proposed := make([]edge, 0, len(waitsOn))
	for _, on := range waitsOn {
		proposed = append(proposed, edge{From: itemID, To: on})
	}
	if err := checkAcyclic(standing, held, proposed); err != nil {
		return Item{}, err
	}

	if _, err := tx.Exec(ctx, `update `+Table+` set waits_on = $1 where id = $2`,
		joinIDs(waitsOn), itemID); err != nil {
		return Item{}, fmt.Errorf("item: repointing %s: %w", itemID, err)
	}
	it.WaitsOn = waitsOn
	return it, nil
}
