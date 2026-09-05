package release

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
	// ErrNotFound is returned where the named release does not exist.
	ErrNotFound = errors.New("release: no release has that id")
	// ErrServiceIDEmpty is returned by [Writer.Mint] for a release naming no
	// service. record's doc.go states what a link is checked for.
	ErrServiceIDEmpty = errors.New("release: the service id is empty")
	// ErrBuildIDEmpty is returned by [Writer.Mint] for a release naming no
	// build. Every release names one, including the one minted over a commit a
	// human accepted, which the queue builds before it mints.
	ErrBuildIDEmpty = errors.New("release: the build id is empty")
	// ErrCommitEmpty is returned by [Writer.Mint] for a release naming no
	// commit. The record is keyed on the commit, so a release without one could
	// not be written again without writing a second release.
	ErrCommitEmpty = errors.New("release: the commit is empty")
)

// Release is one release record as it is stored.
type Release struct {
	ID        string
	Actor     record.Actor
	At        string
	ServiceID string
	Number    int64
	BuildID   string
	// Commit is the commit on master this release is made of. The record is
	// keyed on it, so the fast-forward and this write are one operation
	// restartable from either side.
	Commit string
	// ItemID is the item that caused the release, and is empty on a release
	// over a commit a human accepted, which names a build and no item. The
	// column is null there rather than empty, which is what lets one item per
	// release be a partial unique index in the store.
	ItemID string
}

// NamesAnItem reports whether a gate decided this release. A release naming no
// item is one no gate decided: no consumer contract is derived for it, its
// authorship rollup names nothing the factory wrote, and every traversal from it
// ends at the acceptance rather than at an intent.
func (r Release) NamesAnItem() bool { return r.ItemID != "" }

// Minting is what a release names at the event that writes it: the service, the
// build it is made of, the commit that build was made from, and the item that
// caused it where one did.
//
// There are two such events and one writer. The fast-forward that merges a
// candidate names an item; a human's acceptance of a commit the queue did not
// make names none, and the queue mints over it once the acceptance stands.
type Minting struct {
	ServiceID string
	BuildID   string
	Commit    string
	// ItemID is empty on a release over an accepted commit.
	ItemID string
	// Floor is a number the mint may not seat at or below, and 0 where the
	// caller has none. The number minted is one above the higher of the
	// service's highest record and this, which is what the first mint after the
	// factory's records were restored from a backup needs: the numbers of the
	// releases the restore lost are above the highest record and must not be
	// reused, and what says how high they went is a store outside the records.
	// The queue is the caller that reads that store and hands the reading here;
	// this package reads nothing but its own table.
	Floor int64
}

// Writer is the one writer of release records.
type Writer struct {
	pool  *pgxpool.Pool
	token lease.Token
}

// NewWriter returns the writer over pool, fencing every write with token.
func NewWriter(pool *pgxpool.Pool, token lease.Token) *Writer {
	return &Writer{pool: pool, token: token}
}

// Mint is [Writer.MintWith] with nothing written beside the release.
func (w *Writer) Mint(ctx context.Context, actor record.Actor, m Minting) (Release, error) {
	return w.MintWith(ctx, actor, m, nil)
}

// MintWith writes the release record and its number in one transaction: the
// number is one above the higher of the highest the service has and
// [Minting.Floor], read under a per-service advisory lock so two mints for one
// service serialise and take consecutive numbers. Numbers are never reused — nothing here deletes, and a rolled-back
// release keeps its number. The order the numbers come out in is the order the
// caller mints in, which for the queue is master's order.
//
// The write is restartable: the record is keyed on the commit, so minting the
// same commit again writes nothing and returns the release already written,
// with inside not called a second time. That is what makes the fast-forward and
// this write one operation either side can be resumed from — the queue reads
// master at every start and completes its own unfinished merge with the write
// the fast-forward already implied.
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
func (w *Writer) MintWith(ctx context.Context, actor record.Actor, m Minting,
	inside func(context.Context, pgx.Tx, Release) error) (Release, error) {
	if err := actor.Validate(); err != nil {
		return Release{}, err
	}
	if m.ServiceID == "" {
		return Release{}, ErrServiceIDEmpty
	}
	if m.BuildID == "" {
		return Release{}, ErrBuildIDEmpty
	}
	if m.Commit == "" {
		return Release{}, ErrCommitEmpty
	}

	tx, err := w.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return Release{}, fmt.Errorf("release: beginning the mint: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, w.token); err != nil {
		return Release{}, err
	}

	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, AdvisoryLockKey(m.ServiceID)); err != nil {
		return Release{}, fmt.Errorf("release: taking the mint lock for %s: %w", m.ServiceID, err)
	}

	already, err := scan(tx.QueryRow(ctx, selectRelease+` where service_id = $1 and commit_id = $2`,
		m.ServiceID, m.Commit))
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return Release{}, fmt.Errorf("release: committing the read of commit %s of %s: %w",
				m.Commit, m.ServiceID, err)
		}
		return already, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Release{}, fmt.Errorf("release: reading the release of commit %s of %s: %w",
			m.Commit, m.ServiceID, err)
	}

	var number int64
	err = tx.QueryRow(ctx, `select greatest(coalesce(max(number), 0), $2) + 1 from `+Table+` where service_id = $1`,
		m.ServiceID, m.Floor).Scan(&number)
	if err != nil {
		return Release{}, fmt.Errorf("release: reading the highest number of %s: %w", m.ServiceID, err)
	}

	r := Release{
		ID:        record.NewID(IDPrefix),
		Actor:     actor,
		At:        record.Now(),
		ServiceID: m.ServiceID,
		Number:    number,
		BuildID:   m.BuildID,
		Commit:    m.Commit,
		ItemID:    m.ItemID,
	}
	if _, err := tx.Exec(ctx, `insert into `+Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, service_id, number, build_id, commit_id, item_id)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		r.ID, FormatVersion, string(r.Actor.Kind), r.Actor.Key, string(r.Actor.Basis), r.At, r.ServiceID, r.Number,
		r.BuildID, r.Commit, storedItem(r.ItemID),
	); err != nil {
		return Release{}, fmt.Errorf("release: minting number %d of %s: %w", r.Number, m.ServiceID, err)
	}

	if inside != nil {
		if err := inside(ctx, tx, r); err != nil {
			return Release{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Release{}, fmt.Errorf("release: committing number %d of %s: %w", r.Number, m.ServiceID, err)
	}
	return r, nil
}

// storedItem is the item column's value: null for a release that names none, so
// that one_release_per_item covers the rows that name one and leaves the others
// alone. A unique constraint over an empty string would admit one accepted
// commit per service and refuse the second.
func storedItem(itemID string) any {
	if itemID == "" {
		return nil
	}
	return itemID
}

const selectRelease = `select id, actor_kind, actor_key, actor_key_basis, at, service_id, number, build_id,
	commit_id, item_id
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
	var kind, basis string
	var itemID *string
	if err := row.Scan(&r.ID, &kind, &r.Actor.Key, &basis, &r.At, &r.ServiceID, &r.Number,
		&r.BuildID, &r.Commit, &itemID); err != nil {
		return Release{}, err
	}
	r.Actor.Kind = record.Kind(kind)
	r.Actor.Basis = record.Basis(basis)
	if itemID != nil {
		r.ItemID = *itemID
	}
	return r, nil
}
