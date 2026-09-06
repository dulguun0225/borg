// The database tests of this package are in criterion_test rather than in
// criterion, because they open the pool through package postgres, and an
// external test package keeps that a test edge rather than an import of the
// package that will one day apply this schema. The DDL is applied here with
// pool.Exec, statement by statement, because postgres.Apply does not know
// this package until integration.
//
// db_test.go is the criterion record, its withdrawal, and the queries over
// both; result_db_test.go is what a run produced. Both use newSet below.
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
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

// newSet gives a test a schema of its own with the criterion DDL applied
// inside it. The schema is dropped when the test ends, so a rerun on a
// database a previous run left dirty starts clean.
func newSet(t *testing.T) (context.Context, *pgxpool.Pool, lease.Token) {
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
	for n, statement := range lease.DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("applying lease statement %d: %v", n+1, err)
		}
	}
	for n, statement := range criterion.DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("applying criterion statement %d: %v", n+1, err)
		}
	}
	token, err := lease.Acquire(ctx, pool, "test", time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	return ctx, pool, token
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

var store = record.Actor{Kind: record.KindComponent, Key: "artifact.store", Basis: record.BasisClaimed}

// of is the criterion's three links as most tests need them.
var of = criterion.Of{ServiceID: "svc_a", SpecArtifactID: "art_a", ItemID: "it_a"}

// matched is a sentence fitting the event pattern, with the requirement every
// criterion that fits a pattern names.
func matched(sentence string) criterion.Draft {
	return criterion.Draft{Sentence: sentence, RequirementID: "rq_a"}
}

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

// TestInsertRefusesAnUnmatchedSentenceWithNoReason is the first half of the
// rule about a sentence fitting no pattern: it is admitted only with a tagged
// reason.
func TestInsertRefusesAnUnmatchedSentenceWithNoReason(t *testing.T) {
	ctx, pool, _ := newSet(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning a transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = criterion.Insert(ctx, tx, store, of, criterion.Draft{Sentence: "The checkout page loads fast."})
	if !errors.Is(err, criterion.ErrReasonMissing) {
		t.Fatalf("Insert = %v, want ErrReasonMissing", err)
	}
}

// TestInsertRefusesAMatchedSentenceCarryingAReason is the second half: only a
// sentence fitting no pattern carries a reason.
func TestInsertRefusesAMatchedSentenceCarryingAReason(t *testing.T) {
	ctx, pool, _ := newSet(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning a transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	draft := matched("The system shall respond within one second.")
	draft.NoPatternReason = "just in case"
	if _, err := criterion.Insert(ctx, tx, store, of, draft); !errors.Is(err, criterion.ErrReasonRefused) {
		t.Fatalf("Insert = %v, want ErrReasonRefused", err)
	}
}

// TestInsertRefusesACriterionNamingNoRequirement: a criterion names the
// requirement it answers, and the Spec gate rejects in both directions over
// that field — so a criterion that answers nothing nameable is not written at
// all.
func TestInsertRefusesACriterionNamingNoRequirement(t *testing.T) {
	ctx, pool, _ := newSet(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning a transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	draft := criterion.Draft{Sentence: "The system shall respond within one second."}
	if _, err := criterion.Insert(ctx, tx, store, of, draft); !errors.Is(err, criterion.ErrRequirementIDEmpty) {
		t.Errorf("Insert = %v, want ErrRequirementIDEmpty", err)
	}

	// Around the writer, the CHECK constraint refuses the same row.
	_, err = pool.Exec(ctx, `insert into criterion
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, service_id, spec_artifact_id, item_id,
		sentence, pattern, no_pattern_reason, requirement_id, constraint_derived, hazard_derived)
		values ($1, $2, 'component', 'artifact.store', 'claimed', $3, 'svc_a', 'art_a', 'it_a',
		'The system shall hold.', 'always_true', '', '', '{}', '')`,
		record.NewID(criterion.IDPrefix), criterion.FormatVersion, record.Now())
	if err == nil || !strings.Contains(err.Error(), "requirement_id_present_on_a_pattern") {
		t.Errorf("inserting a criterion naming no requirement = %v, want a violation of requirement_id_present_on_a_pattern", err)
	}
}

// TestInsertWritesTheProvenanceAndInForceReadsItBack is a matched sentence and
// one fitting no pattern written with their provenance, and the in-force query
// returning both whole. The three provenance fields are written once, in the
// same call, and never again.
func TestInsertWritesTheProvenanceAndInForceReadsItBack(t *testing.T) {
	ctx, pool, _ := newSet(t)

	var derived, unpatterned criterion.Criterion
	inTx(ctx, t, pool, func(tx pgx.Tx) error {
		var err error
		draft := matched("When a report arrives, the system shall open an intent.")
		draft.ConstraintDerived = []string{"cn_a", "cn_b"}
		draft.HazardDerived = "ar_a"
		derived, err = criterion.Insert(ctx, tx, store, of, draft)
		if err != nil {
			return err
		}
		unpatterned, err = criterion.Insert(ctx, tx, store, of, criterion.Draft{
			Sentence:        "The checkout page loads fast.",
			NoPatternReason: "no pattern fits a latency feel",
		})
		return err
	})

	if derived.Pattern != criterion.PatternEvent {
		t.Errorf("the matched criterion is a %s, want %s", derived.Pattern, criterion.PatternEvent)
	}
	if unpatterned.Pattern != criterion.PatternNoPattern {
		t.Errorf("the unmatched criterion is a %s, want %s", unpatterned.Pattern, criterion.PatternNoPattern)
	}

	inForce, err := criterion.InForce(ctx, pool, "svc_a", []string{"it_a"})
	if err != nil {
		t.Fatalf("InForce: %v", err)
	}
	if len(inForce) != 2 {
		t.Fatalf("InForce returned %d criteria, want 2", len(inForce))
	}
	byID := map[string]criterion.Criterion{inForce[0].ID: inForce[0], inForce[1].ID: inForce[1]}
	for _, want := range []criterion.Criterion{derived, unpatterned} {
		got, ok := byID[want.ID]
		if !ok {
			t.Errorf("InForce does not return %s", want.ID)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("InForce returned %+v, want %+v", got, want)
		}
		if _, err := time.Parse(record.TimeLayout, got.At); err != nil {
			t.Errorf("%s has timestamp %q: %v", got.ID, got.At, err)
		}
	}
	if got := byID[derived.ID]; got.RequirementID != "rq_a" || got.HazardDerived != "ar_a" ||
		!reflect.DeepEqual(got.ConstraintDerived, []string{"cn_a", "cn_b"}) {
		t.Errorf("the provenance read back as %+v", got)
	}
}

// TestTheStoreRefusesAReasonMismatchInsertedAroundTheWriter is the CHECK
// constraint doing what Insert already refused: a row written around the
// writer with a reason on a matched pattern is refused by the store.
func TestTheStoreRefusesAReasonMismatchInsertedAroundTheWriter(t *testing.T) {
	ctx, pool, _ := newSet(t)

	_, err := pool.Exec(ctx, `insert into criterion
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, service_id, spec_artifact_id, item_id,
		sentence, pattern, no_pattern_reason, requirement_id, constraint_derived, hazard_derived)
		values ($1, $2, 'component', 'artifact.store', 'claimed', $3, 'svc_a', 'art_a', 'it_a',
		'The system shall hold.', 'always_true', 'a reason it must not carry', 'rq_a', '{}', '')`,
		record.NewID(criterion.IDPrefix), criterion.FormatVersion, record.Now())
	if err == nil {
		t.Fatal("the store accepted a reason on a matched pattern")
	}
}

// TestAnEmptyLinkIsRefusedTwice covers this package's three link columns. An
// empty link names nothing, so it is refused by the writer and by the store,
// the way every other required field is; record's doc.go states what a link is
// checked for.
func TestAnEmptyLinkIsRefusedTwice(t *testing.T) {
	ctx, pool, _ := newSet(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning a transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	draft := matched("The system shall respond within one second.")
	for _, missing := range []struct {
		of   criterion.Of
		want error
	}{
		{criterion.Of{SpecArtifactID: "art_a", ItemID: "it_a"}, criterion.ErrServiceIDEmpty},
		{criterion.Of{ServiceID: "svc_a", ItemID: "it_a"}, criterion.ErrSpecArtifactIDEmpty},
		{criterion.Of{ServiceID: "svc_a", SpecArtifactID: "art_a"}, criterion.ErrItemIDEmpty},
	} {
		if _, err := criterion.Insert(ctx, tx, store, missing.of, draft); !errors.Is(err, missing.want) {
			t.Errorf("Insert with %+v = %v, want %v", missing.of, err, missing.want)
		}
	}

	_, err = pool.Exec(ctx, `insert into criterion
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, service_id, spec_artifact_id, item_id,
		sentence, pattern, no_pattern_reason, requirement_id, constraint_derived, hazard_derived)
		values ($1, $2, 'component', 'artifact.store', 'claimed', $3, '', 'art_a', 'it_a',
		'The system shall hold.', 'always_true', '', 'rq_a', '{}', '')`,
		record.NewID(criterion.IDPrefix), criterion.FormatVersion, record.Now())
	if err == nil || !strings.Contains(err.Error(), "service_id_present") {
		t.Errorf("inserting a criterion naming no service = %v, want a violation of service_id_present", err)
	}
}

// TestInForceIsPerBuild is the query's own rule: a build is a set of items, and a
// criterion introduced by an item not in that set is a promise the build's tree
// could not keep. Holding it in force would reject every candidate decomposed in parallel
// with the one that introduced it.
func TestInForceIsPerBuild(t *testing.T) {
	ctx, pool, _ := newSet(t)
	var mine criterion.Criterion
	inTx(ctx, t, pool, func(tx pgx.Tx) error {
		var err error
		mine, err = criterion.Insert(ctx, tx, store,
			criterion.Of{ServiceID: "svc_a", SpecArtifactID: "art_a", ItemID: "it_mine"},
			matched("When asked for its health, the system shall respond ok."))
		if err != nil {
			return err
		}
		_, err = criterion.Insert(ctx, tx, store,
			criterion.Of{ServiceID: "svc_a", SpecArtifactID: "art_b", ItemID: "it_theirs"},
			matched("When asked for its version, the system shall respond two."))
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

	// No items is no criteria and no error: a build with no items is not something
	// decomposition produces, and an empty set matching everything is the wrong direction.
	if inForce, err = criterion.InForce(ctx, pool, "svc_a", nil); err != nil || len(inForce) != 0 {
		t.Errorf("InForce over no items = %d criteria, %v", len(inForce), err)
	}
}

// TestAWithdrawalIsReadPerBuildToo is the other half of the in-force query: a
// criterion is in force where the item that introduced it is in the build and
// no spec version in that build withdraws it. The reading is right for master
// and for the withdrawing candidate at once, where a field written at either
// moment would be wrong for the other.
func TestAWithdrawalIsReadPerBuildToo(t *testing.T) {
	ctx, pool, _ := newSet(t)
	var standing criterion.Criterion
	inTx(ctx, t, pool, func(tx pgx.Tx) error {
		var err error
		standing, err = criterion.Insert(ctx, tx, store,
			criterion.Of{ServiceID: "svc_a", SpecArtifactID: "art_a", ItemID: "it_first"},
			matched("When asked for its health, the system shall respond ok."))
		return err
	})
	inTx(ctx, t, pool, func(tx pgx.Tx) error {
		return criterion.Withdraw(ctx, tx, store,
			criterion.Of{ServiceID: "svc_a", SpecArtifactID: "art_b", ItemID: "it_withdrawing"}, standing.ID)
	})

	// The build of the item that introduced it, without the withdrawing item,
	// still promises it.
	inForce, err := criterion.InForce(ctx, pool, "svc_a", []string{"it_first"})
	if err != nil {
		t.Fatalf("InForce: %v", err)
	}
	if len(inForce) != 1 {
		t.Errorf("a build without the withdrawing item holds %d criteria, want 1", len(inForce))
	}

	// The withdrawing candidate's own build holds both items and promises none.
	inForce, err = criterion.InForce(ctx, pool, "svc_a", []string{"it_first", "it_withdrawing"})
	if err != nil {
		t.Fatalf("InForce over the withdrawing build: %v", err)
	}
	if len(inForce) != 0 {
		t.Errorf("the withdrawing build holds %+v in force, want none", inForce)
	}

	withdrawn, err := criterion.Withdrawn(ctx, pool, []string{"it_first", "it_withdrawing"})
	if err != nil {
		t.Fatalf("Withdrawn: %v", err)
	}
	if len(withdrawn) != 1 || withdrawn[0] != standing.ID {
		t.Errorf("Withdrawn = %v, want [%s]", withdrawn, standing.ID)
	}
	if withdrawn, err = criterion.Withdrawn(ctx, pool, []string{"it_first"}); err != nil || len(withdrawn) != 0 {
		t.Errorf("Withdrawn over a build without the withdrawing item = %v, %v", withdrawn, err)
	}
}

// TestWithdrawRefusesAHalfNamedRow: the withdrawal names the version that
// withdraws, the item that version belongs to, and the criterion, because in
// force is read per build off all three.
func TestWithdrawRefusesAHalfNamedRow(t *testing.T) {
	ctx, pool, _ := newSet(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning a transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := criterion.Withdraw(ctx, tx, store, criterion.Of{ItemID: "it_a"}, "cr_a"); !errors.Is(err, criterion.ErrSpecArtifactIDEmpty) {
		t.Errorf("Withdraw naming no version = %v, want ErrSpecArtifactIDEmpty", err)
	}
	if err := criterion.Withdraw(ctx, tx, store, criterion.Of{SpecArtifactID: "art_a"}, "cr_a"); !errors.Is(err, criterion.ErrItemIDEmpty) {
		t.Errorf("Withdraw naming no item = %v, want ErrItemIDEmpty", err)
	}
	if err := criterion.Withdraw(ctx, tx, store, of, ""); !errors.Is(err, criterion.ErrCriterionIDEmpty) {
		t.Errorf("Withdraw naming no criterion = %v, want ErrCriterionIDEmpty", err)
	}
}

// TestTheProvenanceQueries: the three provenance fields are links and not
// marks, so which criteria stand for a constraint, which were drafted under a
// constraint since withdrawn, and which controls a named hazard are queries
// over them.
func TestTheProvenanceQueries(t *testing.T) {
	ctx, pool, _ := newSet(t)
	var standsFor, underWithdrawn, hazard criterion.Criterion
	inTx(ctx, t, pool, func(tx pgx.Tx) error {
		var err error
		draft := matched("When a report arrives, the system shall open an intent.")
		draft.ConstraintDerived = []string{"cn_live"}
		standsFor, err = criterion.Insert(ctx, tx, store, of, draft)
		if err != nil {
			return err
		}
		draft = matched("When asked for its health, the system shall respond ok.")
		draft.ConstraintDerived = []string{"cn_gone"}
		underWithdrawn, err = criterion.Insert(ctx, tx, store, of, draft)
		if err != nil {
			return err
		}
		draft = matched("If the deletion is requested twice, then the system shall refuse the second.")
		draft.HazardDerived = "ar_irreversible"
		hazard, err = criterion.Insert(ctx, tx, store, of, draft)
		return err
	})

	found, err := criterion.ForConstraint(ctx, pool, "svc_a", []string{"it_a"}, "cn_live")
	if err != nil {
		t.Fatalf("ForConstraint: %v", err)
	}
	if len(found) != 1 || found[0].ID != standsFor.ID {
		t.Errorf("ForConstraint = %+v, want %s alone", found, standsFor.ID)
	}

	found, err = criterion.UnderWithdrawnConstraints(ctx, pool, "svc_a", []string{"it_a"}, []string{"cn_gone"})
	if err != nil {
		t.Fatalf("UnderWithdrawnConstraints: %v", err)
	}
	if len(found) != 1 || found[0].ID != underWithdrawn.ID {
		t.Errorf("UnderWithdrawnConstraints = %+v, want %s alone", found, underWithdrawn.ID)
	}

	found, err = criterion.ControllingHazard(ctx, pool, "svc_a", []string{"it_a"}, "ar_irreversible")
	if err != nil {
		t.Fatalf("ControllingHazard: %v", err)
	}
	if len(found) != 1 || found[0].ID != hazard.ID {
		t.Errorf("ControllingHazard = %+v, want %s alone", found, hazard.ID)
	}
	if found, err = criterion.ControllingHazard(ctx, pool, "svc_a", []string{"it_a"}, "ar_other"); err != nil || len(found) != 0 {
		t.Errorf("ControllingHazard over an area nothing bounds = %+v, %v", found, err)
	}

	// A withdrawn criterion answers none of the three: each is the in-force set
	// narrowed.
	inTx(ctx, t, pool, func(tx pgx.Tx) error {
		return criterion.Withdraw(ctx, tx, store,
			criterion.Of{ServiceID: "svc_a", SpecArtifactID: "art_b", ItemID: "it_a"}, standsFor.ID)
	})
	if found, err = criterion.ForConstraint(ctx, pool, "svc_a", []string{"it_a"}, "cn_live"); err != nil || len(found) != 0 {
		t.Errorf("ForConstraint after the withdrawal = %+v, %v", found, err)
	}
}

// TestABadActorIsRefused: the actor is validated by the writer before the row
// is composed, and by the CHECK constraints behind it.
func TestABadActorIsRefused(t *testing.T) {
	ctx, pool, _ := newSet(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning a transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := criterion.Insert(ctx, tx, record.Actor{}, of, matched("The system shall hold.")); !errors.Is(err, record.ErrKindUnknown) {
		t.Errorf("Insert with no actor = %v, want ErrKindUnknown", err)
	}
	if err := criterion.Withdraw(ctx, tx, record.Actor{}, of, "cr_a"); !errors.Is(err, record.ErrKindUnknown) {
		t.Errorf("Withdraw with no actor = %v, want ErrKindUnknown", err)
	}
}
