package window

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
	// ErrNotAHuman is returned by [WriteMark] for an actor that is not a human. A
	// crossing is not proof that the release caused it, and the judgment that it
	// did not is a named human's at Ops — the one judgment in a mechanism the
	// design otherwise keeps to arithmetic.
	ErrNotAHuman = errors.New("window: a rollback is marked by a named human at Ops")
	// ErrMarkIncomplete is returned by [WriteMark] for a mark naming no rollback,
	// no service, or no reason.
	ErrMarkIncomplete = errors.New("window: a mark names the rollback it corrects, its service, and the reason")
	// ErrAlreadyMarked is returned by [WriteMark] for a rollback already marked.
	// The mark is written once: a second one would be two reasons on one
	// correction.
	ErrAlreadyMarked = errors.New("window: that rollback is marked already, and a mark is written once")
)

// Mark is a rollback a named human at Ops marked as not caused by the release,
// with the reason they stated. It points at the rollback's deploy record.
//
// It is read by everything that learns from outcomes and by nothing that acts:
// the rollback stands, the incident stands, and nothing the factory did on its
// own is undone by it. What it changes is the evidence — the per-author prior,
// the window's size and its power, and the window limit all exclude a marked
// rollback — and, before the revert ships, the revert item and the hold, which
// are two other packages' writes and not this one's.
type Mark struct {
	ID    string
	Actor record.Actor
	At    string
	// DeployID is the rollback's own deploy record, which is what the mark points
	// at. The rollback is a deploy event and every field of it is on that record.
	DeployID  string
	ServiceID string
	Reason    string
}

// WriteMark writes the mark inside the caller's transaction, fencing it with
// token first. It takes a transaction rather than opening one because the caller
// is the command-line interface acting for a named human at Ops, which ends the
// revert item in the same event where the revert has not shipped — and a mark
// that landed without that would leave the item to ship a revert nobody wants.
//
// The caller is Ops and there is no other: [ErrNotAHuman] refuses a component,
// and so does the CHECK in [DDL].
func WriteMark(ctx context.Context, tx pgx.Tx, token lease.Token, actor record.Actor,
	deployID, serviceID, reason string) (Mark, error) {
	if err := lease.Fence(ctx, tx, token); err != nil {
		return Mark{}, err
	}
	if err := actor.Validate(); err != nil {
		return Mark{}, err
	}
	if actor.Kind != record.KindHuman {
		return Mark{}, fmt.Errorf("%w: %s %q", ErrNotAHuman, actor.Kind, actor.Key)
	}
	for _, required := range []struct{ what, value string }{
		{"rollback", deployID}, {"service", serviceID}, {"reason", reason},
	} {
		if required.value == "" {
			return Mark{}, fmt.Errorf("%w: no %s", ErrMarkIncomplete, required.what)
		}
	}

	m := Mark{
		ID:        record.NewID(MarkIDPrefix),
		Actor:     actor,
		At:        record.Now(),
		DeployID:  deployID,
		ServiceID: serviceID,
		Reason:    reason,
	}
	var already int
	err := tx.QueryRow(ctx, `select 1 from `+MarkTable+` where deploy_id = $1`, deployID).Scan(&already)
	if err == nil {
		return Mark{}, fmt.Errorf("%w: %s", ErrAlreadyMarked, deployID)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Mark{}, fmt.Errorf("window: reading the mark on %s: %w", deployID, err)
	}
	_, err = tx.Exec(ctx, `insert into `+MarkTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, deploy_id, service_id, reason)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		m.ID, FormatVersionMark, string(m.Actor.Kind), m.Actor.Key, string(m.Actor.Basis), m.At,
		m.DeployID, m.ServiceID, m.Reason)
	if err != nil {
		return Mark{}, fmt.Errorf("window: marking the rollback %s: %w", deployID, err)
	}
	return m, nil
}

const selectMark = `select id, actor_kind, actor_key, actor_key_basis, at, deploy_id, service_id, reason
	from ` + MarkTable

// Marked is the mark on one rollback, and false where the rollback carries
// none. It is what a mechanism learning from that rollback asks before it folds
// the outcome in.
func Marked(ctx context.Context, pool *pgxpool.Pool, deployID string) (Mark, bool, error) {
	if deployID == "" {
		return Mark{}, false, nil
	}
	m, err := scanMark(pool.QueryRow(ctx, selectMark+` where deploy_id = $1`, deployID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Mark{}, false, nil
	} else if err != nil {
		return Mark{}, false, fmt.Errorf("window: reading the mark on %s: %w", deployID, err)
	}
	return m, true, nil
}

// Marks is every mark in the store, oldest first. It is the whole-table read the
// score makes, for the reason its other whole-table reads are: the subjects it
// learns about are the services the records name.
func Marks(ctx context.Context, pool *pgxpool.Pool) ([]Mark, error) {
	rows, err := pool.Query(ctx, selectMark+` order by at, id`)
	if err != nil {
		return nil, fmt.Errorf("window: reading the marks: %w", err)
	}
	defer rows.Close()

	var marks []Mark
	for rows.Next() {
		m, err := scanMark(rows)
		if err != nil {
			return nil, fmt.Errorf("window: reading a mark: %w", err)
		}
		marks = append(marks, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("window: reading the marks: %w", err)
	}
	return marks, nil
}

func scanMark(row pgx.Row) (Mark, error) {
	var m Mark
	var kind, basis string
	err := row.Scan(&m.ID, &kind, &m.Actor.Key, &basis, &m.At, &m.DeployID, &m.ServiceID, &m.Reason)
	if err != nil {
		return Mark{}, err
	}
	m.Actor.Kind = record.Kind(kind)
	m.Actor.Basis = record.Basis(basis)
	return m, nil
}
