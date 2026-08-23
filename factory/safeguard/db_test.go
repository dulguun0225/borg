// The database tests of this package are in safeguard_test rather than in
// safeguard, because they open the pool through package postgres, which imports
// this one to apply its DDL. deps.txt records the edge as "test safeguard ->
// postgres".
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package safeguard_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/safeguard"
)

var owner = record.Actor{Kind: record.KindHuman, Name: "owner"}

var (
	onAService = safeguard.Subject{Kind: safeguard.SubjectService, ID: "svc_0000000000000000000000000000000a"}
	onAnArea   = safeguard.Subject{Kind: safeguard.SubjectArea, ID: "ar_0000000000000000000000000000000a"}
	onARow     = safeguard.Subject{Kind: safeguard.SubjectGateRow, ID: "deploy_to_production"}
)

func newTable(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m2_sfg_" + hex.EncodeToString(suffix[:])

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

// place writes one safeguard in its own transaction, which is how its one real
// caller writes it — inside the transaction that appends the policy version.
func place(t *testing.T, ctx context.Context, pool *pgxpool.Pool, parameter gatepolicy.Parameter,
	subject safeguard.Subject, bound safeguard.Bound) safeguard.Safeguard {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	placed, err := safeguard.Insert(ctx, tx, owner, parameter, subject, bound)
	if err != nil {
		t.Fatalf("Insert(%s on %s): %v", parameter, subject, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return placed
}

// TestTheDirectionIsReadFromTheParameter: an owner placing a safeguard chooses
// the subject and the bound and never which way the bound points, the direction
// differing per parameter and pointing the same way in each.
func TestTheDirectionIsReadFromTheParameter(t *testing.T) {
	ctx, pool := newTable(t)

	ceiling := place(t, ctx, pool, gatepolicy.WindowLimit, onAService, safeguard.Bound{Number: 2})
	if ceiling.Direction != gatepolicy.DirectionCeiling {
		t.Errorf("a safeguard on the window limit is a %s, want a ceiling", ceiling.Direction)
	}
	floor := place(t, ctx, pool, gatepolicy.WindowConfidence, onAService, safeguard.Bound{Number: 0.99})
	if floor.Direction != gatepolicy.DirectionFloor {
		t.Errorf("a safeguard on the window's confidence is a %s, want a floor", floor.Direction)
	}
	adds := place(t, ctx, pool, gatepolicy.RiskThreshold, onARow, safeguard.Bound{})
	if adds.Direction != gatepolicy.DirectionAddsAHuman {
		t.Errorf("a safeguard on the risk threshold is a %s, want one that adds a human", adds.Direction)
	}
	if adds.Bound.Number != 0 || adds.Bound.List != nil || !adds.Bound.Predicate.IsZero() {
		t.Errorf("a safeguard that adds a human carries a bound: %+v", adds)
	}
	list := place(t, ctx, pool, gatepolicy.AllowedPredicateKinds, onAService,
		safeguard.Bound{List: []string{"status", "schema"}})
	if !slices.Equal(list.Bound.List, []string{"status", "schema"}) {
		t.Errorf("the allowed-kinds safeguard's bound reads back as %v", list.Bound.List)
	}
	// A safeguard's predicate is the third shape of bound and the only parameter
	// that takes it. Its subject is a contract element, which is what doc.go names
	// as the reason a safeguard is a record rather than a field.
	predicate := place(t, ctx, pool, gatepolicy.SafeguardPredicate,
		safeguard.Subject{Kind: safeguard.SubjectContractElement, ID: "con_a.Status"},
		safeguard.Bound{Predicate: safeguard.Predicate{Kind: gatepolicy.PredicatePopulated}})
	if predicate.Direction != gatepolicy.DirectionFloor {
		t.Errorf("a safeguard's predicate is a %s, want a floor — it adds a consumer contract and removes none", predicate.Direction)
	}
	if predicate.Bound.Predicate.Kind != gatepolicy.PredicatePopulated {
		t.Errorf("the safeguard's predicate reads back as %+v", predicate.Bound.Predicate)
	}
	if predicate.Bound.Number != 0 || predicate.Bound.List != nil {
		t.Errorf("a safeguard's predicate carries a second shape of bound: %+v", predicate.Bound)
	}
}

// TestABoundOfTheWrongShapeIsRefused: a bound on a safeguard that adds a human,
// a list where a number belongs or the reverse, and a missing bound where one is
// required.
func TestABoundOfTheWrongShapeIsRefused(t *testing.T) {
	ctx, pool := newTable(t)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	cases := []struct {
		name      string
		parameter gatepolicy.Parameter
		bound     safeguard.Bound
		want      error
	}{
		{"a bound on a safeguard that adds a human", gatepolicy.RiskThreshold, safeguard.Bound{Number: 0.5}, safeguard.ErrBoundRefused},
		{"a list on a safeguard that adds a human", gatepolicy.RiskThreshold, safeguard.Bound{List: []string{"x"}}, safeguard.ErrBoundRefused},
		{"a list where a number belongs", gatepolicy.WindowLimit, safeguard.Bound{List: []string{"x"}}, safeguard.ErrBoundRefused},
		{"a number where a list belongs", gatepolicy.AllowedPredicateKinds, safeguard.Bound{Number: 3}, safeguard.ErrBoundRefused},
		{"no bound at all", gatepolicy.WindowLimit, safeguard.Bound{}, safeguard.ErrBoundMissing},
		{"an empty list", gatepolicy.AllowedPredicateKinds, safeguard.Bound{}, safeguard.ErrBoundMissing},
		{"a number where a predicate belongs", gatepolicy.SafeguardPredicate, safeguard.Bound{Number: 3}, safeguard.ErrBoundRefused},
		{"no predicate at all", gatepolicy.SafeguardPredicate, safeguard.Bound{}, safeguard.ErrBoundMissing},
		{"a predicate kind nothing decides", gatepolicy.SafeguardPredicate,
			safeguard.Bound{Predicate: safeguard.Predicate{Kind: "shape"}}, gatepolicy.ErrPredicateKindUnknown},
		{"an argument a kind does not take", gatepolicy.SafeguardPredicate,
			safeguard.Bound{Predicate: safeguard.Predicate{Kind: gatepolicy.PredicateRead, Argument: "millis"}}, safeguard.ErrBoundRefused},
		{"a kind whose argument is missing", gatepolicy.SafeguardPredicate,
			safeguard.Bound{Predicate: safeguard.Predicate{Kind: gatepolicy.PredicateUnit}}, safeguard.ErrBoundRefused},
	}
	for _, c := range cases {
		if _, err := safeguard.Insert(ctx, tx, owner, c.parameter, onAService, c.bound); !errors.Is(err, c.want) {
			t.Errorf("Insert with %s = %v, want %v", c.name, err, c.want)
		}
	}

	two := safeguard.Bound{Number: 2}
	if _, err := safeguard.Insert(ctx, tx, owner, "no_such_parameter", onAService, two); !errors.Is(err, gatepolicy.ErrUnknown) {
		t.Errorf("a safeguard on a parameter that does not exist = %v, want ErrUnknown", err)
	}
	if _, err := safeguard.Insert(ctx, tx, owner, gatepolicy.WindowLimit,
		safeguard.Subject{Kind: "project", ID: "prj_a"}, two); !errors.Is(err, safeguard.ErrSubjectKindUnknown) {
		t.Errorf("a safeguard on a project = %v, want ErrSubjectKindUnknown", err)
	}
	if _, err := safeguard.Insert(ctx, tx, owner, gatepolicy.WindowLimit,
		safeguard.Subject{Kind: safeguard.SubjectService}, two); !errors.Is(err, safeguard.ErrSubjectIDEmpty) {
		t.Errorf("a safeguard naming no subject = %v, want ErrSubjectIDEmpty", err)
	}
	if _, err := safeguard.Insert(ctx, tx, record.Actor{}, gatepolicy.WindowLimit, onAService, two); !errors.Is(err, record.ErrKindUnknown) {
		t.Errorf("a safeguard with no actor = %v, want ErrKindUnknown", err)
	}
}

// TestASubjectKindTheStoreDoesNotKnowIsRefusedTwice: by the writer, and by the
// store around it.
func TestASubjectKindTheStoreDoesNotKnowIsRefusedTwice(t *testing.T) {
	ctx, pool := newTable(t)

	if _, err := pool.Exec(ctx, `insert into `+safeguard.Table+`
		(id, actor_kind, actor_name, at, parameter, subject_kind, subject_id, direction, bound, bound_list, withdrawn)
		values ('sfg_x', 'human', 'owner', $1, 'window_limit', 'project', 'prj_a', 'ceiling', 2, '', false)`,
		record.Now()); err == nil {
		t.Error("the store accepted a subject kind written around the writer")
	}
	if _, err := pool.Exec(ctx, `insert into `+safeguard.Table+`
		(id, actor_kind, actor_name, at, parameter, subject_kind, subject_id, direction, bound, bound_list, withdrawn)
		values ('sfg_y', 'human', 'owner', $1, 'window_limit', 'service', 'svc_a', 'ceiling', 2, 'status', false)`,
		record.Now()); err == nil {
		t.Error("the store accepted a safeguard carrying both a number and a list")
	}
}

// TestDDLListsEverySubjectKind keeps the CHECK constraint and
// [safeguard.SubjectKinds] from disagreeing.
func TestDDLListsEverySubjectKind(t *testing.T) {
	const open = "subject_kind in ("
	statement := safeguard.DDL[0]
	i := strings.Index(statement, open)
	if i < 0 {
		t.Fatalf("the DDL has no %q list", open)
	}
	rest := statement[i+len(open):]
	listed := strings.Split(rest[:strings.Index(rest, ")")], ",")
	if len(listed) != len(safeguard.SubjectKinds) {
		t.Fatalf("the constraint lists %d subject kinds, SubjectKinds has %d", len(listed), len(safeguard.SubjectKinds))
	}
	for n, k := range safeguard.SubjectKinds {
		if got, want := strings.TrimSpace(listed[n]), "'"+string(k)+"'"; got != want {
			t.Errorf("the constraint lists %s where SubjectKinds has %s", got, want)
		}
	}
}

// TestAWithdrawnSafeguardIsNotInForceAndIsStillReadable: withdrawing stops a
// mechanism reading it, and the row stays so a safeguard that was in force when
// a decision was taken is still readable beside it.
func TestAWithdrawnSafeguardIsNotInForceAndIsStillReadable(t *testing.T) {
	ctx, pool := newTable(t)

	placed := place(t, ctx, pool, gatepolicy.WindowLimit, onAService, safeguard.Bound{Number: 2})
	subjects := []safeguard.Subject{onAService, onAnArea, onARow}

	inForce, err := safeguard.BySubjects(ctx, pool, gatepolicy.WindowLimit, subjects)
	if err != nil {
		t.Fatalf("BySubjects: %v", err)
	}
	if len(inForce) != 1 || inForce[0].ID != placed.ID {
		t.Fatalf("the safeguard in force is %v, want the one placed", ids(inForce))
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := safeguard.Withdraw(ctx, tx, placed.ID); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	inForce, err = safeguard.BySubjects(ctx, pool, gatepolicy.WindowLimit, subjects)
	if err != nil {
		t.Fatalf("BySubjects: %v", err)
	}
	if len(inForce) != 0 {
		t.Errorf("a withdrawn safeguard is still in force: %v", ids(inForce))
	}
	all, err := safeguard.All(ctx, pool)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 1 || !all[0].Withdrawn {
		t.Errorf("the withdrawn safeguard reads back as %+v, want one row marked withdrawn", all)
	}

	withdrawAgain, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = withdrawAgain.Rollback(ctx) }()
	if err := safeguard.Withdraw(ctx, withdrawAgain, "sfg_nothing"); !errors.Is(err, safeguard.ErrNotFound) {
		t.Errorf("withdrawing a safeguard that does not exist = %v, want ErrNotFound", err)
	}
}

// TestBySubjectsReadsEverySubjectAtOnce: a mechanism reads more than one subject
// at a time — a gate firing reads the row, the service, and every area in the
// item's chain — and a safeguard on a subject outside that list reaches nothing.
func TestBySubjectsReadsEverySubjectAtOnce(t *testing.T) {
	ctx, pool := newTable(t)

	onService := place(t, ctx, pool, gatepolicy.AttemptLimit, onAService, safeguard.Bound{Number: 2})
	onArea := place(t, ctx, pool, gatepolicy.AttemptLimit, onAnArea, safeguard.Bound{Number: 4})
	elsewhere := place(t, ctx, pool, gatepolicy.AttemptLimit,
		safeguard.Subject{Kind: safeguard.SubjectArea, ID: "ar_somewhere_else"}, safeguard.Bound{Number: 1})

	read, err := safeguard.BySubjects(ctx, pool, gatepolicy.AttemptLimit, []safeguard.Subject{onAService, onAnArea})
	if err != nil {
		t.Fatalf("BySubjects: %v", err)
	}
	if len(read) != 2 {
		t.Fatalf("the safeguards in force are %v, want the two on the subjects asked about", ids(read))
	}
	found := ids(read)
	if !slices.Contains(found, onService.ID) || !slices.Contains(found, onArea.ID) {
		t.Errorf("the safeguards in force are %v, want %s and %s", found, onService.ID, onArea.ID)
	}
	if slices.Contains(found, elsewhere.ID) {
		t.Errorf("a safeguard on a subject nobody asked about is in force: %v", found)
	}

	// Another parameter's safeguards are not this parameter's.
	other, err := safeguard.BySubjects(ctx, pool, gatepolicy.WindowLimit, []safeguard.Subject{onAService, onAnArea})
	if err != nil {
		t.Fatalf("BySubjects: %v", err)
	}
	if len(other) != 0 {
		t.Errorf("a safeguard on the attempt limit is in force on the window limit: %v", ids(other))
	}

	// No subjects is no safeguards and no error: a mechanism with nothing to read
	// against reads nothing rather than everything.
	none, err := safeguard.BySubjects(ctx, pool, gatepolicy.AttemptLimit, nil)
	if err != nil || len(none) != 0 {
		t.Errorf("BySubjects with no subjects = %v, %v", ids(none), err)
	}
}

func ids(safeguards []safeguard.Safeguard) []string {
	read := make([]string, 0, len(safeguards))
	for _, p := range safeguards {
		read = append(read, p.ID)
	}
	return read
}
