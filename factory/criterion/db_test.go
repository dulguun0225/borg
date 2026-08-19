// The database tests of this package are in criterion_test rather than in
// criterion, because they open the pool through package postgres, and an
// external test package keeps that a test edge rather than an import of the
// package that will one day apply this schema. The DDL is applied here with
// pool.Exec, statement by statement, because postgres.Apply does not know
// this package until integration.
//
// None of these tests skips when the database is unreachable. The milestone
// is demonstrated by them running, so an unreachable database fails the run.
package criterion_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

// newSet gives a test a schema of its own with the criterion DDL applied
// inside it. The schema is dropped when the test ends, so a rerun on a
// database a previous run left dirty starts clean.
func newSet(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m1cr_" + hex.EncodeToString(suffix[:])

	pool, err := postgres.Open(ctx, inSchema(t, postgres.URL(), schema))
	if err != nil {
		t.Fatalf("the database at %s is not reachable, and these tests do not skip: %v", postgres.URL(), err)
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
	for n, statement := range criterion.DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("applying criterion statement %d: %v", n+1, err)
		}
	}
	return ctx, pool
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

var store = record.Actor{Kind: record.KindComponent, Name: "artifact.store"}

// inTx runs f inside a transaction and commits it, failing the test on any
// error. Insert takes a transaction because the artifact store calls it
// inside its own; these tests stand where the store does.
func inTx(ctx context.Context, t *testing.T, pool *pgxpool.Pool, f func(pgx.Tx) error) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning a transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := f(tx); err != nil {
		t.Fatalf("inside the transaction: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("committing: %v", err)
	}
}

// TestInsertRefusesAnUnmatchedSentenceWithNoReason is the escape rule's
// first half: a sentence fitting no pattern is admitted only with a tagged
// reason.
func TestInsertRefusesAnUnmatchedSentenceWithNoReason(t *testing.T) {
	ctx, pool := newSet(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning a transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = criterion.Insert(ctx, tx, store, "svc_a", "art_a", "it_a", "The checkout page loads fast.", "")
	if !errors.Is(err, criterion.ErrReasonMissing) {
		t.Fatalf("Insert = %v, want ErrReasonMissing", err)
	}
}

// TestInsertRefusesAMatchedSentenceCarryingAReason is the second half: only
// an escape carries a reason.
func TestInsertRefusesAMatchedSentenceCarryingAReason(t *testing.T) {
	ctx, pool := newSet(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning a transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = criterion.Insert(ctx, tx, store, "svc_a", "art_a", "it_a",
		"The system shall respond within one second.", "just in case")
	if !errors.Is(err, criterion.ErrReasonRefused) {
		t.Fatalf("Insert = %v, want ErrReasonRefused", err)
	}
}

// TestInsertWritesAndInForceReadsBack is a matched sentence and an escape
// written, and the in-force query returning both whole.
func TestInsertWritesAndInForceReadsBack(t *testing.T) {
	ctx, pool := newSet(t)

	var matched, escaped criterion.Criterion
	inTx(ctx, t, pool, func(tx pgx.Tx) error {
		var err error
		matched, err = criterion.Insert(ctx, tx, store, "svc_a", "art_a", "it_a",
			"When a report arrives, the system shall open an intent.", "")
		if err != nil {
			return err
		}
		escaped, err = criterion.Insert(ctx, tx, store, "svc_a", "art_a", "it_a",
			"The checkout page loads fast.", "no pattern fits a latency feel")
		return err
	})

	if matched.Pattern != criterion.PatternEvent {
		t.Errorf("the matched criterion is a %s, want %s", matched.Pattern, criterion.PatternEvent)
	}
	if escaped.Pattern != criterion.PatternEscape {
		t.Errorf("the escaped criterion is a %s, want %s", escaped.Pattern, criterion.PatternEscape)
	}

	// A criterion of another service must not appear in svc_a's set.
	inTx(ctx, t, pool, func(tx pgx.Tx) error {
		_, err := criterion.Insert(ctx, tx, store, "svc_b", "art_b", "it_a",
			"The system shall not appear in another service's set.", "")
		return err
	})

	inForce, err := criterion.InForce(ctx, pool, "svc_a", []string{"it_a"})
	if err != nil {
		t.Fatalf("InForce: %v", err)
	}
	if len(inForce) != 2 {
		t.Fatalf("InForce returned %d criteria, want 2", len(inForce))
	}
	byID := map[string]criterion.Criterion{inForce[0].ID: inForce[0], inForce[1].ID: inForce[1]}
	for _, want := range []criterion.Criterion{matched, escaped} {
		got, ok := byID[want.ID]
		if !ok {
			t.Errorf("InForce does not return %s", want.ID)
			continue
		}
		if got != want {
			t.Errorf("InForce returned %+v, want %+v", got, want)
		}
		if _, err := time.Parse(record.TimeLayout, got.At); err != nil {
			t.Errorf("%s has timestamp %q: %v", got.ID, got.At, err)
		}
	}
}

// TestTheStoreRefusesAReasonMismatchInsertedAroundTheWriter is the CHECK
// constraint doing what Insert already refused: a row written around the
// writer with a reason on a matched pattern is refused by the store.
func TestTheStoreRefusesAReasonMismatchInsertedAroundTheWriter(t *testing.T) {
	ctx, pool := newSet(t)

	_, err := pool.Exec(ctx, `insert into criterion
		(id, actor_kind, actor_name, at, service_id, spec_artifact_id, sentence, pattern, escape_reason)
		values ($1, 'component', 'artifact.store', $2, 'svc_a', 'art_a', 'The system shall hold.', 'always_true', 'a reason it must not carry')`,
		record.NewID(criterion.IDPrefix), record.Now())
	if err == nil {
		t.Fatal("the store accepted a reason on a matched pattern")
	}
}

// TestAnEmptyLinkIsRefusedTwice covers this package's two link columns at one
// of them. An empty link names nothing, so it is refused by the writer and by
// the store, the way every other required field is; record's doc.go states
// what a link is checked for.
func TestAnEmptyLinkIsRefusedTwice(t *testing.T) {
	ctx, pool := newSet(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning a transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const matched = "The system shall respond within one second."
	if _, err := criterion.Insert(ctx, tx, store, "", "art_a", "it_a", matched, ""); !errors.Is(err, criterion.ErrServiceIDEmpty) {
		t.Errorf("Insert naming no service = %v, want ErrServiceIDEmpty", err)
	}
	if _, err := criterion.Insert(ctx, tx, store, "svc_a", "", "it_a", matched, ""); !errors.Is(err, criterion.ErrSpecArtifactIDEmpty) {
		t.Errorf("Insert naming no spec version = %v, want ErrSpecArtifactIDEmpty", err)
	}
	if _, err := criterion.Insert(ctx, tx, store, "svc_a", "art_a", "", matched, ""); !errors.Is(err, criterion.ErrItemIDEmpty) {
		t.Errorf("Insert naming no item = %v, want ErrItemIDEmpty", err)
	}

	_, err = pool.Exec(ctx, `insert into criterion
		(id, actor_kind, actor_name, at, service_id, spec_artifact_id, item_id, sentence, pattern, escape_reason)
		values ($1, 'component', 'artifact.store', $2, '', 'art_a', 'it_a', 'The system shall hold.', 'always_true', '')`,
		record.NewID(criterion.IDPrefix), record.Now())
	if err == nil || !strings.Contains(err.Error(), "service_id_present") {
		t.Errorf("inserting a criterion naming no service = %v, want a violation of service_id_present", err)
	}
}

// deployAgent is who writes what a run on a candidate environment produced: the
// one component that reaches a deploy target is the one that reports what it
// observed there.
var deployAgent = record.Actor{Kind: record.KindComponent, Name: "deploy"}

// TestRecordResultsWritesWhatTheRunProduced: the identity of a result is the build
// plus the criterion id, so recording the same pair again is one row updated and
// not a second, and a re-verification's new build is a new pair.
func TestRecordResultsWritesWhatTheRunProduced(t *testing.T) {
	ctx, pool := newSet(t)
	const build, other = "bl_a", "bl_b"
	first, second := "cr_"+strings.Repeat("a", 32), "cr_"+strings.Repeat("b", 32)

	if err := criterion.RecordResults(ctx, pool, deployAgent, build, map[string]criterion.Outcome{
		first:  criterion.OutcomePassed,
		second: criterion.OutcomeUndecided,
	}); err != nil {
		t.Fatalf("RecordResults: %v", err)
	}

	results, err := criterion.ResultsForBuild(ctx, pool, build)
	if err != nil {
		t.Fatalf("ResultsForBuild: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("%d results were written, two criteria were decided: %+v", len(results), results)
	}
	byID := map[string]criterion.Outcome{}
	for _, r := range results {
		byID[r.CriterionID] = r.Outcome
		if r.BuildID != build {
			t.Errorf("a result names build %s, want %s", r.BuildID, build)
		}
		if r.Actor != deployAgent {
			t.Errorf("a result's actor is %+v, want the deploy agent", r.Actor)
		}
		if _, err := time.Parse(record.TimeLayout, r.At); err != nil {
			t.Errorf("%s has timestamp %q: %v", r.ID, r.At, err)
		}
	}
	if byID[first] != criterion.OutcomePassed || byID[second] != criterion.OutcomeUndecided {
		t.Errorf("the outcomes read back as %+v", byID)
	}

	// The same pair again is one row: a recomposed environment can produce a
	// different outcome over the same build, and what is stored is what was
	// observed last.
	if err := criterion.RecordResults(ctx, pool, deployAgent, build,
		map[string]criterion.Outcome{first: criterion.OutcomeFailed}); err != nil {
		t.Fatalf("RecordResults again: %v", err)
	}
	results, err = criterion.ResultsForBuild(ctx, pool, build)
	if err != nil {
		t.Fatalf("ResultsForBuild: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("%d results exist after recording one again, the pair is the identity", len(results))
	}

	// Another build is another pair, which is what makes a re-verification's
	// results its own.
	if err := criterion.RecordResults(ctx, pool, deployAgent, other,
		map[string]criterion.Outcome{first: criterion.OutcomePassed}); err != nil {
		t.Fatalf("RecordResults over another build: %v", err)
	}
	if results, err = criterion.ResultsForBuild(ctx, pool, other); err != nil || len(results) != 1 {
		t.Fatalf("ResultsForBuild(%s) = %d results, %v", other, len(results), err)
	}
}

// TestRecordResultsRefusesWhatTheStoreAlsoRefuses: the writer refuses an outcome
// outside the three and a half-named pair, and the store refuses each around it.
func TestRecordResultsRefusesWhatTheStoreAlsoRefuses(t *testing.T) {
	ctx, pool := newSet(t)
	const id = "cr_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	if err := criterion.RecordResults(ctx, pool, deployAgent, "", map[string]criterion.Outcome{
		id: criterion.OutcomePassed,
	}); !errors.Is(err, criterion.ErrBuildIDEmpty) {
		t.Errorf("RecordResults naming no build = %v, want ErrBuildIDEmpty", err)
	}
	if err := criterion.RecordResults(ctx, pool, deployAgent, "bl_a", map[string]criterion.Outcome{
		"": criterion.OutcomePassed,
	}); !errors.Is(err, criterion.ErrCriterionIDEmpty) {
		t.Errorf("RecordResults naming no criterion = %v, want ErrCriterionIDEmpty", err)
	}
	if err := criterion.RecordResults(ctx, pool, deployAgent, "bl_a", map[string]criterion.Outcome{
		id: criterion.Outcome("flaky"),
	}); !errors.Is(err, criterion.ErrOutcomeUnknown) {
		t.Errorf("RecordResults with an outcome outside the three = %v, want ErrOutcomeUnknown", err)
	}
	if err := criterion.RecordResults(ctx, pool, record.Actor{}, "bl_a", map[string]criterion.Outcome{
		id: criterion.OutcomePassed,
	}); !errors.Is(err, record.ErrKindUnknown) {
		t.Errorf("RecordResults with no actor = %v, want ErrKindUnknown", err)
	}

	_, err := pool.Exec(ctx, `insert into `+criterion.ResultTable+`
		(id, actor_kind, actor_name, at, build_id, criterion_id, outcome)
		values ($1, 'component', 'deploy', $2, 'bl_a', $3, 'flaky')`,
		record.NewID(criterion.ResultIDPrefix), record.Now(), id)
	if err == nil || !strings.Contains(err.Error(), "outcome_known") {
		t.Errorf("inserting an unknown outcome = %v, want a violation of outcome_known", err)
	}
}

// TestInForceIsPerBuild is the query's own rule: a build is a set of items, and a
// criterion introduced by an item not in that set is a promise the build's tree
// could not keep. Holding it in force would reject every candidate cut in parallel
// with the one that introduced it.
func TestInForceIsPerBuild(t *testing.T) {
	ctx, pool := newSet(t)
	var mine, theirs criterion.Criterion
	inTx(ctx, t, pool, func(tx pgx.Tx) error {
		var err error
		mine, err = criterion.Insert(ctx, tx, store, "svc_a", "art_a", "it_mine",
			"When asked for its health, the system shall respond ok.", "")
		if err != nil {
			return err
		}
		theirs, err = criterion.Insert(ctx, tx, store, "svc_a", "art_b", "it_theirs",
			"When asked for its version, the system shall respond two.", "")
		return err
	})

	inForce, err := criterion.InForce(ctx, pool, "svc_a", []string{"it_mine"})
	if err != nil {
		t.Fatalf("InForce: %v", err)
	}
	if len(inForce) != 1 || inForce[0].ID != mine.ID {
		t.Errorf("the set in force for one item's build is %+v, want %s alone", inForce, mine.ID)
	}

	inForce, err = criterion.InForce(ctx, pool, "svc_a", []string{"it_mine", "it_theirs"})
	if err != nil {
		t.Fatalf("InForce over both items: %v", err)
	}
	if len(inForce) != 2 {
		t.Errorf("%d criteria are in force for a build holding both items, want 2", len(inForce))
	}
	_ = theirs

	// No items is no criteria and no error: a build with no items is not something
	// the cut produces, and an empty set matching everything is the wrong direction.
	if inForce, err = criterion.InForce(ctx, pool, "svc_a", nil); err != nil || len(inForce) != 0 {
		t.Errorf("InForce over no items = %d criteria, %v", len(inForce), err)
	}
}
