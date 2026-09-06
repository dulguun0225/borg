// The database tests of this package are in legalhold_test rather than in
// legalhold, because they open the pool through package postgres, which
// imports this one to apply its DDL. deps.txt records the edge as "test
// legalhold -> postgres lease".
//
// None of these tests skips when the database is unreachable. The milestone
// is demonstrated by them running, so an unreachable database fails the run.
package legalhold_test

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

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/legalhold"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

var owner = record.Actor{Kind: record.KindHuman, Key: "owner", Basis: record.BasisClaimed}

func newTable(t *testing.T) (context.Context, *pgxpool.Pool, lease.Token) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m2_lgh_" + hex.EncodeToString(suffix[:])

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

// TestReachingIsFalseUntilAHoldIsPlaced: a subject nothing has held over
// reaches nothing.
func TestReachingIsFalseUntilAHoldIsPlaced(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := legalhold.NewWriter(pool, token)

	subject := legalhold.Subject{Kind: legalhold.SubjectService, ID: "svc_a"}
	reaching, err := legalhold.Reaching(ctx, pool, subject)
	if err != nil {
		t.Fatalf("Reaching: %v", err)
	}
	if reaching {
		t.Fatalf("a subject nothing has held over reaches a hold")
	}

	placed, err := w.Insert(ctx, owner, subject, "a litigation hold on this service's records")
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if placed.Reason == "" {
		t.Fatalf("the hold reads back with no reason: %+v", placed)
	}

	reaching, err = legalhold.Reaching(ctx, pool, subject)
	if err != nil {
		t.Fatalf("Reaching: %v", err)
	}
	if !reaching {
		t.Errorf("a hold placed on the subject does not reach it")
	}

	other := legalhold.Subject{Kind: legalhold.SubjectService, ID: "svc_b"}
	reaching, err = legalhold.Reaching(ctx, pool, other)
	if err != nil {
		t.Fatalf("Reaching: %v", err)
	}
	if reaching {
		t.Errorf("a hold on one service reaches another")
	}
}

// TestAHoldOnTheWholeFactoryReachesEverySubject: the factory subject is the
// whole install, and reaches every subject asked about.
func TestAHoldOnTheWholeFactoryReachesEverySubject(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := legalhold.NewWriter(pool, token)

	if _, err := w.Insert(ctx, owner, legalhold.Subject{Kind: legalhold.SubjectFactory}, "a hold over the whole install"); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	for _, subject := range []legalhold.Subject{
		{Kind: legalhold.SubjectService, ID: "svc_a"},
		{Kind: legalhold.SubjectProject, ID: "prj_a"},
		{Kind: legalhold.SubjectFactory},
	} {
		reaching, err := legalhold.Reaching(ctx, pool, subject)
		if err != nil {
			t.Fatalf("Reaching(%s): %v", subject, err)
		}
		if !reaching {
			t.Errorf("a hold on the whole factory does not reach %s", subject)
		}
	}
}

// TestAWithdrawalIsPendingUntilApproved: the hold stands until an approved
// withdrawal names it.
func TestAWithdrawalIsPendingUntilApproved(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := legalhold.NewWriter(pool, token)

	subject := legalhold.Subject{Kind: legalhold.SubjectProject, ID: "prj_a"}
	placed, err := w.Insert(ctx, owner, subject, "a hold pending review")
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	pending, err := w.InsertWithdrawal(ctx, owner, placed.ID)
	if err != nil {
		t.Fatalf("InsertWithdrawal: %v", err)
	}
	if pending.Approved {
		t.Fatalf("a fresh withdrawal reads back approved: %+v", pending)
	}

	reaching, err := legalhold.Reaching(ctx, pool, subject)
	if err != nil {
		t.Fatalf("Reaching: %v", err)
	}
	if !reaching {
		t.Errorf("a hold with a pending withdrawal does not reach its subject")
	}

	if err := w.ApproveWithdrawal(ctx, pending.ID); err != nil {
		t.Fatalf("ApproveWithdrawal: %v", err)
	}

	reaching, err = legalhold.Reaching(ctx, pool, subject)
	if err != nil {
		t.Fatalf("Reaching: %v", err)
	}
	if reaching {
		t.Errorf("a hold with an approved withdrawal still reaches its subject")
	}

	if err := w.ApproveWithdrawal(ctx, pending.ID); !errors.Is(err, legalhold.ErrAlreadyApproved) {
		t.Errorf("approving an already-approved withdrawal = %v, want ErrAlreadyApproved", err)
	}
	if err := w.ApproveWithdrawal(ctx, "lghw_nothing"); !errors.Is(err, legalhold.ErrWithdrawalNotFound) {
		t.Errorf("approving a withdrawal that does not exist = %v, want ErrWithdrawalNotFound", err)
	}
}

// TestASubjectIsRequiredExceptOnTheWholeFactory: [legalhold.SubjectFactory]
// names no subject of its own, and the other two kinds require one.
func TestASubjectIsRequiredExceptOnTheWholeFactory(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := legalhold.NewWriter(pool, token)

	if _, err := w.Insert(ctx, owner, legalhold.Subject{Kind: legalhold.SubjectService}, "a reason"); !errors.Is(err, legalhold.ErrSubjectIDEmpty) {
		t.Errorf("a service hold naming no service = %v, want ErrSubjectIDEmpty", err)
	}
	if _, err := w.Insert(ctx, owner, legalhold.Subject{Kind: legalhold.SubjectFactory, ID: "prj_a"}, "a reason"); !errors.Is(err, legalhold.ErrSubjectIDRefused) {
		t.Errorf("a factory-wide hold naming a subject = %v, want ErrSubjectIDRefused", err)
	}
	if _, err := w.Insert(ctx, owner, legalhold.Subject{Kind: "installation"}, "a reason"); !errors.Is(err, legalhold.ErrSubjectKindUnknown) {
		t.Errorf("an unknown subject kind = %v, want ErrSubjectKindUnknown", err)
	}
	if _, err := w.Insert(ctx, owner, legalhold.Subject{Kind: legalhold.SubjectService, ID: "svc_a"}, ""); !errors.Is(err, legalhold.ErrReasonEmpty) {
		t.Errorf("a hold with no reason = %v, want ErrReasonEmpty", err)
	}
	if _, err := w.InsertWithdrawal(ctx, owner, ""); !errors.Is(err, legalhold.ErrHoldIDEmpty) {
		t.Errorf("a withdrawal naming no hold = %v, want ErrHoldIDEmpty", err)
	}
}

// TestDDLListsEverySubjectKind keeps the CHECK constraint and
// [legalhold.SubjectKinds] from disagreeing.
func TestDDLListsEverySubjectKind(t *testing.T) {
	const open = "subject_kind in ("
	statement := legalhold.DDL[0]
	i := strings.Index(statement, open)
	if i < 0 {
		t.Fatalf("the DDL has no %q list", open)
	}
	rest := statement[i+len(open):]
	listed := strings.Split(rest[:strings.Index(rest, ")")], ",")
	if len(listed) != len(legalhold.SubjectKinds) {
		t.Fatalf("the constraint lists %d subject kinds, SubjectKinds has %d", len(listed), len(legalhold.SubjectKinds))
	}
	for n, k := range legalhold.SubjectKinds {
		if got, want := strings.TrimSpace(listed[n]), "'"+string(k)+"'"; got != want {
			t.Errorf("the constraint lists %s where SubjectKinds has %s", got, want)
		}
	}
}

// TestStandingIsEveryHoldWithNoApprovedWithdrawal: what a truncation of the
// decision log is refused against — every hold in force, whatever its subject,
// and none an approved withdrawal has ended.
func TestStandingIsEveryHoldWithNoApprovedWithdrawal(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := legalhold.NewWriter(pool, token)

	standing, err := legalhold.Standing(ctx, pool)
	if err != nil {
		t.Fatalf("Standing: %v", err)
	}
	if len(standing) != 0 {
		t.Fatalf("Standing on an install with no hold = %v, want none", standing)
	}

	onAService, err := w.Insert(ctx, owner,
		legalhold.Subject{Kind: legalhold.SubjectService, ID: "svc_a"}, "a litigation hold")
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if _, err := w.Insert(ctx, owner,
		legalhold.Subject{Kind: legalhold.SubjectFactory}, "a hold over the whole install"); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	standing, err = legalhold.Standing(ctx, pool)
	if err != nil {
		t.Fatalf("Standing: %v", err)
	}
	if len(standing) != 2 {
		t.Fatalf("Standing = %v, want the two holds placed", standing)
	}
	if standing[0].ID != onAService.ID || standing[0].Subject.ID != "svc_a" ||
		standing[0].Reason != "a litigation hold" {
		t.Errorf("the first hold reads back as %+v, want the one placed on svc_a", standing[0])
	}

	withdrawal, err := w.InsertWithdrawal(ctx, owner, onAService.ID)
	if err != nil {
		t.Fatalf("InsertWithdrawal: %v", err)
	}
	if err := w.ApproveWithdrawal(ctx, withdrawal.ID); err != nil {
		t.Fatalf("ApproveWithdrawal: %v", err)
	}

	standing, err = legalhold.Standing(ctx, pool)
	if err != nil {
		t.Fatalf("Standing: %v", err)
	}
	if len(standing) != 1 || standing[0].Subject.Kind != legalhold.SubjectFactory {
		t.Errorf("Standing after one withdrawal = %v, want the factory-wide hold alone", standing)
	}
}
