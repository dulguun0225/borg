package criterion_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/jackc/pgx/v5"
)

func TestRejectedSpecContributesNeitherCriteriaNorWithdrawals(t *testing.T) {
	ctx, pool, _ := newSet(t)
	var original criterion.Criterion
	inTx(ctx, t, pool, func(tx pgx.Tx) error {
		var err error
		original, err = criterion.Insert(ctx, tx, store, of, matched("The system shall preserve the original promise."))
		if err != nil {
			return err
		}
		rejected := criterion.Of{ServiceID: of.ServiceID, ItemID: of.ItemID, SpecArtifactID: "art_rejected"}
		if err = criterion.Withdraw(ctx, tx, store, rejected, original.ID); err != nil {
			return err
		}
		_, err = criterion.Insert(ctx, tx, store, rejected, matched("The system shall introduce another promise."))
		return err
	})
	active, err := criterion.InForce(ctx, pool, of.ServiceID, []string{of.ItemID}, "art_rejected")
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != original.ID {
		t.Fatalf("criteria after rejecting revision = %v, want only %s", active, original.ID)
	}
	withdrawn, err := criterion.Withdrawn(ctx, pool, []string{of.ItemID}, "art_rejected")
	if err != nil {
		t.Fatal(err)
	}
	if len(withdrawn) != 0 {
		t.Fatalf("rejected withdrawal still applies: %v", withdrawn)
	}
}
