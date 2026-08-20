// The database tests of this package are in pin_test rather than in pin,
// because they open the pool through package postgres, which imports this one to
// apply its DDL. deps.txt records the edge as "test pin -> postgres".
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package pin_test

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
	"github.com/dulguun0225/borg/factory/pin"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

var owner = record.Actor{Kind: record.KindHuman, Name: "owner"}

var (
	onAService = pin.Subject{Kind: pin.SubjectService, ID: "svc_0000000000000000000000000000000a"}
	onAnArea   = pin.Subject{Kind: pin.SubjectArea, ID: "ar_0000000000000000000000000000000a"}
	onARow     = pin.Subject{Kind: pin.SubjectGateRow, ID: "deploy_to_production"}
)

func newTable(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m2_pin_" + hex.EncodeToString(suffix[:])

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

// place writes one pin in its own transaction, which is how its one real caller
// writes it — inside the transaction that appends the policy version.
func place(t *testing.T, ctx context.Context, pool *pgxpool.Pool, parameter gatepolicy.Parameter,
	subject pin.Subject, bound pin.Bound) pin.Pin {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	placed, err := pin.Insert(ctx, tx, owner, parameter, subject, bound)
	if err != nil {
		t.Fatalf("Insert(%s on %s): %v", parameter, subject, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return placed
}

// TestTheDirectionIsReadFromTheParameter: an owner placing a pin chooses the
// subject and the bound and never which way the bound points, the direction
// differing per parameter and pointing the same way in each.
func TestTheDirectionIsReadFromTheParameter(t *testing.T) {
	ctx, pool := newTable(t)

	ceiling := place(t, ctx, pool, gatepolicy.K, onAService, pin.Bound{Number: 2})
	if ceiling.Direction != gatepolicy.DirectionCeiling {
		t.Errorf("a pin on K is a %s, want a ceiling", ceiling.Direction)
	}
	floor := place(t, ctx, pool, gatepolicy.WindowConfidence, onAService, pin.Bound{Number: 0.99})
	if floor.Direction != gatepolicy.DirectionFloor {
		t.Errorf("a pin on the window's confidence is a %s, want a floor", floor.Direction)
	}
	adds := place(t, ctx, pool, gatepolicy.RiskThreshold, onARow, pin.Bound{})
	if adds.Direction != gatepolicy.DirectionAddsAHuman {
		t.Errorf("a pin on the risk threshold is a %s, want one that adds a human", adds.Direction)
	}
	if adds.Bound.Number != 0 || adds.Bound.List != nil || !adds.Bound.Predicate.IsZero() {
		t.Errorf("a pin that adds a human carries a bound: %+v", adds)
	}
	list := place(t, ctx, pool, gatepolicy.PredicateCatalog, onAService,
		pin.Bound{List: []string{"status", "schema"}})
	if !slices.Equal(list.Bound.List, []string{"status", "schema"}) {
		t.Errorf("the catalog pin's bound reads back as %v", list.Bound.List)
	}
	// A pinned predicate is the third shape of bound and the only parameter that
	// takes it. Its subject is a contract element, which is what doc.go names as the
	// reason a pin is a record rather than a field.
	predicate := place(t, ctx, pool, gatepolicy.PinnedPredicate,
		pin.Subject{Kind: pin.SubjectContractElement, ID: "con_a.Status"},
		pin.Bound{Predicate: pin.Predicate{Kind: gatepolicy.PredicatePopulated}})
	if predicate.Direction != gatepolicy.DirectionFloor {
		t.Errorf("a pinned predicate is a %s, want a floor — it adds a declaration and removes none", predicate.Direction)
	}
	if predicate.Bound.Predicate.Kind != gatepolicy.PredicatePopulated {
		t.Errorf("the pinned predicate reads back as %+v", predicate.Bound.Predicate)
	}
	if predicate.Bound.Number != 0 || predicate.Bound.List != nil {
		t.Errorf("a pinned predicate carries a second shape of bound: %+v", predicate.Bound)
	}
}

// TestABoundOfTheWrongShapeIsRefused: a bound on a pin that adds a human, a list
// where a number belongs or the reverse, and a missing bound where one is
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
		bound     pin.Bound
		want      error
	}{
		{"a bound on a pin that adds a human", gatepolicy.RiskThreshold, pin.Bound{Number: 0.5}, pin.ErrBoundRefused},
		{"a list on a pin that adds a human", gatepolicy.RiskThreshold, pin.Bound{List: []string{"x"}}, pin.ErrBoundRefused},
		{"a list where a number belongs", gatepolicy.K, pin.Bound{List: []string{"x"}}, pin.ErrBoundRefused},
		{"a number where a list belongs", gatepolicy.PredicateCatalog, pin.Bound{Number: 3}, pin.ErrBoundRefused},
		{"no bound at all", gatepolicy.K, pin.Bound{}, pin.ErrBoundMissing},
		{"an empty list", gatepolicy.PredicateCatalog, pin.Bound{}, pin.ErrBoundMissing},
		{"a number where a predicate belongs", gatepolicy.PinnedPredicate, pin.Bound{Number: 3}, pin.ErrBoundRefused},
		{"no predicate at all", gatepolicy.PinnedPredicate, pin.Bound{}, pin.ErrBoundMissing},
		{"a predicate kind nothing decides", gatepolicy.PinnedPredicate,
			pin.Bound{Predicate: pin.Predicate{Kind: "shape"}}, gatepolicy.ErrPredicateKindUnknown},
		{"an argument a kind does not take", gatepolicy.PinnedPredicate,
			pin.Bound{Predicate: pin.Predicate{Kind: gatepolicy.PredicateRead, Argument: "millis"}}, pin.ErrBoundRefused},
		{"a kind whose argument is missing", gatepolicy.PinnedPredicate,
			pin.Bound{Predicate: pin.Predicate{Kind: gatepolicy.PredicateUnit}}, pin.ErrBoundRefused},
	}
	for _, c := range cases {
		if _, err := pin.Insert(ctx, tx, owner, c.parameter, onAService, c.bound); !errors.Is(err, c.want) {
			t.Errorf("Insert with %s = %v, want %v", c.name, err, c.want)
		}
	}

	two := pin.Bound{Number: 2}
	if _, err := pin.Insert(ctx, tx, owner, "no_such_parameter", onAService, two); !errors.Is(err, gatepolicy.ErrUnknown) {
		t.Errorf("a pin on a parameter that does not exist = %v, want ErrUnknown", err)
	}
	if _, err := pin.Insert(ctx, tx, owner, gatepolicy.K,
		pin.Subject{Kind: "project", ID: "prj_a"}, two); !errors.Is(err, pin.ErrSubjectKindUnknown) {
		t.Errorf("a pin on a project = %v, want ErrSubjectKindUnknown", err)
	}
	if _, err := pin.Insert(ctx, tx, owner, gatepolicy.K,
		pin.Subject{Kind: pin.SubjectService}, two); !errors.Is(err, pin.ErrSubjectIDEmpty) {
		t.Errorf("a pin naming no subject = %v, want ErrSubjectIDEmpty", err)
	}
	if _, err := pin.Insert(ctx, tx, record.Actor{}, gatepolicy.K, onAService, two); !errors.Is(err, record.ErrKindUnknown) {
		t.Errorf("a pin with no actor = %v, want ErrKindUnknown", err)
	}
}

// TestASubjectKindTheStoreDoesNotKnowIsRefusedTwice: by the writer, and by the
// store around it.
func TestASubjectKindTheStoreDoesNotKnowIsRefusedTwice(t *testing.T) {
	ctx, pool := newTable(t)

	if _, err := pool.Exec(ctx, `insert into `+pin.Table+`
		(id, actor_kind, actor_name, at, parameter, subject_kind, subject_id, direction, bound, bound_list, withdrawn)
		values ('pin_x', 'human', 'owner', $1, 'k', 'project', 'prj_a', 'ceiling', 2, '', false)`,
		record.Now()); err == nil {
		t.Error("the store accepted a subject kind written around the writer")
	}
	if _, err := pool.Exec(ctx, `insert into `+pin.Table+`
		(id, actor_kind, actor_name, at, parameter, subject_kind, subject_id, direction, bound, bound_list, withdrawn)
		values ('pin_y', 'human', 'owner', $1, 'k', 'service', 'svc_a', 'ceiling', 2, 'status', false)`,
		record.Now()); err == nil {
		t.Error("the store accepted a pin carrying both a number and a list")
	}
}

// TestDDLListsEverySubjectKind keeps the CHECK constraint and
// [pin.SubjectKinds] from disagreeing.
func TestDDLListsEverySubjectKind(t *testing.T) {
	const open = "subject_kind in ("
	statement := pin.DDL[0]
	i := strings.Index(statement, open)
	if i < 0 {
		t.Fatalf("the DDL has no %q list", open)
	}
	rest := statement[i+len(open):]
	listed := strings.Split(rest[:strings.Index(rest, ")")], ",")
	if len(listed) != len(pin.SubjectKinds) {
		t.Fatalf("the constraint lists %d subject kinds, SubjectKinds has %d", len(listed), len(pin.SubjectKinds))
	}
	for n, k := range pin.SubjectKinds {
		if got, want := strings.TrimSpace(listed[n]), "'"+string(k)+"'"; got != want {
			t.Errorf("the constraint lists %s where SubjectKinds has %s", got, want)
		}
	}
}

// TestAWithdrawnPinIsNotInForceAndIsStillReadable: withdrawing stops a mechanism
// reading it, and the row stays so a pin that was in force when a decision was
// taken is still readable beside it.
func TestAWithdrawnPinIsNotInForceAndIsStillReadable(t *testing.T) {
	ctx, pool := newTable(t)

	placed := place(t, ctx, pool, gatepolicy.K, onAService, pin.Bound{Number: 2})
	subjects := []pin.Subject{onAService, onAnArea, onARow}

	inForce, err := pin.BySubjects(ctx, pool, gatepolicy.K, subjects)
	if err != nil {
		t.Fatalf("BySubjects: %v", err)
	}
	if len(inForce) != 1 || inForce[0].ID != placed.ID {
		t.Fatalf("the pin in force is %v, want the one placed", ids(inForce))
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := pin.Withdraw(ctx, tx, placed.ID); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	inForce, err = pin.BySubjects(ctx, pool, gatepolicy.K, subjects)
	if err != nil {
		t.Fatalf("BySubjects: %v", err)
	}
	if len(inForce) != 0 {
		t.Errorf("a withdrawn pin is still in force: %v", ids(inForce))
	}
	all, err := pin.All(ctx, pool)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 1 || !all[0].Withdrawn {
		t.Errorf("the withdrawn pin reads back as %+v, want one row marked withdrawn", all)
	}

	withdrawAgain, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = withdrawAgain.Rollback(ctx) }()
	if err := pin.Withdraw(ctx, withdrawAgain, "pin_nothing"); !errors.Is(err, pin.ErrNotFound) {
		t.Errorf("withdrawing a pin that does not exist = %v, want ErrNotFound", err)
	}
}

// TestBySubjectsReadsEverySubjectAtOnce: a mechanism reads more than one subject
// at a time — a gate firing reads the row, the service, and every area in the
// item's chain — and a pin on a subject outside that list reaches nothing.
func TestBySubjectsReadsEverySubjectAtOnce(t *testing.T) {
	ctx, pool := newTable(t)

	onService := place(t, ctx, pool, gatepolicy.AttemptBound, onAService, pin.Bound{Number: 2})
	onArea := place(t, ctx, pool, gatepolicy.AttemptBound, onAnArea, pin.Bound{Number: 4})
	elsewhere := place(t, ctx, pool, gatepolicy.AttemptBound,
		pin.Subject{Kind: pin.SubjectArea, ID: "ar_somewhere_else"}, pin.Bound{Number: 1})

	read, err := pin.BySubjects(ctx, pool, gatepolicy.AttemptBound, []pin.Subject{onAService, onAnArea})
	if err != nil {
		t.Fatalf("BySubjects: %v", err)
	}
	if len(read) != 2 {
		t.Fatalf("the pins in force are %v, want the two on the subjects asked about", ids(read))
	}
	found := ids(read)
	if !slices.Contains(found, onService.ID) || !slices.Contains(found, onArea.ID) {
		t.Errorf("the pins in force are %v, want %s and %s", found, onService.ID, onArea.ID)
	}
	if slices.Contains(found, elsewhere.ID) {
		t.Errorf("a pin on a subject nobody asked about is in force: %v", found)
	}

	// Another parameter's pins are not this parameter's.
	other, err := pin.BySubjects(ctx, pool, gatepolicy.K, []pin.Subject{onAService, onAnArea})
	if err != nil {
		t.Fatalf("BySubjects: %v", err)
	}
	if len(other) != 0 {
		t.Errorf("a pin on the attempt bound is in force on K: %v", ids(other))
	}

	// No subjects is no pins and no error: a mechanism with nothing to read
	// against reads nothing rather than everything.
	none, err := pin.BySubjects(ctx, pool, gatepolicy.AttemptBound, nil)
	if err != nil || len(none) != 0 {
		t.Errorf("BySubjects with no subjects = %v, %v", ids(none), err)
	}
}

func ids(pins []pin.Pin) []string {
	read := make([]string, 0, len(pins))
	for _, p := range pins {
		read = append(read, p.ID)
	}
	return read
}
