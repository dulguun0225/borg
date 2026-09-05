// The database tests of this package are in decisionlog_test rather than in
// decisionlog, so that a test reaches the store the way a caller of this
// package would. They apply [lease.DDL] and [decisionlog.DDL] directly in a
// schema of their own, rather than through package postgres:
// postgres.Apply reaches nearly every package in the module, most of which
// do not compile while record.Actor is mid-change, and this package's own
// tests do not need any of them. postgres is where the two DDL lists are
// composed for a real install; here they are applied the same way lease's
// own tests apply lease.DDL.
//
// None of these tests skips when the database is unreachable. The milestone
// is demonstrated by them running, so an unreachable database fails the run.
package decisionlog_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

const (
	defaultURL     = "postgres://factory:factory@localhost:5433/factory"
	databaseURLEnv = "DATABASE_URL"
)

func databaseURL() string {
	if u := os.Getenv(databaseURLEnv); u != "" {
		return u
	}
	return defaultURL
}

// newLog gives a test a schema of its own, lease's DDL and this package's DDL
// applied inside it, a lease acquired for it, a writer, and the token that
// acquisition returned, for a test that needs a second writer or a
// [decisionlog.Reader] over the same schema. The schema is dropped when the
// test ends, so a rerun on a database a previous run left dirty starts clean.
func newLog(t *testing.T) (context.Context, *pgxpool.Pool, *decisionlog.Writer, lease.Token) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "dl_" + hex.EncodeToString(suffix[:])

	pool, err := pgxpool.New(ctx, inSchema(t, databaseURL(), schema))
	if err != nil {
		t.Fatalf("opening the pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("the database at %s is not reachable, and these tests do not skip: %v", databaseURL(), err)
	}
	t.Cleanup(func() {
		// t.Context is already cancelled by the time cleanup runs.
		drop, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := pool.Exec(drop, `drop schema if exists `+pgx.Identifier{schema}.Sanitize()+` cascade`); err != nil {
			t.Errorf("dropping schema %s: %v", schema, err)
		}
		pool.Close()
	})

	if _, err := pool.Exec(ctx, `create schema `+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("creating schema %s: %v", schema, err)
	}
	for n, statement := range append(append([]string{}, lease.DDL...), decisionlog.DDL...) {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("applying statement %d: %v", n+1, err)
		}
	}

	token, err := lease.Acquire(ctx, pool, "test", time.Minute)
	if err != nil {
		t.Fatalf("acquiring the lease: %v", err)
	}
	return ctx, pool, decisionlog.NewWriter(pool, token), token
}

// inSchema points a connection URL at one schema and nothing else, so every
// unqualified name in the DDL and in the writer's statements resolves there.
func inSchema(t *testing.T, base, schema string) string {
	t.Helper()
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parsing %s: %v", base, err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// owner is a human actor, key claimed rather than verified — the state every
// key holds before anything checks a caller. gate and notifierActor are
// components: gate.merge_to_master is the design's own example of a gate row,
// and the notifier is the component the design names for a page event.
var owner = record.Actor{Kind: record.KindHuman, Key: "person:abc", Basis: record.BasisClaimed}
var otherHuman = record.Actor{Kind: record.KindHuman, Key: "person:def", Basis: record.BasisClaimed}
var gate = record.Actor{Kind: record.KindComponent, Key: "gate.merge_to_master"}
var notifierActor = record.Actor{Kind: record.KindComponent, Key: "notifier"}

// insertAround writes a row without going through the writer, which is how a
// test reaches the constraints rather than the methods. It fills the hash
// columns with the values given rather than computing them, because what is
// under test is what the store refuses and not what the chain says.
func insertAround(ctx context.Context, pool *pgxpool.Pool, row decisionlog.Row) error {
	_, err := pool.Exec(ctx, `insert into decision_log
		(seq, id, format_version, actor_kind, actor_key, actor_key_basis, at, shape, payload,
		 policy_version, score_version, part, closes, verdict, reason, opened_in_work_at, self_approval,
		 prev_hash, hash)
		values (nextval('`+decisionlog.Sequence+`'), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`,
		row.ID, row.FormatVersion, string(row.Actor.Kind), row.Actor.Key, string(row.Actor.Basis), row.At,
		string(row.Shape), row.Payload, row.PolicyVersion, row.ScoreVersion, string(row.Part), row.Closes,
		row.Verdict, row.Reason, row.OpenedInWorkAt, row.SelfApproval, row.PrevHash, row.Hash)
	return err
}

// aRow is a row that the store accepts, for a test to spoil one field of.
// It is a wait's opening, which is the shape with the fewest columns any
// other shape has to leave empty.
func aRow() decisionlog.Row {
	id := record.NewID("dl")
	return decisionlog.Row{
		ID:            id,
		FormatVersion: "wait/1",
		Actor:         gate,
		At:            record.Now(),
		Shape:         decisionlog.ShapeWait,
		Part:          decisionlog.PartOpen,
		Payload:       "written around the writer",
		PrevHash:      "prev-" + id,
		Hash:          "hash-" + id,
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
