package redaction

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

// redactionColumns is the stored fields in the order [scan] reads them,
// written once so a select and its scan cannot drift apart.
const redactionColumns = `id, actor_kind, actor_key, actor_key_basis, at,
	target_kind, target_id, spans, reason, erasure_key`

// OverKind is every redaction naming a target of one kind, oldest first. It
// is what each target's own writer walks on its pass: intake reads the
// statements, the artifact store reads the versions, and the composition
// hands the report store its own, that store being a second database.
func OverKind(ctx context.Context, pool *pgxpool.Pool, kind TargetKind) ([]Redaction, error) {
	rows, err := pool.Query(ctx, `select `+redactionColumns+` from `+Table+`
		where target_kind = $1 order by at, id`, string(kind))
	if err != nil {
		return nil, fmt.Errorf("redaction: reading the redactions over every %s: %w", kind, err)
	}
	defer rows.Close()
	return collect(rows, string(kind))
}

// ForTarget is every redaction naming one target, oldest first. It is what a
// read of that target's words is served through from the moment the redaction
// exists, whether or not the writer's own destruction pass has reached the
// record yet.
func ForTarget(ctx context.Context, pool *pgxpool.Pool, target Target) ([]Redaction, error) {
	rows, err := pool.Query(ctx, `select `+redactionColumns+` from `+Table+`
		where target_kind = $1 and target_id = $2 order by at, id`,
		string(target.Kind), target.ID)
	if err != nil {
		return nil, fmt.Errorf("redaction: reading the redactions naming %s: %w", target, err)
	}
	defer rows.Close()
	return collect(rows, target.String())
}

// ByErasureKey is the redaction written under one erasure-list row's key, and
// false where none is. It is what makes the record the keyed second step of
// the event: the erasure performed again derives the key of the row already
// appended, finds the record already written, and writes nothing.
func ByErasureKey(ctx context.Context, pool *pgxpool.Pool, key string) (Redaction, bool, error) {
	return byErasureKey(ctx, pool, key)
}

// byErasureKeyTx is [ByErasureKey] read inside a transaction, which is what
// [Insert] checks before it writes: the keyed repeat it finds this way is
// returned rather than left to reach the unique index [DDL] carries on the
// column.
func byErasureKeyTx(ctx context.Context, tx pgx.Tx, key string) (Redaction, bool, error) {
	return byErasureKey(ctx, tx, key)
}

// querier is the read [pgxpool.Pool] and [pgx.Tx] share, so one query can run
// against either.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func byErasureKey(ctx context.Context, q querier, key string) (Redaction, bool, error) {
	r, err := scan(q.QueryRow(ctx, `select `+redactionColumns+` from `+Table+`
		where erasure_key = $1`, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return Redaction{}, false, nil
	} else if err != nil {
		return Redaction{}, false, fmt.Errorf("redaction: reading the redaction keyed %s: %w", key, err)
	}
	return r, true, nil
}

// Get is one redaction by id, which is what an auditor shown a policy version
// naming one reads.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (Redaction, error) {
	r, err := scan(pool.QueryRow(ctx, `select `+redactionColumns+` from `+Table+` where id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Redaction{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	} else if err != nil {
		return Redaction{}, fmt.Errorf("redaction: reading %s: %w", id, err)
	}
	return r, nil
}

// collect reads every row of a query over [redactionColumns]. what names what
// was asked for in the error text.
func collect(rows pgx.Rows, what string) ([]Redaction, error) {
	var read []Redaction
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("redaction: reading a redaction of %s: %w", what, err)
		}
		read = append(read, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("redaction: reading the redactions of %s: %w", what, err)
	}
	return read, nil
}

// scan reads one row of [redactionColumns] into a [Redaction].
func scan(row pgx.Row) (Redaction, error) {
	var r Redaction
	var kind, basis, targetKind, spans string
	if err := row.Scan(&r.ID, &kind, &r.Actor.Key, &basis, &r.At,
		&targetKind, &r.Target.ID, &spans, &r.Reason, &r.ErasureKey); err != nil {
		return Redaction{}, err
	}
	r.Actor.Kind, r.Actor.Basis = record.Kind(kind), record.Basis(basis)
	r.Target.Kind = TargetKind(targetKind)
	parsed, err := parseSpans(spans)
	if err != nil {
		return Redaction{}, err
	}
	r.Spans = parsed
	return r, nil
}
