package constraint

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

const selectConstraint = `select id, actor_kind, actor_key, actor_key_basis, at,
	kind, reach, subject_id, statement, requires_seam5_enforced,
	binds_from_date, binds_from_zone, review_by_date, review_by_zone, replaces_id, withdrawn_at
	from ` + Table

// row is what [pgxpool.Pool.QueryRow], [pgx.Tx.QueryRow] and one row of
// [pgxpool.Pool.Query] all satisfy, so [get] reads through a pool or a
// transaction alike.
type row interface {
	Scan(dest ...any) error
}

func scan(r row) (Constraint, error) {
	var c Constraint
	var kind, reach, actorKind, actorBasis string
	var bindsFromDate, bindsFromZone, reviewByDate, reviewByZone, withdrawnAt *string
	if err := r.Scan(&c.ID, &actorKind, &c.Actor.Key, &actorBasis, &c.At,
		&kind, &reach, &c.SubjectID, &c.Statement, &c.RequiresSeam5Enforced,
		&bindsFromDate, &bindsFromZone, &reviewByDate, &reviewByZone, &c.ReplacesID, &withdrawnAt,
	); err != nil {
		return Constraint{}, err
	}
	c.Actor.Kind, c.Actor.Basis = record.Kind(actorKind), record.Basis(actorBasis)
	c.Kind, c.Reach = Kind(kind), Reach(reach)
	if bindsFromDate != nil && bindsFromZone != nil {
		c.BindsFrom = &CalendarDate{Date: *bindsFromDate, Zone: *bindsFromZone}
	}
	if reviewByDate != nil && reviewByZone != nil {
		c.ReviewBy = &CalendarDate{Date: *reviewByDate, Zone: *reviewByZone}
	}
	if withdrawnAt != nil {
		c.WithdrawnAt = *withdrawnAt
	}
	return c, nil
}

// get reads one constraint by id, through a pool or, inside a write, through
// the transaction that just changed it.
func get(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, id string) (Constraint, error) {
	c, err := scan(q.QueryRow(ctx, selectConstraint+` where id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Constraint{}, fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return Constraint{}, fmt.Errorf("constraint: reading %s: %w", id, err)
	}
	return c, nil
}

// Get is one constraint by id.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (Constraint, error) {
	return get(ctx, pool, id)
}

// Over is what an item's own draft reads a constraint against: its service's
// project, its area chain up to that project, and the intent it answers.
type Over struct {
	ProjectID string
	AreaChain []string
	IntentID  string
}

func rows(ctx context.Context, pool *pgxpool.Pool, query string, args ...any) ([]Constraint, error) {
	r, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("constraint: reading constraints: %w", err)
	}
	defer r.Close()

	var read []Constraint
	for r.Next() {
		c, err := scan(r)
		if err != nil {
			return nil, fmt.Errorf("constraint: reading a constraint: %w", err)
		}
		read = append(read, c)
	}
	if err := r.Err(); err != nil {
		return nil, fmt.Errorf("constraint: reading constraints: %w", err)
	}
	return read, nil
}

// InForce is every constraint in force at at over over, in insertion order:
// the factory's own, over.ProjectID's own, any area in over.AreaChain's own,
// and over.IntentID's own — the set drafting reads.
func InForce(ctx context.Context, pool *pgxpool.Pool, over Over, at time.Time) ([]Constraint, error) {
	areaChain := over.AreaChain
	if areaChain == nil {
		areaChain = []string{}
	}
	read, err := rows(ctx, pool, selectConstraint+`
		where withdrawn_at is null
		and (reach = 'factory'
			or (reach = 'project' and subject_id = $1)
			or (reach = 'area' and subject_id = any($2))
			or (reach = 'intent' and subject_id = $3))
		order by at, id`,
		over.ProjectID, areaChain, over.IntentID)
	if err != nil {
		return nil, err
	}
	return filterInForce(read, at), nil
}

// InForceForInterview is every constraint in force at at over the factory's
// own and intentID's own alone — the set the interview reads, deliberately
// less than [InForce]: before decomposition, which project the work is in is
// a guess and an intent's items may not share one.
func InForceForInterview(ctx context.Context, pool *pgxpool.Pool, intentID string, at time.Time) ([]Constraint, error) {
	return InForce(ctx, pool, Over{IntentID: intentID}, at)
}

// Permanent is every constraint in force at at whose reach is not
// [ReachIntent], ordered by reach and then insertion — Factory's list.
func Permanent(ctx context.Context, pool *pgxpool.Pool, at time.Time) ([]Constraint, error) {
	read, err := rows(ctx, pool, selectConstraint+`
		where withdrawn_at is null and reach <> 'intent'
		order by reach, at, id`)
	if err != nil {
		return nil, err
	}
	return filterInForce(read, at), nil
}

// DueForReview is every constraint not withdrawn whose review date has ended
// in its zone by at and which no other constraint names in ReplacesID — the
// row Work shows for whoever holds duty 2.
func DueForReview(ctx context.Context, pool *pgxpool.Pool, at time.Time) ([]Constraint, error) {
	read, err := rows(ctx, pool, selectConstraint+`
		where withdrawn_at is null
		and review_by_date is not null
		and not exists (select 1 from `+Table+` r where r.replaces_id = `+Table+`.id)
		order by at, id`)
	if err != nil {
		return nil, err
	}
	var due []Constraint
	for _, c := range read {
		if c.reviewEndedBy(at) {
			due = append(due, c)
		}
	}
	return due, nil
}

func filterInForce(read []Constraint, at time.Time) []Constraint {
	var in []Constraint
	for _, c := range read {
		if c.InForceAt(at) {
			in = append(in, c)
		}
	}
	return in
}
