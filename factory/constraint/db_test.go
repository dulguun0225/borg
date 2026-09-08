// The database tests of this package are in constraint_test rather than in
// constraint, because they open the pool through package postgres, which
// imports this one to apply its DDL. deps.txt records the edge as
// "test constraint -> postgres lease". This file holds arrival, reach,
// calendar dates, InForce and the withdraw-and-replace versioning of the
// document kind; notice_test.go holds the notice kind's tests, split out
// because this file would pass the 500-line bound. The two are one external
// test package, sharing newTable, owner and aComponent.
//
// None of these tests skips when the database is unreachable. The milestone
// is demonstrated by them running, so an unreachable database fails the run.
package constraint_test

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

	"github.com/dulguun0225/borg/factory/constraint"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

var owner = record.Actor{Kind: record.KindHuman, Key: "owner", Basis: record.BasisClaimed}
var aComponent = record.Actor{Kind: record.KindComponent, Key: "intake", Basis: record.BasisClaimed}

func newTable(t *testing.T) (context.Context, *pgxpool.Pool, lease.Token) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m8_cst_" + hex.EncodeToString(suffix[:])

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

// TestArriveReadsBackEveryField: every field a constraint carries round-trips
// through [constraint.Get].
func TestArriveReadsBackEveryField(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := constraint.NewWriter(pool, token)

	n := constraint.New{
		Kind:                  constraint.KindDocument,
		Reach:                 constraint.ReachProject,
		SubjectID:             "prj_a",
		Statement:             "every screen carries a name on every control",
		RequiresSeam5Enforced: true,
		BindsFrom:             &constraint.CalendarDate{Date: "2020-01-01", Zone: "UTC"},
		ReviewBy:              &constraint.CalendarDate{Date: "2030-01-01", Zone: "UTC"},
	}
	arrived, err := w.Arrive(ctx, owner, n)
	if err != nil {
		t.Fatalf("Arrive: %v", err)
	}

	got, err := constraint.Get(ctx, pool, arrived.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Kind != n.Kind || got.Reach != n.Reach || got.SubjectID != n.SubjectID ||
		got.Statement != n.Statement || got.RequiresSeam5Enforced != n.RequiresSeam5Enforced {
		t.Fatalf("Get = %+v, want the fields arrived with %+v", got, n)
	}
	if got.Actor != owner {
		t.Errorf("Get.Actor = %+v, want %+v", got.Actor, owner)
	}
	if got.BindsFrom == nil || *got.BindsFrom != *n.BindsFrom {
		t.Errorf("Get.BindsFrom = %+v, want %+v", got.BindsFrom, n.BindsFrom)
	}
	if got.ReviewBy == nil || *got.ReviewBy != *n.ReviewBy {
		t.Errorf("Get.ReviewBy = %+v, want %+v", got.ReviewBy, n.ReviewBy)
	}
	if got.ReplacesID != "" {
		t.Errorf("a fresh constraint replaces something: %q", got.ReplacesID)
	}
	if got.WithdrawnAt != "" {
		t.Errorf("a fresh constraint reads back withdrawn: %+v", got)
	}
}

// TestArriveRefusesAComponentActor: the actor a constraint is written with is
// the owner who supplied it, and the writer refuses anything else.
func TestArriveRefusesAComponentActor(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := constraint.NewWriter(pool, token)

	n := constraint.New{Kind: constraint.KindDocument, Reach: constraint.ReachFactory, Statement: "a law"}
	if _, err := w.Arrive(ctx, aComponent, n); !errors.Is(err, constraint.ErrActorNotHuman) {
		t.Errorf("Arrive with a component actor = %v, want ErrActorNotHuman", err)
	}
}

// TestReachAndSubjectAgreeOrTheWriteIsRefused: the factory reach names no
// subject, and every other reach names one.
func TestReachAndSubjectAgreeOrTheWriteIsRefused(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := constraint.NewWriter(pool, token)

	withSubject := constraint.New{
		Kind: constraint.KindDocument, Reach: constraint.ReachFactory,
		SubjectID: "prj_a", Statement: "a law",
	}
	if _, err := w.Arrive(ctx, owner, withSubject); !errors.Is(err, constraint.ErrSubjectIDMustBeEmpty) {
		t.Errorf("Arrive with reach factory and a subject = %v, want ErrSubjectIDMustBeEmpty", err)
	}

	withoutSubject := constraint.New{
		Kind: constraint.KindDocument, Reach: constraint.ReachProject, Statement: "a law",
	}
	if _, err := w.Arrive(ctx, owner, withoutSubject); !errors.Is(err, constraint.ErrSubjectIDRequired) {
		t.Errorf("Arrive with reach project and no subject = %v, want ErrSubjectIDRequired", err)
	}
}

// TestACalendarDateNamesBothADateAndAKnownZone: the two refusals a supplied
// date is checked against.
func TestACalendarDateNamesBothADateAndAKnownZone(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := constraint.NewWriter(pool, token)

	unknownZone := constraint.New{
		Kind: constraint.KindDocument, Reach: constraint.ReachFactory, Statement: "a law",
		BindsFrom: &constraint.CalendarDate{Date: "2026-01-01", Zone: "Nowhere/Imagined"},
	}
	if _, err := w.Arrive(ctx, owner, unknownZone); !errors.Is(err, constraint.ErrCalendarDateZoneUnknown) {
		t.Errorf("Arrive with an unknown zone = %v, want ErrCalendarDateZoneUnknown", err)
	}

	noZone := constraint.New{
		Kind: constraint.KindDocument, Reach: constraint.ReachFactory, Statement: "a law",
		BindsFrom: &constraint.CalendarDate{Date: "2026-01-01"},
	}
	if _, err := w.Arrive(ctx, owner, noZone); !errors.Is(err, constraint.ErrCalendarDateIncomplete) {
		t.Errorf("Arrive with no zone = %v, want ErrCalendarDateIncomplete", err)
	}
}

// TestInForceReadsTheFactoryTheProjectTheAreaAndTheIntent: the set drafting
// reads over an item, and not a sibling project's own.
func TestInForceReadsTheFactoryTheProjectTheAreaAndTheIntent(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := constraint.NewWriter(pool, token)
	now := time.Now()

	factoryC, err := w.Arrive(ctx, owner, constraint.New{
		Kind: constraint.KindDocument, Reach: constraint.ReachFactory, Statement: "a law binding everyone",
	})
	if err != nil {
		t.Fatalf("Arrive (factory): %v", err)
	}
	projectC, err := w.Arrive(ctx, owner, constraint.New{
		Kind: constraint.KindDocument, Reach: constraint.ReachProject, SubjectID: "prj_a", Statement: "project a's rule",
	})
	if err != nil {
		t.Fatalf("Arrive (project): %v", err)
	}
	areaC, err := w.Arrive(ctx, owner, constraint.New{
		Kind: constraint.KindDocument, Reach: constraint.ReachArea, SubjectID: "area_a1", Statement: "area a1's rule",
	})
	if err != nil {
		t.Fatalf("Arrive (area): %v", err)
	}
	intentC, err := w.Arrive(ctx, owner, constraint.New{
		Kind: constraint.KindDocument, Reach: constraint.ReachIntent, SubjectID: "int_a", Statement: "this request's own rule",
	})
	if err != nil {
		t.Fatalf("Arrive (intent): %v", err)
	}
	siblingC, err := w.Arrive(ctx, owner, constraint.New{
		Kind: constraint.KindDocument, Reach: constraint.ReachProject, SubjectID: "prj_b", Statement: "project b's rule",
	})
	if err != nil {
		t.Fatalf("Arrive (sibling project): %v", err)
	}

	over := constraint.Over{ProjectID: "prj_a", AreaChain: []string{"area_a1"}, IntentID: "int_a"}
	got, err := constraint.InForce(ctx, pool, over, now)
	if err != nil {
		t.Fatalf("InForce: %v", err)
	}

	want := map[string]bool{factoryC.ID: true, projectC.ID: true, areaC.ID: true, intentC.ID: true}
	if len(got) != len(want) {
		t.Fatalf("InForce returned %d constraints, want %d: %+v", len(got), len(want), got)
	}
	for _, c := range got {
		if !want[c.ID] {
			t.Errorf("InForce returned %s, not in %s's reach", c.ID, "prj_a")
		}
		if c.ID == siblingC.ID {
			t.Errorf("InForce returned the sibling project's own constraint %s", siblingC.ID)
		}
	}
}

// TestABindsFromDateTomorrowIsNotYetInForce: a constraint whose binds-from
// date has not started in its zone is not in [InForce]'s set, and
// [Constraint.InForceAt] agrees.
func TestABindsFromDateTomorrowIsNotYetInForce(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := constraint.NewWriter(pool, token)

	now := time.Now().UTC()
	tomorrow := now.AddDate(0, 0, 1).Format("2006-01-02")
	arrived, err := w.Arrive(ctx, owner, constraint.New{
		Kind: constraint.KindDocument, Reach: constraint.ReachFactory, Statement: "a law phased in tomorrow",
		BindsFrom: &constraint.CalendarDate{Date: tomorrow, Zone: "UTC"},
	})
	if err != nil {
		t.Fatalf("Arrive: %v", err)
	}

	if arrived.InForceAt(now) {
		t.Errorf("a constraint binding from tomorrow reads in force today")
	}

	got, err := constraint.InForce(ctx, pool, constraint.Over{}, now)
	if err != nil {
		t.Fatalf("InForce: %v", err)
	}
	for _, c := range got {
		if c.ID == arrived.ID {
			t.Errorf("InForce at %v returned a constraint binding from %s", now, tomorrow)
		}
	}

	inTwoDays := now.AddDate(0, 0, 2)
	if !arrived.InForceAt(inTwoDays) {
		t.Errorf("a constraint binding from tomorrow is not in force two days from now")
	}
}

// TestInForceForInterviewOmitsTheProjectsOwn: the interview reads less than
// drafting does — the factory's own and the intent's own alone.
func TestInForceForInterviewOmitsTheProjectsOwn(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := constraint.NewWriter(pool, token)
	now := time.Now()

	factoryC, err := w.Arrive(ctx, owner, constraint.New{
		Kind: constraint.KindDocument, Reach: constraint.ReachFactory, Statement: "a law binding everyone",
	})
	if err != nil {
		t.Fatalf("Arrive (factory): %v", err)
	}
	projectC, err := w.Arrive(ctx, owner, constraint.New{
		Kind: constraint.KindDocument, Reach: constraint.ReachProject, SubjectID: "prj_a", Statement: "project a's rule",
	})
	if err != nil {
		t.Fatalf("Arrive (project): %v", err)
	}
	intentC, err := w.Arrive(ctx, owner, constraint.New{
		Kind: constraint.KindDocument, Reach: constraint.ReachIntent, SubjectID: "int_a", Statement: "this request's own rule",
	})
	if err != nil {
		t.Fatalf("Arrive (intent): %v", err)
	}

	got, err := constraint.InForceForInterview(ctx, pool, "int_a", now)
	if err != nil {
		t.Fatalf("InForceForInterview: %v", err)
	}
	seen := map[string]bool{}
	for _, c := range got {
		seen[c.ID] = true
	}
	if !seen[factoryC.ID] || !seen[intentC.ID] {
		t.Fatalf("InForceForInterview = %+v, want the factory's own and the intent's own", got)
	}
	if seen[projectC.ID] {
		t.Errorf("InForceForInterview returned the project's own constraint %s", projectC.ID)
	}
}

// TestReplaceWithdrawsTheOldAndNamesItOnTheNew: [constraint.Writer.Replace]
// is one write withdrawing the record it replaces, and a second replace of
// an already-withdrawn record is refused.
func TestReplaceWithdrawsTheOldAndNamesItOnTheNew(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := constraint.NewWriter(pool, token)

	original, err := w.Arrive(ctx, owner, constraint.New{
		Kind: constraint.KindDocument, Reach: constraint.ReachFactory, Statement: "the original wording",
	})
	if err != nil {
		t.Fatalf("Arrive: %v", err)
	}

	replacement, err := w.Replace(ctx, owner, original.ID, constraint.New{
		Kind: constraint.KindDocument, Reach: constraint.ReachFactory, Statement: "the corrected wording",
	})
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if replacement.ReplacesID != original.ID {
		t.Errorf("replacement.ReplacesID = %q, want %q", replacement.ReplacesID, original.ID)
	}

	withdrawnOriginal, err := constraint.Get(ctx, pool, original.ID)
	if err != nil {
		t.Fatalf("Get (original): %v", err)
	}
	if withdrawnOriginal.WithdrawnAt == "" {
		t.Errorf("the replaced record reads back not withdrawn: %+v", withdrawnOriginal)
	}

	if _, err := w.Replace(ctx, owner, original.ID, constraint.New{
		Kind: constraint.KindDocument, Reach: constraint.ReachFactory, Statement: "a second correction",
	}); !errors.Is(err, constraint.ErrAlreadyWithdrawn) {
		t.Errorf("Replace of an already-withdrawn record = %v, want ErrAlreadyWithdrawn", err)
	}
}

// TestDueForReviewFindsAConstraintPastItsDateAndNotOneReplaced: the row Work
// shows for whoever holds duty 2, and a replaced constraint is not counted
// twice.
func TestDueForReviewFindsAConstraintPastItsDateAndNotOneReplaced(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := constraint.NewWriter(pool, token)
	now := time.Now()

	pastDue, err := w.Arrive(ctx, owner, constraint.New{
		Kind: constraint.KindDocument, Reach: constraint.ReachFactory, Statement: "review this",
		ReviewBy: &constraint.CalendarDate{Date: "2020-01-01", Zone: "UTC"},
	})
	if err != nil {
		t.Fatalf("Arrive (past due): %v", err)
	}

	replacedPastDue, err := w.Arrive(ctx, owner, constraint.New{
		Kind: constraint.KindDocument, Reach: constraint.ReachFactory, Statement: "review this too",
		ReviewBy: &constraint.CalendarDate{Date: "2020-01-01", Zone: "UTC"},
	})
	if err != nil {
		t.Fatalf("Arrive (replaced past due): %v", err)
	}
	if _, err := w.Replace(ctx, owner, replacedPastDue.ID, constraint.New{
		Kind: constraint.KindDocument, Reach: constraint.ReachFactory, Statement: "the reworded rule",
		ReviewBy: &constraint.CalendarDate{Date: "2030-01-01", Zone: "UTC"},
	}); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	notYetDue, err := w.Arrive(ctx, owner, constraint.New{
		Kind: constraint.KindDocument, Reach: constraint.ReachFactory, Statement: "review this later",
		ReviewBy: &constraint.CalendarDate{Date: "2099-01-01", Zone: "UTC"},
	})
	if err != nil {
		t.Fatalf("Arrive (not yet due): %v", err)
	}

	got, err := constraint.DueForReview(ctx, pool, now)
	if err != nil {
		t.Fatalf("DueForReview: %v", err)
	}
	seen := map[string]bool{}
	for _, c := range got {
		seen[c.ID] = true
	}
	if !seen[pastDue.ID] {
		t.Errorf("DueForReview = %+v, want the past-due constraint %s among them", got, pastDue.ID)
	}
	if seen[replacedPastDue.ID] {
		t.Errorf("DueForReview returned the replaced constraint %s", replacedPastDue.ID)
	}
	if seen[notYetDue.ID] {
		t.Errorf("DueForReview returned a constraint not yet due %s", notYetDue.ID)
	}
}
