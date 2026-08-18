package build

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrCommitHashEmpty is returned by [Writer.Create] for a build naming no
	// commit.
	ErrCommitHashEmpty = errors.New("build: the commit hash is empty")
	// ErrItemIDEmpty is returned by [Writer.Create] for a build naming no
	// item. record's doc.go states what a link is checked for.
	ErrItemIDEmpty = errors.New("build: the item id is empty")
	// ErrNotFound is returned where the named build does not exist.
	ErrNotFound = errors.New("build: no build has that id")
)

// Build is one build record as it is stored.
type Build struct {
	ID         string
	Actor      record.Actor
	At         string
	ItemID     string
	CommitHash string
}

// Writer is the one writer of build records.
type Writer struct {
	pool *pgxpool.Pool
}

// NewWriter returns the writer over pool.
func NewWriter(pool *pgxpool.Pool) *Writer { return &Writer{pool: pool} }

// Create writes the build record, once. The record is never written again —
// there is no update method — and building the same commit for the same item
// a second time is refused by the store's unique constraint rather than
// given a second record.
func (w *Writer) Create(ctx context.Context, actor record.Actor, itemID, commitHash string) (Build, error) {
	if err := actor.Validate(); err != nil {
		return Build{}, err
	}
	if itemID == "" {
		return Build{}, ErrItemIDEmpty
	}
	if commitHash == "" {
		return Build{}, ErrCommitHashEmpty
	}

	b := Build{
		ID:         record.NewID(IDPrefix),
		Actor:      actor,
		At:         record.Now(),
		ItemID:     itemID,
		CommitHash: commitHash,
	}
	_, err := w.pool.Exec(ctx, `insert into `+Table+`
		(id, actor_kind, actor_name, at, item_id, commit_hash)
		values ($1, $2, $3, $4, $5, $6)`,
		b.ID, string(b.Actor.Kind), b.Actor.Name, b.At, b.ItemID, b.CommitHash,
	)
	if err != nil {
		return Build{}, fmt.Errorf("build: creating %s: %w", b.ID, err)
	}
	return b, nil
}

// Get is one build by id. It takes the pool and not a [Writer], because
// reading a build is not a reason to be handed the thing that writes them.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (Build, error) {
	var b Build
	var kind string
	err := pool.QueryRow(ctx, `select id, actor_kind, actor_name, at, item_id, commit_hash
		from `+Table+` where id = $1`, id).
		Scan(&b.ID, &kind, &b.Actor.Name, &b.At, &b.ItemID, &b.CommitHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return Build{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	} else if err != nil {
		return Build{}, fmt.Errorf("build: reading %s: %w", id, err)
	}
	b.Actor.Kind = record.Kind(kind)
	return b, nil
}
