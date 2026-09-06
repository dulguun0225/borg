package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/project"
	"github.com/dulguun0225/borg/factory/record"
)

// humanNamed is the actor an authoring write is made as: the per-person key the
// People mapping gives the name -human carries. Every record holds a key and a
// human at a terminal types a name, so this is the one place in the command-line
// interface the two meet, and it is the composition's rather than any package's
// — package people owns the mapping and reads no other record, and every writer
// below takes the actor already resolved.
//
// A name with no mapping gets one: a key minted here and written with the basis
// claimed, which is what the key's basis says — a name typed at a terminal and
// verified by nothing. The write is made as the new key itself, the human
// declaring their own mapping, there being nobody else on a fresh install to
// declare it for them.
func humanNamed(ctx context.Context, pool *pgxpool.Pool, token lease.Token, name string) (record.Actor, error) {
	if name == "" {
		return record.Actor{}, errors.New("factory: -human names the human this is done as, and is empty")
	}
	key, found, err := people.KeyNamed(ctx, pool, name)
	if err != nil {
		return record.Actor{}, err
	}
	if !found {
		key = record.NewID(personKeyPrefix)
		actor := record.Actor{Kind: record.KindHuman, Key: key, Basis: record.BasisClaimed}
		if _, err := people.WriteMapping(ctx, pool, token, actor, key, name); err != nil {
			return record.Actor{}, err
		}
	}
	return record.Actor{Kind: record.KindHuman, Key: key, Basis: record.BasisClaimed}, nil
}

// personKeyPrefix is what a minted per-person key is prefixed with. The key is
// opaque and the prefix is what makes one readable as a person's key in a record
// that holds several kinds of id.
const personKeyPrefix = "person"

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
	if errors.Is(err, policy.ErrNoVersion) || errors.Is(err, factorysettings.ErrNotFound) || errors.Is(err, project.ErrNotFound) {
		return fmt.Errorf("%w\nthe factory is not installed: the run's first take creates the factory-wide settings record, the project, and production's environment, and there is nothing to author on until it has", err)
	}
	return err
}
