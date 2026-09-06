package project

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
	// ErrNameEmpty is returned for a project with no name.
	ErrNameEmpty = errors.New("project: the name is empty")
	// ErrNotFound is returned by [Get] where no project has the id.
	ErrNotFound = errors.New("project: no project has that id")
	// ErrServicesStandInIt is returned by [End] where a service in the project is
	// not retired. A project is ended once every service in it is, each ending by
	// an owner's write on its own record.
	ErrServicesStandInIt = errors.New("project: a service in the project is not retired")
	// ErrAlreadyEnded is returned by [End] for a project already ended. Ending one
	// is one event, and the production environment that ends with it is withdrawn
	// once.
	ErrAlreadyEnded = errors.New("project: the project is already ended")
)

// Project is one project as it is stored: its identity, and when an owner ended
// it.
type Project struct {
	ID    string
	Actor record.Actor
	At    string
	Name  string
	// EndedAt is when an owner ended the project at Factory, and is empty while
	// it stands.
	EndedAt string
}

// Ended is whether an owner has ended the project. A reader that skips an ended
// project asks this rather than comparing the timestamp itself.
func (p Project) Ended() bool { return p.EndedAt != "" }

// Writer is the table's one writer: an owner at Factory.
type Writer struct {
	pool  *pgxpool.Pool
	token lease.Token
}

// NewWriter returns the writer over pool, fencing every write with token.
func NewWriter(pool *pgxpool.Pool, token lease.Token) *Writer {
	return &Writer{pool: pool, token: token}
}

// Create writes a project in a transaction of its own. The write that creates a
// project writes production's environment in the same event, and that
// composition is package policy's, which calls [Insert] inside its own
// transaction; this is the same write where there is nothing to compose it with.
func (w *Writer) Create(ctx context.Context, actor record.Actor, name string) (Project, error) {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return Project{}, fmt.Errorf("project: beginning the creation of %q: %w", name, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	p, err := Insert(ctx, tx, w.token, actor, name)
	if err != nil {
		return Project{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Project{}, fmt.Errorf("project: committing the creation of %q: %w", name, err)
	}
	return p, nil
}

// Insert writes a project inside tx, fencing it with token first. Its caller is
// package policy, which writes production's environment in the same
// transaction. A name already taken is refused by the store's unique constraint
// and not by a pre-check here, a pre-check and an insert being two statements a
// second creation can interleave.
func Insert(ctx context.Context, tx pgx.Tx, token lease.Token, actor record.Actor, name string) (Project, error) {
	if err := lease.Fence(ctx, tx, token); err != nil {
		return Project{}, err
	}
	if err := actor.Validate(); err != nil {
		return Project{}, err
	}
	if name == "" {
		return Project{}, ErrNameEmpty
	}

	p := Project{ID: record.NewID(IDPrefix), Actor: actor, At: record.Now(), Name: name}
	_, err := tx.Exec(ctx, `insert into `+Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, name, ended_at)
		values ($1, $2, $3, $4, $5, $6, $7, '')`,
		p.ID, FormatVersion, string(p.Actor.Kind), p.Actor.Key, string(p.Actor.Basis), p.At, p.Name,
	)
	if err != nil {
		return Project{}, fmt.Errorf("project: creating %q: %w", name, err)
	}
	return p, nil
}

// End writes when an owner ended the project, which they may do once every
// service in it is retired. Its caller is package policy, which withdraws
// production's environment in the same transaction — the project's production
// environment ends with it — and appends the policy version there.
//
// servicesNotRetired is a count the caller read, not a query made here: a
// service record is another package's and the direction between the two is
// service -> project, so the refusal is stated here over a number the caller
// read inside the same transaction. What that costs is a caller that can pass a
// count it did not read.
//
// The row stays and is never deleted, for the reason it never was: an area, a
// constraint, a safeguard or a scope naming the project would otherwise point at
// nothing.
func End(ctx context.Context, tx pgx.Tx, token lease.Token, actor record.Actor,
	id string, servicesNotRetired int) error {
	if err := lease.Fence(ctx, tx, token); err != nil {
		return err
	}
	if err := actor.Validate(); err != nil {
		return err
	}
	if servicesNotRetired < 0 {
		return fmt.Errorf("project: the services not retired is not a count: %d", servicesNotRetired)
	}
	if servicesNotRetired > 0 {
		return fmt.Errorf("%w: %d of them in %s", ErrServicesStandInIt, servicesNotRetired, id)
	}
	var endedAt string
	err := tx.QueryRow(ctx, `select ended_at from `+Table+` where id = $1 for update`, id).Scan(&endedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	} else if err != nil {
		return fmt.Errorf("project: reading %s: %w", id, err)
	}
	if endedAt != "" {
		return fmt.Errorf("%w: %s at %s", ErrAlreadyEnded, id, endedAt)
	}
	if _, err := tx.Exec(ctx, `update `+Table+` set ended_at = $1 where id = $2`, record.Now(), id); err != nil {
		return fmt.Errorf("project: ending %s: %w", id, err)
	}
	return nil
}

const selectProject = `select id, actor_kind, actor_key, actor_key_basis, at, name, ended_at from ` + Table

// Get is one project by id. It takes the pool and not a [Writer], because
// reading a project is not a reason to be handed the thing that creates them.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (Project, error) {
	return scan(pool.QueryRow(ctx, selectProject+` where id = $1`, id), id)
}

// ByName is the project of that name, and false where none has it. The name is
// unique in the store, so at most one row can answer. An absent project is false
// and not an error, because absence is the case the caller acts on: a service
// cannot be created until the project it names exists.
func ByName(ctx context.Context, pool *pgxpool.Pool, name string) (Project, bool, error) {
	p, err := scan(pool.QueryRow(ctx, selectProject+` where name = $1`, name), name)
	if errors.Is(err, ErrNotFound) {
		return Project{}, false, nil
	} else if err != nil {
		return Project{}, false, err
	}
	return p, true, nil
}

// All is every project, in the order they were created.
func All(ctx context.Context, pool *pgxpool.Pool) ([]Project, error) {
	rows, err := pool.Query(ctx, selectProject+` order by at, id`)
	if err != nil {
		return nil, fmt.Errorf("project: reading the projects: %w", err)
	}
	defer rows.Close()

	var read []Project
	for rows.Next() {
		p, err := scan(rows, "a project")
		if err != nil {
			return nil, err
		}
		read = append(read, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("project: reading the projects: %w", err)
	}
	return read, nil
}

func scan(row pgx.Row, named string) (Project, error) {
	var p Project
	var kind, basis string
	err := row.Scan(&p.ID, &kind, &p.Actor.Key, &basis, &p.At, &p.Name, &p.EndedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, fmt.Errorf("%w: %s", ErrNotFound, named)
	} else if err != nil {
		return Project{}, fmt.Errorf("project: reading %s: %w", named, err)
	}
	p.Actor.Kind = record.Kind(kind)
	p.Actor.Basis = record.Basis(basis)
	return p, nil
}
