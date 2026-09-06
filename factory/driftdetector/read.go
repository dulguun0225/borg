package driftdetector

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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
// targets says both rather than picking one. An uncleared chain mismatch holds
// every service's production deploys, so it is folded into the answer for
// every serviceID asked about.
func (s *Store) Mismatch(ctx context.Context, serviceID string) (bool, string, error) {
	standing, err := Uncleared(ctx, s.pool, serviceID)
	if err != nil {
		return false, "", err
	}
	chain, err := UnclearedChain(ctx, s.pool)
	if err != nil {
		return false, "", err
	}
	standing = append(standing, chain...)
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
// first: the target mismatches the first comparison raised, and the
// stale-component mismatches the third raised for a component whose stopping
// holds that service. Both hold that service's production deploys and both are
// what the notifier pages about. A chain mismatch is never in this answer —
// [UnclearedChain] is where it is read, because it holds every service at once.
//
// An empty service is every uncleared mismatch of those two kinds, which is what
// a reader of the whole store — the command-line interface's own printing — asks
// for.
func Uncleared(ctx context.Context, pool *pgxpool.Pool, serviceID string) ([]Mismatch, error) {
	kinds := ` and kind in ('` + MismatchKindTarget + `', '` + MismatchKindStaleComponent + `')`
	where := ` where cleared_at = ''` + kinds + ` order by at, id`
	args := []any{}
	if serviceID != "" {
		where = ` where cleared_at = ''` + kinds + ` and service_id = $1 order by at, id`
		args = append(args, serviceID)
	}
	return mismatches(ctx, pool, where, args...)
}

// UnclearedChain is every uncleared chain mismatch, oldest first. There is at
// most one at a time in ordinary operation — [Writer.RaiseChainMismatch]
// leaves one standing alone rather than raising a second — but a reader
// asks for every one there is rather than assuming that.
func UnclearedChain(ctx context.Context, pool *pgxpool.Pool) ([]Mismatch, error) {
	return mismatches(ctx, pool, ` where cleared_at = '' and kind = '`+MismatchKindChain+`' order by at, id`)
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

const selectMismatch = `select id, actor_kind, actor_key, actor_key_basis, at, kind, service_id, target, component,
	running_build, recorded_release_id, recorded_build_id, detail, later_agreements, cleared_at, cleared_by
	from ` + MismatchTable

func scanMismatch(row pgx.Row) (Mismatch, error) {
	var m Mismatch
	var actorKind, basis string
	err := row.Scan(&m.ID, &actorKind, &m.Actor.Key, &basis, &m.At, &m.Kind, &m.ServiceID, &m.Target, &m.Component,
		&m.RunningBuild, &m.RecordedReleaseID, &m.RecordedBuildID, &m.Detail, &m.LaterAgreements,
		&m.ClearedAt, &m.ClearedBy)
	if err != nil {
		return Mismatch{}, err
	}
	m.Actor.Kind = record.Kind(actorKind)
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
		where kind = '`+MismatchKindTarget+`' and service_id = $1 and target = $2 and cleared_at = '' order by at limit 1`,
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
		running_build, recorded_release_id, recorded_build_id, agreed, digest_reported, interval_seconds, further_pass_owed
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
		var seconds int64
		var furtherPassOwed bool
		if err := rows.Scan(&c.ID, &kind, &c.Actor.Key, &basis, &c.At, &c.ServiceID, &c.Target,
			&c.Reached, &c.Why, &c.RunningBuild, &c.RecordedReleaseID, &c.RecordedBuildID, &c.Agreed,
			&c.DigestReported, &seconds, &furtherPassOwed); err != nil {
			return nil, fmt.Errorf("driftdetector: reading a last check: %w", err)
		}
		c.Actor.Kind = record.Kind(kind)
		c.Actor.Basis = record.Basis(basis)
		c.Interval = time.Duration(seconds) * time.Second
		c.FurtherPassOwed = furtherPassOwed
		read = append(read, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("driftdetector: reading the last checks: %w", err)
	}
	return read, nil
}

// StaleAgainst is every last check of this store older than maxAge as of now.
// It is the read a safeguard on the drift detector's last check is answered by:
// the detector supplies its own interval, so what an owner may add is a maximum
// age of their own, bound through gatepolicy's
// DriftDetectorLastCheckMaxAge and read here.
//
// It is a second reading beside [LastCheck.Stale] and not a replacement for it:
// that one asks whether the detector has missed a pass it promised, and this one
// asks whether an owner is willing to deploy against a check this old. A maxAge
// of nothing returns nothing, an owner having authored no safeguard.
func StaleAgainst(ctx context.Context, pool *pgxpool.Pool, maxAge time.Duration, now time.Time) ([]LastCheck, error) {
	if maxAge <= 0 {
		return nil, nil
	}
	all, err := LastChecks(ctx, pool, "")
	if err != nil {
		return nil, err
	}
	var older []LastCheck
	for _, c := range all {
		checked, err := record.ParseTime(c.At)
		if err != nil {
			return nil, fmt.Errorf("driftdetector: the time on %s: %w", c.ID, err)
		}
		if now.After(checked.Add(maxAge)) {
			older = append(older, c)
		}
	}
	return older, nil
}
