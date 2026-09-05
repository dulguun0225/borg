package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

// owner is the actor an authoring write is made as. Gate policy is authored by a
// human and package policy refuses anything else, so this is a human by
// construction and the name is whatever the owner typed.
func owner(name string) record.Actor {
	return record.Actor{Kind: record.KindHuman, Name: name}
}

// withPool opens the database, applies the schema, and runs one command against
// it. The schema is applied here for the reason the run applies it: these
// subcommands are the first thing an owner may reach on a fresh install, and a
// factory whose policy tables do not exist yet cannot be authored on.
//
// An error saying the factory has not been installed is answered with what to do
// about it. The two records an owner authors on are created by the run's first
// take, which is the one command that knows the targets production is reached at
// and the credential it is reached with; there is nothing to author on until
// then, and an error naming a missing version says that badly on its own.
func withPool(command func(context.Context, *pgxpool.Pool) error) error {
	ctx := context.Background()
	pool, err := postgres.Open(ctx, postgres.URL())
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := postgres.Apply(ctx, pool); err != nil {
		return err
	}
	err = command(ctx, pool)
	if errors.Is(err, policy.ErrNoVersion) || errors.Is(err, factorysettings.ErrNotFound) {
		return fmt.Errorf("%w\nthe factory is not installed: the run's first take creates the factory-wide settings record and production's environment, and there is nothing to author on until it has", err)
	}
	return err
}
