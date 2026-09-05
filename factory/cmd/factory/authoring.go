package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

// owner is the actor an authoring write is made as. Gate policy is authored by a
// human and package policy refuses anything else, so this is a human by
// construction; the key is derived from the name this pass gives it, and the
// name itself is not on the actor — a later pass adds the People key-to-name
// mapping, and until then a caller that wants the name to print keeps it
// separately.
func owner(name string) record.Actor {
	return record.Actor{Kind: record.KindHuman, Key: "person:" + name, Basis: record.BasisClaimed}
}

// withPool opens the database, applies the schema, acquires the lease, and runs
// one command against the pool with the token every writer and every read event
// carries. The schema is applied here for the reason the run applies it: these
// subcommands are the first thing an owner may reach on a fresh install, and a
// factory whose policy tables do not exist yet cannot be authored on. The lease
// is acquired here for the reason the run acquires it: this process reaches the
// store for the life of the command, per ../../../end-goal/one-process.md, and a
// held lease is a start failure.
//
// An error saying the factory has not been installed is answered with what to do
// about it. The two records an owner authors on are created by the run's first
// take, which is the one command that knows the targets production is reached at
// and the credential it is reached with; there is nothing to author on until
// then, and an error naming a missing version says that badly on its own.
func withPool(command func(context.Context, *pgxpool.Pool, lease.Token) error) error {
	ctx := context.Background()
	pool, err := postgres.Open(ctx, postgres.URL())
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := postgres.Apply(ctx, pool); err != nil {
		return err
	}
	token, stopLease, err := acquireLease(ctx, pool)
	if err != nil {
		return err
	}
	defer stopLease()
	err = command(ctx, pool, token)
	if errors.Is(err, policy.ErrNoVersion) || errors.Is(err, factorysettings.ErrNotFound) {
		return fmt.Errorf("%w\nthe factory is not installed: the run's first take creates the factory-wide settings record and production's environment, and there is nothing to author on until it has", err)
	}
	return err
}
