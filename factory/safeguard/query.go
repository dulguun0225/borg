package safeguard

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/record"
)

const selectSafeguards = `select id, actor_kind, actor_key, actor_key_basis, at, parameter, subject_kind,
	subject_id, subject_key, direction, bound, bound_list, predicate_kind, predicate_argument,
	route_duty, route_human_key
	from ` + Table

// BySubjects is every safeguard in force on one parameter across any of the
// subjects, which is the one read a mechanism a safeguard binds performs. A
// safeguard an approved [Withdrawal] names is not in force and is not
// returned. It takes the pool and not a [Writer], because reading safeguards
// is not a reason to be handed the thing that places them.
//
// The subjects are a list because a mechanism reads more than one at a time: a
// gate firing on an item reads the row's own subjects — the item's service and
// every area in the item's chain — and a safeguard on any of them reaches the
// firing. A subject's [Subject.Key] is matched exactly, so a caller asking
// about one gate row, stage, duty, severity, quantity or service reads only
// the safeguards keyed to it.
func BySubjects(ctx context.Context, pool *pgxpool.Pool, parameter gatepolicy.Parameter, subjects []Subject) ([]Safeguard, error) {
	if len(subjects) == 0 {
		return nil, nil
	}
	kinds := make([]string, 0, len(subjects))
	ids := make([]string, 0, len(subjects))
	keys := make([]string, 0, len(subjects))
	for _, s := range subjects {
		kinds = append(kinds, string(s.Kind))
		ids = append(ids, s.ID)
		keys = append(keys, s.Key)
	}
	rows, err := pool.Query(ctx, selectSafeguards+`
		where parameter = $1
		and (subject_kind, subject_id, subject_key) in (select * from unnest($2::text[], $3::text[], $4::text[]))
		and not exists (select 1 from `+WithdrawalTable+` w where w.safeguard_id = `+Table+`.id and w.approved)
		order by at, id`, string(parameter), kinds, ids, keys)
	if err != nil {
		return nil, fmt.Errorf("safeguard: reading the safeguards on %s: %w", parameter, err)
	}
	defer rows.Close()

	var read []Safeguard
	for rows.Next() {
		p, err := scan(rows)
		if err != nil {
			return nil, err
		}
		read = append(read, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("safeguard: reading the safeguards on %s: %w", parameter, err)
	}
	return read, nil
}

// All is every safeguard ever placed, an approved withdrawal's included, in
// the order they were placed, with [Safeguard.Withdrawn] read off
// [WithdrawalTable]. It is what the command-line interface prints; a mechanism reads
// [BySubjects].
func All(ctx context.Context, pool *pgxpool.Pool) ([]Safeguard, error) {
	rows, err := pool.Query(ctx, `select id, actor_kind, actor_key, actor_key_basis, at, parameter, subject_kind,
		subject_id, subject_key, direction, bound, bound_list, predicate_kind, predicate_argument,
		route_duty, route_human_key,
		exists (select 1 from `+WithdrawalTable+` w where w.safeguard_id = `+Table+`.id and w.approved)
		from `+Table+` order by at, id`)
	if err != nil {
		return nil, fmt.Errorf("safeguard: reading the safeguards: %w", err)
	}
	defer rows.Close()

	var read []Safeguard
	for rows.Next() {
		p, err := scanWithWithdrawn(rows)
		if err != nil {
			return nil, err
		}
		read = append(read, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("safeguard: reading the safeguards: %w", err)
	}
	return read, nil
}

func scan(rows pgx.Rows) (Safeguard, error) {
	var p Safeguard
	var kind, basis, parameter, subjectKind, direction, boundList, predicateKind string
	var bound *float64
	var duty *int
	err := rows.Scan(&p.ID, &kind, &p.Actor.Key, &basis, &p.At, &parameter, &subjectKind,
		&p.Subject.ID, &p.Subject.Key, &direction, &bound, &boundList, &predicateKind, &p.Bound.Predicate.Argument,
		&duty, &p.Routing.HumanKey)
	if err != nil {
		return Safeguard{}, fmt.Errorf("safeguard: reading a safeguard: %w", err)
	}
	fill(&p, kind, basis, parameter, subjectKind, direction, boundList, predicateKind, bound, duty)
	return p, nil
}

func scanWithWithdrawn(rows pgx.Rows) (Safeguard, error) {
	var p Safeguard
	var kind, basis, parameter, subjectKind, direction, boundList, predicateKind string
	var bound *float64
	var duty *int
	err := rows.Scan(&p.ID, &kind, &p.Actor.Key, &basis, &p.At, &parameter, &subjectKind,
		&p.Subject.ID, &p.Subject.Key, &direction, &bound, &boundList, &predicateKind, &p.Bound.Predicate.Argument,
		&duty, &p.Routing.HumanKey, &p.Withdrawn)
	if err != nil {
		return Safeguard{}, fmt.Errorf("safeguard: reading a safeguard: %w", err)
	}
	fill(&p, kind, basis, parameter, subjectKind, direction, boundList, predicateKind, bound, duty)
	return p, nil
}

// fill sets the fields common to [scan] and [scanWithWithdrawn] once decoded,
// so the two queries' extra column is the only thing that differs between
// them.
func fill(p *Safeguard, kind, basis, parameter, subjectKind, direction, boundList, predicateKind string,
	bound *float64, duty *int) {
	p.Actor.Kind = record.Kind(kind)
	p.Actor.Basis = record.Basis(basis)
	p.Parameter = gatepolicy.Parameter(parameter)
	p.Subject.Kind = SubjectKind(subjectKind)
	p.Direction = gatepolicy.Direction(direction)
	p.Bound.Predicate.Kind = gatepolicy.PredicateKind(predicateKind)
	if bound != nil {
		p.Bound.Number = *bound
	}
	if boundList != "" {
		p.Bound.List = strings.Split(boundList, "\n")
	}
	if duty != nil {
		p.Routing.Duty = *duty
	}
}
