// The database test of the Spec gate's mechanical rejection: a build in an
// area graded irreversible with no criterion in force bounding that area's
// hazardous operation. It shares db_test.go's newSet and the actors and drafts
// it declares.
package criterion_test

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/criterion"
)

// TestCheckHazardControlledIsTheSpecGatesRejection: the gate rejects,
// mechanically and whatever the score returns, a build in an irreversible area
// with no criterion in force naming its operation, and passes one where a
// criterion does. A build in an area of any other grade is not this
// rejection's business.
func TestCheckHazardControlledIsTheSpecGatesRejection(t *testing.T) {
	ctx, pool, _ := newSet(t)
	build := []string{"it_a"}

	// Nothing bounds the area yet, and the grade in force is irreversible.
	err := criterion.CheckHazardControlled(ctx, pool, "svc_a", build, "ar_irreversible", true)
	var uncontrolled *criterion.HazardUncontrolledError
	if !errors.As(err, &uncontrolled) {
		t.Fatalf("CheckHazardControlled with nothing bounding the operation = %v, want a HazardUncontrolledError", err)
	}
	if uncontrolled.AreaID != "ar_irreversible" {
		t.Errorf("the rejection names area %q, want ar_irreversible", uncontrolled.AreaID)
	}

	// The same build in an area of another grade is nil: the derivation and
	// the rejection are the irreversible grade's alone.
	if err := criterion.CheckHazardControlled(ctx, pool, "svc_a", build, "ar_recoverable", false); err != nil {
		t.Errorf("CheckHazardControlled on an area not graded irreversible = %v, want nil", err)
	}
	if err := criterion.CheckHazardControlled(ctx, pool, "svc_a", build, "", true); !errors.Is(err, criterion.ErrAreaIDEmpty) {
		t.Errorf("CheckHazardControlled with a grade and no area = %v, want ErrAreaIDEmpty", err)
	}

	// A criterion in force naming the area is what clears it.
	var bounding criterion.Criterion
	inTx(ctx, t, pool, func(tx pgx.Tx) error {
		draft := matched("If the payout count has reached the bound, then the system shall refuse the payout.")
		draft.HazardDerived = "ar_irreversible"
		var err error
		bounding, err = criterion.Insert(ctx, tx, store, of, draft)
		return err
	})
	if err := criterion.CheckHazardControlled(ctx, pool, "svc_a", build, "ar_irreversible", true); err != nil {
		t.Errorf("CheckHazardControlled with %s in force = %v, want nil", bounding.ID, err)
	}

	// Withdrawn, it is not in force, and the rejection stands again: the query
	// is the in-force set narrowed.
	inTx(ctx, t, pool, func(tx pgx.Tx) error {
		return criterion.Withdraw(ctx, tx, store,
			criterion.Of{ServiceID: "svc_a", SpecArtifactID: "art_b", ItemID: "it_a"}, bounding.ID)
	})
	if err := criterion.CheckHazardControlled(ctx, pool, "svc_a", build, "ar_irreversible", true); !errors.As(err, &uncontrolled) {
		t.Errorf("CheckHazardControlled after the withdrawal = %v, want a HazardUncontrolledError", err)
	}
}
