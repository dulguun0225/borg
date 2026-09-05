// The database tests of this package are in area_test rather than in area,
// because they open the pool through package postgres. An external test
// package is a separate package to the compiler, so the edge is a test edge
// and not a cycle. deps.txt records it as "test area -> postgres lease".
//
// The package's own DDL is applied statement by statement rather than through
// postgres.Apply, so these tests depend on this package's schema alone.
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package area_test

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

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

var owner = record.Actor{Kind: record.KindHuman, Key: "person:owner", Basis: record.BasisClaimed}

// aProject is a project id, standing in for a row this package cannot import
// and does not check exists: there are no foreign keys between records.
const aProject = "prj_test"

func newTable(t *testing.T) (context.Context, *pgxpool.Pool, *area.Writer) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m2_area_" + hex.EncodeToString(suffix[:])

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
	for n, statement := range lease.DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("applying lease statement %d: %v", n+1, err)
		}
	}
	for n, statement := range area.DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("applying area statement %d: %v", n+1, err)
		}
	}
	token, err := lease.Acquire(ctx, pool, "test", time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	return ctx, pool, area.NewWriter(pool, token)
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

// TestDeclareGetAndByName: an owner declares a grouping and the item-size
// target on it is absent, which is not a target of zero — the value in force
// is what the score supplies.
func TestDeclareGetAndByName(t *testing.T) {
	ctx, pool, w := newTable(t)

	declared, err := w.Declare(ctx, owner, "payments", area.InsideProject(aProject), area.Hazard{})
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}
	if declared.Actor != owner {
		t.Errorf("the area's actor is %+v, want the owner", declared.Actor)
	}
	if declared.Inside.ProjectID != aProject || declared.Inside.AreaID != "" {
		t.Errorf("the area's inside is %+v, want the project alone", declared.Inside)
	}
	if declared.Hazard.Named() {
		t.Errorf("a freshly declared area names a grade: %+v", declared.Hazard)
	}
	if declared.ItemSizeTarget.Present {
		t.Errorf("a freshly declared area carries an authored target: %+v", declared.ItemSizeTarget)
	}

	read, err := area.Get(ctx, pool, declared.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read != declared {
		t.Errorf("read back %+v, want %+v", read, declared)
	}
	if _, err := area.Get(ctx, pool, "ar_missing"); !errors.Is(err, area.ErrNotFound) {
		t.Errorf("Get on a missing id = %v, want ErrNotFound", err)
	}

	byName, found, err := area.ByName(ctx, pool, "payments")
	if err != nil || !found {
		t.Fatalf("ByName = %+v, %v, %v", byName, found, err)
	}
	if byName.ID != declared.ID {
		t.Errorf("ByName found %s, want %s", byName.ID, declared.ID)
	}
	if _, found, err := area.ByName(ctx, pool, "marketing"); err != nil || found {
		t.Errorf("ByName of an area nobody declared = %v, %v", found, err)
	}
}

// TestDeclareRefusals: the writer's own checks, made before an insert is even
// attempted.
func TestDeclareRefusals(t *testing.T) {
	ctx, _, w := newTable(t)

	if _, err := w.Declare(ctx, owner, "", area.InsideProject(aProject), area.Hazard{}); !errors.Is(err, area.ErrNameEmpty) {
		t.Errorf("Declare with no name = %v, want ErrNameEmpty", err)
	}
	if _, err := w.Declare(ctx, record.Actor{}, "billing", area.InsideProject(aProject), area.Hazard{}); !errors.Is(err, record.ErrKindUnknown) {
		t.Errorf("Declare with no actor = %v, want ErrKindUnknown", err)
	}
	if _, err := w.Declare(ctx, owner, "billing", area.Inside{}, area.Hazard{}); !errors.Is(err, area.ErrInsideIsOneOfTwo) {
		t.Errorf("Declare inside neither an area nor a project = %v, want ErrInsideIsOneOfTwo", err)
	}
	if _, err := w.Declare(ctx, owner, "billing", area.Inside{AreaID: "ar_x", ProjectID: "prj_x"}, area.Hazard{}); !errors.Is(err, area.ErrInsideIsOneOfTwo) {
		t.Errorf("Declare inside both an area and a project = %v, want ErrInsideIsOneOfTwo", err)
	}
}

// TestOneNamePerArea: the store refuses the second declaration of one name, so
// two owners declaring at once leave one area rather than two.
func TestOneNamePerArea(t *testing.T) {
	ctx, _, w := newTable(t)

	if _, err := w.Declare(ctx, owner, "payments", area.InsideProject(aProject), area.Hazard{}); err != nil {
		t.Fatalf("Declare: %v", err)
	}
	if _, err := w.Declare(ctx, owner, "payments", area.InsideProject(aProject), area.Hazard{}); err == nil {
		t.Fatal("the second declaration of one name was accepted")
	}
}

// TestTheChainIsWalkedNarrowestFirst: a safeguard drawn on any area in the
// chain reaches an item in the narrowest, so the walk is what a mechanism
// reads, and it ends at the project rather than running off the end.
func TestTheChainIsWalkedNarrowestFirst(t *testing.T) {
	ctx, pool, w := newTable(t)

	outer, err := w.Declare(ctx, owner, "payments", area.InsideProject(aProject), area.Hazard{})
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}
	inner, err := w.Declare(ctx, owner, "payments/refunds", area.InsideArea(outer.ID), area.Hazard{})
	if err != nil {
		t.Fatalf("Declare inside: %v", err)
	}

	chain, project, err := area.Chain(ctx, pool, inner.ID)
	if err != nil {
		t.Fatalf("Chain: %v", err)
	}
	if len(chain) != 2 || chain[0].ID != inner.ID || chain[1].ID != outer.ID {
		t.Fatalf("the chain is %v, want the narrowest then the one it lies inside", names(chain))
	}
	if project != aProject {
		t.Errorf("Chain ended at project %q, want %q", project, aProject)
	}

	// An item may name no area, and the answer for one is no areas rather than
	// an error.
	empty, emptyProject, err := area.Chain(ctx, pool, "")
	if err != nil || len(empty) != 0 || emptyProject != "" {
		t.Errorf("Chain(\"\") = %v, %q, %v, want no areas, no project and no error", names(empty), emptyProject, err)
	}
}

// TestAChainThatCyclesIsFound: nothing in the store refuses one, there being no
// foreign keys between records, so the walk is where it is found rather than
// where it loops. Two areas each inside the other satisfy the store's
// exactly-one-of-two check on every row, so the cycle is closed by raw SQL.
func TestAChainThatCyclesIsFound(t *testing.T) {
	ctx, pool, w := newTable(t)

	first, err := w.Declare(ctx, owner, "one", area.InsideProject(aProject), area.Hazard{})
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}
	second, err := w.Declare(ctx, owner, "two", area.InsideArea(first.ID), area.Hazard{})
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}
	if _, err := pool.Exec(ctx, `update `+area.Table+` set inside_area_id = $1, project_id = '' where id = $2`,
		second.ID, first.ID); err != nil {
		t.Fatalf("closing the loop by raw SQL: %v", err)
	}

	if _, _, err := area.Chain(ctx, pool, first.ID); !errors.Is(err, area.ErrChainCycles) {
		t.Fatalf("Chain over a cycle = %v, want ErrChainCycles", err)
	}
}

// TestHazardSeverity: the value in force for an area is the highest grade
// named anywhere on its chain, so declaring a finer area never lowers it.
func TestHazardSeverity(t *testing.T) {
	ctx, pool, w := newTable(t)

	outer, err := w.Declare(ctx, owner, "payments", area.InsideProject(aProject),
		area.Hazard{Grade: area.GradeIrreversible, Operation: "payout", Bound: 10, BoundPeriodSeconds: 3600})
	if err != nil {
		t.Fatalf("Declare irreversible: %v", err)
	}
	if outer.Hazard.Grade != area.GradeIrreversible {
		t.Errorf("the declared grade is %q, want irreversible", outer.Hazard.Grade)
	}

	inner, err := w.Declare(ctx, owner, "payments/refunds", area.InsideArea(outer.ID), area.Hazard{})
	if err != nil {
		t.Fatalf("Declare inside, naming no grade: %v", err)
	}

	grade, err := area.SeverityInForce(ctx, pool, inner.ID)
	if err != nil {
		t.Fatalf("SeverityInForce: %v", err)
	}
	if grade != area.GradeIrreversible {
		t.Errorf("SeverityInForce = %q, want irreversible even though the finer area names none", grade)
	}

	standalone, err := w.Declare(ctx, owner, "marketing", area.InsideProject(aProject), area.Hazard{})
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}
	if grade, err := area.SeverityInForce(ctx, pool, standalone.ID); err != nil || grade != area.GradeNegligible {
		t.Errorf("SeverityInForce over a chain naming nothing = %q, %v, want negligible", grade, err)
	}
	if grade, err := area.SeverityInForce(ctx, pool, ""); err != nil || grade != area.GradeNegligible {
		t.Errorf("SeverityInForce(\"\") = %q, %v, want negligible and no error", grade, err)
	}
}

// TestHazardRefusals: an irreversible grade is not written without its
// hazardous operation, its bound, and the period the bound is counted over,
// and an unknown grade is refused before either.
func TestHazardRefusals(t *testing.T) {
	ctx, _, w := newTable(t)

	if _, err := w.Declare(ctx, owner, "payments", area.InsideProject(aProject),
		area.Hazard{Grade: "catastrophic"}); !errors.Is(err, area.ErrGradeUnknown) {
		t.Errorf("Declare with an unknown grade = %v, want ErrGradeUnknown", err)
	}
	for _, missing := range []area.Hazard{
		{Grade: area.GradeIrreversible},
		{Grade: area.GradeIrreversible, Operation: "payout"},
		{Grade: area.GradeIrreversible, Operation: "payout", Bound: 10},
	} {
		if _, err := w.Declare(ctx, owner, "payments", area.InsideProject(aProject), missing); !errors.Is(err, area.ErrIrreversibleNeedsItsBound) {
			t.Errorf("Declare irreversible as %+v = %v, want ErrIrreversibleNeedsItsBound", missing, err)
		}
	}
}

// TestTheTargetIsAuthoredInsideATransaction: the write takes a transaction
// because its one caller appends the policy version in the same one, so the
// field and the version commit together or not at all.
func TestTheTargetIsAuthoredInsideATransaction(t *testing.T) {
	ctx, pool, w := newTable(t)

	declared, err := w.Declare(ctx, owner, "payments", area.InsideProject(aProject), area.Hazard{})
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}

	// A rolled-back transaction leaves the field as it was, which is what makes
	// the pair atomic for the caller that appends a version beside it.
	rolled, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := area.SetItemSizeTarget(ctx, rolled, declared.ID, 250); err != nil {
		t.Fatalf("SetItemSizeTarget: %v", err)
	}
	if err := rolled.Rollback(ctx); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	read, err := area.Get(ctx, pool, declared.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.ItemSizeTarget.Present {
		t.Errorf("a rolled-back authoring left the target at %+v", read.ItemSizeTarget)
	}

	committed, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := area.SetItemSizeTarget(ctx, committed, declared.ID, 250); err != nil {
		t.Fatalf("SetItemSizeTarget: %v", err)
	}
	if err := committed.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	read, err = area.Get(ctx, pool, declared.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !read.ItemSizeTarget.Present || read.ItemSizeTarget.Number != 250 {
		t.Errorf("the authored target reads back as %+v, want 250 present", read.ItemSizeTarget)
	}
}

// TestATargetThatNoItemCouldMeetIsRefusedTwice: by the writer, and by the store
// around it.
func TestATargetThatNoItemCouldMeetIsRefusedTwice(t *testing.T) {
	ctx, pool, w := newTable(t)

	declared, err := w.Declare(ctx, owner, "payments", area.InsideProject(aProject), area.Hazard{})
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := area.SetItemSizeTarget(ctx, tx, declared.ID, 0); !errors.Is(err, area.ErrTargetNotPositive) {
		t.Errorf("SetItemSizeTarget(0) = %v, want ErrTargetNotPositive", err)
	}
	if err := area.SetItemSizeTarget(ctx, tx, "ar_nothing", 250); !errors.Is(err, area.ErrNotFound) {
		t.Errorf("SetItemSizeTarget on an area that does not exist = %v, want ErrNotFound", err)
	}

	if _, err := pool.Exec(ctx, `update `+area.Table+` set item_size_target = -1 where id = $1`, declared.ID); err == nil {
		t.Error("the store accepted a negative item-size target written around the writer")
	}
}

// TestTheStoreRefusesAroundTheWriter inserts by raw SQL, so what it exercises
// is the CHECK constraints and not the writer's own refusals.
func TestTheStoreRefusesAroundTheWriter(t *testing.T) {
	ctx, pool, _ := newTable(t)

	insert := `insert into ` + area.Table + `
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, name,
		inside_area_id, project_id, grade, hazardous_operation, hazard_bound, hazard_bound_period_seconds)
		values ($1, '` + area.FormatVersion + `', 'human', 'owner', 'claimed', $2, $3, $4, $5, $6, $7, $8, $9)`
	for _, refused := range []struct {
		name       string
		areaName   string
		insideArea string
		project    string
		grade      string
		operation  string
		bound      any
		period     any
		constraint string
	}{
		{"an empty name", "", "", aProject, "", "", nil, nil, "name_present"},
		{"neither an area nor a project", "billing", "", "", "", "", nil, nil, "inside_is_one_of_two"},
		{"both an area and a project", "billing", "ar_x", aProject, "", "", nil, nil, "inside_is_one_of_two"},
		{"an unknown grade", "billing", "", aProject, "catastrophic", "", nil, nil, "grade_known"},
		{"irreversible without its operation", "billing", "", aProject, "irreversible", "", nil, nil, "irreversible_names_its_operation_and_bound"},
	} {
		_, err := pool.Exec(ctx, insert,
			record.NewID(area.IDPrefix), record.Now(), refused.areaName,
			refused.insideArea, refused.project, refused.grade, refused.operation, refused.bound, refused.period)
		if err == nil || !strings.Contains(err.Error(), refused.constraint) {
			t.Errorf("inserting %s = %v, want a violation of %s", refused.name, err, refused.constraint)
		}
	}
}

// TestInsertIsFenced: [Writer.Declare] fences the transaction it opens for
// itself with the token it was constructed with.
func TestInsertIsFenced(t *testing.T) {
	ctx, pool, _ := newTable(t)

	stale := area.NewWriter(pool, lease.Token(0))
	if _, err := stale.Declare(ctx, owner, "payments", area.InsideProject(aProject), area.Hazard{}); !errors.Is(err, lease.ErrFenced) {
		t.Errorf("Declare with a stale token = %v, want lease.ErrFenced", err)
	}
}

func names(areas []area.Area) []string {
	read := make([]string, 0, len(areas))
	for _, a := range areas {
		read = append(read, a.Name)
	}
	return read
}
