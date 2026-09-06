// The database tests of this package are in postgres_test, which opens the
// pool through the package itself and applies the whole schema into a schema
// of its own.
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package postgres_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

// newSchema gives a test a schema of its own with nothing in it, dropped when
// the test ends.
func newSchema(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m2_pg_" + hex.EncodeToString(suffix[:])

	pool, err := postgres.Open(ctx, inSchema(t, postgres.URL(), schema))
	if err != nil {
		t.Fatalf("the database at %s is not reachable, and these tests do not skip: %v", postgres.URL(), err)
	}
	t.Cleanup(func() {
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
	return ctx, pool
}

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

// writeChange puts a row into the history directly, which is how a test stands
// up a store an earlier or a later version left behind.
func writeChange(t *testing.T, ctx context.Context, pool *pgxpool.Pool, c postgres.Change, checksum string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `insert into `+postgres.HistoryTable+`
		(id, version, checksum, effect, snapshot, applied_at) values ($1, $2, $3, $4, $5, $6)`,
		c.ID, c.Version, checksum, string(c.Effect), c.Snapshot, record.Now()); err != nil {
		t.Fatalf("writing the history row %s: %v", c.ID, err)
	}
}

// TestStartRecordsEveryChangeOnceAndReadsItBack: the first start records what
// this version declares, and a second start against the same store records
// nothing — a change already recorded is skipped.
func TestStartRecordsEveryChangeOnceAndReadsItBack(t *testing.T) {
	ctx, pool := newSchema(t)

	applied, err := postgres.Start(ctx, pool)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if len(applied) != len(postgres.Changes) {
		t.Fatalf("the first start applied %d changes, want the %d this version declares",
			len(applied), len(postgres.Changes))
	}

	held, err := postgres.History(ctx, pool)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(held) != len(postgres.Changes) || held[0].ID != postgres.Changes[0].ID {
		t.Fatalf("the history holds %+v, want the changes this version declares", held)
	}
	if held[0].Version != postgres.Changes[0].Version || held[0].Effect != postgres.Changes[0].Effect ||
		held[0].Checksum == "" || held[0].AppliedAt == "" {
		t.Errorf("the recorded change is %+v, want its version, effect, checksum and time", held[0])
	}

	again, err := postgres.Start(ctx, pool)
	if err != nil {
		t.Fatalf("Start a second time: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("the second start applied %v, want nothing", again)
	}
}

// TestStartRefusesARemovalItDoesNotDeclare: the store has had something taken
// out of it that this version still reads, so the version refuses to start.
func TestStartRefusesARemovalItDoesNotDeclare(t *testing.T) {
	ctx, pool := newSchema(t)
	if _, err := postgres.Start(ctx, pool); err != nil {
		t.Fatalf("Start: %v", err)
	}

	writeChange(t, ctx, pool, postgres.Change{
		Version: postgres.Version + 1, ID: "a form dropped by a later version",
		Effect: postgres.EffectRemoval, Snapshot: "snapshot-1",
	}, "whatever the later version's text hashed to")

	if _, err := postgres.Start(ctx, pool); !errors.Is(err, postgres.ErrRemovalNotDeclared) {
		t.Errorf("Start against a removal this version does not declare = %v, want ErrRemovalNotDeclared", err)
	}
}

// TestStartAgainstTheVersionAfterAndTheOneBeyondIt: a version starts against a
// store the version after it widened, and refuses one a version further ahead
// touched — a skipped version is not a supported upgrade.
func TestStartAgainstTheVersionAfterAndTheOneBeyondIt(t *testing.T) {
	ctx, pool := newSchema(t)
	if _, err := postgres.Start(ctx, pool); err != nil {
		t.Fatalf("Start: %v", err)
	}

	writeChange(t, ctx, pool, postgres.Change{
		Version: postgres.Version + 1, ID: "a column the next version added beside the old one",
		Effect: postgres.EffectWidening,
	}, "the next version's checksum")

	if _, err := postgres.Start(ctx, pool); err != nil {
		t.Fatalf("Start against a widening from the version after = %v, want it to start", err)
	}

	writeChange(t, ctx, pool, postgres.Change{
		Version: postgres.Version + 2, ID: "a column a version two ahead added",
		Effect: postgres.EffectWidening,
	}, "a checksum from further ahead")

	if _, err := postgres.Start(ctx, pool); !errors.Is(err, postgres.ErrVersionAhead) {
		t.Errorf("Start against a version two ahead = %v, want ErrVersionAhead", err)
	}
}

// TestStartRefusesAChangeTheHistoryCannotHonour: the history holds a change
// this version declares under another checksum, so the store is not in the
// state the version's own text stands for.
func TestStartRefusesAChangeTheHistoryCannotHonour(t *testing.T) {
	ctx, pool := newSchema(t)
	if _, err := postgres.Start(ctx, pool); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if _, err := pool.Exec(ctx, `update `+postgres.HistoryTable+` set checksum = $2 where id = $1`,
		postgres.Changes[0].ID, "a checksum no text of this version hashes to"); err != nil {
		t.Fatalf("rewriting the recorded checksum: %v", err)
	}

	if _, err := postgres.Start(ctx, pool); !errors.Is(err, postgres.ErrHistoryDisagrees) {
		t.Errorf("Start against a checksum this version does not declare = %v, want ErrHistoryDisagrees", err)
	}
}

// TestStartRefusesADeclaredRemovalWithNoSnapshot: before a removal is applied a
// snapshot of this store is taken and verified and named on the install-event
// row, and a snapshot that cannot be taken is an upgrade not performed. The step
// that takes one is not built, so the check is over what a version declares.
func TestStartRefusesADeclaredRemovalWithNoSnapshot(t *testing.T) {
	ctx, pool := newSchema(t)

	declared := postgres.Changes
	t.Cleanup(func() { postgres.Changes = declared })
	postgres.Changes = append(append([]postgres.Change{}, declared...), postgres.Change{
		Version: postgres.Version, ID: "a one-way step", Text: "drop the form the version before read",
		Effect: postgres.EffectRemoval,
	})

	if _, err := postgres.Start(ctx, pool); !errors.Is(err, postgres.ErrRemovalWithoutASnapshot) {
		t.Errorf("Start with a removal declared and no snapshot = %v, want ErrRemovalWithoutASnapshot", err)
	}
}
