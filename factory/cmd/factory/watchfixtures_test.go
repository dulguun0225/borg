// Helpers shared by the watch tests: driving a release to a rolled-back
// state, composing the path over one test's deps, and the drift
// detector's own store.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/driftdetector"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/incident"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/window"
)

// rolledBack is what [rollBackABadRelease] leaves behind: the revert intent the
// health monitor raised and the statement a later run works it through.
type rolledBack struct {
	revertIntentID  string
	revertStatement string
}

// rollBackABadRelease ships a good release and then a bad one, and returns once the
// bad one has been failed and rolled back. It is the state three of the tests here
// start from, so it is written once — and it asserts its own outcome, because a test
// that began from a state it did not reach would report the wrong thing.
func rollBackABadRelease(ctx context.Context, t *testing.T, d deps, out *bytes.Buffer) rolledBack {
	t.Helper()
	if _, err := run(ctx, d, of(theStatement)); err != nil {
		t.Fatalf("the first run stopped: %v\noutput so far:\n%s", err, out)
	}

	d.in = strings.NewReader(approvals)
	d.model = interviewed(2)
	res, err := run(ctx, d, of(theSecondStatement))
	if err != nil {
		t.Fatalf("the bad run stopped: %v\noutput so far:\n%s", err, out)
	}
	bad := only(t, res)
	w, err := window.Get(ctx, d.pool, bad.windowID)
	if err != nil {
		t.Fatalf("reading the bad release's window: %v", err)
	}
	if w.Exit != window.ExitFailed {
		t.Fatalf("the bad release's window closed %q, want harm:\n%s", w.Exit, out)
	}

	rollback, found, err := deploy.NewestRollback(ctx, d.pool, res.serviceID, res.environmentID)
	if err != nil || !found {
		t.Fatalf("NewestRollback = found %v, %v", found, err)
	}
	// The rollback's deploy record names the release it failed and not the intent
	// the crossing raised: that intent is on the incident the health monitor
	// raised at the same crossing, and the failed release is the link between the
	// two — the same walk the production deploy row's own hold makes.
	open, found, err := incident.Open(ctx, d.pool, res.serviceID, rollback.Undoing.FailedReleaseID)
	if err != nil || !found {
		t.Fatalf("incident.Open over the failed release = found %v, %v", found, err)
	}
	revert, err := intent.Get(ctx, d.pool, open.IntentID)
	if err != nil {
		t.Fatalf("reading the revert intent: %v", err)
	}
	return rolledBack{revertIntentID: revert.ID, revertStatement: revert.Statement}
}

// p composes the path over the same deps a run uses, for a test that drives one step
// rather than the whole thing.
func p(ctx context.Context, t *testing.T, d deps) *path {
	t.Helper()
	composed, err := compose(ctx, d)
	if err != nil {
		t.Fatalf("composing the path: %v", err)
	}
	return composed
}

// watching is the service one call of the health monitor is about.
func watching(s shipped, name string) healthmonitor.Watching {
	return healthmonitor.Watching{ID: s.serviceID, Name: name, EnvironmentID: s.environmentID}
}

// serviceOf is the id of the service these tests run against.
func serviceOf(ctx context.Context, t *testing.T, d deps) string {
	t.Helper()
	var id string
	if err := d.pool.QueryRow(ctx, `select id from service where name = $1`, theService).Scan(&id); err != nil {
		t.Fatalf("reading the service's id: %v", err)
	}
	return id
}

// newDriftDetectorStore is the drift detector's own store for one test: a schema
// of its own, its own schema applied by its own applier, and nothing of the
// factory's in it. The factory reads it and never writes it, which is what a
// pool handed to the path as its driftdetector is.
//
// It is opened on the same server the factory's tests use, with a schema of its own,
// rather than on [driftdetector.DefaultURL]. What makes this store independent is that no
// factory component writes it and that it is reached through a URL of its own — not
// which machine it is on — so a test naming a second server would be checking the
// deployment rather than the code, and would fail wherever the factory's database is
// not where that default says.
func newDriftDetectorStore(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the drift detector's schema: %v", err)
	}
	schema := "driftdetector_" + hex.EncodeToString(suffix[:])

	pool, err := driftdetector.Open(ctx, inSchema(t, postgres.URL(), schema))
	if err != nil {
		t.Fatalf("the drift detector's store is not reachable, and these tests do not skip: %v", err)
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
	if err := driftdetector.Apply(ctx, pool); err != nil {
		t.Fatalf("applying the drift detector's schema: %v", err)
	}
	return pool
}
