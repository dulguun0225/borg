package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/lease"
)

// areaCommand declares an area, which is what a safeguard or an item-size target
// is drawn on. It is duty 9's other write: an owner declares the groupings the
// rest of the factory is scoped against.
func areaCommand(args []string) error {
	flags := flag.NewFlagSet("area", flag.ContinueOnError)
	inside := flags.String("inside", "", "the area this one lies inside, by name; empty at the outermost")
	human := flags.String("human", "owner", "the owner declaring it")

	// The name is taken off the front before the flags are parsed, because
	// `area payments -inside greeting` is what a person types and Go's flag
	// package stops parsing at the first argument that is not a flag.
	name := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		name, args = args[0], args[1:]
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if name == "" || flags.NArg() != 0 {
		return errors.New("factory area: one argument, the area's name, and then any flags")
	}

	return withPool(func(ctx context.Context, pool *pgxpool.Pool, token lease.Token) error {
		insideID := ""
		if *inside != "" {
			outer, found, err := area.ByName(ctx, pool, *inside)
			if err != nil {
				return err
			}
			if !found {
				return fmt.Errorf("factory area: no area is named %q, so %q cannot lie inside it", *inside, name)
			}
			insideID = outer.ID
		}
		declared, err := area.NewWriter(pool, token).Declare(ctx, owner(*human), name, insideID)
		if err != nil {
			return err
		}
		fmt.Printf("Area %s declared as %s\n", declared.Name, declared.ID)
		return nil
	})
}
