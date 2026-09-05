package main

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/halt"
	"github.com/dulguun0225/borg/factory/lease"
)

// haltCommand sets the one authored record whose subject is the factory, or
// withdraws one. Package policy does not import package halt yet — neither
// package's doc.go names the other as a caller — so this writes directly
// through [halt.Writer] rather than through [policy.Factory], which is the
// one exception this interface's authoring subcommands make to going through
// policy: nothing here appends a policy version, and setting a halt and
// withdrawing it are not writes an owner authors gate policy through.
func haltCommand(args []string) error {
	flags := flag.NewFlagSet("halt", flag.ContinueOnError)
	reason := flags.String("reason", "", "why the factory is halted (required unless -withdraw)")
	withdraw := flags.String("withdraw", "", "the id of a halt to withdraw instead of setting one")
	human := flags.String("human", "owner", "the owner acting")
	if err := flags.Parse(args); err != nil {
		return err
	}

	return withPool(func(ctx context.Context, pool *pgxpool.Pool, token lease.Token) error {
		actor := owner(*human)
		if *withdraw != "" {
			wd, err := halt.NewWriter(pool, token).InsertWithdrawal(ctx, actor, *withdraw)
			if err != nil {
				return err
			}
			fmt.Printf("Withdrawal %s of halt %s written, pending approval\n", wd.ID, *withdraw)
			fmt.Println("The gate row A halt's withdrawal decides is not built, so this stands pending until a human approves it; nothing here fires that row")
			return nil
		}
		if *reason == "" {
			return errors.New("factory halt: -reason is required, or -withdraw <halt-id>")
		}
		h, err := halt.NewWriter(pool, token).Insert(ctx, actor, *reason)
		if err != nil {
			return err
		}
		fmt.Printf("Halt %s set by %s %s: %s\n", h.ID, actor.Kind, *human, h.Reason)
		fmt.Println("Nothing here fires the deploy-to-production hold or stops the merge queue's fast-forward; package halt's doc.go names what reads it standing")
		return nil
	})
}
