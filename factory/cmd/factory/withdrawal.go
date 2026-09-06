package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/halt"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/legalhold"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/safeguard"
	"github.com/dulguun0225/borg/factory/score"
)

// The four rows outside every item that a human at this terminal closes: a
// safeguard's withdrawal, a halt's withdrawal, a legal hold's withdrawal, and
// the shortening of decision-log retention. Each fires like any other row, reads
// no threshold and no factor set, and waits on a human always.
//
// Each of the four is two appends and then one write: the gate fires the row,
// the human closes it, and the record's own approval is made from that close and
// names it. Nothing here approves a record without firing the row first — a
// record that removed a protection with no decision on it is the mechanism those
// rows exist to refuse — and package policy refuses an approval naming no close
// event, so the order cannot be taken the other way round.
//
// A withdrawal's row is routed away from the human who wrote the withdrawal: the
// actor on that record is [gate.RoutedTo.NotHuman], and the safeguard's own
// routing field gives the duty or the named human its own withdrawal waits on. A
// shortening of decision-log retention routes away from the human who authored
// the shorter value; nothing writes a shorter value before the row decides it,
// so there is no such actor to route away from and that row names none.

// approveWithdrawal is the four approvals, in the order the flags name them.
// Exactly one is given: an approve that named two would be one command deciding
// two rows, and each of the four is a row of its own.
func approveWithdrawal(safeguardWithdrawal, haltWithdrawal, legalHoldWithdrawal string,
	retention int64, human string) error {
	named := 0
	for _, given := range []bool{
		safeguardWithdrawal != "", haltWithdrawal != "", legalHoldWithdrawal != "", retention > 0,
	} {
		if given {
			named++
		}
	}
	if named != 1 {
		return errors.New("factory approve: one of -safeguard-withdrawal, -halt-withdrawal, " +
			"-legal-hold-withdrawal and -retention, and no more")
	}

	return withPool(func(ctx context.Context, pool *pgxpool.Pool, token lease.Token) error {
		actor, err := humanNamed(ctx, pool, token, human)
		if err != nil {
			return err
		}
		g, err := rowGate(ctx, pool, token)
		if err != nil {
			return err
		}
		factory := policy.NewFactory(pool, token)
		switch {
		case safeguardWithdrawal != "":
			routed, err := safeguardWithdrawalRouting(ctx, pool, safeguardWithdrawal)
			if err != nil {
				return err
			}
			closed, err := decideOutsideEveryItem(ctx, g, gate.SafeguardWithdrawal, routed, actor)
			if err != nil {
				return err
			}
			version, err := factory.ApproveSafeguardWithdrawal(ctx, actor, safeguardWithdrawal, closed.ID)
			if err != nil {
				return err
			}
			fmt.Printf("Withdrawal %s approved at %s by close event %s; the safeguard is out of force, policy version %s\n",
				safeguardWithdrawal, gate.SafeguardWithdrawal, closed.ID, version.ID)
		case haltWithdrawal != "":
			written, err := halt.GetWithdrawal(ctx, pool, haltWithdrawal)
			if err != nil {
				return err
			}
			routed := gate.RoutedTo{NotHuman: written.Actor.Key}
			closed, err := decideOutsideEveryItem(ctx, g, gate.HaltWithdrawal, routed, actor)
			if err != nil {
				return err
			}
			version, err := factory.ApproveHaltWithdrawal(ctx, actor, haltWithdrawal, closed.ID)
			if err != nil {
				return err
			}
			fmt.Printf("Withdrawal %s approved at %s by close event %s; the halt is ended and the factory runs, policy version %s\n",
				haltWithdrawal, gate.HaltWithdrawal, closed.ID, version.ID)
		case legalHoldWithdrawal != "":
			written, err := legalhold.GetWithdrawal(ctx, pool, legalHoldWithdrawal)
			if err != nil {
				return err
			}
			routed := gate.RoutedTo{NotHuman: written.Actor.Key}
			closed, err := decideOutsideEveryItem(ctx, g, gate.LegalHoldWithdrawal, routed, actor)
			if err != nil {
				return err
			}
			version, err := factory.ApproveLegalHoldWithdrawal(ctx, actor, legalHoldWithdrawal, closed.ID)
			if err != nil {
				return err
			}
			fmt.Printf("Withdrawal %s approved at %s by close event %s; the legal hold is lifted, policy version %s\n",
				legalHoldWithdrawal, gate.LegalHoldWithdrawal, closed.ID, version.ID)
		default:
			closed, err := decideOutsideEveryItem(ctx, g, gate.DecisionLogRetentionShortening,
				gate.RoutedTo{}, actor)
			if err != nil {
				return err
			}
			version, err := factory.ApproveRetentionShortening(ctx, actor, retention, closed.ID)
			if err != nil {
				return err
			}
			fmt.Printf("Decision-log retention shortened to %d second(s) at %s by close event %s; policy version %s\n",
				retention, gate.DecisionLogRetentionShortening, closed.ID, version.ID)
			fmt.Println("What a shortening costs is what the log no longer holds, which is why it is decided at a row and not authored")
		}
		return nil
	})
}

// decideOutsideEveryItem fires one row that belongs to no item and takes the
// human's approve at it. Two appends: the open event, which names who the row
// waits on and the human it may not be closed by, and the close event, which is
// what the record's own approval is then written from.
//
// There is no verdict to choose here. Approve and reject are what these rows
// offer, and a reject leaves the record standing — which is what not running
// this command already does, so the command that is run is the approval.
func decideOutsideEveryItem(ctx context.Context, g *gate.Gate, row gate.Row,
	routed gate.RoutedTo, actor record.Actor) (decisionlog.Row, error) {
	opened, err := g.Fire(ctx, gate.Firing{Row: row, RoutedTo: routed})
	if err != nil {
		return decisionlog.Row{}, err
	}
	fmt.Printf("Gate %s fired; decision %s waits on %s\n", row, opened.Row.ID, waitedOn(opened.WaitsOn))
	closed, err := g.Decide(ctx, opened, gate.Given{Actor: actor, Verdict: gate.VerdictApprove})
	if err != nil {
		return decisionlog.Row{}, err
	}
	if closed.SelfApproval {
		fmt.Printf("  %s wrote the record and decided it, no second holder existing; the close says so\n", actor.Key)
	}
	return closed, nil
}

// safeguardWithdrawalRouting is who the row that decides one safeguard's
// withdrawal waits on: the duty or the named human the safeguard's own routing
// field gives, and the owner where it names neither — and never the actor on the
// withdrawal.
func safeguardWithdrawalRouting(ctx context.Context, pool *pgxpool.Pool, withdrawalID string) (gate.RoutedTo, error) {
	written, err := safeguard.GetWithdrawal(ctx, pool, withdrawalID)
	if err != nil {
		return gate.RoutedTo{}, err
	}
	placed, err := safeguard.All(ctx, pool)
	if err != nil {
		return gate.RoutedTo{}, err
	}
	routed := gate.RoutedTo{NotHuman: written.Actor.Key}
	for _, one := range placed {
		if one.ID != written.SafeguardID {
			continue
		}
		routed.Duty = people.Duty(one.Routing.Duty)
		routed.Human = one.Routing.HumanKey
		return routed, nil
	}
	return gate.RoutedTo{}, fmt.Errorf("%w: %s", safeguard.ErrNotFound, written.SafeguardID)
}

// rowGate is the gate a row outside every item is fired through: the log, the
// score version in force, and the policy read against it. It composes none of
// what a row on an item's path reads — no holds, no drift detector's store, no
// reader of an intent's state — because none of the four rows here reads one:
// each decides a record, names no item, and reads no threshold.
//
// The score version is ensured rather than read, the way a run's own
// composition ensures it: every decision names the version in force at its
// firing, and a factory that has never run a path has none to name.
func rowGate(ctx context.Context, pool *pgxpool.Pool, token lease.Token) (*gate.Gate, error) {
	version, err := score.NewWriter(pool, token, marksOf(pool)).Ensure(ctx, scoreActor)
	if err != nil {
		return nil, err
	}
	return gate.New(gate.Composition{
		Pool:   pool,
		Token:  token,
		Log:    decisionlog.NewWriter(pool, token),
		Score:  score.New(pool, version, score.NeverDraw{}, marksOf(pool), token),
		Policy: policy.NewReader(pool, token, version),
	}), nil
}
