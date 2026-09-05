package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/record"
)

// priorityCommand writes the priority an owner reorders a queue with. It is the
// design's settable order at every queue an item waits in as an item — the gates
// up to and including Merge to master, and the merge queue — so an owner who
// rushes an item to the front here has rushed it at every gate it has left, and
// has no way at all to reorder a deploy.
//
// It goes through dispatch and not beside it, the item having one writer after the
// decomposition. Work is the screen that will call this, and there is none yet.
func priorityCommand(args []string) error {
	flags := flag.NewFlagSet("priority", flag.ContinueOnError)
	priority := flags.Int("priority", 0, "the priority; a greater number goes first, and decomposition writes nothing")
	human := flags.String("human", "owner", "the owner reordering the queue")

	// The item id is taken off the front before the flags are parsed, the way
	// `area <name>` is: it is what a person types first.
	id := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id, args = args[0], args[1:]
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if id == "" || flags.NArg() != 0 {
		return errors.New("factory priority: one argument, the item's id, and then any flags")
	}

	return withPool(func(ctx context.Context, pool *pgxpool.Pool) error {
		owner := record.Actor{Kind: record.KindHuman, Name: *human}
		it, err := item.NewDispatch(pool).SetPriority(ctx, owner, id, *priority)
		if err != nil {
			return err
		}
		fmt.Printf("Item %s has priority %d, set by %s %s\n", it.ID, it.Priority, owner.Kind, owner.Name)
		fmt.Printf("It is at stage %s; the priority orders every queue it waits in as an item and no deploy\n", it.Stage)
		return nil
	})
}
