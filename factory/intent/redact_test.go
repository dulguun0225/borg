package intent_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/erasurelist"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/redaction"
)

// redacting writes one redaction naming an intent's statement, the way the
// erasure at Factory does, and returns it. The erasure-list row it appends
// goes to a list of its own — never the one [listIn] returns — because this
// package writes none of its own and the check against [listIn]'s empty one
// is what says so.
func redacting(t *testing.T, ctx context.Context, pool *pgxpool.Pool, intentID string,
	spans ...redaction.Span) redaction.Redaction {
	t.Helper()
	r, err := redaction.NewWriter(pool, currentToken(t, ctx, pool)).Insert(ctx, owner, redaction.Writing{
		Target: redaction.Target{Kind: redaction.KindStatement, ID: intentID},
		Reason: "a person named in the words a report carried",
		Spans:  spans,
	}, nil, erasureAppender(t))
	if err != nil {
		t.Fatalf("writing the redaction of %s: %v", intentID, err)
	}
	return r
}

// erasureAppender is a working [redaction.ErasureAppender] over a fresh file,
// the same call reportstore.Store.AppendErasure makes over its own: a plain
// call to erasurelist.Append. [redaction.Insert] refuses a call supplying
// none, and this package composes no store to hand it one of its own.
func erasureAppender(t *testing.T) redaction.ErasureAppender {
	t.Helper()
	list := filepath.Join(t.TempDir(), "erasure-list")
	return func(kind, key, removed string) error {
		return erasurelist.Append(list, key, kind, removed)
	}
}

// currentToken is the lease's number as the fixture's own acquisition left
// it, for a writer this test composes beside the intake.
func currentToken(t *testing.T, ctx context.Context, pool *pgxpool.Pool) lease.Token {
	t.Helper()
	var number int64
	if err := pool.QueryRow(ctx, `select number from `+lease.Table+` where id = 1`).Scan(&number); err != nil {
		t.Fatalf("reading the lease's number: %v", err)
	}
	return lease.Token(number)
}

// listIn is the erasure list this test's composition holds: a file beside the
// test's own directory, the way it is a file beside the targets on a host.
func listIn(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "erasure-list")
}

// TestAStatementIsServedThroughItsRedactionsBeforeThePassRuns: the erasure is
// effective at the one write, whether or not intake's own destruction has
// reached the row.
func TestAStatementIsServedThroughItsRedactionsBeforeThePassRuns(t *testing.T) {
	ctx, pool, in := newIntake(t)
	taken := requested(t, ctx, in, "the button Ada pressed does nothing")
	redacting(t, ctx, pool, taken.ID, redaction.Span{Start: 11, End: 14})

	read, err := intent.Get(ctx, pool, taken.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if strings.Contains(read.Statement, "Ada") {
		t.Errorf("the statement served as %q, with the words a redaction names still in it", read.Statement)
	}
	if len(read.Statement) != len(taken.Statement) {
		t.Errorf("the statement served as %q, want the same length with a gap where the words were",
			read.Statement)
	}

	every, err := intent.InProject(ctx, pool, taken.ProjectID)
	if err != nil {
		t.Fatalf("InProject: %v", err)
	}
	for _, one := range every {
		if one.ID == taken.ID && strings.Contains(one.Statement, "Ada") {
			t.Errorf("the list read the statement as %q", one.Statement)
		}
	}
}

// TestThePassDestroysTheBytesAndTheLinksStand: what destroys the bytes is
// this package, the statement's writer, and what is erased is text inside the
// record and never the record.
func TestThePassDestroysTheBytesAndTheLinksStand(t *testing.T) {
	ctx, pool, in := newIntake(t)
	taken := requested(t, ctx, in, "the button Ada pressed does nothing")
	redacting(t, ctx, pool, taken.ID, redaction.Span{Start: 11, End: 14})

	destroyed, err := in.RedactionPass(ctx)
	if err != nil {
		t.Fatalf("RedactionPass: %v", err)
	}
	if destroyed != 1 {
		t.Errorf("the pass destroyed %d statements, want 1", destroyed)
	}

	// The pass writes no row of the erasure list: the report store is that
	// list's one writer, and the row for a statement was appended through it by
	// the action before this pass had anything to read.
	list := listIn(t)
	if _, err := in.RedactionPass(ctx); err != nil {
		t.Fatalf("RedactionPass again: %v", err)
	}
	rows, err := erasurelist.ReadKind(list, erasurelist.KindStatement)
	if err != nil || len(rows) != 0 {
		t.Errorf("intake's pass left %+v on an erasure list, %v; want it to write none", rows, err)
	}

	var stored string
	if err := pool.QueryRow(ctx, `select statement from `+intent.Table+` where id = $1`,
		taken.ID).Scan(&stored); err != nil {
		t.Fatalf("reading the stored statement: %v", err)
	}
	if strings.Contains(stored, "Ada") {
		t.Errorf("the stored statement is %q, with the bytes a redaction named still in it", stored)
	}
	read, err := intent.Get(ctx, pool, taken.ID)
	if err != nil {
		t.Fatalf("Get after the pass: %v", err)
	}
	if read.ID != taken.ID || read.Source != taken.Source || read.State != taken.State {
		t.Errorf("the record read back as %+v after the erasure, want %+v", read, taken)
	}
}

// TestThePassPassesOverARedactionOfAnotherKind: each writer destroys the
// bytes inside its own records and no others.
func TestThePassPassesOverARedactionOfAnotherKind(t *testing.T) {
	ctx, pool, in := newIntake(t)
	taken := requested(t, ctx, in, "the button Ada pressed does nothing")

	r := redacting(t, ctx, pool, taken.ID, redaction.Span{Start: 11, End: 14})
	other := r
	other.Target.Kind = redaction.KindArtifactVersion
	if err := in.Redact(ctx, other); !errors.Is(err, intent.ErrNotAStatement) {
		t.Errorf("intake redacting an artifact version = %v, want ErrNotAStatement", err)
	}

	missing := r
	missing.Target = redaction.Target{Kind: redaction.KindStatement, ID: "in_nothing"}
	if err := in.Redact(ctx, missing); !errors.Is(err, intent.ErrIntentNotFound) {
		t.Errorf("intake redacting an intent nothing wrote = %v, want ErrIntentNotFound", err)
	}

	// A span outside the statement destroys nothing: the words are read
	// against it before anything is written.
	outside := r
	outside.Spans = []redaction.Span{{Start: 0, End: len(taken.Statement) + 10}}
	if err := in.Redact(ctx, outside); !errors.Is(err, intent.ErrSpanOutOfRange) {
		t.Errorf("intake redacting a span outside the statement = %v, want ErrSpanOutOfRange", err)
	}
	var stored string
	if err := pool.QueryRow(ctx, `select statement from `+intent.Table+` where id = $1`,
		taken.ID).Scan(&stored); err != nil {
		t.Fatalf("reading the stored statement: %v", err)
	}
	if !strings.Contains(stored, "Ada") {
		t.Errorf("a refused redaction destroyed words: the statement is %q", stored)
	}
}

// TestReplayDestroysAgainWhatTheListSaysWasRemoved: the list is never rolled
// back, so a restore that brought the words back is served through this
// before the store serves anything.
func TestReplayDestroysAgainWhatTheListSaysWasRemoved(t *testing.T) {
	ctx, pool, in := newIntake(t)
	taken := requested(t, ctx, in, "the button Ada pressed does nothing")

	// The rows the erasure action appended through the report store, written
	// here directly because this package composes no store: what a replay
	// reads is the file, whoever wrote it.
	list := filepath.Join(t.TempDir(), "erasure-list")
	if err := erasurelist.Append(list, "key", erasurelist.KindStatement, taken.ID+" 11-14"); err != nil {
		t.Fatalf("appending the erasure-list row: %v", err)
	}
	if err := erasurelist.Append(list, "another", erasurelist.KindMapping, "hk_alice"); err != nil {
		t.Fatalf("appending a row of another kind: %v", err)
	}

	destroyed, err := in.Replay(ctx, list)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if destroyed != 1 {
		t.Errorf("the replay destroyed %d statements, want the 1 row naming one", destroyed)
	}
	var stored string
	if err := pool.QueryRow(ctx, `select statement from `+intent.Table+` where id = $1`,
		taken.ID).Scan(&stored); err != nil {
		t.Fatalf("reading the stored statement: %v", err)
	}
	if strings.Contains(stored, "Ada") {
		t.Errorf("the replay left the stored statement as %q", stored)
	}

	if _, err := os.Stat(list); err != nil {
		t.Errorf("the replay disturbed the list: %v", err)
	}
	if _, err := in.Replay(ctx, filepath.Join(t.TempDir(), "none")); err != nil {
		t.Errorf("a replay over a list that does not exist: %v", err)
	}
}
