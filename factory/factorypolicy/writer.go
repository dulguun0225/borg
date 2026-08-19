package factorypolicy

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrNotFound is returned by [Get] where the record has not been created.
	ErrNotFound = errors.New("factorypolicy: the factory policy record does not exist yet")
	// ErrBoundNotPositive is returned by [SetAttemptBound] for a bound that is
	// not above zero. A bound of zero would retry a stage no times and escalate
	// every item before an agent was asked anything.
	ErrBoundNotPositive = errors.New("factorypolicy: an attempt bound is above zero")
	// ErrStageUnknown is returned by [SetAttemptBound] for a stage outside
	// [item.StageOrder]. A bound authored on a stage that does not exist is a
	// value nothing will ever read, and the store has no foreign key to refuse
	// it.
	ErrStageUnknown = errors.New("factorypolicy: the bound names a stage an item cannot be at")
	// ErrThresholdOutOfRange is returned by [SetBriefOrSkillThreshold] for a
	// threshold outside nothing to one, which is the scale the score's number
	// is on.
	ErrThresholdOutOfRange = errors.New("factorypolicy: a threshold is between 0 and 1")
	// ErrCatalogEmpty is returned by [SetPredicateCatalog] for an empty
	// catalog. A pin on the catalog may only extend it, and authoring it empty
	// would be an owner removing every kind of assertion a declaration may
	// draw from.
	ErrCatalogEmpty = errors.New("factorypolicy: an authored predicate catalog names at least one kind of assertion")
)

// Policy is the factory policy record as it is stored.
type Policy struct {
	ID    string
	Actor record.Actor
	At    string
	// PredicateCatalog is what an owner authored, and empty where they
	// authored nothing. Nothing reads it until contracts are built.
	PredicateCatalog []string
	// BriefOrSkillThreshold is what an owner authored for the gate row that
	// decides a version of what an agent is told, and absent where they
	// authored nothing. Nothing reads it until that row is built.
	BriefOrSkillThreshold gatepolicy.Authored
}

// Writer creates the record, as Factory.
type Writer struct {
	pool *pgxpool.Pool
}

// NewWriter returns the writer over pool.
func NewWriter(pool *pgxpool.Pool) *Writer { return &Writer{pool: pool} }

// Ensure returns the factory policy record, creating it with nothing authored
// where it does not exist. It is idempotent: the insert does nothing on the
// singleton conflict, so two callers ensuring at once leave one record.
func (w *Writer) Ensure(ctx context.Context, actor record.Actor) (Policy, error) {
	if err := actor.Validate(); err != nil {
		return Policy{}, err
	}
	_, err := w.pool.Exec(ctx, `insert into `+Table+`
		(id, actor_kind, actor_name, at, only_row, predicate_catalog, brief_or_skill_threshold)
		values ($1, $2, $3, $4, true, '', null)
		on conflict (only_row) do nothing`,
		record.NewID(IDPrefix), string(actor.Kind), actor.Name, record.Now(),
	)
	if err != nil {
		return Policy{}, fmt.Errorf("factorypolicy: creating the record: %w", err)
	}
	return Get(ctx, w.pool)
}

// SetAttemptBound writes the bound an owner authored for one stage, inside tx.
// Its one caller is package policy, which calls it inside the transaction that
// appends the policy version.
func SetAttemptBound(ctx context.Context, tx pgx.Tx, actor record.Actor, policyID string, stage item.Stage, bound int) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	if !slices.Contains(item.StageOrder, stage) {
		return fmt.Errorf("%w: %q", ErrStageUnknown, stage)
	}
	if bound <= 0 {
		return fmt.Errorf("%w: %d", ErrBoundNotPositive, bound)
	}
	_, err := tx.Exec(ctx, `insert into `+BoundTable+`
		(id, actor_kind, actor_name, at, factory_policy_id, stage, bound)
		values ($1, $2, $3, $4, $5, $6, $7)
		on conflict (factory_policy_id, stage) do update set bound = excluded.bound`,
		record.NewID(BoundIDPrefix), string(actor.Kind), actor.Name, record.Now(),
		policyID, string(stage), bound,
	)
	if err != nil {
		return fmt.Errorf("factorypolicy: authoring the %s attempt bound: %w", stage, err)
	}
	return nil
}

// SetPredicateCatalog writes the catalog an owner authored, inside tx.
func SetPredicateCatalog(ctx context.Context, tx pgx.Tx, policyID string, catalog []string) error {
	if len(catalog) == 0 {
		return ErrCatalogEmpty
	}
	return update(ctx, tx, policyID, `predicate_catalog = $1`, strings.Join(catalog, "\n"))
}

// SetBriefOrSkillThreshold writes the threshold an owner authored for the gate
// row that decides a version of what an agent is told, inside tx.
func SetBriefOrSkillThreshold(ctx context.Context, tx pgx.Tx, policyID string, threshold float64) error {
	if threshold < 0 || threshold > 1 {
		return fmt.Errorf("%w: %v", ErrThresholdOutOfRange, threshold)
	}
	return update(ctx, tx, policyID, `brief_or_skill_threshold = $1`, threshold)
}

func update(ctx context.Context, tx pgx.Tx, policyID, assignment string, value any) error {
	// assignment is a constant of this package and never input, so writing it
	// into the statement is not a place anything can be injected.
	tag, err := tx.Exec(ctx, `update `+Table+` set `+assignment+` where id = $2`, value, policyID)
	if err != nil {
		return fmt.Errorf("factorypolicy: authoring on %s: %w", policyID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, policyID)
	}
	return nil
}

// Get is the factory policy record. It takes the pool and not a [Writer],
// because reading it is not a reason to be handed the thing that creates it.
func Get(ctx context.Context, pool *pgxpool.Pool) (Policy, error) {
	var p Policy
	var kind, catalog string
	var threshold *float64
	err := pool.QueryRow(ctx, `select id, actor_kind, actor_name, at, predicate_catalog, brief_or_skill_threshold
		from `+Table+` where only_row`).
		Scan(&p.ID, &kind, &p.Actor.Name, &p.At, &catalog, &threshold)
	if errors.Is(err, pgx.ErrNoRows) {
		return Policy{}, ErrNotFound
	} else if err != nil {
		return Policy{}, fmt.Errorf("factorypolicy: reading the record: %w", err)
	}
	p.Actor.Kind = record.Kind(kind)
	if catalog != "" {
		p.PredicateCatalog = strings.Split(catalog, "\n")
	}
	if threshold != nil {
		p.BriefOrSkillThreshold = gatepolicy.Authored{Number: *threshold, Present: true}
	}
	return p, nil
}

// AttemptBound is the bound an owner authored for one stage, and absent where
// they authored none — where the value in force is what the score supplies. The
// bound is a whole number of attempts and is read back as the same type every
// authored parameter is, because what clamps it is one arithmetic for all of
// them.
func AttemptBound(ctx context.Context, pool *pgxpool.Pool, policyID string, stage item.Stage) (gatepolicy.Authored, error) {
	var bound int
	err := pool.QueryRow(ctx, `select bound from `+BoundTable+`
		where factory_policy_id = $1 and stage = $2`, policyID, string(stage)).Scan(&bound)
	if errors.Is(err, pgx.ErrNoRows) {
		return gatepolicy.Authored{}, nil
	} else if err != nil {
		return gatepolicy.Authored{}, fmt.Errorf("factorypolicy: reading the %s attempt bound: %w", stage, err)
	}
	return gatepolicy.Authored{Number: float64(bound), Present: true}, nil
}
