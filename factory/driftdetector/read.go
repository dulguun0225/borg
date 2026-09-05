package driftdetector

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

// Store is what the factory holds of this package: the reads and no writer. It
// satisfies the interface package gate asks a mismatch of at the production deploy
// row, and it is the whole of what a factory component may do with this store.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns the reads over pool. A factory component is handed one of these;
// [NewWriter] is the drift detector's own.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Mismatch is whether an uncleared mismatch stands for the service, and what
// disagrees. It is the read package gate makes at the moment the production deploy
// gate fires, and the shape of it is that interface's.
//
// More than one target may disagree at once and the answer is one sentence, because
// the gate's open event holds one: the sentences are joined, so a row naming two
// targets says both rather than picking one.
func (s *Store) Mismatch(ctx context.Context, serviceID string) (bool, string, error) {
	standing, err := Uncleared(ctx, s.pool, serviceID)
	if err != nil {
		return false, "", err
	}
	if len(standing) == 0 {
		return false, "", nil
	}
	said := make([]string, 0, len(standing))
	for _, m := range standing {
		said = append(said, m.Why())
	}
	return true, strings.Join(said, "; "), nil
}

// Uncleared is every mismatch of one service that no human has cleared, oldest
// first. It is what holds that service's production deploys, and what the notifier
// pages about.
//
// An empty service is every uncleared mismatch, which is what a reader of the whole
// store — the crude interface's own printing — asks for.
func Uncleared(ctx context.Context, pool *pgxpool.Pool, serviceID string) ([]Mismatch, error) {
	where := ` where cleared_at = '' order by at, id`
	args := []any{}
	if serviceID != "" {
		where = ` where cleared_at = '' and service_id = $1 order by at, id`
		args = append(args, serviceID)
	}
	return mismatches(ctx, pool, where, args...)
}

// All is every mismatch, cleared or not, oldest first. A cleared one is kept: the
// trail of what stopped a deploy is read from this store beside the log, and a
// mismatch removed on clearing would take half of it away.
func All(ctx context.Context, pool *pgxpool.Pool) ([]Mismatch, error) {
	return mismatches(ctx, pool, ` order by at, id`)
}

// Get is one mismatch by id.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (Mismatch, error) {
	m, err := scanMismatch(pool.QueryRow(ctx, selectMismatch+` where id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Mismatch{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	} else if err != nil {
		return Mismatch{}, fmt.Errorf("driftdetector: reading %s: %w", id, err)
	}
	return m, nil
}

const selectMismatch = `select id, actor_kind, actor_key, actor_key_basis, at, service_id, target, running_build,
	recorded_release_id, recorded_build_id, later_agreements, cleared_at, cleared_by
	from ` + MismatchTable

func scanMismatch(row pgx.Row) (Mismatch, error) {
	var m Mismatch
	var kind, basis string
	err := row.Scan(&m.ID, &kind, &m.Actor.Key, &basis, &m.At, &m.ServiceID, &m.Target, &m.RunningBuild,
		&m.RecordedReleaseID, &m.RecordedBuildID, &m.LaterAgreements, &m.ClearedAt, &m.ClearedBy)
	if err != nil {
		return Mismatch{}, err
	}
	m.Actor.Kind = record.Kind(kind)
	m.Actor.Basis = record.Basis(basis)
	return m, nil
}

// mismatches is every read that returns more than one. The suffix is a constant of
// this package at each call site and never input, so writing it into the statement is
// not a place anything can be injected.
func mismatches(ctx context.Context, pool *pgxpool.Pool, suffix string, args ...any) ([]Mismatch, error) {
	rows, err := pool.Query(ctx, selectMismatch+suffix, args...)
	if err != nil {
		return nil, fmt.Errorf("driftdetector: reading the mismatches: %w", err)
	}
	defer rows.Close()

	var read []Mismatch
	for rows.Next() {
		m, err := scanMismatch(rows)
		if err != nil {
			return nil, fmt.Errorf("driftdetector: reading a mismatch: %w", err)
		}
		read = append(read, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("driftdetector: reading the mismatches: %w", err)
	}
	return read, nil
}

// unclearedOn is the uncleared mismatch for one service and one target, which is
// what [Writer.Record] asks before it raises another: a mismatch remains until a
// human clears it, so a second pass finding the same disagreement records an
// agreement or nothing rather than a second row.
func unclearedOn(ctx context.Context, pool *pgxpool.Pool, serviceID, target string) (Mismatch, bool, error) {
	m, err := scanMismatch(pool.QueryRow(ctx, selectMismatch+`
		where service_id = $1 and target = $2 and cleared_at = '' order by at limit 1`,
		serviceID, target))
	if errors.Is(err, pgx.ErrNoRows) {
		return Mismatch{}, false, nil
	} else if err != nil {
		return Mismatch{}, false, fmt.Errorf("driftdetector: reading the mismatch on %s: %w", target, err)
	}
	return m, true, nil
}

// LastChecks is the last check of every target of one service, or of every
// target where the service is empty. It is what says whether the independent
// driftdetector is still running: a check that silently stops is worse than the bug
// it catches, so this is read rather than the absence of mismatches being taken
// as health.
func LastChecks(ctx context.Context, pool *pgxpool.Pool, serviceID string) ([]LastCheck, error) {
	statement := `select id, actor_kind, actor_key, actor_key_basis, at, service_id, target, reached, why,
		running_build, recorded_release_id, recorded_build_id, agreed
		from ` + LastCheckTable
	args := []any{}
	if serviceID != "" {
		statement += ` where service_id = $1`
		args = append(args, serviceID)
	}
	statement += ` order by service_id, target`

	rows, err := pool.Query(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("driftdetector: reading the last checks: %w", err)
	}
	defer rows.Close()

	var read []LastCheck
	for rows.Next() {
		var c LastCheck
		var kind, basis string
		if err := rows.Scan(&c.ID, &kind, &c.Actor.Key, &basis, &c.At, &c.ServiceID, &c.Target,
			&c.Reached, &c.Why, &c.RunningBuild, &c.RecordedReleaseID, &c.RecordedBuildID, &c.Agreed); err != nil {
			return nil, fmt.Errorf("driftdetector: reading a last check: %w", err)
		}
		c.Actor.Kind = record.Kind(kind)
		c.Actor.Basis = record.Basis(basis)
		read = append(read, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("driftdetector: reading the last checks: %w", err)
	}
	return read, nil
}
