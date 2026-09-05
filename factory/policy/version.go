package policy

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/record"
)

// ErrNoVersion is returned by [Get] where no version has the id, and by
// [InForce] where nothing has been written yet — a factory that has not been
// installed has no policy version, and a gate cannot fire without one.
var ErrNoVersion = errors.New("policy: no policy version")

// Action is what an authoring write was.
type Action string

const (
	// ActionCreated is a record Factory created: the factory-wide settings record, or
	// production's environment. It authors no parameter and is the first
	// version a factory has.
	ActionCreated Action = "created"
	// ActionAuthored is an owner authoring one parameter on one record.
	ActionAuthored Action = "authored"
	// ActionSafeguardAdded is an owner placing a safeguard.
	ActionSafeguardAdded Action = "safeguard_added"
	// ActionWithdrawn is an owner withdrawing one.
	ActionWithdrawn Action = "withdrawn"
)

// Subject is where an authoring write landed: the record, and the second name
// its scope needs where it needs one — the gate row for a threshold, the stage
// for an attempt limit.
type Subject struct {
	Kind      string
	ID        string
	Qualifier string
}

func (s Subject) String() string {
	if s.Qualifier == "" {
		return s.Kind + ":" + s.ID
	}
	return s.Kind + ":" + s.ID + ":" + s.Qualifier
}

// Version is one policy version as it is stored: one authoring write, and the
// version it replaced.
type Version struct {
	ID          string
	Actor       record.Actor
	At          string
	Action      Action
	Parameter   gatepolicy.Parameter
	Subject     Subject
	SafeguardID string
	Supersedes  string
}

// append writes one version inside tx. Every authoring write goes through the
// same call, so no write can move the policy without moving the version.
func appendVersion(ctx context.Context, tx pgx.Tx, actor record.Actor, action Action,
	parameter gatepolicy.Parameter, subject Subject, safeguardID string) (Version, error) {
	if err := actor.Validate(); err != nil {
		return Version{}, err
	}

	var supersedes string
	err := tx.QueryRow(ctx, `select id from `+Table+` order by at desc, id desc limit 1`).Scan(&supersedes)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Version{}, fmt.Errorf("policy: reading the version in force: %w", err)
	}

	v := Version{
		ID:          record.NewID(IDPrefix),
		Actor:       actor,
		At:          record.Now(),
		Action:      action,
		Parameter:   parameter,
		Subject:     subject,
		SafeguardID: safeguardID,
		Supersedes:  supersedes,
	}
	_, err = tx.Exec(ctx, `insert into `+Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, action, parameter, subject_kind, subject_id, qualifier, safeguard_id, supersedes)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		v.ID, FormatVersion, string(v.Actor.Kind), v.Actor.Key, string(v.Actor.Basis), v.At, string(v.Action),
		string(v.Parameter), v.Subject.Kind, v.Subject.ID, v.Subject.Qualifier,
		v.SafeguardID, v.Supersedes,
	)
	if err != nil {
		return Version{}, fmt.Errorf("policy: appending a version for %s on %s: %w", action, subject, err)
	}
	return v, nil
}

const selectVersion = `select id, actor_kind, actor_key, actor_key_basis, at, action, parameter,
	subject_kind, subject_id, qualifier, safeguard_id, supersedes
	from ` + Table

// InForce is the policy version in force, which is the newest row. A gate
// firing names it, so a factory with no version is [ErrNoVersion] and not an
// empty string passed off as a version.
func InForce(ctx context.Context, pool *pgxpool.Pool) (Version, error) {
	return scanVersion(pool.QueryRow(ctx, selectVersion+` order by at desc, id desc limit 1`), "in force")
}

// Get is one version by id, which is what a reader of a decision follows to the
// write that was the last one before it was decided.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (Version, error) {
	return scanVersion(pool.QueryRow(ctx, selectVersion+` where id = $1`, id), id)
}

// All is every policy version, oldest first, which is how the writes before a
// version are replayed.
func All(ctx context.Context, pool *pgxpool.Pool) ([]Version, error) {
	rows, err := pool.Query(ctx, selectVersion+` order by at, id`)
	if err != nil {
		return nil, fmt.Errorf("policy: reading the versions: %w", err)
	}
	defer rows.Close()

	var read []Version
	for rows.Next() {
		v, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		read = append(read, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("policy: reading the versions: %w", err)
	}
	return read, nil
}

func scanVersion(row pgx.Row, named string) (Version, error) {
	var v Version
	var kind, basis, action, parameter string
	err := row.Scan(&v.ID, &kind, &v.Actor.Key, &basis, &v.At, &action, &parameter,
		&v.Subject.Kind, &v.Subject.ID, &v.Subject.Qualifier, &v.SafeguardID, &v.Supersedes)
	if errors.Is(err, pgx.ErrNoRows) {
		return Version{}, fmt.Errorf("%w: %s", ErrNoVersion, named)
	} else if err != nil {
		return Version{}, fmt.Errorf("policy: reading the version %s: %w", named, err)
	}
	v.Actor.Kind = record.Kind(kind)
	v.Actor.Basis = record.Basis(basis)
	v.Action = Action(action)
	v.Parameter = gatepolicy.Parameter(parameter)
	return v, nil
}

func scanRow(rows pgx.Rows) (Version, error) {
	var v Version
	var kind, basis, action, parameter string
	err := rows.Scan(&v.ID, &kind, &v.Actor.Key, &basis, &v.At, &action, &parameter,
		&v.Subject.Kind, &v.Subject.ID, &v.Subject.Qualifier, &v.SafeguardID, &v.Supersedes)
	if err != nil {
		return Version{}, fmt.Errorf("policy: reading a version: %w", err)
	}
	v.Actor.Kind = record.Kind(kind)
	v.Actor.Basis = record.Basis(basis)
	v.Action = Action(action)
	v.Parameter = gatepolicy.Parameter(parameter)
	return v, nil
}
