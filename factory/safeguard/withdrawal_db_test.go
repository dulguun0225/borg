// Withdrawal tests are here rather than in db_test.go for locality: db_test.go
// held over 500 lines once these were added. It shares that file's helpers —
// newTable, place, owner, the subjects, and ids — being the same package's
// test binary.
package safeguard_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/safeguard"
)

// TestAWithdrawalIsPendingUntilApproved: the safeguard stays in force until an
// approved withdrawal names it, the treatment [_A safeguard's
// withdrawal_](../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/10-a-safeguards-withdrawal.md)
// gives it.
func TestAWithdrawalIsPendingUntilApproved(t *testing.T) {
	ctx, pool, token := newTable(t)

	placed := place(t, ctx, pool, token, gatepolicy.WindowLimit, onAService, safeguard.Bound{Number: 2})
	subjects := []safeguard.Subject{onAService, onAnArea}

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
	withdrawal, err := safeguard.InsertWithdrawal(ctx, tx, token, owner, placed.ID)
	if err != nil {
		t.Fatalf("InsertWithdrawal: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if withdrawal.Approved {
		t.Fatalf("a fresh withdrawal reads back approved: %+v", withdrawal)
	}

	// A withdrawal nobody approved leaves the safeguard standing.
	inForce, err = safeguard.BySubjects(ctx, pool, gatepolicy.WindowLimit, subjects)
	if err != nil {
		t.Fatalf("BySubjects: %v", err)
	}
	if len(inForce) != 1 {
		t.Errorf("a safeguard with a pending withdrawal is not in force: %v", ids(inForce))
	}

	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := safeguard.ApproveWithdrawal(ctx, tx, token, withdrawal.ID); err != nil {
		t.Fatalf("ApproveWithdrawal: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	inForce, err = safeguard.BySubjects(ctx, pool, gatepolicy.WindowLimit, subjects)
	if err != nil {
		t.Fatalf("BySubjects: %v", err)
	}
	if len(inForce) != 0 {
		t.Errorf("a safeguard with an approved withdrawal is still in force: %v", ids(inForce))
	}
	all, err := safeguard.All(ctx, pool)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 1 || !all[0].Withdrawn {
		t.Errorf("the withdrawn safeguard reads back as %+v, want one row marked withdrawn", all)
	}

	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := safeguard.ApproveWithdrawal(ctx, tx, token, withdrawal.ID); !errors.Is(err, safeguard.ErrAlreadyApproved) {
		t.Errorf("approving an already-approved withdrawal = %v, want ErrAlreadyApproved", err)
	}
	if err := safeguard.ApproveWithdrawal(ctx, tx, token, "sfgw_nothing"); !errors.Is(err, safeguard.ErrWithdrawalNotFound) {
		t.Errorf("approving a withdrawal that does not exist = %v, want ErrWithdrawalNotFound", err)
	}
}

// TestNothingHereWithdrawsInOneCall: this package writes a withdrawal pending
// and approves one, and offers no call that does both. What sits between them is
// the gate row, so a combined call would be a safeguard leaving force with no
// decision on it.
func TestNothingHereWithdrawsInOneCall(t *testing.T) {
	ctx, pool, token := newTable(t)

	placed := place(t, ctx, pool, token, gatepolicy.WindowLimit, onAService, safeguard.Bound{Number: 2})

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	written, err := safeguard.InsertWithdrawal(ctx, tx, token, owner, placed.ID)
	if err != nil {
		t.Fatalf("InsertWithdrawal: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// The one write left the safeguard standing: nothing in this package took it
	// out of force, which is what the row's approval is for.
	inForce, err := safeguard.BySubjects(ctx, pool, gatepolicy.WindowLimit, []safeguard.Subject{onAService})
	if err != nil {
		t.Fatalf("BySubjects: %v", err)
	}
	if len(inForce) != 1 || inForce[0].ID != placed.ID {
		t.Errorf("writing a withdrawal took the safeguard out of force on its own: %v", ids(inForce))
	}
	if written.Approved {
		t.Errorf("the withdrawal %s reads back approved with nothing having decided it", written.ID)
	}
}
