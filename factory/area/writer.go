package area

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrNameEmpty is returned by [Writer.Declare] for an area with no name.
	ErrNameEmpty = errors.New("area: the name is empty")
	// ErrNotFound is returned where no area has the id or the name.
	ErrNotFound = errors.New("area: no area has that id")
	// ErrTargetNotPositive is returned by [SetItemSizeTarget] for a target
	// that is not above zero. The target is how large an item is meant to be,
	// so a zero or a negative one is a target no item can meet.
	ErrTargetNotPositive = errors.New("area: the item-size target is above zero")
	// ErrChainCycles is returned by [Chain] where the inside links lead back
	// to an area the walk has already crossed. Nothing in the store refuses
	// one, there being no foreign keys between records, so the walk is where
	// it is found.
	ErrChainCycles = errors.New("area: the inside links form a cycle")
)

// Area is one area as it is stored: an owner's grouping, the area it lies
// inside, and the item-size target authored on it.
type Area struct {
	ID    string
	Actor record.Actor
	At    string
	Name  string
	// Inside is the area this one lies inside, and is empty at the outermost.
	Inside string
	// ItemSizeTarget is what an owner authored, and absent where they authored
	// nothing and the score supplies it instead.
	ItemSizeTarget gatepolicy.Authored
}

// Writer is the table's one writer: an owner declaring an area at Factory.
type Writer struct {
	pool *pgxpool.Pool
}

// NewWriter returns the writer over pool.
func NewWriter(pool *pgxpool.Pool) *Writer { return &Writer{pool: pool} }

// Declare writes an area, inside the area named by inside where that is not
// empty. A name already taken is refused by the store's unique constraint
// rather than by a pre-check here, a pre-check and an insert being two
// statements a second declaration can interleave.
func (w *Writer) Declare(ctx context.Context, actor record.Actor, name, inside string) (Area, error) {
	if err := actor.Validate(); err != nil {
		return Area{}, err
	}
	if name == "" {
		return Area{}, ErrNameEmpty
	}

	a := Area{
		ID:     record.NewID(IDPrefix),
		Actor:  actor,
		At:     record.Now(),
		Name:   name,
		Inside: inside,
	}
	_, err := w.pool.Exec(ctx, `insert into `+Table+`
		(id, actor_kind, actor_name, at, name, inside, item_size_target)
		values ($1, $2, $3, $4, $5, $6, null)`,
		a.ID, string(a.Actor.Kind), a.Actor.Name, a.At, a.Name, a.Inside,
	)
	if err != nil {
		return Area{}, fmt.Errorf("area: declaring %q: %w", name, err)
	}
	return a, nil
}

// SetItemSizeTarget writes the target an owner authored on one area, inside tx.
// Its one caller is package policy, which calls it inside the transaction that
// appends the policy version, so the field and the version commit together or
// not at all. Nothing else may call it; doc.go says why the caller is another
// package.
func SetItemSizeTarget(ctx context.Context, tx pgx.Tx, areaID string, target float64) error {
	if target <= 0 {
		return fmt.Errorf("%w: %v", ErrTargetNotPositive, target)
	}
	tag, err := tx.Exec(ctx, `update `+Table+` set item_size_target = $1 where id = $2`, target, areaID)
	if err != nil {
		return fmt.Errorf("area: authoring the item-size target on %s: %w", areaID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, areaID)
	}
	return nil
}

const selectArea = `select id, actor_kind, actor_name, at, name, inside, item_size_target
	from ` + Table

// Get is one area by id. It takes the pool and not a [Writer], because reading
// an area is not a reason to be handed the thing that declares them.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (Area, error) {
	return scan(pool.QueryRow(ctx, selectArea+` where id = $1`, id), id)
}

// ByName is the area of that name, and false where none has it. The name is
// unique in the store, so at most one row can answer.
func ByName(ctx context.Context, pool *pgxpool.Pool, name string) (Area, bool, error) {
	a, err := scan(pool.QueryRow(ctx, selectArea+` where name = $1`, name), name)
	if errors.Is(err, ErrNotFound) {
		return Area{}, false, nil
	} else if err != nil {
		return Area{}, false, err
	}
	return a, true, nil
}

// Chain is the area of that id and every area it lies inside, narrowest first.
// A mechanism a safeguard binds reads the chain rather than the one area,
// because a safeguard drawn on any area in it reaches an item in the narrowest.
//
// An empty id is no areas and no error: an item may name no area, and the
// caller's answer for one is the same as for an area with no safeguard on it.
func Chain(ctx context.Context, pool *pgxpool.Pool, areaID string) ([]Area, error) {
	var chain []Area
	seen := map[string]bool{}
	for id := areaID; id != ""; {
		if seen[id] {
			return nil, fmt.Errorf("%w: %s is inside itself", ErrChainCycles, id)
		}
		seen[id] = true
		a, err := Get(ctx, pool, id)
		if err != nil {
			return nil, err
		}
		chain = append(chain, a)
		id = a.Inside
	}
	return chain, nil
}

func scan(row pgx.Row, named string) (Area, error) {
	var a Area
	var kind string
	var target *float64
	err := row.Scan(&a.ID, &kind, &a.Actor.Name, &a.At, &a.Name, &a.Inside, &target)
	if errors.Is(err, pgx.ErrNoRows) {
		return Area{}, fmt.Errorf("%w: %s", ErrNotFound, named)
	} else if err != nil {
		return Area{}, fmt.Errorf("area: reading %s: %w", named, err)
	}
	a.Actor.Kind = record.Kind(kind)
	if target != nil {
		a.ItemSizeTarget = gatepolicy.Authored{Number: *target, Present: true}
	}
	return a, nil
}
