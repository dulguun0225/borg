package people

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

// Declaration is one row of the holding table: a per-person key holding one
// duty or one obligation. What that key lent is [Credential], a row of its
// own.
type Declaration struct {
	ID    string
	Actor record.Actor
	At    string
	// Key is the per-person opaque key this row is about. It is never a
	// name: [NameOf] is the one place a key resolves to one, through
	// [MappingTable].
	Key string
	// Duty is the duty held, and is zero where the row names an obligation.
	Duty Duty
	// Obligation is the obligation held, and is empty where the row names a
	// duty.
	Obligation Obligation
	// WithdrawnAt is when the holding ended, and is empty while it stands.
	// The row is kept, so a page delivered to a holder who has since
	// stopped holding is still readable against the row that routed it.
	WithdrawnAt string
}

// Holds reports whether the declaration still stands.
func (d Declaration) Holds() bool { return d.WithdrawnAt == "" }

const selectDeclaration = `select id, actor_kind, actor_key, actor_key_basis, at, person_key, duty, obligation,
	withdrawn_at
	from ` + Table

func scan(row pgx.Row) (Declaration, error) {
	var d Declaration
	var kind, basis, obligation string
	var duty int
	if err := row.Scan(&d.ID, &kind, &d.Actor.Key, &basis, &d.At, &d.Key, &duty, &obligation,
		&d.WithdrawnAt); err != nil {
		return Declaration{}, err
	}
	d.Actor.Kind = record.Kind(kind)
	d.Actor.Basis = record.Basis(basis)
	d.Duty = Duty(duty)
	d.Obligation = Obligation(obligation)
	return d, nil
}

// Get is one declaration by id. It takes the pool and not a [Writer],
// because reading who holds what is not a reason to be handed the thing
// that declares it.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (Declaration, error) {
	d, err := scan(pool.QueryRow(ctx, selectDeclaration+` where id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Declaration{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	} else if err != nil {
		return Declaration{}, fmt.Errorf("people: reading %s: %w", id, err)
	}
	return d, nil
}

// ByHolding is the declaration of one key and one holding, whether or not it
// still stands.
func ByHolding(ctx context.Context, pool *pgxpool.Pool, key string, holding Holding) (Declaration, error) {
	d, err := scan(pool.QueryRow(ctx, selectDeclaration+`
		where person_key = $1 and duty = $2 and obligation = $3`,
		key, int(holding.Duty), string(holding.Obligation)))
	if errors.Is(err, pgx.ErrNoRows) {
		return Declaration{}, fmt.Errorf("%w: %s holding %s", ErrNotFound, key, holding)
	} else if err != nil {
		return Declaration{}, fmt.Errorf("people: reading the declaration that %s holds %s: %w", key, holding, err)
	}
	return d, nil
}

// Holders is every per-person key that holds this duty or obligation and has
// not withdrawn from it, in the order it was declared. It is what the
// notifier routes on, and no holders is a routing answer and not a missing
// one: the page widens to the owner, who is the person that would have
// written the row.
func Holders(ctx context.Context, pool *pgxpool.Pool, holding Holding) ([]string, error) {
	if err := holding.validate(); err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `select person_key from `+Table+`
		where duty = $1 and obligation = $2 and withdrawn_at = '' order by at, id`,
		int(holding.Duty), string(holding.Obligation))
	if err != nil {
		return nil, fmt.Errorf("people: reading who holds %s: %w", holding, err)
	}
	defer rows.Close()

	var holders []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("people: reading a holder of %s: %w", holding, err)
		}
		holders = append(holders, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("people: reading who holds %s: %w", holding, err)
	}
	return holders, nil
}

// All is every declaration, standing or withdrawn, in the order it was
// declared. It is what the command-line interface prints.
func All(ctx context.Context, pool *pgxpool.Pool) ([]Declaration, error) {
	rows, err := pool.Query(ctx, selectDeclaration+` order by at, id`)
	if err != nil {
		return nil, fmt.Errorf("people: reading the declaration: %w", err)
	}
	defer rows.Close()

	var read []Declaration
	for rows.Next() {
		d, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("people: reading a row of the declaration: %w", err)
		}
		read = append(read, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("people: reading the declaration: %w", err)
	}
	return read, nil
}
