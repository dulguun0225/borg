// The database tests of the two provenance queries the score reads: which
// criteria are human-confirmed, and which of the criteria a spec version
// withdraws have a provenance naming an authority. They share db_test.go's
// newSet and the actors and drafts it declares.
package criterion_test

import (
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/criterion"
)

// TestHumanConfirmedIsAQueryOverTheIntroducingDecision: human-confirmed is no
// field of the table — it is a query over the introducing spec version's
// decision, and the caller passes which versions a human decided.
func TestHumanConfirmedIsAQueryOverTheIntroducingDecision(t *testing.T) {
	ctx, pool, _ := newSet(t)
	build := []string{"it_a", "it_b"}

	var confirmed, drafted criterion.Criterion
	inTx(ctx, t, pool, func(tx pgx.Tx) error {
		var err error
		confirmed, err = criterion.Insert(ctx, tx, store, of,
			matched("When a payout is requested, the system shall record it."))
		if err != nil {
			return err
		}
		drafted, err = criterion.Insert(ctx, tx, store,
			criterion.Of{ServiceID: "svc_a", SpecArtifactID: "art_b", ItemID: "it_b"},
			matched("When a request arrives, the system shall answer it."))
		return err
	})

	read, err := criterion.HumanConfirmed(ctx, pool, "svc_a", build, []string{"art_a"})
	if err != nil {
		t.Fatalf("HumanConfirmed: %v", err)
	}
	if len(read) != 1 || read[0].ID != confirmed.ID {
		t.Errorf("HumanConfirmed = %+v, want the one criterion %s introduced by art_a", read, confirmed.ID)
	}

	// A version no human decided confirms nothing, and neither does an empty
	// set: a criterion whose introducing decision was cut reads as unknown
	// provenance rather than as confirmed.
	if none, err := criterion.HumanConfirmed(ctx, pool, "svc_a", build, nil); err != nil || len(none) != 0 {
		t.Errorf("HumanConfirmed with no version named = %+v, %v", none, err)
	}

	// Both versions decided by a human is both criteria, in force order.
	both, err := criterion.HumanConfirmed(ctx, pool, "svc_a", build, []string{"art_a", "art_b"})
	if err != nil {
		t.Fatalf("HumanConfirmed: %v", err)
	}
	if len(both) != 2 || both[1].ID != drafted.ID {
		t.Errorf("HumanConfirmed = %+v, want both criteria", both)
	}

	// Withdrawn, it is no longer in force and no longer human-confirmed for the
	// build: the query is the in-force set narrowed.
	inTx(ctx, t, pool, func(tx pgx.Tx) error {
		return criterion.Withdraw(ctx, tx, store,
			criterion.Of{ServiceID: "svc_a", SpecArtifactID: "art_c", ItemID: "it_b"}, confirmed.ID)
	})
	after, err := criterion.HumanConfirmed(ctx, pool, "svc_a", build, []string{"art_a"})
	if err != nil {
		t.Fatalf("HumanConfirmed: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("HumanConfirmed after the withdrawal = %+v, want none", after)
	}
}

// TestWithdrawalsWithAnAuthority: withdrawing a human-confirmed,
// constraint-derived or hazard-derived criterion is a resolved factor at the
// Spec row, routed to the human its provenance names, and withdrawing a
// factory-drafted one is silent.
func TestWithdrawalsWithAnAuthority(t *testing.T) {
	ctx, pool, _ := newSet(t)

	var human, byConstraint, byHazard, plain criterion.Criterion
	inTx(ctx, t, pool, func(tx pgx.Tx) error {
		var err error
		if human, err = criterion.Insert(ctx, tx, store, of,
			matched("When a payout is requested, the system shall record it.")); err != nil {
			return err
		}
		fromConstraint := matched("When data leaves the service, the system shall encrypt it.")
		fromConstraint.ConstraintDerived = []string{"cst_a"}
		if byConstraint, err = criterion.Insert(ctx, tx, store, of, fromConstraint); err != nil {
			return err
		}
		fromHazard := matched("If the payout count has reached the bound, then the system shall refuse the payout.")
		fromHazard.HazardDerived = "ar_payments"
		if byHazard, err = criterion.Insert(ctx, tx, store, of, fromHazard); err != nil {
			return err
		}
		// Introduced by a version no human decided and naming no constraint and
		// no area: factory-drafted, whose withdrawal names no authority.
		plain, err = criterion.Insert(ctx, tx, store,
			criterion.Of{ServiceID: "svc_a", SpecArtifactID: "art_drafted", ItemID: "it_a"},
			matched("When a request arrives, the system shall answer it."))
		return err
	})

	withdrawing := criterion.Of{ServiceID: "svc_a", SpecArtifactID: "art_w", ItemID: "it_b"}
	inTx(ctx, t, pool, func(tx pgx.Tx) error {
		for _, id := range []string{human.ID, byConstraint.ID, byHazard.ID, plain.ID} {
			if err := criterion.Withdraw(ctx, tx, store, withdrawing, id); err != nil {
				return err
			}
		}
		return nil
	})

	routed, err := criterion.WithdrawalsWithAnAuthority(ctx, pool, "art_w", []string{"art_a"})
	if err != nil {
		t.Fatalf("WithdrawalsWithAnAuthority: %v", err)
	}
	if len(routed) != 3 {
		t.Fatalf("WithdrawalsWithAnAuthority = %+v, want the three with an authority", routed)
	}
	by := map[string][]criterion.Provenance{}
	for _, one := range routed {
		by[one.Criterion.ID] = one.Provenances
	}
	if _, held := by[plain.ID]; held {
		t.Error("a factory-drafted criterion's withdrawal was routed to a human")
	}
	if got := by[byConstraint.ID]; !slices.Contains(got, criterion.ProvenanceConstraintDerived) {
		t.Errorf("the constraint-derived criterion's provenances are %v", got)
	}
	if got := by[byHazard.ID]; !slices.Contains(got, criterion.ProvenanceHazardDerived) {
		t.Errorf("the hazard-derived criterion's provenances are %v", got)
	}
	// Every one of the three was introduced by art_a, which a human decided, so
	// each carries human-confirmed as well — a criterion may have more than one
	// source, and the routing is per source.
	for id, sources := range by {
		if !slices.Contains(sources, criterion.ProvenanceHumanConfirmed) {
			t.Errorf("%s's provenances are %v, want human-confirmed among them", id, sources)
		}
	}

	// With no version named as decided by a human, only the two fields answer.
	fields, err := criterion.WithdrawalsWithAnAuthority(ctx, pool, "art_w", nil)
	if err != nil {
		t.Fatalf("WithdrawalsWithAnAuthority: %v", err)
	}
	if len(fields) != 2 {
		t.Errorf("WithdrawalsWithAnAuthority = %+v, want the constraint-derived and the hazard-derived one", fields)
	}
	if none, err := criterion.WithdrawalsWithAnAuthority(ctx, pool, "", nil); err != nil || none != nil {
		t.Errorf("WithdrawalsWithAnAuthority with no version named = %+v, %v", none, err)
	}
}
