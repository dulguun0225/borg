package release

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrNotFound is returned where the named release does not exist.
	ErrNotFound = errors.New("release: no release has that id")
	// ErrServiceIDEmpty is returned by [Writer.Mint] for a release naming no
	// service. record's doc.go states what a link is checked for.
	ErrServiceIDEmpty = errors.New("release: the service id is empty")
	// ErrBuildIDEmpty is returned by [Writer.Mint] for a release naming no
	// build.
	ErrBuildIDEmpty = errors.New("release: the build id is empty")
	// ErrItemIDEmpty is returned by [Writer.Mint] for a release naming no
	// item.
	ErrItemIDEmpty = errors.New("release: the item id is empty")
)

// Release is one release record as it is stored.
type Release struct {
	ID        string
	Actor     record.Actor
	At        string
	ServiceID string
	Number    int64
	BuildID   string
	ItemID    string
}

// Writer is the one writer of release records.
type Writer struct {
	pool *pgxpool.Pool
}

// NewWriter returns the writer over pool.
func NewWriter(pool *pgxpool.Pool) *Writer { return &Writer{pool: pool} }

// Mint writes the release record and its number in one transaction: the
// number is one above the highest the service has, read under a per-service
// advisory lock so two mints for one service serialise and take consecutive
// numbers. Numbers are never reused — nothing here deletes, and a rolled-back
// release keeps its number.
//
// The transaction runs at read committed, stated explicitly rather than
// inherited: at repeatable read the snapshot is taken before the lock is
// granted, so the highest number read would be the highest as of before the
// previous mint committed, and two releases would take one number. The unique
// constraint would refuse the second, but as an error where the lock makes it
// a wait.
func (w *Writer) Mint(ctx context.Context, actor record.Actor, serviceID, buildID, itemID string) (Release, error) {
	if err := actor.Validate(); err != nil {
		return Release{}, err
	}
	if serviceID == "" {
		return Release{}, ErrServiceIDEmpty
	}
	if buildID == "" {
		return Release{}, ErrBuildIDEmpty
	}
	if itemID == "" {
		return Release{}, ErrItemIDEmpty
	}

	tx, err := w.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return Release{}, fmt.Errorf("release: beginning the mint: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, AdvisoryLockKey(serviceID)); err != nil {
		return Release{}, fmt.Errorf("release: taking the mint lock for %s: %w", serviceID, err)
	}

	var number int64
	err = tx.QueryRow(ctx, `select coalesce(max(number), 0) + 1 from `+Table+` where service_id = $1`, serviceID).
		Scan(&number)
	if err != nil {
		return Release{}, fmt.Errorf("release: reading the highest number of %s: %w", serviceID, err)
	}

	r := Release{
		ID:        record.NewID(IDPrefix),
		Actor:     actor,
		At:        record.Now(),
		ServiceID: serviceID,
		Number:    number,
		BuildID:   buildID,
		ItemID:    itemID,
	}
	if _, err := tx.Exec(ctx, `insert into `+Table+`
		(id, actor_kind, actor_name, at, service_id, number, build_id, item_id)
		values ($1, $2, $3, $4, $5, $6, $7, $8)`,
		r.ID, string(r.Actor.Kind), r.Actor.Name, r.At, r.ServiceID, r.Number, r.BuildID, r.ItemID,
	); err != nil {
		return Release{}, fmt.Errorf("release: minting number %d of %s: %w", r.Number, serviceID, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Release{}, fmt.Errorf("release: committing number %d of %s: %w", r.Number, serviceID, err)
	}
	return r, nil
}

// Get is one release by id. It takes the pool and not a [Writer], because
// reading a release is not a reason to be handed the thing that mints them.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (Release, error) {
	var r Release
	var kind string
	err := pool.QueryRow(ctx, `select id, actor_kind, actor_name, at, service_id, number, build_id, item_id
		from `+Table+` where id = $1`, id).
		Scan(&r.ID, &kind, &r.Actor.Name, &r.At, &r.ServiceID, &r.Number, &r.BuildID, &r.ItemID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Release{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	} else if err != nil {
		return Release{}, fmt.Errorf("release: reading %s: %w", id, err)
	}
	r.Actor.Kind = record.Kind(kind)
	return r, nil
}
