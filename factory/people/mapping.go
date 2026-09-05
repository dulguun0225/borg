package people

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
	// ErrNameEmpty is returned for a mapping naming no human.
	ErrNameEmpty = errors.New("people: a mapping names the human the key maps to")
	// ErrMappingNotFound is returned where no mapping has that key.
	ErrMappingNotFound = errors.New("people: no mapping has that key")
	// ErrLegalHoldReaches is returned by [DeleteMapping] where the caller's
	// own check reports a legal hold reaching a record this key is written
	// on. The refusal is recorded by the erasure list, once built; today it
	// is this error, for a caller to record.
	ErrLegalHoldReaches = errors.New("people: a legal hold reaches a record this key is written on, so the mapping stands")
)

// Mapping is the one place a per-person key maps to a name, kept outside
// the chain: what deferred.md's seam 1 calls the record an erasure reaches.
// It also carries the hours a service pages within where the human holds no
// hours of their own — both unbuilt, schema.go says why.
type Mapping struct {
	ID    string
	Actor record.Actor
	At    string
	Key   string
	Name  string
	// HoursStart, HoursEnd and Zone are unbuilt: nothing writes them yet.
	HoursStart string
	HoursEnd   string
	Zone       string
}

// WriteMapping sets or replaces the name and the hours a key maps to. It is
// an upsert on the key, so writing it twice for one key updates the one row
// rather than adding a second, and it is the one People write that appends
// no policy version: the mapping stays outside the chain so it can be
// changed independently of it, and so that erasing it later deletes the
// mapping and nothing else.
func WriteMapping(ctx context.Context, pool *pgxpool.Pool, token lease.Token, actor record.Actor,
	key, name, hoursStart, hoursEnd, zone string) (Mapping, error) {
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
		Key: key, Name: name, HoursStart: hoursStart, HoursEnd: hoursEnd, Zone: zone,
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
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, person_key, name, hours_start, hours_end, zone)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		on conflict (person_key) do update set
			name = excluded.name, hours_start = excluded.hours_start, hours_end = excluded.hours_end, zone = excluded.zone`,
		m.ID, MappingFormatVersion, string(m.Actor.Kind), m.Actor.Key, string(m.Actor.Basis), m.At,
		m.Key, m.Name, m.HoursStart, m.HoursEnd, m.Zone,
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
// itself included. reaches is called first and, where it reports true, the
// deletion is refused with [ErrLegalHoldReaches]: what a hold over a
// decision preserves is who approved it, and the mapping is the only thing
// that says so. reaches is the caller's own check — this package holds no
// join from a key to the records it names, which is the erasure list's walk
// and is not built — and a nil reaches never refuses.
func DeleteMapping(ctx context.Context, pool *pgxpool.Pool, token lease.Token, key string,
	reaches func(ctx context.Context) (bool, error)) error {
	if key == "" {
		return ErrKeyEmpty
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

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("people: beginning: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, token); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from `+MappingTable+` where person_key = $1`, key); err != nil {
		return fmt.Errorf("people: deleting the mapping of %s: %w", key, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("people: committing: %w", err)
	}
	return nil
}

// GetMapping is the mapping for one key, or [ErrMappingNotFound].
func GetMapping(ctx context.Context, pool *pgxpool.Pool, key string) (Mapping, error) {
	var m Mapping
	var kind, basis string
	err := pool.QueryRow(ctx, `select id, actor_kind, actor_key, actor_key_basis, at, person_key, name,
		hours_start, hours_end, zone from `+MappingTable+` where person_key = $1`, key).
		Scan(&m.ID, &kind, &m.Actor.Key, &basis, &m.At, &m.Key, &m.Name, &m.HoursStart, &m.HoursEnd, &m.Zone)
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
