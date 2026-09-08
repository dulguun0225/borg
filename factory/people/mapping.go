package people

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/legalhold"
	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrNameEmpty is returned for a mapping naming no human.
	ErrNameEmpty = errors.New("people: a mapping names the human the key maps to")
	// ErrMappingNotFound is returned where no mapping has that key.
	ErrMappingNotFound = errors.New("people: no mapping has that key")
	// ErrLegalHoldReaches is returned by [DeleteMapping] where the caller's
	// own check reports a legal hold reaching a record this key is written
	// on. Recording the refusal is not built; today it is this error, for a
	// caller to record. Nothing is appended to the erasure list for it, so no
	// restore is needed to undo the refusal.
	ErrLegalHoldReaches = errors.New("people: a legal hold reaches a record this key is written on, so the mapping stands")
	// ErrNoErasureList is returned by [DeleteMapping] for a call supplying no
	// appender. The erasure-list row is what says the name must not come back
	// with a restore, so a deletion that cannot append one is refused rather
	// than performed without it.
	ErrNoErasureList = errors.New("people: deleting a mapping appends an erasure-list row, and no appender was supplied")
)

// Mapping is the one place a per-person key maps to a name, kept outside
// the chain: what ../../end-goal/deferred.md's seam 1 calls the record an erasure reaches.
// A name and a key are the whole of it — the hours a service pages within are
// a field of the service record, and a wait naming no service pages at any
// hour, so there is nothing per human for this row to carry.
type Mapping struct {
	ID    string
	Actor record.Actor
	At    string
	Key   string
	Name  string
}

// WriteMapping sets or replaces the name a key maps to. It is
// an upsert on the key, so writing it twice for one key updates the one row
// rather than adding a second, and it is the one People write that appends
// no policy version: the mapping stays outside the chain so it can be
// changed independently of it, and so that erasing it later deletes the
// mapping and nothing else.
func WriteMapping(ctx context.Context, pool *pgxpool.Pool, token lease.Token, actor record.Actor,
	key, name string) (Mapping, error) {
	if err := actor.Validate(); err != nil {
		return Mapping{}, err
	}
	if actor.Kind != record.KindHuman {
		return Mapping{}, fmt.Errorf("%w: %s %q", ErrNotAnOwner, actor.Kind, actor.Key)
	}
	if key == "" {
		return Mapping{}, ErrKeyEmpty
	}
	if name == "" {
		return Mapping{}, ErrNameEmpty
	}

	m := Mapping{
		ID: record.NewID(MappingIDPrefix), Actor: actor, At: record.Now(),
		Key: key, Name: name,
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return Mapping{}, fmt.Errorf("people: beginning: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, token); err != nil {
		return Mapping{}, err
	}
	_, err = tx.Exec(ctx, `insert into `+MappingTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, person_key, name)
		values ($1, $2, $3, $4, $5, $6, $7, $8)
		on conflict (person_key) do update set name = excluded.name`,
		m.ID, MappingFormatVersion, string(m.Actor.Kind), m.Actor.Key, string(m.Actor.Basis), m.At,
		m.Key, m.Name,
	)
	if err != nil {
		return Mapping{}, fmt.Errorf("people: mapping %s to %q: %w", key, name, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Mapping{}, fmt.Errorf("people: committing: %w", err)
	}
	return GetMapping(ctx, pool, key)
}

// DeleteMapping is the one record an erasure reaches: it deletes the mapping
// for key and leaves every record that key is written on standing, the key
// itself included. It is refused with [ErrLegalHoldReaches] where a hold
// stands: what a hold over a decision preserves is who approved it, and the
// mapping is the only thing that says so.
//
// Two checks make that refusal, because a legal hold's subject is a service, a
// project, or the whole install and never a person. A hold on the whole
// install reaches every mapping there is, and this package reads that one
// itself through [legalhold.Reaching]. A hold on one service or one project
// reaches a mapping only through the records that key is written on, which is
// the walk this package cannot make — so that half is reaches, the caller's
// own check, and a nil reaches never refuses. Both are made before anything is
// written, so a refused deletion appends no erasure-list row and no restore is
// needed to undo it.
//
// appendErasure appends that row, and it lands before the deletion: a stop
// between the two leaves a row saying a name was removed and the name still
// there, which is the event visibly owing rather than visibly done, and the row
// is what a restore is replayed against. It is the caller's because the erasure
// list has one writer and it is the report store, which this package does not
// import; key is what the row is written under, so the same deletion made again
// appends nothing. A call supplying none is [ErrNoErasureList].
func DeleteMapping(ctx context.Context, pool *pgxpool.Pool, token lease.Token, key string,
	reaches func(ctx context.Context) (bool, error),
	appendErasure func(ctx context.Context, key string) error) error {
	if key == "" {
		return ErrKeyEmpty
	}
	if appendErasure == nil {
		return fmt.Errorf("%w: %s", ErrNoErasureList, key)
	}
	held, err := legalhold.Reaching(ctx, pool, legalhold.Subject{Kind: legalhold.SubjectFactory})
	if err != nil {
		return fmt.Errorf("people: reading whether a legal hold stands over the install: %w", err)
	}
	if held {
		return fmt.Errorf("%w: %s, under a hold on the whole install", ErrLegalHoldReaches, key)
	}
	if reaches != nil {
		held, err := reaches(ctx)
		if err != nil {
			return fmt.Errorf("people: checking whether a legal hold reaches %s: %w", key, err)
		}
		if held {
			return fmt.Errorf("%w: %s", ErrLegalHoldReaches, key)
		}
	}

	if err := appendErasure(ctx, key); err != nil {
		return fmt.Errorf("people: appending the erasure-list row for %s: %w", key, err)
	}
	if _, err := deleteMapping(ctx, pool, token, key); err != nil {
		return err
	}
	return nil
}

// Replay deletes again the mappings the erasure list says were erased, and
// returns how many it deleted. It is what this store runs against whatever a
// restore brought back before it serves again: the list is never rolled back,
// so a backup taken before a deletion carries the name and this is what takes
// it out again.
//
// erased is the keys the rows of kind mapping name, read and handed in by the
// composition — the list has one writer and it is the report store, and this
// package imports neither. No legal hold is read here: the deletion already
// happened and was not refused, and a hold placed since does not put a name
// back that is gone everywhere but in a backup.
func Replay(ctx context.Context, pool *pgxpool.Pool, token lease.Token, erased []string) (int, error) {
	deleted := 0
	for _, key := range erased {
		if key == "" {
			return deleted, ErrKeyEmpty
		}
		found, err := deleteMapping(ctx, pool, token, key)
		if err != nil {
			return deleted, err
		}
		if found {
			deleted++
		}
	}
	return deleted, nil
}

// deleteMapping is the delete itself, fenced and in its own transaction, and
// whether there was a row to delete.
func deleteMapping(ctx context.Context, pool *pgxpool.Pool, token lease.Token, key string) (bool, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("people: beginning: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, token); err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `delete from `+MappingTable+` where person_key = $1`, key)
	if err != nil {
		return false, fmt.Errorf("people: deleting the mapping of %s: %w", key, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("people: committing: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// GetMapping is the mapping for one key, or [ErrMappingNotFound].
func GetMapping(ctx context.Context, pool *pgxpool.Pool, key string) (Mapping, error) {
	var m Mapping
	var kind, basis string
	err := pool.QueryRow(ctx, `select id, actor_kind, actor_key, actor_key_basis, at, person_key, name
		from `+MappingTable+` where person_key = $1`, key).
		Scan(&m.ID, &kind, &m.Actor.Key, &basis, &m.At, &m.Key, &m.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return Mapping{}, fmt.Errorf("%w: %s", ErrMappingNotFound, key)
	} else if err != nil {
		return Mapping{}, fmt.Errorf("people: reading the mapping of %s: %w", key, err)
	}
	m.Actor.Kind = record.Kind(kind)
	m.Actor.Basis = record.Basis(basis)
	return m, nil
}

// KeyNamed is the mapping read the other way: the key that maps to name, and
// false where no mapping does. It exists because a human at a terminal types a
// name and every record holds a key, so something has to cross that gap; the
// name is not unique in the table — [WriteMapping] conflicts on the key alone —
// so where two keys map to one name this answers the oldest mapping, which is
// the one an earlier command already wrote records under.
//
// What it costs is that two people of one name cannot be told apart by name.
// The mapping is the one place a name exists at all, and the factory holds no
// second identifier to disambiguate with.
func KeyNamed(ctx context.Context, pool *pgxpool.Pool, name string) (string, bool, error) {
	if name == "" {
		return "", false, ErrNameEmpty
	}
	var key string
	err := pool.QueryRow(ctx, `select person_key from `+MappingTable+`
		where name = $1 order by at, id limit 1`, name).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	} else if err != nil {
		return "", false, fmt.Errorf("people: reading the key named %q: %w", name, err)
	}
	return key, true, nil
}

// NameOf resolves a key to a name, the way every screen and every page
// event does, and [ErrMappingNotFound] where the key's mapping was erased
// or never written — a name resolved from a key that stands on every record
// it was ever written to, its mapping gone.
func NameOf(ctx context.Context, pool *pgxpool.Pool, key string) (string, error) {
	m, err := GetMapping(ctx, pool, key)
	if err != nil {
		return "", err
	}
	return m.Name, nil
}
