// The decision log's retention pass as this interface runs it: the truncation
// row it appends names who ran it, the value it enforced, the boundary it cut
// to, and the two versions in force at the cut, without which package
// decisionlog refuses the cut outright.
package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/lease"
)

// TestTheRetentionPassNamesTheTwoVersionsInForce: the truncation appends a
// chained row of its own naming the policy version and the score version in
// force at the cut, so a decision after the cut naming a version before it is
// still read against what it was decided under. A cut that named neither is
// refused, so a pass that did not read them could not run at all.
func TestTheRetentionPassNamesTheTwoVersionsInForce(t *testing.T) {
	ctx, pool := newOwner(t)
	install(t, ctx, pool)

	before := logRows(t, ctx, pool)
	if len(before) == 0 {
		t.Fatal("the install left no row in the log, so there is nothing to cut to")
	}
	boundary := before[len(before)-1].ID

	if err := truncateCommand([]string{"-boundary", boundary}); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	var truncation decisionlog.Row
	for _, row := range logRows(t, ctx, pool) {
		if row.Shape == decisionlog.ShapeTruncation {
			truncation = row
		}
	}
	if truncation.ID == "" {
		t.Fatal("the log holds no truncation row, and that row is what says a cut happened and where")
	}
	if truncation.PolicyVersion == "" || truncation.ScoreVersion == "" {
		t.Errorf("the truncation names policy version %q and score version %q, want both in force at the cut",
			truncation.PolicyVersion, truncation.ScoreVersion)
	}
	if truncation.Actor.Kind == "" {
		t.Error("the truncation names no actor, and the row says who authored the value it enforced")
	}
}

// logRows is every row of the log, read the way [policyVersions] reads the
// versions: the lease is taken again, each subcommand having taken one of its
// own for the life of the command and released it when it ended.
func logRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool) []decisionlog.Row {
	t.Helper()
	token, err := lease.Acquire(ctx, pool, defaultInstance(), -time.Second)
	if err != nil {
		t.Fatalf("acquiring the lease: %v", err)
	}
	rows, err := decisionlog.NewReader(pool, token).
		Read(ctx, asPrincipal(owner(t, ctx, pool, token, "owner")))
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	return rows
}
