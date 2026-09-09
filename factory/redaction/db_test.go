// The database tests of this package are in redaction_test rather than in
// redaction, because they open the pool through package postgres, which
// imports this one to apply its DDL. deps.txt records the edge as "test
// redaction -> postgres lease".
//
// None of these tests skips when the database is unreachable. The milestone
// is demonstrated by them running, so an unreachable database fails the run.
package redaction_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/erasurelist"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/legalhold"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/redaction"
)

var owner = record.Actor{Kind: record.KindHuman, Key: "owner", Basis: record.BasisClaimed}

// erasureListAppender is a working [redaction.ErasureAppender] over a fresh
// file, the same call reportstore.Store.AppendErasure makes over its own: a
// plain call to erasurelist.Append. It returns the list's path beside it, so
// a test can read the row back and check the order and the key.
func erasureListAppender(t *testing.T) (string, redaction.ErasureAppender) {
	t.Helper()
	list := filepath.Join(t.TempDir(), "erasure-list")
	return list, func(kind, key, removed string) error {
		return erasurelist.Append(list, key, kind, removed)
	}
}

func newTable(t *testing.T) (context.Context, *pgxpool.Pool, lease.Token) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m9_rdn_" + hex.EncodeToString(suffix[:])

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
	if err := postgres.Apply(ctx, pool); err != nil {
		t.Fatalf("applying the schema: %v", err)
	}
	token, err := lease.Acquire(ctx, pool, "test", time.Minute)
	if err != nil {
		t.Fatalf("acquiring the lease: %v", err)
	}
	return ctx, pool, token
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

// TestARedactionReadsBackAsItWasWritten: the actor, the target, the reason
// and the spans, and the erasure key the row it follows was appended under.
func TestARedactionReadsBackAsItWasWritten(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := redaction.NewWriter(pool, token)
	_, appendErasure := erasureListAppender(t)

	writing := redaction.Writing{
		Target: redaction.Target{Kind: redaction.KindStatement, ID: "in_1"},
		Reason: "a person's name in the words a report carried",
		Spans:  []redaction.Span{{Start: 4, End: 9}, {Start: 20, End: 26}},
	}
	written, err := w.Insert(ctx, owner, writing, nil, appendErasure)
	if err != nil {
		t.Fatalf("writing the redaction: %v", err)
	}
	if written.ErasureKey != redaction.Key(owner, writing) {
		t.Errorf("the record names erasure key %s, the caller appended the row under %s",
			written.ErasureKey, redaction.Key(owner, writing))
	}

	read, err := redaction.Get(ctx, pool, written.ID)
	if err != nil {
		t.Fatalf("reading it back: %v", err)
	}
	if read.Actor != owner {
		t.Errorf("the actor read back as %+v, want %+v", read.Actor, owner)
	}
	if read.Target != writing.Target {
		t.Errorf("the target read back as %s, want %s", read.Target, writing.Target)
	}
	if read.Reason != writing.Reason {
		t.Errorf("the reason read back as %q, want %q", read.Reason, writing.Reason)
	}
	if len(read.Spans) != 2 || read.Spans[0] != writing.Spans[0] || read.Spans[1] != writing.Spans[1] {
		t.Errorf("the spans read back as %+v, want %+v", read.Spans, writing.Spans)
	}
	if read.At != written.At || read.ErasureKey != written.ErasureKey {
		t.Errorf("read back as %+v, written as %+v", read, written)
	}
}

// TestTheRecordCarriesNoWords: there is no column here a word of the target
// can be stored in. The record is the bounds and the reason, and the erasure
// list beside it is the same discipline for the row on the host.
func TestTheRecordCarriesNoWords(t *testing.T) {
	ctx, pool, _ := newTable(t)

	// The schema is named, because every test in this run has a schema of its
	// own and each one holds a table of this name.
	rows, err := pool.Query(ctx, `select column_name from information_schema.columns
		where table_schema = current_schema() and table_name = $1 order by column_name`, redaction.Table)
	if err != nil {
		t.Fatalf("reading the columns of %s: %v", redaction.Table, err)
	}
	defer rows.Close()

	want := map[string]bool{
		"id": true, "format_version": true, "actor_kind": true, "actor_key": true,
		"actor_key_basis": true, "at": true, "target_kind": true, "target_id": true,
		"spans": true, "reason": true, "erasure_key": true,
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("reading a column name: %v", err)
		}
		if !want[name] {
			t.Errorf("%s carries the column %s, which the record has no field for", redaction.Table, name)
		}
		delete(want, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading the columns of %s: %v", redaction.Table, err)
	}
	for name := range want {
		t.Errorf("%s is missing the column %s", redaction.Table, name)
	}
}

// TestTheErasurePerformedAgainWritesNothing: the key is derived from what the
// erasure is over, so the second performance finds the record the first wrote
// through its erasure key and returns it, writing no second row — [Insert]
// makes the keyed repeat a no-op itself, rather than reaching the unique index.
func TestTheErasurePerformedAgainWritesNothing(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := redaction.NewWriter(pool, token)
	list, appendErasure := erasureListAppender(t)

	writing := redaction.Writing{
		Target: redaction.Target{Kind: redaction.KindReport, ID: "rep_1"},
		Reason: "the reporter asked for their words to go",
		Spans:  []redaction.Span{{Start: 0, End: 5}},
	}
	first, err := w.Insert(ctx, owner, writing, nil, appendErasure)
	if err != nil {
		t.Fatalf("writing the redaction: %v", err)
	}

	found, ok, err := redaction.ByErasureKey(ctx, pool, redaction.Key(owner, writing))
	if err != nil {
		t.Fatalf("reading by the erasure key: %v", err)
	}
	if !ok || found.ID != first.ID {
		t.Fatalf("the erasure key found %+v, want %s", found, first.ID)
	}
	again, err := w.Insert(ctx, owner, writing, nil, appendErasure)
	if err != nil {
		t.Fatalf("writing the same erasure again: %v", err)
	}
	if again.ID != first.ID {
		t.Errorf("the same erasure written twice returned %s, want the first performance's %s",
			again.ID, first.ID)
	}

	over, err := redaction.ForTarget(ctx, pool, writing.Target)
	if err != nil {
		t.Fatalf("reading the redactions over the target: %v", err)
	}
	if len(over) != 1 {
		t.Errorf("the same erasure written twice left %d records, want one", len(over))
	}

	rows, err := erasurelist.ReadKind(list, string(redaction.KindReport))
	if err != nil {
		t.Fatalf("reading the erasure list: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("the erasure list holds %d row(s) after the same erasure was performed twice, want one",
			len(rows))
	}
}

// TestARedactionIsRefusedWhileALegalHoldStands: both halves of the refusal —
// the hold over the whole install, which this package reads itself, and the
// narrower reading its caller makes — and nothing is written for either.
func TestARedactionIsRefusedWhileALegalHoldStands(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := redaction.NewWriter(pool, token)
	writing := redaction.Writing{
		Target: redaction.Target{Kind: redaction.KindArtifactVersion, ID: "art_1"},
		Reason: "words quoted into a spec",
		Spans:  []redaction.Span{{Start: 1, End: 2}},
	}

	reaches := func(context.Context) (bool, error) { return true, nil }
	if _, err := w.Insert(ctx, owner, writing, reaches, nil); !errors.Is(err, redaction.ErrLegalHoldReaches) {
		t.Errorf("a redaction the caller's check refuses = %v, want ErrLegalHoldReaches", err)
	}

	holds := legalhold.NewWriter(pool, token)
	if _, err := holds.Insert(ctx, owner, legalhold.Subject{Kind: legalhold.SubjectFactory},
		"counsel asked for everything to stand"); err != nil {
		t.Fatalf("setting the hold: %v", err)
	}
	if _, err := w.Insert(ctx, owner, writing, nil, nil); !errors.Is(err, redaction.ErrLegalHoldReaches) {
		t.Errorf("a redaction under a hold on the whole install = %v, want ErrLegalHoldReaches", err)
	}

	over, err := redaction.OverKind(ctx, pool, redaction.KindArtifactVersion)
	if err != nil {
		t.Fatalf("reading the redactions over an artifact version: %v", err)
	}
	if len(over) != 0 {
		t.Errorf("a refused redaction wrote %d records", len(over))
	}
}

// TestARedactionNamesATargetAReasonAndItsSpans: each field the record cannot
// be read without, refused by the writer before the store sees it.
func TestARedactionNamesATargetAReasonAndItsSpans(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := redaction.NewWriter(pool, token)

	full := redaction.Writing{
		Target: redaction.Target{Kind: redaction.KindStatement, ID: "in_1"},
		Reason: "why",
		Spans:  []redaction.Span{{Start: 0, End: 1}},
	}
	for _, c := range []struct {
		what    string
		writing redaction.Writing
		want    error
	}{
		{"a kind outside the three", redaction.Writing{
			Target: redaction.Target{Kind: "criterion", ID: "cr_1"}, Reason: full.Reason, Spans: full.Spans,
		}, redaction.ErrTargetKindUnknown},
		{"no target", redaction.Writing{
			Target: redaction.Target{Kind: redaction.KindStatement}, Reason: full.Reason, Spans: full.Spans,
		}, redaction.ErrTargetIDEmpty},
		{"no reason", redaction.Writing{Target: full.Target, Spans: full.Spans}, redaction.ErrReasonEmpty},
		{"no span", redaction.Writing{Target: full.Target, Reason: full.Reason}, redaction.ErrSpansEmpty},
		{"a span that is no range", redaction.Writing{
			Target: full.Target, Reason: full.Reason, Spans: []redaction.Span{{Start: 9, End: 2}},
		}, redaction.ErrSpanOutOfRange},
	} {
		if _, err := w.Insert(ctx, owner, c.writing, nil, nil); !errors.Is(err, c.want) {
			t.Errorf("a redaction with %s = %v, want %v", c.what, err, c.want)
		}
	}
	if _, err := redaction.Get(ctx, pool, "rdn_nothing"); !errors.Is(err, redaction.ErrNotFound) {
		t.Errorf("reading a redaction nothing wrote = %v, want ErrNotFound", err)
	}
}

// TestEachWriterReadsOnlyTheRedactionsNamingItsOwnRecords: the pass each
// target's writer makes is a read by kind, and the read that serves one
// target's words is a read by that target.
func TestEachWriterReadsOnlyTheRedactionsNamingItsOwnRecords(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := redaction.NewWriter(pool, token)
	_, appendErasure := erasureListAppender(t)

	for _, target := range []redaction.Target{
		{Kind: redaction.KindReport, ID: "rep_1"},
		{Kind: redaction.KindStatement, ID: "in_1"},
		{Kind: redaction.KindStatement, ID: "in_2"},
		{Kind: redaction.KindArtifactVersion, ID: "art_1"},
	} {
		if _, err := w.Insert(ctx, owner, redaction.Writing{
			Target: target, Reason: "a person's name", Spans: []redaction.Span{{Start: 0, End: 3}},
		}, nil, appendErasure); err != nil {
			t.Fatalf("writing the redaction of %s: %v", target, err)
		}
	}

	statements, err := redaction.OverKind(ctx, pool, redaction.KindStatement)
	if err != nil {
		t.Fatalf("reading the redactions over the statements: %v", err)
	}
	if len(statements) != 2 {
		t.Errorf("intake's pass reads %d redactions, want the 2 naming a statement", len(statements))
	}
	for _, r := range statements {
		if r.Target.Kind != redaction.KindStatement {
			t.Errorf("intake's pass reads a redaction of %s", r.Target)
		}
	}

	one, err := redaction.ForTarget(ctx, pool, redaction.Target{Kind: redaction.KindStatement, ID: "in_2"})
	if err != nil {
		t.Fatalf("reading the redactions naming one statement: %v", err)
	}
	if len(one) != 1 || one[0].Target.ID != "in_2" {
		t.Errorf("the read serving one statement's words read %+v", one)
	}
}

// TestDDLListsEveryTargetKind keeps the CHECK constraint and
// [redaction.TargetKinds] from disagreeing.
func TestDDLListsEveryTargetKind(t *testing.T) {
	const open = "target_kind in ("
	statement := redaction.DDL[0]
	i := strings.Index(statement, open)
	if i < 0 {
		t.Fatalf("the DDL has no %q list", open)
	}
	rest := statement[i+len(open):]
	listed := strings.Split(rest[:strings.Index(rest, ")")], ",")
	if len(listed) != len(redaction.TargetKinds) {
		t.Fatalf("the constraint lists %d target kinds, TargetKinds has %d",
			len(listed), len(redaction.TargetKinds))
	}
	for n, k := range redaction.TargetKinds {
		if got, want := strings.TrimSpace(listed[n]), "'"+string(k)+"'"; got != want {
			t.Errorf("the constraint lists %s where TargetKinds has %s", got, want)
		}
	}
}

// TestAWriteIsFencedByTheLease: a token another acquisition has moved past
// writes nothing, the check every write in the store makes inside its own
// transaction.
func TestAWriteIsFencedByTheLease(t *testing.T) {
	ctx, pool, token := newTable(t)
	if err := lease.Release(ctx, pool, token); err != nil {
		t.Fatalf("releasing the lease: %v", err)
	}
	if _, err := lease.Acquire(ctx, pool, "another", time.Minute); err != nil {
		t.Fatalf("acquiring the lease again: %v", err)
	}
	_, err := redaction.NewWriter(pool, token).Insert(ctx, owner, redaction.Writing{
		Target: redaction.Target{Kind: redaction.KindReport, ID: "rep_1"},
		Reason: "why", Spans: []redaction.Span{{Start: 0, End: 1}},
	}, nil, nil)
	if !errors.Is(err, lease.ErrFenced) {
		t.Errorf("a write under a token the lease has moved past = %v, want ErrFenced", err)
	}
}

// TestTheErasureListRowLandsBeforeTheRecord: [redaction.Insert] calls
// appendErasure before it writes the record, keyed the same way, so a caller
// counting on the order can watch it inside the appender itself.
func TestTheErasureListRowLandsBeforeTheRecord(t *testing.T) {
	ctx, pool, token := newTable(t)
	list, appendErasure := erasureListAppender(t)
	called := false
	watching := func(kind, key, removed string) error {
		called = true
		_, ok, err := redaction.ByErasureKey(ctx, pool, key)
		if err != nil {
			t.Fatalf("reading by the erasure key inside the appender: %v", err)
		}
		if ok {
			t.Errorf("a record already stood under the key when the erasure-list row was appended")
		}
		return appendErasure(kind, key, removed)
	}

	writing := redaction.Writing{
		Target: redaction.Target{Kind: redaction.KindStatement, ID: "in_1"},
		Reason: "why this went",
		Spans:  []redaction.Span{{Start: 0, End: 2}},
	}
	written, err := redaction.NewWriter(pool, token).Insert(ctx, owner, writing, nil, watching)
	if err != nil {
		t.Fatalf("writing the redaction: %v", err)
	}
	if !called {
		t.Fatalf("Insert never called the appender")
	}
	rows, err := erasurelist.ReadKind(list, string(redaction.KindStatement))
	if err != nil {
		t.Fatalf("reading the erasure list: %v", err)
	}
	if len(rows) != 1 || rows[0].Key != written.ErasureKey {
		t.Errorf("the list holds %+v, want the one row keyed %s", rows, written.ErasureKey)
	}
}

// TestAStopBetweenTheRowAndTheRecordLeavesTheRowAndNoRecord simulates the
// failure a caller's transaction can still meet after appendErasure has
// already run: a colliding id makes the record's own insert fail, and what
// stands afterward is the row and no record, the event visibly owing rather
// than visibly done.
func TestAStopBetweenTheRowAndTheRecordLeavesTheRowAndNoRecord(t *testing.T) {
	ctx, pool, token := newTable(t)
	list, appendErasure := erasureListAppender(t)

	// A row of this id already stands, under a different erasure key, so the
	// performance below collides on the primary key rather than on the keyed
	// repeat [Insert] itself refuses to duplicate.
	seed, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning the seed: %v", err)
	}
	if _, err := redaction.Insert(ctx, seed, token, owner, "rdn_collide", redaction.Writing{
		Target: redaction.Target{Kind: redaction.KindStatement, ID: "in_seed"},
		Reason: "seeding a collision", Spans: []redaction.Span{{Start: 0, End: 1}},
	}, appendErasure); err != nil {
		t.Fatalf("seeding the colliding row: %v", err)
	}
	if err := seed.Commit(ctx); err != nil {
		t.Fatalf("committing the seed: %v", err)
	}

	writing := redaction.Writing{
		Target: redaction.Target{Kind: redaction.KindReport, ID: "rep_stop"},
		Reason: "a stop between the row and the record",
		Spans:  []redaction.Span{{Start: 0, End: 3}},
	}
	key := redaction.Key(owner, writing)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning: %v", err)
	}
	if _, err := redaction.Insert(ctx, tx, token, owner, "rdn_collide", writing, appendErasure); err == nil {
		t.Fatalf("writing a redaction under a colliding id: want an error, wrote one")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rolling back the failed performance: %v", err)
	}

	rows, err := erasurelist.ReadKind(list, string(redaction.KindReport))
	if err != nil {
		t.Fatalf("reading the erasure list: %v", err)
	}
	if len(rows) != 1 || rows[0].Key != key {
		t.Errorf("the erasure-list row is %+v, want the one row keyed %s", rows, key)
	}

	if _, ok, err := redaction.ByErasureKey(ctx, pool, key); err != nil {
		t.Fatalf("reading by the erasure key: %v", err)
	} else if ok {
		t.Errorf("a record stands under the key, though the insert that would have written it failed")
	}
}
