package service

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
	// ErrNameEmpty is returned by [Writer.Create] for a service with no name.
	ErrNameEmpty = errors.New("service: the name is empty")
	// ErrRepositoryEmpty is returned by [Writer.Create] for a service with no
	// repository.
	ErrRepositoryEmpty = errors.New("service: the repository is empty")
	// ErrNotFound is returned by [Get] where no service has the id.
	ErrNotFound = errors.New("service: no service has that id")
)

// Service is one service as it is stored: the identity and repository decomposition
// wrote, and the parameters an owner authored on it.
type Service struct {
	ID         string
	Actor      record.Actor
	At         string
	Name       string
	Repository string
	Parameters Parameters
}

// Writer is the table's one writer, held by decomposition.
type Writer struct {
	pool *pgxpool.Pool
}

// NewWriter returns the writer over pool.
func NewWriter(pool *pgxpool.Pool) *Writer { return &Writer{pool: pool} }

// Create writes a service. A name already taken is refused by the store's
// unique constraint, and the error carries that refusal rather than this
// package pre-checking — a pre-check and an insert are two statements a
// concurrent decomposition can interleave.
func (w *Writer) Create(ctx context.Context, actor record.Actor, name, repository string) (Service, error) {
	if err := actor.Validate(); err != nil {
		return Service{}, err
	}
	if name == "" {
		return Service{}, ErrNameEmpty
	}
	if repository == "" {
		return Service{}, ErrRepositoryEmpty
	}

	s := Service{
		ID:         record.NewID(IDPrefix),
		Actor:      actor,
		At:         record.Now(),
		Name:       name,
		Repository: repository,
	}
	_, err := w.pool.Exec(ctx, `insert into `+Table+`
		(id, actor_kind, actor_name, at, name, repository,
		window_size, window_confidence, window_cap_seconds, window_limit)
		values ($1, $2, $3, $4, $5, $6, null, null, null, null)`,
		s.ID, string(s.Actor.Kind), s.Actor.Name, s.At, s.Name, s.Repository,
	)
	if err != nil {
		return Service{}, fmt.Errorf("service: creating %q: %w", name, err)
	}
	return s, nil
}

const selectService = `select id, actor_kind, actor_name, at, name, repository,
	window_size, window_confidence, window_cap_seconds, window_limit
	from ` + Table

// Get is one service by id. It takes the pool and not a [Writer], because
// reading a service is not a reason to be handed the thing that creates them.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (Service, error) {
	return scan(pool.QueryRow(ctx, selectService+` where id = $1`, id), id)
}

// ByName is the service of that name, and false where no service has it. The
// name is unique in the store, so at most one row can answer. It takes the
// pool and not a [Writer], for the same reason [Get] does: reading a service
// is not a reason to be handed the thing that creates them.
//
// This is what decomposition calls before it creates: a service the work changes
// may not exist yet, and decomposition writes a service's identity once, so the
// second item on that service reaches the record the first one wrote. An
// absent service is false and not an error, because absence is the case the
// caller acts on.
//
// What the pair costs: the look-up and the create are two statements, so two
// decompositions of one new service name can both find nothing, and what refuses the
// second create is the store's unique constraint rather than this function.
func ByName(ctx context.Context, pool *pgxpool.Pool, name string) (Service, bool, error) {
	s, err := scan(pool.QueryRow(ctx, selectService+` where name = $1`, name), name)
	if errors.Is(err, ErrNotFound) {
		return Service{}, false, nil
	} else if err != nil {
		return Service{}, false, err
	}
	return s, true, nil
}

// All is every service, in the order they were created. Its reader is the
// independent checker's own process, which is the one thing that has to walk
// every service there is: it compares what each production target runs against
// what the factory recorded, and nothing tells it which services to ask about.
//
// It takes the pool and not a [Writer], for the reason [Get] does — and the
// independent checker holds no writer of anything in the factory at all.
func All(ctx context.Context, pool *pgxpool.Pool) ([]Service, error) {
	rows, err := pool.Query(ctx, selectService+` order by at, id`)
	if err != nil {
		return nil, fmt.Errorf("service: reading the services: %w", err)
	}
	defer rows.Close()

	var read []Service
	for rows.Next() {
		s, err := scan(rows, "a service")
		if err != nil {
			return nil, err
		}
		read = append(read, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("service: reading the services: %w", err)
	}
	return read, nil
}

// scan reads one row, turning each null parameter column into an unauthored
// value rather than a zero.
func scan(row pgx.Row, named string) (Service, error) {
	var s Service
	var kind string
	var size, confidence, cap, limit *float64
	err := row.Scan(&s.ID, &kind, &s.Actor.Name, &s.At, &s.Name, &s.Repository,
		&size, &confidence, &cap, &limit)
	if errors.Is(err, pgx.ErrNoRows) {
		return Service{}, fmt.Errorf("%w: %s", ErrNotFound, named)
	} else if err != nil {
		return Service{}, fmt.Errorf("service: reading %s: %w", named, err)
	}
	s.Actor.Kind = record.Kind(kind)
	s.Parameters = Parameters{
		WindowSize:       authored(size),
		WindowConfidence: authored(confidence),
		WindowCapSeconds: authored(cap),
		WindowLimit:      authored(limit),
	}
	return s, nil
}

func authored(column *float64) gatepolicy.Authored {
	if column == nil {
		return gatepolicy.Authored{}
	}
	return gatepolicy.Authored{Number: *column, Present: true}
}
