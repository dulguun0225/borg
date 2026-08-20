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

// Mint is [Writer.MintWith] with nothing written beside the release.
func (w *Writer) Mint(ctx context.Context, actor record.Actor, serviceID, buildID, itemID string) (Release, error) {
	return w.MintWith(ctx, actor, serviceID, buildID, itemID, nil)
}

// MintWith writes the release record and its number in one transaction: the
// number is one above the highest the service has, read under a per-service
// advisory lock so two mints for one service serialise and take consecutive
// numbers. Numbers are never reused — nothing here deletes, and a rolled-back
// release keeps its number.
//
// inside is what the caller writes in the same transaction, given the release as
// it is about to be committed. It is the merge queue's, and what it writes is the
// contract versions the release publishes: a contract changes only inside its
// service's items and every write to it happens at a release, so the mint is the
// event, and one merge must not be able to leave a number with no version or a
// version under a number nothing minted. A nil hook is a mint with nothing beside
// it, which is what [Writer.Mint] is.
//
// The hook is an argument and not a second writer this package holds, so nothing
// here imports what it writes: this package sees a function that takes a
// transaction, and the caller sees the release it is writing against. What it
// costs is that a hook which errors takes the number down with it — the release is
// not minted and the item is re-verified on the next run, which is the safer of the
// two half-writes and the same choice the queue's own ordering makes.
//
// The transaction runs at read committed, stated explicitly rather than
// inherited: at repeatable read the snapshot is taken before the lock is
// granted, so the highest number read would be the highest as of before the
// previous mint committed, and two releases would take one number. The unique
// constraint would refuse the second, but as an error where the lock makes it
// a wait.
func (w *Writer) MintWith(ctx context.Context, actor record.Actor, serviceID, buildID, itemID string,
	inside func(context.Context, pgx.Tx, Release) error) (Release, error) {
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

	if inside != nil {
		if err := inside(ctx, tx, r); err != nil {
			return Release{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Release{}, fmt.Errorf("release: committing number %d of %s: %w", r.Number, serviceID, err)
	}
	return r, nil
}

const selectRelease = `select id, actor_kind, actor_name, at, service_id, number, build_id, item_id
	from ` + Table

// Get is one release by id. It takes the pool and not a [Writer], because
// reading a release is not a reason to be handed the thing that mints them.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (Release, error) {
	r, err := scan(pool.QueryRow(ctx, selectRelease+` where id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Release{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	} else if err != nil {
		return Release{}, fmt.Errorf("release: reading %s: %w", id, err)
	}
	return r, nil
}

func scan(row pgx.Row) (Release, error) {
	var r Release
	var kind string
	if err := row.Scan(&r.ID, &kind, &r.Actor.Name, &r.At, &r.ServiceID, &r.Number, &r.BuildID, &r.ItemID); err != nil {
		return Release{}, err
	}
	r.Actor.Kind = record.Kind(kind)
	return r, nil
}
