package criterion_test

import (
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/record"
)

func TestWithdrawalsForServiceKeepsTheWithdrawalActor(t *testing.T) {
	ctx, pool, _ := newSet(t)
	introduced := criterion.Of{ServiceID: "svc_a", SpecArtifactID: "art_stats", ItemID: "it_stats"}
	actor := record.Actor{Kind: record.KindHuman, Key: "human_stats", Basis: record.BasisClaimed}
	var made criterion.Criterion
	inTx(ctx, t, pool, func(tx pgx.Tx) error {
		var err error
		made, err = criterion.Insert(ctx, tx, store, introduced, matched("The system shall hold."))
		if err != nil {
			return err
		}
		return criterion.Withdraw(ctx, tx, actor, criterion.Of{SpecArtifactID: "art_withdraw_stats", ItemID: "it_stats"}, made.ID)
	})
	read, err := criterion.WithdrawalsForService(ctx, pool, "svc_a", []string{"it_stats"})
	if err != nil {
		t.Fatalf("WithdrawalsForService: %v", err)
	}
	if len(read) != 1 || read[0].CriterionID != made.ID || read[0].Actor != actor {
		t.Fatalf("withdrawals = %+v, want %s by %+v", read, made.ID, actor)
	}
}
