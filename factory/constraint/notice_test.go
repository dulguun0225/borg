// notice_test.go is the notice kind's tests: arrival and read-back through
// [constraint.NoticeInForce], the refusal of a reach other than one project,
// withdraw-and-replace versioning, no notice reading as none, and
// [constraint.InForce] never returning a notice-kind constraint. Split out of
// db_test.go, whose newTable, owner and aComponent it shares, to keep that
// file under the line bound.
package constraint_test

import (
	"errors"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/constraint"
)

// TestNoticeWrittenAndReadInForce: a notice-kind constraint, reach one
// project, is what [constraint.NoticeInForce] returns for that project.
func TestNoticeWrittenAndReadInForce(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := constraint.NewWriter(pool, token)

	arrived, err := w.Arrive(ctx, owner, constraint.New{
		Kind: constraint.KindNotice, Reach: constraint.ReachProject, SubjectID: "prj_a",
		Statement: "we collect the text you submit and nothing else",
	})
	if err != nil {
		t.Fatalf("Arrive: %v", err)
	}

	got, ok, err := constraint.NoticeInForce(ctx, pool, "prj_a")
	if err != nil {
		t.Fatalf("NoticeInForce: %v", err)
	}
	if !ok {
		t.Fatalf("NoticeInForce = not found, want %s", arrived.ID)
	}
	if got.ID != arrived.ID {
		t.Errorf("NoticeInForce.ID = %q, want %q", got.ID, arrived.ID)
	}
}

// TestANoticeWhoseReachIsNotOneProjectIsRefused: [constraint.ErrNoticeReachMustBeProject].
func TestANoticeWhoseReachIsNotOneProjectIsRefused(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := constraint.NewWriter(pool, token)

	n := constraint.New{Kind: constraint.KindNotice, Reach: constraint.ReachFactory, Statement: "a factory-wide notice"}
	if _, err := w.Arrive(ctx, owner, n); !errors.Is(err, constraint.ErrNoticeReachMustBeProject) {
		t.Errorf("Arrive with a notice reaching the factory = %v, want ErrNoticeReachMustBeProject", err)
	}
}

// TestAWithdrawnAndReplacedNoticeReadsAsTheReplacement: [constraint.NoticeInForce]
// follows the same withdraw-and-replace versioning as every other constraint.
func TestAWithdrawnAndReplacedNoticeReadsAsTheReplacement(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := constraint.NewWriter(pool, token)

	original, err := w.Arrive(ctx, owner, constraint.New{
		Kind: constraint.KindNotice, Reach: constraint.ReachProject, SubjectID: "prj_a",
		Statement: "the original notice",
	})
	if err != nil {
		t.Fatalf("Arrive: %v", err)
	}

	replacement, err := w.Replace(ctx, owner, original.ID, constraint.New{
		Kind: constraint.KindNotice, Reach: constraint.ReachProject, SubjectID: "prj_a",
		Statement: "the corrected notice",
	})
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}

	got, ok, err := constraint.NoticeInForce(ctx, pool, "prj_a")
	if err != nil {
		t.Fatalf("NoticeInForce: %v", err)
	}
	if !ok {
		t.Fatalf("NoticeInForce = not found, want the replacement %s", replacement.ID)
	}
	if got.ID != replacement.ID {
		t.Errorf("NoticeInForce.ID = %q, want the replacement %q", got.ID, replacement.ID)
	}
}

// TestNoNoticeReadsAsNone: a project an owner authored no notice for reads
// none, and not an error.
func TestNoNoticeReadsAsNone(t *testing.T) {
	ctx, pool, _ := newTable(t)

	_, ok, err := constraint.NoticeInForce(ctx, pool, "prj_with_no_notice")
	if err != nil {
		t.Fatalf("NoticeInForce: %v", err)
	}
	if ok {
		t.Errorf("NoticeInForce over a project with no notice = found, want none")
	}
}

// TestInForceExcludesANotice: a notice-kind constraint is read only by
// [constraint.NoticeInForce], never by [constraint.InForce] — a notice is
// read by no drafting stage.
func TestInForceExcludesANotice(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := constraint.NewWriter(pool, token)
	now := time.Now()

	notice, err := w.Arrive(ctx, owner, constraint.New{
		Kind: constraint.KindNotice, Reach: constraint.ReachProject, SubjectID: "prj_a",
		Statement: "a notice a reporter sees before submitting",
	})
	if err != nil {
		t.Fatalf("Arrive: %v", err)
	}

	got, err := constraint.InForce(ctx, pool, constraint.Over{ProjectID: "prj_a"}, now)
	if err != nil {
		t.Fatalf("InForce: %v", err)
	}
	for _, c := range got {
		if c.ID == notice.ID {
			t.Errorf("InForce returned the notice-kind constraint %s", notice.ID)
		}
	}
}
