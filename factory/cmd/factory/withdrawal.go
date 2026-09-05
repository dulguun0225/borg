package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/policy"
)

// The three rows outside every item that a human at this terminal can close: a
// safeguard's withdrawal, a halt's withdrawal, and the shortening of
// decision-log retention. Each fires like any other row, reads no threshold and
// no factor set, and waits on a human always — and what each one's approval does
// is a write of package policy's, which is what these three call.
//
// They are reached through `approve` because that is what approve is: the
// verdict a human gives at a row the factory cannot close for itself. What is
// not built is the firing: each of the three would fire the row and this closes
// the record's own approval directly, so the decision is in the policy version
// and not in a two-row decision of the log. `factory approve <item-id>` is the
// one of the four that does fire a row, the production deploy row being on an
// item's path.

// approveWithdrawal is the three approvals, in the order the flags name them.
// Exactly one is given: an approve that named two would be one command deciding
// two rows, and each of the three is a row of its own.
func approveWithdrawal(safeguardWithdrawal, haltWithdrawal string, retention int64, human string) error {
	named := 0
	for _, given := range []bool{safeguardWithdrawal != "", haltWithdrawal != "", retention > 0} {
		if given {
			named++
		}
	}
	if named != 1 {
		return errors.New("factory approve: one of -safeguard-withdrawal, -halt-withdrawal and -retention, and no more")
	}

	return withPool(func(ctx context.Context, pool *pgxpool.Pool, token lease.Token) error {
		actor, err := humanNamed(ctx, pool, token, human)
		if err != nil {
			return err
		}
		factory := policy.NewFactory(pool, token)
		switch {
		case safeguardWithdrawal != "":
			version, err := factory.ApproveSafeguardWithdrawal(ctx, actor, safeguardWithdrawal)
			if err != nil {
				return err
			}
			fmt.Printf("Withdrawal %s approved at %s; the safeguard is out of force, policy version %s\n",
				safeguardWithdrawal, gate.SafeguardWithdrawal, version.ID)
		case haltWithdrawal != "":
			version, err := factory.ApproveHaltWithdrawal(ctx, actor, haltWithdrawal)
			if err != nil {
				return err
			}
			fmt.Printf("Withdrawal %s approved at %s; the halt is ended and the factory runs, policy version %s\n",
				haltWithdrawal, gate.HaltWithdrawal, version.ID)
		default:
			version, err := factory.ApproveRetentionShortening(ctx, actor, retention)
			if err != nil {
				return err
			}
			fmt.Printf("Decision-log retention shortened to %d second(s) at %s; policy version %s\n",
				retention, gate.DecisionLogRetentionShortening, version.ID)
			fmt.Println("What a shortening costs is what the log no longer holds, which is why it is decided at a row and not authored")
		}
		return nil
	})
}
