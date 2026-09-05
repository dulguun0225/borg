package area

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrNameEmpty is returned by [Writer.Declare] for an area with no name.
	ErrNameEmpty = errors.New("area: the name is empty")
	// ErrNotFound is returned where no area has the id or the name.
	ErrNotFound = errors.New("area: no area has that id")
	// ErrInsideIsOneOfTwo is returned by [Writer.Declare] for an area that lies
	// inside both an area and a project, or inside neither. A chain ends at a
	// project and nowhere else, so an area inside nothing would be a chain
	// running off the end.
	ErrInsideIsOneOfTwo = errors.New("area: an area lies inside one area or one project")
	// ErrTargetNotPositive is returned by [SetItemSizeTarget] for a target
	// that is not above zero. The target is how many of its intent's
	// requirements an item is meant to answer, so a zero or a negative one is a
	// target no item can meet.
	ErrTargetNotPositive = errors.New("area: the item-size target is above zero")
	// ErrChainCycles is returned by [Chain] where the inside links lead back
	// to an area the walk has already crossed. Nothing in the store refuses
	// one, there being no foreign keys between records, so the walk is where
	// it is found.
	ErrChainCycles = errors.New("area: the inside links form a cycle")
)

// Inside is what one area lies inside: another area, or the project the chain
// ends at. Exactly one of the two is set, which [Writer.Declare] refuses to
// leave otherwise.
type Inside struct {
	AreaID    string
	ProjectID string
}

// InsideArea is an area inside another area.
func InsideArea(areaID string) Inside { return Inside{AreaID: areaID} }

// InsideProject is an area at the top of its chain, lying inside the project
// directly.
func InsideProject(projectID string) Inside { return Inside{ProjectID: projectID} }

func (i Inside) valid() bool { return (i.AreaID != "") != (i.ProjectID != "") }

// Area is one area as it is stored: an owner's grouping, what it lies inside,
// the hazard severity declared on it, and the item-size target authored on it.
type Area struct {
	ID    string
	Actor record.Actor
	At    string
	Name  string
	// Inside is the area or the project this one lies inside.
	Inside Inside
	// Hazard is the severity declared here, and the zero value where this area
	// names none.
	Hazard Hazard
	// ItemSizeTarget is what an owner authored, in the count of its intent's
	// requirements an item answers, and absent where they authored nothing and
	// the score supplies it instead.
	ItemSizeTarget gatepolicy.Authored
}

// Writer is the table's one writer: an owner declaring an area at Factory.
type Writer struct {
	pool  *pgxpool.Pool
	token lease.Token
}

// NewWriter returns the writer over pool, fencing every write with token.
func NewWriter(pool *pgxpool.Pool, token lease.Token) *Writer {
	return &Writer{pool: pool, token: token}
}

// Declare writes an area inside what inside names, with the hazard severity the
// owner declared on it. A name already taken is refused by the store's unique
// constraint rather than by a pre-check here, a pre-check and an insert being two
// statements a second declaration can interleave.
//
// The zero [Hazard] is an area that names no grade, which is what most are.
func (w *Writer) Declare(ctx context.Context, actor record.Actor, name string, inside Inside, hazard Hazard) (Area, error) {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return Area{}, fmt.Errorf("area: beginning the declaration of %q: %w", name, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	a, err := Insert(ctx, tx, w.token, actor, name, inside, hazard)
	if err != nil {
		return Area{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Area{}, fmt.Errorf("area: committing the declaration of %q: %w", name, err)
	}
	return a, nil
}

// Insert writes an area inside tx, fencing it with token first. Its caller is
// package policy, which appends the policy version in the same transaction,
// so the area and the version commit together or not at all; [Writer.Declare]
// is the same write where there is nothing to compose it with.
func Insert(ctx context.Context, tx pgx.Tx, token lease.Token, actor record.Actor, name string,
	inside Inside, hazard Hazard) (Area, error) {
	if err := lease.Fence(ctx, tx, token); err != nil {
		return Area{}, err
	}
	if err := actor.Validate(); err != nil {
		return Area{}, err
	}
	if name == "" {
		return Area{}, ErrNameEmpty
	}
	if !inside.valid() {
		return Area{}, fmt.Errorf("%w: %q lies inside area %q and project %q", ErrInsideIsOneOfTwo, name, inside.AreaID, inside.ProjectID)
	}
	if err := checkHazard(hazard); err != nil {
		return Area{}, err
	}

	a := Area{
		ID:     record.NewID(IDPrefix),
		Actor:  actor,
		At:     record.Now(),
		Name:   name,
		Inside: inside,
		Hazard: hazard,
	}
	_, err := tx.Exec(ctx, `insert into `+Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, name, inside_area_id, project_id,
		grade, hazardous_operation, hazard_bound, hazard_bound_period_seconds, item_size_target)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, null)`,
		a.ID, FormatVersion, string(a.Actor.Kind), a.Actor.Key, string(a.Actor.Basis), a.At, a.Name,
		a.Inside.AreaID, a.Inside.ProjectID,
		string(a.Hazard.Grade), a.Hazard.Operation, bound(a.Hazard.Bound), bound(a.Hazard.BoundPeriodSeconds),
	)
	if err != nil {
		return Area{}, fmt.Errorf("area: declaring %q: %w", name, err)
	}
	return a, nil
}

// bound is a hazard bound as it is stored: null where the area named no grade,
// so an area with no hazard holds no bound of zero.
func bound(value float64) *float64 {
	if value == 0 {
		return nil
	}
	return &value
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

const selectArea = `select id, actor_kind, actor_key, actor_key_basis, at, name, inside_area_id, project_id,
	grade, hazardous_operation, hazard_bound, hazard_bound_period_seconds, item_size_target
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

// Chain is the area of that id and every area it lies inside, narrowest first,
// with the id of the project the chain ends at. A mechanism a safeguard binds
// reads the chain rather than the one area, because a safeguard drawn on any
// area in it reaches an item in the narrowest, and it reads the project because
// the project is the widest reach short of the factory a safeguard, a
// constraint, or a fleet entry's scope may name.
//
// The project is returned as an id rather than as a record: package area may not
// import package project — deps.txt has no such edge and the chain needs the id
// alone — so a caller that wants the record reads it there.
//
// An empty id is no areas, no project and no error: an item may name no area,
// and the caller's answer for one is the same as for an area with no safeguard
// on it.
func Chain(ctx context.Context, pool *pgxpool.Pool, areaID string) ([]Area, string, error) {
	var chain []Area
	seen := map[string]bool{}
	for id := areaID; id != ""; {
		if seen[id] {
			return nil, "", fmt.Errorf("%w: %s is inside itself", ErrChainCycles, id)
		}
		seen[id] = true
		a, err := Get(ctx, pool, id)
		if err != nil {
			return nil, "", err
		}
		chain = append(chain, a)
		if a.Inside.ProjectID != "" {
			return chain, a.Inside.ProjectID, nil
		}
		id = a.Inside.AreaID
	}
	return chain, "", nil
}

func scan(row pgx.Row, named string) (Area, error) {
	var a Area
	var kind, basis, grade string
	var hazardBound, hazardPeriod, target *float64
	err := row.Scan(&a.ID, &kind, &a.Actor.Key, &basis, &a.At, &a.Name,
		&a.Inside.AreaID, &a.Inside.ProjectID,
		&grade, &a.Hazard.Operation, &hazardBound, &hazardPeriod, &target)
	if errors.Is(err, pgx.ErrNoRows) {
		return Area{}, fmt.Errorf("%w: %s", ErrNotFound, named)
	} else if err != nil {
		return Area{}, fmt.Errorf("area: reading %s: %w", named, err)
	}
	a.Actor.Kind = record.Kind(kind)
	a.Actor.Basis = record.Basis(basis)
	a.Hazard.Grade = Grade(grade)
	if hazardBound != nil {
		a.Hazard.Bound = *hazardBound
	}
	if hazardPeriod != nil {
		a.Hazard.BoundPeriodSeconds = *hazardPeriod
	}
	if target != nil {
		a.ItemSizeTarget = gatepolicy.Authored{Number: *target, Present: true}
	}
	return a, nil
}
