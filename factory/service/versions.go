package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

// Version is one authoring of what the candidate's store starts with, or of the
// non-production values the configuration takes on a candidate. Each authoring
// is a version written beside the earlier ones and never editing one, so the
// version a run was composed from stays readable and the composition a run's
// results carry names it.
type Version struct {
	ID        string
	Actor     record.Actor
	At        string
	ServiceID string
	// Digest is of the content, computed at the write.
	Digest string
	// Content is the seed the owner authored — a snapshot they anonymised, or
	// synthetic rows they supply — or the value set, as they wrote it. Where an
	// owner authors no seed at all the store starts empty, which is no version
	// here rather than a version with nothing in it.
	Content string
}

// ErrVersionNotFound is returned by [SeedInForce] and [ValueSetInForce] where the
// owner has authored none. It is a value and not a fault: a service whose owner
// authored no seed starts its candidate's store empty, and one that authored no
// value set runs its criteria against unset values.
var ErrVersionNotFound = errors.New("service: nothing is authored")

// AuthorSeed writes one version of what the candidate's store starts with.
func AuthorSeed(ctx context.Context, tx pgx.Tx, token lease.Token, actor record.Actor,
	serviceID, content string) (Version, error) {
	return authorVersion(ctx, tx, token, actor, SeedTable, SeedIDPrefix, FormatVersionSeed, serviceID, content)
}

// AuthorValueSet writes one version of the non-production values the service's
// configuration takes on a candidate, which the deployer writes onto the
// candidate environment at composition so a candidate never holds a production
// value.
func AuthorValueSet(ctx context.Context, tx pgx.Tx, token lease.Token, actor record.Actor,
	serviceID, content string) (Version, error) {
	return authorVersion(ctx, tx, token, actor, ValueSetTable, ValueSetIDPrefix, FormatVersionValueSet, serviceID, content)
}

func authorVersion(ctx context.Context, tx pgx.Tx, token lease.Token, actor record.Actor,
	table, idPrefix, formatVersion, serviceID, content string) (Version, error) {
	if err := lease.Fence(ctx, tx, token); err != nil {
		return Version{}, err
	}
	if err := actor.Validate(); err != nil {
		return Version{}, err
	}
	if err := mustExist(ctx, tx, serviceID); err != nil {
		return Version{}, err
	}
	sum := sha256.Sum256([]byte(content))
	v := Version{
		ID:        record.NewID(idPrefix),
		Actor:     actor,
		At:        record.Now(),
		ServiceID: serviceID,
		Digest:    hex.EncodeToString(sum[:]),
		Content:   content,
	}
	_, err := tx.Exec(ctx, `insert into `+table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, service_id, digest, content)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		v.ID, formatVersion, string(v.Actor.Kind), v.Actor.Key, string(v.Actor.Basis), v.At,
		v.ServiceID, v.Digest, v.Content,
	)
	if err != nil {
		return Version{}, fmt.Errorf("service: authoring a version on %s: %w", serviceID, err)
	}
	return v, nil
}

// SeedInForce is the newest seed version of one service, and [ErrVersionNotFound]
// where the owner authored none.
func SeedInForce(ctx context.Context, pool *pgxpool.Pool, serviceID string) (Version, error) {
	return inForce(ctx, pool, SeedTable, serviceID)
}

// ValueSetInForce is the newest value-set version of one service, and
// [ErrVersionNotFound] where the owner authored none.
func ValueSetInForce(ctx context.Context, pool *pgxpool.Pool, serviceID string) (Version, error) {
	return inForce(ctx, pool, ValueSetTable, serviceID)
}

// SeedVersions is every seed version of one service, newest first, which is what
// a reader following a composition back to the version a run was built from
// walks.
func SeedVersions(ctx context.Context, pool *pgxpool.Pool, serviceID string) ([]Version, error) {
	return versions(ctx, pool, SeedTable, serviceID)
}

// ValueSetVersions is every value-set version of one service, newest first.
func ValueSetVersions(ctx context.Context, pool *pgxpool.Pool, serviceID string) ([]Version, error) {
	return versions(ctx, pool, ValueSetTable, serviceID)
}

const selectVersion = `select id, actor_kind, actor_key, actor_key_basis, at, service_id, digest, content from `

func inForce(ctx context.Context, pool *pgxpool.Pool, table, serviceID string) (Version, error) {
	row := pool.QueryRow(ctx, selectVersion+table+` where service_id = $1 order by at desc, id desc limit 1`, serviceID)
	return scanVersion(row, table, serviceID)
}

func versions(ctx context.Context, pool *pgxpool.Pool, table, serviceID string) ([]Version, error) {
	rows, err := pool.Query(ctx, selectVersion+table+` where service_id = $1 order by at desc, id desc`, serviceID)
	if err != nil {
		return nil, fmt.Errorf("service: reading the versions of %s: %w", serviceID, err)
	}
	defer rows.Close()

	var read []Version
	for rows.Next() {
		v, err := scanVersion(rows, table, serviceID)
		if err != nil {
			return nil, err
		}
		read = append(read, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("service: reading the versions of %s: %w", serviceID, err)
	}
	return read, nil
}

func scanVersion(row pgx.Row, table, serviceID string) (Version, error) {
	var v Version
	var kind, basis string
	err := row.Scan(&v.ID, &kind, &v.Actor.Key, &basis, &v.At, &v.ServiceID, &v.Digest, &v.Content)
	if errors.Is(err, pgx.ErrNoRows) {
		return Version{}, fmt.Errorf("%w: no %s for %s", ErrVersionNotFound, table, serviceID)
	} else if err != nil {
		return Version{}, fmt.Errorf("service: reading a version of %s: %w", serviceID, err)
	}
	v.Actor.Kind = record.Kind(kind)
	v.Actor.Basis = record.Basis(basis)
	return v, nil
}
