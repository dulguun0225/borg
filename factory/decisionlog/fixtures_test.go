package decisionlog_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/record"
)

// insertAround writes a row without going through the writer, which is how a
// test reaches the constraints rather than the methods. It fills the hash
// columns with the values given rather than computing them, because what is
// under test is what the store refuses and not what the chain says.
func insertAround(ctx context.Context, pool *pgxpool.Pool, row decisionlog.Row) error {
	_, err := pool.Exec(ctx, `insert into decision_log
		(seq, id, actor_kind, actor_name, at, shape, payload, policy_version, score_version, part, closes, prev_hash, hash)
		values (nextval('`+decisionlog.Sequence+`'), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		row.ID, string(row.Actor.Kind), row.Actor.Name, row.At, string(row.Shape),
		row.Payload, row.PolicyVersion, row.ScoreVersion, string(row.Part), row.Closes,
		row.PrevHash, row.Hash)
	return err
}

// aRow is a row that the store accepts, for a test to spoil one field of.
func aRow() decisionlog.Row {
	id := record.NewID("dl")
	return decisionlog.Row{
		ID:       id,
		Actor:    gate,
		At:       record.Now(),
		Shape:    decisionlog.ShapeWait,
		Payload:  "written around the writer",
		PrevHash: "prev-" + id,
		Hash:     "hash-" + id,
	}
}

// refusedBy is the constraint the store named, or a failure where it accepted
// the row.
func refusedBy(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("the store accepted the row, want it refused")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("the store returned %v, want a constraint violation", err)
	}
	return pgErr.ConstraintName
}
